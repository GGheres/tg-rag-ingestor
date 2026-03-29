package export

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultMistralAPIBaseURL           = "https://api.mistral.ai"
	defaultMistralOCRModel             = "mistral-ocr-latest"
	defaultMistralOCRTimeout           = 15 * time.Minute
	defaultMistralRetryAttempts        = 4
	defaultMistralRetryBaseDelay       = 1200 * time.Millisecond
	maxMistralUploadFileBytes    int64 = 512 << 20 // 512 MB
	maxMistralResponseBodyBytes  int64 = 16 << 20
)

var (
	mistralOCRClientOnce sync.Once
	mistralOCRClientInst *mistralOCRClient
	mistralOCRClientErr  error
)

type mistralOCRClient struct {
	apiKey         string
	baseURL        string
	model          string
	retryAttempts  int
	retryBaseDelay time.Duration
	httpClient     *http.Client
}

type mistralHTTPError struct {
	operation  string
	statusCode int
	body       string
}

func (e *mistralHTTPError) Error() string {
	return fmt.Sprintf("%s failed (%d): %s", e.operation, e.statusCode, e.body)
}

type mistralFileUploadResponse struct {
	ID string `json:"id"`
}

type mistralOCRRequest struct {
	Model    string                    `json:"model"`
	Document mistralOCRRequestDocument `json:"document"`
}

type mistralOCRRequestDocument struct {
	Type   string `json:"type,omitempty"`
	FileID string `json:"file_id"`
}

type mistralOCRResponse struct {
	Pages []mistralOCRPage `json:"pages"`
}

type mistralOCRPage struct {
	Index    int    `json:"index"`
	Markdown string `json:"markdown"`
}

func extractFileTextViaMistralOCR(ctx context.Context, path string) (string, error) {
	client, err := getMistralOCRClient()
	if err != nil {
		return "", err
	}
	return client.extractText(ctx, path)
}

func getMistralOCRClient() (*mistralOCRClient, error) {
	mistralOCRClientOnce.Do(func() {
		mistralOCRClientInst, mistralOCRClientErr = newMistralOCRClientFromEnv()
	})
	return mistralOCRClientInst, mistralOCRClientErr
}

func newMistralOCRClientFromEnv() (*mistralOCRClient, error) {
	apiKey := strings.TrimSpace(os.Getenv("MISTRAL_API_KEY"))
	if apiKey == "" {
		return nil, errors.New("MISTRAL_API_KEY is not set")
	}

	baseURL := strings.TrimSpace(os.Getenv("MISTRAL_API_BASE_URL"))
	if baseURL == "" {
		baseURL = defaultMistralAPIBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	model := strings.TrimSpace(os.Getenv("MISTRAL_OCR_MODEL"))
	if model == "" {
		model = defaultMistralOCRModel
	}

	timeout := parseEnvDurationSeconds("MISTRAL_OCR_TIMEOUT_SECONDS", defaultMistralOCRTimeout)
	retryAttempts := parseEnvInt("MISTRAL_OCR_RETRY_ATTEMPTS", defaultMistralRetryAttempts)
	if retryAttempts < 1 {
		retryAttempts = defaultMistralRetryAttempts
	}
	retryBaseDelay := parseEnvDurationMilliseconds("MISTRAL_OCR_RETRY_BASE_MS", defaultMistralRetryBaseDelay)
	return &mistralOCRClient{
		apiKey:         apiKey,
		baseURL:        baseURL,
		model:          model,
		retryAttempts:  retryAttempts,
		retryBaseDelay: retryBaseDelay,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

func parseEnvDurationSeconds(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

func parseEnvDurationMilliseconds(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	milliseconds, err := strconv.Atoi(raw)
	if err != nil || milliseconds <= 0 {
		return fallback
	}
	return time.Duration(milliseconds) * time.Millisecond
}

func parseEnvInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func (c *mistralOCRClient) extractText(ctx context.Context, path string) (string, error) {
	preparedPath, cleanup, err := prepareFileForMistral(path)
	if err != nil {
		return "", err
	}
	defer cleanup()

	fileID, err := c.uploadFileWithRetry(ctx, preparedPath)
	if err != nil {
		return "", err
	}
	defer c.deleteFile(context.Background(), fileID)

	ocrResult, err := c.processOCRWithRetry(ctx, fileID)
	if err != nil {
		return "", err
	}

	text := ocrResult.mergedMarkdown()
	text = normalizeRAGText(text)
	if strings.TrimSpace(text) == "" {
		return "", errors.New("mistral ocr returned empty text")
	}
	return text, nil
}

func (c *mistralOCRClient) uploadFileWithRetry(ctx context.Context, path string) (string, error) {
	var lastErr error
	for attempt := 1; attempt <= c.retryAttempts; attempt++ {
		fileID, err := c.uploadFile(ctx, path)
		if err == nil {
			return fileID, nil
		}
		lastErr = err
		if !shouldRetryMistralError(ctx, err) || attempt == c.retryAttempts {
			break
		}
		if err := sleepWithContext(ctx, retryBackoff(c.retryBaseDelay, attempt)); err != nil {
			break
		}
	}
	return "", lastErr
}

func (c *mistralOCRClient) processOCRWithRetry(ctx context.Context, fileID string) (mistralOCRResponse, error) {
	var lastErr error
	for attempt := 1; attempt <= c.retryAttempts; attempt++ {
		ocrResult, err := c.processOCR(ctx, fileID)
		if err == nil {
			return ocrResult, nil
		}
		lastErr = err
		if !shouldRetryMistralError(ctx, err) || attempt == c.retryAttempts {
			break
		}
		if err := sleepWithContext(ctx, retryBackoff(c.retryBaseDelay, attempt)); err != nil {
			break
		}
	}
	return mistralOCRResponse{}, lastErr
}

func shouldRetryMistralError(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if ctx.Err() != nil {
		return false
	}

	var apiErr *mistralHTTPError
	if errors.As(err, &apiErr) {
		if apiErr.statusCode == http.StatusTooManyRequests {
			return true
		}
		if apiErr.statusCode >= http.StatusInternalServerError {
			return true
		}
		if apiErr.statusCode == http.StatusRequestTimeout {
			return true
		}
		return false
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	low := strings.ToLower(err.Error())
	return strings.Contains(low, "unexpected eof") ||
		strings.Contains(low, "connection reset") ||
		strings.Contains(low, "broken pipe") ||
		strings.Contains(low, "timeout")
}

func retryBackoff(base time.Duration, attempt int) time.Duration {
	if base <= 0 {
		base = defaultMistralRetryBaseDelay
	}
	if attempt <= 1 {
		return base
	}
	delay := base * time.Duration(1<<(attempt-1))
	if delay > 15*time.Second {
		return 15 * time.Second
	}
	return delay
}

func sleepWithContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func prepareFileForMistral(path string) (string, func(), error) {
	base := strings.TrimSpace(filepath.Base(path))
	if strings.EqualFold(base, ".ds_store") {
		return "", func() {}, errors.New("system file is not a document: .DS_Store")
	}

	ext := strings.ToLower(filepath.Ext(base))
	if isMistralNativeExtension(ext) {
		return path, func() {}, nil
	}

	raw, truncated, err := readLimitedFile(path, maxMistralUploadFileBytes)
	if err != nil {
		return "", func() {}, fmt.Errorf("read legacy doc file: %w", err)
	}
	if truncated {
		return "", func() {}, fmt.Errorf("legacy doc file exceeds %d bytes", maxMistralUploadFileBytes)
	}

	detected := strings.ToLower(strings.TrimSpace(http.DetectContentType(raw)))
	if !isLikelyTextMIME(detected) {
		return path, func() {}, nil
	}

	content := strings.ToValidUTF8(string(raw), " ")
	if strings.Contains(detected, "html") {
		if visible := extractVisibleHTMLText(content); strings.TrimSpace(visible) != "" {
			content = visible
		}
	}
	content = normalizeRAGText(content)
	if strings.TrimSpace(content) == "" {
		return "", func() {}, errors.New("legacy doc has no text after conversion")
	}

	tempDir, err := os.MkdirTemp("", "mistral_legacy_doc_*")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temp dir for converted doc: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(tempDir)
	}

	docxPath := filepath.Join(tempDir, strings.TrimSuffix(base, filepath.Ext(base))+".docx")
	if err := writePlainTextAsDOCX(docxPath, content); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("write temporary docx for mistral: %w", err)
	}
	return docxPath, cleanup, nil
}

func isMistralNativeExtension(ext string) bool {
	switch ext {
	case ".pdf", ".docx", ".pptx", ".xlsx", ".epub", ".rtf", ".odt",
		".png", ".jpg", ".jpeg", ".avif", ".gif", ".bmp", ".tif", ".tiff", ".webp":
		return true
	default:
		return false
	}
}

func isLikelyTextMIME(mime string) bool {
	mime = strings.ToLower(strings.TrimSpace(mime))
	if strings.HasPrefix(mime, "text/") {
		return true
	}
	if strings.Contains(mime, "html") || strings.Contains(mime, "xml") {
		return true
	}
	if mime == "application/json" || mime == "application/javascript" || mime == "application/x-javascript" {
		return true
	}
	return false
}

func createMultipartFilePart(writer *multipart.Writer, path string) (io.Writer, error) {
	filename := filepath.Base(path)
	contentDisposition := mime.FormatMediaType("form-data", map[string]string{
		"name":     "file",
		"filename": filename,
	})
	if contentDisposition == "" {
		contentDisposition = fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename)
	}

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", contentDisposition)
	header.Set("Content-Type", mistralUploadMIMEForPath(path))
	return writer.CreatePart(header)
}

func mistralUploadMIMEForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf":
		return "application/pdf"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".epub":
		return "application/epub+zip"
	case ".rtf":
		return "application/rtf"
	case ".odt":
		return "application/vnd.oasis.opendocument.text"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".avif":
		return "image/avif"
	case ".gif":
		return "image/gif"
	case ".bmp":
		return "image/bmp"
	case ".tif", ".tiff":
		return "image/tiff"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

func writePlainTextAsDOCX(path string, content string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	zipWriter := zip.NewWriter(file)
	defer zipWriter.Close()

	if err := writeZipEntry(zipWriter, "[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`); err != nil {
		return err
	}

	if err := writeZipEntry(zipWriter, "_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`); err != nil {
		return err
	}

	if err := writeZipEntry(zipWriter, "word/document.xml", buildDOCXDocumentXML(content)); err != nil {
		return err
	}

	return nil
}

func writeZipEntry(writer *zip.Writer, name string, content string) error {
	entry, err := writer.Create(name)
	if err != nil {
		return err
	}
	_, err = io.WriteString(entry, content)
	return err
}

func buildDOCXDocumentXML(content string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var body strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimRight(line, "\r")
		if strings.TrimSpace(trimmed) == "" {
			body.WriteString("<w:p/>")
			continue
		}
		body.WriteString("<w:p><w:r><w:t xml:space=\"preserve\">")
		body.WriteString(escapeXMLText(trimmed))
		body.WriteString("</w:t></w:r></w:p>")
	}

	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>` + body.String() + `<w:sectPr/>
  </w:body>
</w:document>`
}

func escapeXMLText(text string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(text)
}

func (c *mistralOCRClient) uploadFile(ctx context.Context, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("stat file: %w", err)
	}
	if info.IsDir() {
		return "", errors.New("path points to directory")
	}
	if info.Size() > maxMistralUploadFileBytes {
		return "", fmt.Errorf("file too large for Mistral upload: %d bytes (max %d)", info.Size(), maxMistralUploadFileBytes)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("purpose", "ocr"); err != nil {
		_ = writer.Close()
		return "", fmt.Errorf("write multipart field: %w", err)
	}
	part, err := createMultipartFilePart(writer, path)
	if err != nil {
		_ = writer.Close()
		return "", fmt.Errorf("create multipart file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		_ = writer.Close()
		return "", fmt.Errorf("copy file into multipart payload: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close multipart payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/files", &body)
	if err != nil {
		return "", fmt.Errorf("build upload request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload to mistral failed: %w", err)
	}
	defer resp.Body.Close()

	responseBody, _, err := readFromReaderLimited(resp.Body, maxMistralResponseBodyBytes)
	if err != nil {
		return "", fmt.Errorf("read upload response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", &mistralHTTPError{
			operation:  "mistral upload",
			statusCode: resp.StatusCode,
			body:       truncateString(strings.TrimSpace(string(responseBody)), 600),
		}
	}

	var parsed mistralFileUploadResponse
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return "", fmt.Errorf("decode upload response: %w", err)
	}
	if strings.TrimSpace(parsed.ID) == "" {
		return "", errors.New("mistral upload returned empty file id")
	}
	return parsed.ID, nil
}

func (c *mistralOCRClient) processOCR(ctx context.Context, fileID string) (mistralOCRResponse, error) {
	requestBody := mistralOCRRequest{
		Model: c.model,
		Document: mistralOCRRequestDocument{
			Type:   "file",
			FileID: fileID,
		},
	}
	rawRequestBody, err := json.Marshal(requestBody)
	if err != nil {
		return mistralOCRResponse{}, fmt.Errorf("encode ocr request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/ocr", bytes.NewReader(rawRequestBody))
	if err != nil {
		return mistralOCRResponse{}, fmt.Errorf("build ocr request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return mistralOCRResponse{}, fmt.Errorf("call mistral ocr failed: %w", err)
	}
	defer resp.Body.Close()

	responseBody, _, err := readFromReaderLimited(resp.Body, maxMistralResponseBodyBytes)
	if err != nil {
		return mistralOCRResponse{}, fmt.Errorf("read ocr response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return mistralOCRResponse{}, &mistralHTTPError{
			operation:  "mistral ocr",
			statusCode: resp.StatusCode,
			body:       truncateString(strings.TrimSpace(string(responseBody)), 600),
		}
	}

	var parsed mistralOCRResponse
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return mistralOCRResponse{}, fmt.Errorf("decode ocr response: %w", err)
	}
	return parsed, nil
}

func (c *mistralOCRClient) deleteFile(ctx context.Context, fileID string) {
	if strings.TrimSpace(fileID) == "" {
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/v1/files/"+fileID, nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	_, _, _ = readFromReaderLimited(resp.Body, 1024)
}

func (r mistralOCRResponse) mergedMarkdown() string {
	if len(r.Pages) == 0 {
		return ""
	}
	var out strings.Builder
	for _, page := range r.Pages {
		content := strings.TrimSpace(page.Markdown)
		if content == "" {
			continue
		}
		if out.Len() > 0 {
			out.WriteString("\n\n")
		}
		if len(r.Pages) > 1 {
			out.WriteString(fmt.Sprintf("[PAGE %d]\n", page.Index+1))
		}
		out.WriteString(content)
	}
	return out.String()
}
