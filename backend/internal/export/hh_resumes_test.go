package export

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"log/slog"

	"tg-rag-ingestor/backend/internal/model"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestWriteCombinedMarkdown(t *testing.T) {
	dir := t.TempDir()
	exporter := NewHHExporter(model.OSFileSystem{}, testLogger())

	age := 30
	resumes := []*model.Resume{
		{
			ID:           "resume_1",
			FirstName:    "Иван",
			LastName:     "Иванов",
			Title:        "Go Developer",
			Age:          &age,
			AlternateURL: "https://hh.ru/resume/resume_1",
			UpdatedAt:    "2026-03-26T00:00:00+0300",
			SkillSet:     []string{"Go", "PostgreSQL"},
			Experience: []model.Experience{
				{
					Company:  "Acme",
					Position: "Senior Engineer",
					Start:    "2022-01",
				},
			},
		},
	}

	mdPath, err := exporter.WriteCombinedMarkdown(resumes, "vacancy_1", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	content := string(data)
	contains := []string{
		"КАНДИДАТ: Иванов Иван",
		"RESUME ID: resume_1",
		"VACANCY ID: vacancy_1",
		"Желаемая должность: Go Developer",
		"Навыки: Go, PostgreSQL",
	}
	for _, item := range contains {
		if !strings.Contains(content, item) {
			t.Fatalf("markdown is missing %q", item)
		}
	}
}

func TestWriteManifestAndCollectFiles(t *testing.T) {
	dir := t.TempDir()
	originalsDir := filepath.Join(dir, "originals")
	if err := os.MkdirAll(originalsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(originalsDir, "resume_1.pdf"), []byte("pdf"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	exporter := NewHHExporter(model.OSFileSystem{}, testLogger())
	manifest := &model.RunManifest{
		VacancyID: "vacancy_1",
		Provider:  "headhunter_employer_api",
	}
	if _, err := exporter.WriteManifest(manifest, dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	files, err := CollectExtractionFiles(model.OSFileSystem{}, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(files, ",")
	if !strings.Contains(joined, "manifest.json") {
		t.Fatalf("manifest file not found in collection")
	}
	if !strings.Contains(joined, "originals/resume_1.pdf") {
		t.Fatalf("original file not found in collection")
	}
}

func TestWriteCombinedPDFNoChrome(t *testing.T) {
	dir := t.TempDir()
	exporter := NewHHExporter(model.OSFileSystem{}, testLogger())
	htmlPath := filepath.Join(dir, "combined_resumes.html")
	if err := os.WriteFile(htmlPath, []byte("<html><body>test</body></html>"), 0o644); err != nil {
		t.Fatalf("write html: %v", err)
	}
	_, err := exporter.WriteCombinedPDF(context.Background(), htmlPath, dir)
	if err == nil {
		t.Skip("chrome/chromium found in environment, cannot assert missing binary")
	}
}
