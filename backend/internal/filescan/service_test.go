package filescan

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestScanDirectory_RecursesAndExtractsArchives(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	extractDir := filepath.Join(t.TempDir(), "extracts")

	mustWriteFile(t, filepath.Join(rootDir, "plain", "note.txt"), "hello")
	mustCreateZIP(t, filepath.Join(rootDir, "bundle.zip"), map[string][]byte{
		"inside/report.txt": []byte("zip content"),
	})
	mustCreateTarGz(t, filepath.Join(rootDir, "nested", "archive.tar.gz"), map[string][]byte{
		"deep/log.txt": []byte("tar gz content"),
	})

	service, err := NewService(Config{ExtractDir: extractDir, MaxFiles: 100, MaxArchiveDepth: 4})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	result, err := service.ScanDirectory(context.Background(), ScanDirectoryInput{Path: rootDir})
	if err != nil {
		t.Fatalf("ScanDirectory() error = %v", err)
	}

	gotPaths := make([]string, 0, len(result.Files))
	for _, item := range result.Files {
		gotPaths = append(gotPaths, item.LogicalPath)
	}

	wantPaths := []string{
		"bundle.zip!/inside/report.txt",
		"nested/archive.tar.gz!/deep/log.txt",
		"plain/note.txt",
	}
	if !slices.Equal(gotPaths, wantPaths) {
		t.Fatalf("logical paths mismatch\nwant: %#v\ngot:  %#v", wantPaths, gotPaths)
	}

	if result.RegularFiles != 1 {
		t.Fatalf("RegularFiles = %d, want 1", result.RegularFiles)
	}
	if result.ExtractedFiles != 2 {
		t.Fatalf("ExtractedFiles = %d, want 2", result.ExtractedFiles)
	}
	if result.ArchivesProcessed != 2 {
		t.Fatalf("ArchivesProcessed = %d, want 2", result.ArchivesProcessed)
	}
	if result.TotalFiles != 3 {
		t.Fatalf("TotalFiles = %d, want 3", result.TotalFiles)
	}
}

func TestScanDirectory_ExtractsNestedArchives(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	extractDir := filepath.Join(t.TempDir(), "extracts")

	innerZIP := buildZIPBytes(t, map[string][]byte{
		"deep/final.txt": []byte("nested zip"),
	})
	mustCreateZIP(t, filepath.Join(rootDir, "outer.zip"), map[string][]byte{
		"inner.zip": innerZIP,
	})

	service, err := NewService(Config{ExtractDir: extractDir, MaxFiles: 100, MaxArchiveDepth: 4})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	result, err := service.ScanDirectory(context.Background(), ScanDirectoryInput{Path: rootDir})
	if err != nil {
		t.Fatalf("ScanDirectory() error = %v", err)
	}

	if len(result.Files) != 1 {
		t.Fatalf("len(result.Files) = %d, want 1", len(result.Files))
	}
	if result.Files[0].LogicalPath != "outer.zip!/inner.zip!/deep/final.txt" {
		t.Fatalf("nested logical path = %q", result.Files[0].LogicalPath)
	}
	if result.Files[0].Origin != "archive" {
		t.Fatalf("Origin = %q, want archive", result.Files[0].Origin)
	}
	if result.ArchivesProcessed != 2 {
		t.Fatalf("ArchivesProcessed = %d, want 2", result.ArchivesProcessed)
	}
}

func TestScanDirectory_MissingDirectoryReturnsClearError(t *testing.T) {
	t.Parallel()

	extractDir := filepath.Join(t.TempDir(), "extracts")
	service, err := NewService(Config{ExtractDir: extractDir, MaxFiles: 100, MaxArchiveDepth: 4})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	_, err = service.ScanDirectory(context.Background(), ScanDirectoryInput{
		Path: filepath.Join(t.TempDir(), "missing"),
	})
	if err == nil {
		t.Fatal("ScanDirectory() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "directory does not exist") {
		t.Fatalf("ScanDirectory() error = %q, want to contain %q", err.Error(), "directory does not exist")
	}
}

func TestScanDirectory_AcceptsFileURIPaths(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	rootDir := filepath.Join(baseDir, "dir with space")
	extractDir := filepath.Join(t.TempDir(), "extracts")
	mustWriteFile(t, filepath.Join(rootDir, "a.txt"), "hello")

	service, err := NewService(Config{ExtractDir: extractDir, MaxFiles: 100, MaxArchiveDepth: 4})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	fileURL := (&url.URL{
		Scheme: "file",
		Path:   rootDir,
	}).String()
	inputs := []string{
		"file:" + rootDir,
		fileURL,
	}

	for _, inputPath := range inputs {
		result, scanErr := service.ScanDirectory(context.Background(), ScanDirectoryInput{Path: inputPath})
		if scanErr != nil {
			t.Fatalf("ScanDirectory(%q) error = %v", inputPath, scanErr)
		}
		if result.RootPath != rootDir {
			t.Fatalf("ScanDirectory(%q) RootPath = %q, want %q", inputPath, result.RootPath, rootDir)
		}
		if len(result.Files) != 1 {
			t.Fatalf("ScanDirectory(%q) files = %d, want 1", inputPath, len(result.Files))
		}
	}
}

func mustWriteFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func mustCreateZIP(t *testing.T, path string, entries map[string][]byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create(%q) error = %v", path, err)
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	for name, contents := range entries {
		entryWriter, err := writer.Create(name)
		if err != nil {
			t.Fatalf("Create zip entry %q error = %v", name, err)
		}
		if _, err := entryWriter.Write(contents); err != nil {
			t.Fatalf("Write zip entry %q error = %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close zip writer error = %v", err)
	}
}

func buildZIPBytes(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	buffer := bytes.NewBuffer(nil)
	writer := zip.NewWriter(buffer)
	for name, contents := range entries {
		entryWriter, err := writer.Create(name)
		if err != nil {
			t.Fatalf("Create zip entry %q error = %v", name, err)
		}
		if _, err := entryWriter.Write(contents); err != nil {
			t.Fatalf("Write zip entry %q error = %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close zip writer error = %v", err)
	}
	return buffer.Bytes()
}

func mustCreateTarGz(t *testing.T, path string, entries map[string][]byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create(%q) error = %v", path, err)
	}
	defer file.Close()

	gzipWriter := gzip.NewWriter(file)
	defer gzipWriter.Close()
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	for name, contents := range entries {
		header := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(contents)),
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("WriteHeader(%q) error = %v", name, err)
		}
		if _, err := tarWriter.Write(contents); err != nil {
			t.Fatalf("Write(%q) error = %v", name, err)
		}
	}
}
