package export

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	stdhtml "html"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	xhtml "golang.org/x/net/html"

	"tg-rag-ingestor/backend/internal/filescan"
	"tg-rag-ingestor/backend/internal/naming"
	"tg-rag-ingestor/backend/internal/storage"
)

const (
	maxTextFileBytes         = 8 << 20
	maxPDFTextBytes          = 32 << 20
	maxBinaryReadBytes       = 8 << 20
	maxExtractedRunesPerFile = 200000
	maxOCRPagesPerPDF        = 120
	commandTimeout           = 5 * time.Minute
)

var textLikeExtensions = map[string]struct{}{
	".txt": {}, ".md": {}, ".markdown": {}, ".csv": {}, ".tsv": {},
	".json": {}, ".jsonl": {}, ".yaml": {}, ".yml": {}, ".xml": {},
	".html": {}, ".htm": {}, ".log": {}, ".ini": {}, ".cfg": {}, ".conf": {},
	".sql": {}, ".py": {}, ".go": {}, ".js": {}, ".ts": {}, ".jsx": {},
	".tsx": {}, ".css": {}, ".scss": {}, ".java": {}, ".kt": {}, ".c": {},
	".cpp": {}, ".h": {}, ".hpp": {}, ".sh": {}, ".bash": {}, ".zsh": {},
	".ps1": {}, ".rb": {}, ".php": {}, ".swift": {}, ".rs": {}, ".proto": {},
}

type FilesystemRAGRequest struct {
	ScanResult     filescan.ScanDirectoryResult `json:"-"`
	IncludeSkipped bool                         `json:"include_skipped"`
}

type FilesystemRAGResult struct {
	ExportID      string `json:"export_id"`
	FilePath      string `json:"file_path"`
	RowCount      int    `json:"row_count"`
	ScannedFiles  int    `json:"scanned_files"`
	ExportedFiles int    `json:"exported_files"`
	SkippedFiles  int    `json:"skipped_files"`
}

func (s *Service) ExportFilesystemRAG(ctx context.Context, req FilesystemRAGRequest) (FilesystemRAGResult, error) {
	record, err := s.repo.CreateExport(ctx, storage.CreateExportInput{
		ExportType: "txt_filesystem_rag",
		Status:     "running",
	})
	if err != nil {
		return FilesystemRAGResult{}, err
	}

	if err := os.MkdirAll(s.exportDir, 0o755); err != nil {
		_ = s.repo.FailExport(ctx, record.ID, err.Error())
		return FilesystemRAGResult{}, err
	}

	base := naming.SanitizeFilename(filepath.Base(strings.TrimSpace(req.ScanResult.RootPath)))
	if base == "" {
		base = "filesystem"
	}
	filename := fmt.Sprintf("%s_filesystem_rag_%s.txt", base, time.Now().UTC().Format("20060102_150405"))
	filePath := filepath.Join(s.exportDir, filename)
	file, err := os.Create(filePath)
	if err != nil {
		_ = s.repo.FailExport(ctx, record.ID, err.Error())
		return FilesystemRAGResult{}, err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	rows := 0
	exportedFiles := 0
	skippedFiles := 0

	for _, item := range req.ScanResult.Files {
		status := "ok"
		reason := ""
		text, method, extractErr := extractFilesystemFileText(ctx, item.ResolvedPath)
		if extractErr != nil {
			status = "skipped"
			reason = extractErr.Error()
			skippedFiles++
			if !req.IncludeSkipped {
				continue
			}
			text = "[FILE_SKIPPED] " + reason
		} else {
			text = normalizeRAGText(text)
			if strings.TrimSpace(text) == "" {
				status = "empty"
				reason = "no extractable text"
				skippedFiles++
				if !req.IncludeSkipped {
					continue
				}
				text = "[FILE_EMPTY] no extractable text"
			} else {
				exportedFiles++
			}
		}

		if err := writeFilesystemRAGDocument(writer, req.ScanResult.RootPath, item, text, method, status, reason); err != nil {
			_ = s.repo.FailExport(ctx, record.ID, err.Error())
			return FilesystemRAGResult{}, err
		}
		rows++
	}

	if err := writer.Flush(); err != nil {
		_ = s.repo.FailExport(ctx, record.ID, err.Error())
		return FilesystemRAGResult{}, err
	}

	if err := s.repo.CompleteExport(ctx, record.ID, filePath, rows); err != nil {
		return FilesystemRAGResult{}, err
	}

	return FilesystemRAGResult{
		ExportID:      record.ID,
		FilePath:      filePath,
		RowCount:      rows,
		ScannedFiles:  len(req.ScanResult.Files),
		ExportedFiles: exportedFiles,
		SkippedFiles:  skippedFiles,
	}, nil
}

func writeFilesystemRAGDocument(
	writer *bufio.Writer,
	rootPath string,
	file filescan.ScannedFile,
	text string,
	method string,
	status string,
	reason string,
) error {
	if _, err := writer.WriteString("<<<RAG_DOCUMENT>>>\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "source_type: filesystem_file\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "root_path: %s\n", ragMetaValue(rootPath)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "logical_path: %s\n", ragMetaValue(file.LogicalPath)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "resolved_path: %s\n", ragMetaValue(file.ResolvedPath)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "origin: %s\n", ragMetaValue(file.Origin)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "name: %s\n", ragMetaValue(file.Name)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "size_bytes: %d\n", file.SizeBytes); err != nil {
		return err
	}
	if strings.TrimSpace(method) != "" {
		if _, err := fmt.Fprintf(writer, "extraction_method: %s\n", ragMetaValue(method)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(status) != "" {
		if _, err := fmt.Fprintf(writer, "extraction_status: %s\n", ragMetaValue(status)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(reason) != "" {
		if _, err := fmt.Fprintf(writer, "extraction_note: %s\n", ragMetaValue(reason)); err != nil {
			return err
		}
	}
	if file.ArchivePath != nil && strings.TrimSpace(*file.ArchivePath) != "" {
		if _, err := fmt.Fprintf(writer, "archive_path: %s\n", ragMetaValue(*file.ArchivePath)); err != nil {
			return err
		}
	}
	if file.ArchiveMemberPath != nil && strings.TrimSpace(*file.ArchiveMemberPath) != "" {
		if _, err := fmt.Fprintf(writer, "archive_member_path: %s\n", ragMetaValue(*file.ArchiveMemberPath)); err != nil {
			return err
		}
	}
	if _, err := writer.WriteString("---\n"); err != nil {
		return err
	}
	if _, err := writer.WriteString(normalizeRAGText(text)); err != nil {
		return err
	}
	if _, err := writer.WriteString("\n<<<END_RAG_DOCUMENT>>>\n\n"); err != nil {
		return err
	}
	return nil
}

func ragMetaValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	return value
}

func extractFilesystemFileText(ctx context.Context, path string) (string, string, error) {
	text, err := extractFileTextViaMistralOCR(ctx, path)
	if err != nil {
		return "", "mistral_ocr", err
	}
	text, _ = trimToRuneLimit(text, maxExtractedRunesPerFile)
	return text, "mistral_ocr", nil
}

func readTextFile(path string, maxBytes int64, checkBinary bool) (string, error) {
	data, truncated, err := readLimitedFile(path, maxBytes)
	if err != nil {
		return "", err
	}
	if checkBinary && looksBinary(data) {
		return "", errors.New("binary file content")
	}

	text := strings.ToValidUTF8(string(data), " ")
	text = normalizeRAGText(text)
	if truncated {
		text += fmt.Sprintf("\n[TRUNCATED: file exceeds %d bytes]", maxBytes)
	}
	return text, nil
}

func normalizePDFExtractedText(raw string) string {
	text := normalizeRAGText(raw)
	if strings.TrimSpace(text) == "" {
		return ""
	}
	if looksFragmentedPDFText(text) {
		text = reflowFragmentedPDFText(text)
	}
	return normalizeRAGText(text)
}

func looksFragmentedPDFText(text string) bool {
	lines := strings.Split(text, "\n")
	nonEmpty := 0
	shortLines := 0
	totalWords := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		nonEmpty++
		words := len(strings.Fields(line))
		totalWords += words
		if words <= 2 {
			shortLines++
		}
	}

	if nonEmpty < 40 {
		return false
	}
	avgWords := float64(totalWords) / float64(nonEmpty)
	shortRatio := float64(shortLines) / float64(nonEmpty)
	return avgWords < 2.6 || shortRatio > 0.7
}

func reflowFragmentedPDFText(text string) string {
	lines := strings.Split(text, "\n")
	paragraphs := make([]string, 0, 32)
	current := make([]string, 0, 16)

	flush := func() {
		if len(current) == 0 {
			return
		}
		paragraphs = append(paragraphs, joinPDFLines(current))
		current = current[:0]
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			flush()
			continue
		}
		if looksLikeListLine(line) {
			flush()
			paragraphs = append(paragraphs, line)
			continue
		}
		current = append(current, line)
	}
	flush()

	return strings.Join(paragraphs, "\n\n")
}

func joinPDFLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString(lines[0])

	for i := 1; i < len(lines); i++ {
		next := strings.TrimSpace(lines[i])
		if next == "" {
			continue
		}
		current := out.String()
		if strings.HasSuffix(current, "-") && startsWithLetterOrDigit(next) {
			out.Reset()
			out.WriteString(strings.TrimSuffix(current, "-"))
			out.WriteString(next)
			continue
		}
		out.WriteByte(' ')
		out.WriteString(next)
	}
	return out.String()
}

func startsWithLetterOrDigit(text string) bool {
	if text == "" {
		return false
	}
	first, _ := utf8.DecodeRuneInString(text)
	return unicode.IsLetter(first) || unicode.IsDigit(first)
}

func looksLikeListLine(line string) bool {
	if line == "" {
		return false
	}
	prefixes := []string{"-", "*", "•", "·"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(line, prefix+" ") {
			return true
		}
	}
	if len(line) >= 3 && unicode.IsDigit(rune(line[0])) && line[1] == '.' && line[2] == ' ' {
		return true
	}
	return false
}

func isCommandAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func errorSummary(err error) string {
	if err == nil {
		return "ok"
	}
	return truncateString(strings.TrimSpace(err.Error()), 180)
}

func truncateString(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}

func extractDOCXText(path string) (string, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("open docx: %w", err)
	}
	defer archive.Close()

	parts := make([]string, 0, 4)
	for _, file := range archive.File {
		name := strings.ToLower(file.Name)
		if !strings.HasPrefix(name, "word/") {
			continue
		}
		base := filepath.Base(name)
		if !isDOCXTextPart(base) {
			continue
		}

		rc, err := file.Open()
		if err != nil {
			return "", fmt.Errorf("open docx part %s: %w", name, err)
		}
		data, _, readErr := readFromReaderLimited(rc, maxTextFileBytes)
		_ = rc.Close()
		if readErr != nil {
			return "", fmt.Errorf("read docx part %s: %w", name, readErr)
		}

		text := normalizeRAGText(extractOpenXMLText(data))
		if text != "" {
			parts = append(parts, text)
		}
	}

	if len(parts) == 0 {
		return "", errors.New("docx has no extractable text")
	}
	return strings.Join(parts, "\n\n"), nil
}

func isDOCXTextPart(base string) bool {
	switch {
	case base == "document.xml":
		return true
	case base == "footnotes.xml":
		return true
	case base == "endnotes.xml":
		return true
	case strings.HasPrefix(base, "header") && strings.HasSuffix(base, ".xml"):
		return true
	case strings.HasPrefix(base, "footer") && strings.HasSuffix(base, ".xml"):
		return true
	default:
		return false
	}
}

func extractOpenXMLText(data []byte) string {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var out strings.Builder
	lastDelimiter := true
	lastNewline := false

	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			break
		}

		switch typed := token.(type) {
		case xml.StartElement:
			local := strings.ToLower(typed.Name.Local)
			switch local {
			case "p", "br", "cr", "tr":
				if out.Len() > 0 && !lastNewline {
					out.WriteByte('\n')
				}
				lastDelimiter = true
				lastNewline = true
			case "tab":
				out.WriteByte('\t')
				lastDelimiter = false
				lastNewline = false
			}
		case xml.CharData:
			fragment := strings.TrimSpace(stdhtml.UnescapeString(string(typed)))
			if fragment == "" {
				continue
			}
			if out.Len() > 0 && !lastDelimiter {
				out.WriteByte(' ')
			}
			out.WriteString(fragment)
			lastDelimiter = false
			lastNewline = false
		}
	}

	return out.String()
}

func extractBinaryStrings(path string, maxBytes int64) (string, error) {
	data, _, err := readLimitedFile(path, maxBytes)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", errors.New("empty file")
	}

	text := strings.ToValidUTF8(string(data), " ")
	var out strings.Builder
	var chunk strings.Builder

	flushChunk := func() {
		s := strings.TrimSpace(chunk.String())
		chunk.Reset()
		if utf8.RuneCountInString(s) < 4 {
			return
		}
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(s)
	}

	for _, r := range text {
		if isLooseTextRune(r) {
			chunk.WriteRune(r)
			continue
		}
		flushChunk()
	}
	flushChunk()

	result := normalizeRAGText(out.String())
	if result == "" {
		return "", errors.New("no printable text found in binary file")
	}
	return result, nil
}

func cleanLegacyBinaryExtractedText(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}

	if htmlText := extractVisibleHTMLText(text); strings.TrimSpace(htmlText) != "" {
		text = htmlText
	}

	text = stripBinaryNoiseLines(text)
	return normalizeRAGText(text)
}

func extractVisibleHTMLText(raw string) string {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return ""
	}

	lower := strings.ToLower(candidate)
	if !looksLikeHTMLPayload(lower) {
		return ""
	}

	if idx := strings.Index(lower, "<!doctype"); idx > 0 {
		candidate = candidate[idx:]
	} else if idx := strings.Index(lower, "<html"); idx > 0 {
		candidate = candidate[idx:]
	}

	root, err := xhtml.Parse(strings.NewReader(candidate))
	if err != nil {
		return ""
	}

	lines := make([]string, 0, 128)
	var current strings.Builder
	pushLine := func() {
		line := strings.TrimSpace(current.String())
		current.Reset()
		if line == "" {
			return
		}
		lines = append(lines, line)
	}

	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node == nil {
			return
		}

		switch node.Type {
		case xhtml.TextNode:
			fragment := cleanHTMLTextFragment(node.Data)
			if fragment == "" {
				return
			}
			if current.Len() > 0 && shouldInsertSpace(current.String(), fragment) {
				current.WriteByte(' ')
			}
			current.WriteString(fragment)
			return

		case xhtml.ElementNode:
			tag := strings.ToLower(node.Data)
			if shouldSkipHTMLNode(tag, node.Attr) {
				return
			}
			if tag == "br" {
				pushLine()
				return
			}
			block := isHTMLBlockTag(tag)
			if block {
				pushLine()
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
			if block {
				pushLine()
			}
			return
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}

	walk(root)
	pushLine()

	if len(lines) == 0 {
		return ""
	}
	return normalizeRAGText(strings.Join(lines, "\n"))
}

func cleanHTMLTextFragment(value string) string {
	value = stdhtml.UnescapeString(value)
	value = strings.ReplaceAll(value, "\u00A0", " ")
	value = strings.Join(strings.Fields(value), " ")
	return strings.TrimSpace(value)
}

func looksLikeHTMLPayload(lowerText string) bool {
	hits := 0
	markers := []string{"<html", "<body", "<div", "<span", "<p", "<table", "<br", "</"}
	for _, marker := range markers {
		if strings.Contains(lowerText, marker) {
			hits++
		}
	}
	return hits >= 2
}

func shouldSkipHTMLNode(tag string, attrs []xhtml.Attribute) bool {
	switch tag {
	case "script", "style", "noscript", "template", "head", "meta", "link", "svg", "canvas", "iframe", "input":
		return true
	}

	for _, attr := range attrs {
		key := strings.ToLower(strings.TrimSpace(attr.Key))
		value := strings.ToLower(strings.TrimSpace(attr.Val))

		if key == "hidden" {
			return true
		}
		if key == "type" && value == "hidden" {
			return true
		}
		if key == "style" && (strings.Contains(value, "display:none") || strings.Contains(value, "visibility:hidden")) {
			return true
		}
		if strings.Contains(value, "__viewstate") || strings.Contains(value, "__eventvalidation") || strings.Contains(value, "__viewstategenerator") || strings.Contains(value, "aspnethidden") {
			return true
		}
	}

	return false
}

func isHTMLBlockTag(tag string) bool {
	switch tag {
	case "address", "article", "aside", "blockquote", "dd", "div", "dl", "dt", "fieldset", "figcaption", "figure", "footer",
		"form", "h1", "h2", "h3", "h4", "h5", "h6", "header", "hr", "li", "main", "nav", "ol", "p", "pre", "section", "table",
		"tbody", "td", "tfoot", "th", "thead", "tr", "ul":
		return true
	default:
		return false
	}
}

func shouldInsertSpace(current, next string) bool {
	if current == "" || next == "" {
		return false
	}
	last, _ := utf8.DecodeLastRuneInString(current)
	first, _ := utf8.DecodeRuneInString(next)
	if unicode.IsSpace(last) || unicode.IsSpace(first) {
		return false
	}
	if strings.ContainsRune(".,;:!?)]}", first) {
		return false
	}
	if strings.ContainsRune("([{\"'", last) {
		return false
	}
	return true
}

func stripBinaryNoiseLines(text string) string {
	lines := strings.Split(text, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			filtered = append(filtered, "")
			continue
		}
		if isBinaryNoiseLine(line) {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.Join(filtered, "\n")
}

func isBinaryNoiseLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	if lower == "" {
		return false
	}

	noiseMarkers := []string{
		"__viewstate",
		"__eventvalidation",
		"__viewstategenerator",
		"__requestverificationtoken",
		"aspnethidden",
		"javascript:",
		"window.__",
	}
	for _, marker := range noiseMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}

	if strings.Count(line, "<")+strings.Count(line, ">") >= 8 {
		return true
	}

	return containsLongEncodedToken(line)
}

func containsLongEncodedToken(line string) bool {
	for _, token := range strings.Fields(line) {
		candidate := strings.Trim(token, "\"'()[]{}<>,;:.")
		if len(candidate) < 180 {
			continue
		}
		if isEncodedLikeToken(candidate) {
			return true
		}
	}
	return false
}

func isEncodedLikeToken(token string) bool {
	if len(token) < 180 {
		return false
	}

	allowed := 0
	for _, r := range token {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '+' || r == '/' || r == '=' || r == '_' || r == '-' {
			allowed++
		}
	}

	ratio := float64(allowed) / float64(len(token))
	return ratio >= 0.93
}

func isLooseTextRune(r rune) bool {
	if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsPunct(r) || unicode.IsSpace(r) {
		return true
	}
	return unicode.IsPrint(r)
}

func looksBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return true
	}

	controlBytes := 0
	for _, b := range data {
		if b < 0x09 {
			controlBytes++
			continue
		}
		if b > 0x0D && b < 0x20 {
			controlBytes++
		}
	}

	return float64(controlBytes)/float64(len(data)) > 0.10
}

func readLimitedFile(path string, maxBytes int64) ([]byte, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()

	return readFromReaderLimited(file, maxBytes)
}

func readFromReaderLimited(reader io.Reader, maxBytes int64) ([]byte, bool, error) {
	if maxBytes <= 0 {
		maxBytes = 1
	}

	limited := io.LimitReader(reader, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > maxBytes {
		return data[:maxBytes], true, nil
	}
	return data, false, nil
}

func trimToRuneLimit(text string, limit int) (string, bool) {
	if limit <= 0 {
		return "", true
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text, false
	}
	return string(runes[:limit]) + fmt.Sprintf("\n[TRUNCATED: content cut to %d characters]", limit), true
}
