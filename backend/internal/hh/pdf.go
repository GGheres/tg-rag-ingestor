package hh

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

// GeneratePDF converts the combined HTML to PDF.
// Uses chromedp if available, falls back to Chrome/Chromium headless CLI.
// This is an optional feature — if no browser is available, it returns an error.
func GeneratePDF(ctx context.Context, htmlPath, outputDir string, logger *slog.Logger) (string, error) {
	pdfPath := filepath.Join(outputDir, "combined_resumes.pdf")

	// Try chromedp-based conversion first (requires chromedp dependency).
	// If chromedp is not available, fall back to headless Chrome CLI.
	if err := generatePDFViaHeadlessChrome(ctx, htmlPath, pdfPath, logger); err != nil {
		return "", err
	}

	return pdfPath, nil
}

// generatePDFViaHeadlessChrome uses Chrome/Chromium headless mode via CLI.
// This avoids the heavy chromedp dependency while still producing PDF.
func generatePDFViaHeadlessChrome(ctx context.Context, htmlPath, pdfPath string, logger *slog.Logger) error {
	// Find Chrome or Chromium binary.
	chromePaths := []string{
		"google-chrome",
		"google-chrome-stable",
		"chromium",
		"chromium-browser",
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	}

	var chromeBin string
	for _, p := range chromePaths {
		if path, err := exec.LookPath(p); err == nil {
			chromeBin = path
			break
		}
		// Check absolute paths directly.
		if _, err := os.Stat(p); err == nil {
			chromeBin = p
			break
		}
	}

	if chromeBin == "" {
		return fmt.Errorf("no Chrome/Chromium found — install Chrome or add chromedp dependency for PDF export")
	}

	absHTML, err := filepath.Abs(htmlPath)
	if err != nil {
		return fmt.Errorf("resolve html path: %w", err)
	}

	absPDF, err := filepath.Abs(pdfPath)
	if err != nil {
		return fmt.Errorf("resolve pdf path: %w", err)
	}

	logger.Info("generating PDF via headless Chrome", "chrome", chromeBin)

	cmd := exec.CommandContext(ctx, chromeBin,
		"--headless",
		"--disable-gpu",
		"--no-sandbox",
		"--print-to-pdf="+absPDF,
		"--print-to-pdf-no-header",
		"file://"+absHTML,
	)
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("chrome headless PDF generation failed: %w", err)
	}

	info, err := os.Stat(absPDF)
	if err != nil {
		return fmt.Errorf("verify PDF: %w", err)
	}

	logger.Info("PDF generated", "path", absPDF, "size", info.Size())
	return nil
}
