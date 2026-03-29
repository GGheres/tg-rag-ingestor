package export

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMistralOCRResponseMergedMarkdown(t *testing.T) {
	t.Parallel()

	response := mistralOCRResponse{
		Pages: []mistralOCRPage{
			{Index: 0, Markdown: "# Page one"},
			{Index: 1, Markdown: "Page two text"},
		},
	}

	merged := response.mergedMarkdown()
	if !strings.Contains(merged, "[PAGE 1]") || !strings.Contains(merged, "[PAGE 2]") {
		t.Fatalf("expected page markers in merged markdown, got %q", merged)
	}
	if !strings.Contains(merged, "# Page one") || !strings.Contains(merged, "Page two text") {
		t.Fatalf("expected page content in merged markdown, got %q", merged)
	}
}

func TestParseEnvDurationSeconds(t *testing.T) {
	t.Parallel()

	const key = "TEST_MISTRAL_TIMEOUT_SECONDS"
	_ = os.Unsetenv(key)
	fallback := 90 * time.Second

	if got := parseEnvDurationSeconds(key, fallback); got != fallback {
		t.Fatalf("expected fallback duration for empty env, got %s", got)
	}

	if err := os.Setenv(key, "12"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	defer os.Unsetenv(key)

	if got := parseEnvDurationSeconds(key, fallback); got != 12*time.Second {
		t.Fatalf("expected 12s, got %s", got)
	}
}

func TestPrepareFileForMistral_ConvertsLegacyDocHTMLToDOCX(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.doc")
	payload := `<html><body><h1>Law 22</h1><p>Article text.</p></body></html>`
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	prepared, cleanup, err := prepareFileForMistral(path)
	if err != nil {
		t.Fatalf("prepareFileForMistral error: %v", err)
	}
	defer cleanup()

	if prepared == path {
		t.Fatalf("expected converted file path, got original")
	}
	if filepath.Ext(prepared) != ".docx" {
		t.Fatalf("expected .docx extension, got %q", filepath.Ext(prepared))
	}

	reader, err := zip.OpenReader(prepared)
	if err != nil {
		t.Fatalf("open converted docx: %v", err)
	}
	defer reader.Close()

	var docXML string
	for _, file := range reader.File {
		if file.Name != "word/document.xml" {
			continue
		}
		rc, openErr := file.Open()
		if openErr != nil {
			t.Fatalf("open document.xml: %v", openErr)
		}
		data, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			t.Fatalf("read document.xml: %v", readErr)
		}
		docXML = string(data)
		break
	}
	if docXML == "" {
		t.Fatalf("word/document.xml not found in converted docx")
	}
	if !strings.Contains(docXML, "Law 22") || !strings.Contains(docXML, "Article text.") {
		t.Fatalf("expected content in docx xml, got %q", docXML)
	}
}

func TestPrepareFileForMistral_LeavesNativeFormatsUntouched(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "sample.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	prepared, cleanup, err := prepareFileForMistral(path)
	if err != nil {
		t.Fatalf("prepareFileForMistral error: %v", err)
	}
	defer cleanup()

	if prepared != path {
		t.Fatalf("expected original path for native format, got %q", prepared)
	}
}

func TestShouldRetryMistralError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	if !shouldRetryMistralError(ctx, &mistralHTTPError{statusCode: 429}) {
		t.Fatalf("expected retry for 429")
	}
	if !shouldRetryMistralError(ctx, &mistralHTTPError{statusCode: 503}) {
		t.Fatalf("expected retry for 503")
	}
	if shouldRetryMistralError(ctx, &mistralHTTPError{statusCode: 422}) {
		t.Fatalf("did not expect retry for 422")
	}
}
