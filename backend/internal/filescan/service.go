package filescan

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var supportedArchiveExtensions = []string{
	".zip",
	".tar",
	".tar.gz",
	".tgz",
	".tar.bz2",
	".tbz2",
	".gz",
	".bz2",
}

var knownButUnsupportedArchiveExtensions = []string{
	".7z",
	".rar",
	".tar.xz",
	".xz",
}

type Config struct {
	ExtractDir      string
	MaxFiles        int
	MaxArchiveDepth int
}

type Service struct {
	extractDir      string
	maxFiles        int
	maxArchiveDepth int
}

func NewService(cfg Config) (*Service, error) {
	extractDir := strings.TrimSpace(cfg.ExtractDir)
	if extractDir == "" {
		extractDir = "./data/filescan_extracts"
	}
	maxFiles := cfg.MaxFiles
	if maxFiles <= 0 {
		maxFiles = 20000
	}
	maxArchiveDepth := cfg.MaxArchiveDepth
	if maxArchiveDepth <= 0 {
		maxArchiveDepth = 4
	}
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return nil, fmt.Errorf("create filescan extract dir: %w", err)
	}
	return &Service{
		extractDir:      extractDir,
		maxFiles:        maxFiles,
		maxArchiveDepth: maxArchiveDepth,
	}, nil
}

type ScanDirectoryInput struct {
	Path string `json:"path"`
}

type ScanDirectoryResult struct {
	RootPath                   string           `json:"root_path"`
	ScannedAt                  time.Time        `json:"scanned_at"`
	TotalFiles                 int              `json:"total_files"`
	RegularFiles               int              `json:"regular_files"`
	ExtractedFiles             int              `json:"extracted_files"`
	ArchivesProcessed          int              `json:"archives_processed"`
	SupportedArchiveExtensions []string         `json:"supported_archive_extensions"`
	UnsupportedArchiveFormats  []string         `json:"unsupported_archive_formats"`
	SkippedArchives            []SkippedArchive `json:"skipped_archives"`
	Files                      []ScannedFile    `json:"files"`
}

type ScannedFile struct {
	Name              string  `json:"name"`
	LogicalPath       string  `json:"logical_path"`
	ResolvedPath      string  `json:"resolved_path"`
	SizeBytes         int64   `json:"size_bytes"`
	Origin            string  `json:"origin"`
	ArchivePath       *string `json:"archive_path,omitempty"`
	ArchiveMemberPath *string `json:"archive_member_path,omitempty"`
}

type SkippedArchive struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type scanState struct {
	result     ScanDirectoryResult
	extractDir string
	nextID     int
}

func (s *Service) ScanDirectory(ctx context.Context, input ScanDirectoryInput) (ScanDirectoryResult, error) {
	rootPath := normalizeInputPath(strings.TrimSpace(input.Path))
	if rootPath == "" {
		return ScanDirectoryResult{}, fmt.Errorf("path is required")
	}

	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return ScanDirectoryResult{}, fmt.Errorf("resolve path: %w", err)
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return ScanDirectoryResult{}, fmt.Errorf("directory does not exist: %s%s", absRoot, containerMountHint())
		case errors.Is(err, fs.ErrPermission):
			return ScanDirectoryResult{}, fmt.Errorf("directory is not accessible (permission denied): %s", absRoot)
		default:
			return ScanDirectoryResult{}, fmt.Errorf("inspect path %s: %w", absRoot, err)
		}
	}
	if !info.IsDir() {
		return ScanDirectoryResult{}, fmt.Errorf("path must point to a directory")
	}

	scanID := time.Now().UTC().Format("20060102T150405.000000000")
	extractDir := filepath.Join(s.extractDir, "scan_"+scanID)
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return ScanDirectoryResult{}, fmt.Errorf("create scan extract dir: %w", err)
	}

	state := &scanState{
		result: ScanDirectoryResult{
			RootPath:                   absRoot,
			ScannedAt:                  time.Now().UTC(),
			SupportedArchiveExtensions: append([]string(nil), supportedArchiveExtensions...),
			UnsupportedArchiveFormats:  append([]string(nil), knownButUnsupportedArchiveExtensions...),
			SkippedArchives:            make([]SkippedArchive, 0),
			Files:                      make([]ScannedFile, 0),
		},
		extractDir: extractDir,
	}

	if err := filepath.WalkDir(absRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if isIgnorableSystemFile(entry.Name()) {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		relativePath, err := filepath.Rel(absRoot, path)
		if err != nil {
			return fmt.Errorf("build relative path for %s: %w", path, err)
		}
		return s.scanFile(ctx, state, path, filepath.ToSlash(relativePath), 0)
	}); err != nil {
		return ScanDirectoryResult{}, err
	}

	sort.Slice(state.result.Files, func(i, j int) bool {
		return state.result.Files[i].LogicalPath < state.result.Files[j].LogicalPath
	})
	state.result.TotalFiles = len(state.result.Files)

	return state.result, nil
}

func normalizeInputPath(raw string) string {
	path := strings.TrimSpace(raw)
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(strings.ToLower(path), "file:") {
		return path
	}

	if parsed, err := url.Parse(path); err == nil && strings.EqualFold(parsed.Scheme, "file") {
		candidate := parsed.Path
		if candidate == "" {
			candidate = parsed.Opaque
		}
		if parsed.Host != "" && !strings.EqualFold(parsed.Host, "localhost") {
			candidate = "//" + parsed.Host + candidate
		}
		if candidate != "" {
			if unescaped, unescapeErr := url.PathUnescape(candidate); unescapeErr == nil {
				candidate = unescaped
			}
			return strings.TrimSpace(candidate)
		}
	}

	fallback := strings.TrimPrefix(path, "file://")
	fallback = strings.TrimPrefix(fallback, "file:")
	if unescaped, err := url.PathUnescape(fallback); err == nil {
		return strings.TrimSpace(unescaped)
	}
	return strings.TrimSpace(fallback)
}

func containerMountHint() string {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return " (api is running in Docker; mount this host directory into the api container or use a mounted path)"
	}
	return ""
}

func isIgnorableSystemFile(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch lower {
	case ".ds_store", "thumbs.db", "desktop.ini":
		return true
	default:
		return strings.HasPrefix(lower, "._")
	}
}

func (s *Service) scanFile(ctx context.Context, state *scanState, resolvedPath string, logicalPath string, archiveDepth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if state.result.TotalFiles >= s.maxFiles {
		return fmt.Errorf("file limit exceeded: max %d files per scan", s.maxFiles)
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return fmt.Errorf("stat file %s: %w", resolvedPath, err)
	}

	archiveKind, supported := detectArchiveKind(resolvedPath)
	if archiveKind != "" && supported {
		if archiveDepth >= s.maxArchiveDepth {
			state.result.SkippedArchives = append(state.result.SkippedArchives, SkippedArchive{
				Path:   logicalPath,
				Reason: fmt.Sprintf("archive depth limit exceeded: max %d", s.maxArchiveDepth),
			})
			return s.appendFile(state, ScannedFile{
				Name:         filepath.Base(resolvedPath),
				LogicalPath:  logicalPath,
				ResolvedPath: resolvedPath,
				SizeBytes:    info.Size(),
				Origin:       originForDepth(archiveDepth),
			})
		}
		return s.extractAndScanArchive(ctx, state, resolvedPath, logicalPath, archiveKind, archiveDepth)
	}

	if archiveKind != "" && !supported {
		state.result.SkippedArchives = append(state.result.SkippedArchives, SkippedArchive{
			Path:   logicalPath,
			Reason: fmt.Sprintf("unsupported archive format: %s", archiveKind),
		})
	}

	entry := ScannedFile{
		Name:         filepath.Base(resolvedPath),
		LogicalPath:  logicalPath,
		ResolvedPath: resolvedPath,
		SizeBytes:    info.Size(),
		Origin:       originForDepth(archiveDepth),
	}
	if archiveDepth > 0 {
		archivePath, archiveMemberPath := splitArchiveLogicalPath(logicalPath)
		entry.ArchivePath = ptrString(archivePath)
		entry.ArchiveMemberPath = ptrString(archiveMemberPath)
		state.result.ExtractedFiles++
	} else {
		state.result.RegularFiles++
	}
	return s.appendFile(state, entry)
}

func (s *Service) extractAndScanArchive(ctx context.Context, state *scanState, archivePath string, logicalPath string, archiveKind string, archiveDepth int) error {
	state.result.ArchivesProcessed++
	state.nextID++
	destDir := filepath.Join(state.extractDir, fmt.Sprintf("%06d", state.nextID))
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create archive extract dir: %w", err)
	}

	if err := extractArchive(archivePath, destDir, archiveKind); err != nil {
		state.result.SkippedArchives = append(state.result.SkippedArchives, SkippedArchive{
			Path:   logicalPath,
			Reason: err.Error(),
		})
		info, statErr := os.Stat(archivePath)
		if statErr != nil {
			return fmt.Errorf("extract archive %s: %w", logicalPath, err)
		}
		entry := ScannedFile{
			Name:         filepath.Base(archivePath),
			LogicalPath:  logicalPath,
			ResolvedPath: archivePath,
			SizeBytes:    info.Size(),
			Origin:       originForDepth(archiveDepth),
		}
		if archiveDepth > 0 {
			archivePath, archiveMemberPath := splitArchiveLogicalPath(logicalPath)
			entry.ArchivePath = ptrString(archivePath)
			entry.ArchiveMemberPath = ptrString(archiveMemberPath)
			state.result.ExtractedFiles++
		} else {
			state.result.RegularFiles++
		}
		return s.appendFile(state, entry)
	}

	return filepath.WalkDir(destDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if isIgnorableSystemFile(entry.Name()) {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relativePath, err := filepath.Rel(destDir, path)
		if err != nil {
			return fmt.Errorf("build archive relative path for %s: %w", path, err)
		}
		memberPath := filepath.ToSlash(relativePath)
		return s.scanFile(ctx, state, path, logicalPath+"!/"+memberPath, archiveDepth+1)
	})
}

func (s *Service) appendFile(state *scanState, file ScannedFile) error {
	state.result.Files = append(state.result.Files, file)
	state.result.TotalFiles = len(state.result.Files)
	if state.result.TotalFiles > s.maxFiles {
		return fmt.Errorf("file limit exceeded: max %d files per scan", s.maxFiles)
	}
	return nil
}

func detectArchiveKind(path string) (string, bool) {
	lowerPath := strings.ToLower(strings.TrimSpace(path))
	switch {
	case strings.HasSuffix(lowerPath, ".tar.gz"):
		return ".tar.gz", true
	case strings.HasSuffix(lowerPath, ".tgz"):
		return ".tgz", true
	case strings.HasSuffix(lowerPath, ".tar.bz2"):
		return ".tar.bz2", true
	case strings.HasSuffix(lowerPath, ".tbz2"):
		return ".tbz2", true
	case strings.HasSuffix(lowerPath, ".tar"):
		return ".tar", true
	case strings.HasSuffix(lowerPath, ".zip"):
		return ".zip", true
	case strings.HasSuffix(lowerPath, ".gz"):
		return ".gz", true
	case strings.HasSuffix(lowerPath, ".bz2"):
		return ".bz2", true
	case strings.HasSuffix(lowerPath, ".7z"):
		return ".7z", false
	case strings.HasSuffix(lowerPath, ".rar"):
		return ".rar", false
	case strings.HasSuffix(lowerPath, ".tar.xz"):
		return ".tar.xz", false
	case strings.HasSuffix(lowerPath, ".xz"):
		return ".xz", false
	default:
		return "", false
	}
}

func extractArchive(path string, destDir string, kind string) error {
	switch kind {
	case ".zip":
		return extractZIP(path, destDir)
	case ".tar":
		return extractTarFile(path, destDir)
	case ".tar.gz", ".tgz":
		return extractTarGzipFile(path, destDir)
	case ".tar.bz2", ".tbz2":
		return extractTarBzip2File(path, destDir)
	case ".gz":
		return extractGzipFile(path, destDir)
	case ".bz2":
		return extractBzip2File(path, destDir)
	default:
		return fmt.Errorf("unsupported archive format: %s", kind)
	}
}

func extractZIP(path string, destDir string) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer reader.Close()

	for _, file := range reader.File {
		targetPath, err := safeArchiveTarget(destDir, file.Name)
		if err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return fmt.Errorf("create zip dir: %w", err)
			}
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return fmt.Errorf("open zip entry: %w", err)
		}
		if err := writeFileFromReader(targetPath, rc, file.Mode()); err != nil {
			rc.Close()
			return err
		}
		rc.Close()
	}
	return nil
}

func extractTarFile(path string, destDir string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open tar: %w", err)
	}
	defer file.Close()
	return extractTarReader(file, destDir)
}

func extractTarGzipFile(path string, destDir string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open tar.gz: %w", err)
	}
	defer file.Close()

	reader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("open gzip stream: %w", err)
	}
	defer reader.Close()

	return extractTarReader(reader, destDir)
}

func extractTarBzip2File(path string, destDir string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open tar.bz2: %w", err)
	}
	defer file.Close()

	return extractTarReader(bzip2.NewReader(file), destDir)
}

func extractTarReader(reader io.Reader, destDir string) error {
	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		targetPath, err := safeArchiveTarget(destDir, header.Name)
		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return fmt.Errorf("create tar dir: %w", err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := writeFileFromReader(targetPath, tarReader, fs.FileMode(header.Mode)); err != nil {
				return err
			}
		}
	}
}

func extractGzipFile(path string, destDir string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open gzip: %w", err)
	}
	defer file.Close()

	reader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("open gzip stream: %w", err)
	}
	defer reader.Close()

	targetName := strings.TrimSuffix(filepath.Base(path), ".gz")
	if targetName == "" {
		targetName = filepath.Base(path) + ".out"
	}
	targetPath, err := safeArchiveTarget(destDir, targetName)
	if err != nil {
		return err
	}
	return writeFileFromReader(targetPath, reader, 0o644)
}

func extractBzip2File(path string, destDir string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open bz2: %w", err)
	}
	defer file.Close()

	targetName := strings.TrimSuffix(filepath.Base(path), ".bz2")
	if targetName == "" {
		targetName = filepath.Base(path) + ".out"
	}
	targetPath, err := safeArchiveTarget(destDir, targetName)
	if err != nil {
		return err
	}
	return writeFileFromReader(targetPath, bzip2.NewReader(file), 0o644)
}

func writeFileFromReader(path string, reader io.Reader, mode fs.FileMode) error {
	if mode == 0 {
		mode = 0o644
	}
	parentDir := filepath.Dir(path)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("create file %s: %w", path, err)
	}
	defer file.Close()

	if _, err := io.Copy(file, reader); err != nil {
		return fmt.Errorf("write file %s: %w", path, err)
	}
	return nil
}

func safeArchiveTarget(destDir string, entryName string) (string, error) {
	cleanDest := filepath.Clean(destDir)
	cleanTarget := filepath.Clean(filepath.Join(cleanDest, entryName))
	if cleanTarget != cleanDest && !strings.HasPrefix(cleanTarget, cleanDest+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive entry escapes extraction dir: %s", entryName)
	}
	return cleanTarget, nil
}

func splitArchiveLogicalPath(logicalPath string) (string, string) {
	index := strings.LastIndex(logicalPath, "!/")
	if index < 0 {
		return logicalPath, ""
	}
	return logicalPath[:index], logicalPath[index+2:]
}

func originForDepth(archiveDepth int) string {
	if archiveDepth > 0 {
		return "archive"
	}
	return "filesystem"
}

func ptrString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}
