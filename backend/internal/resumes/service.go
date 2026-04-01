package resumes

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"tg-rag-ingestor/backend/internal/hhclient"
	"tg-rag-ingestor/backend/internal/model"
)

type Service struct {
	client *hhclient.Client
	logger *slog.Logger
}

func New(client *hhclient.Client, logger *slog.Logger) *Service {
	return &Service{
		client: client,
		logger: logger,
	}
}

func (s *Service) FetchFull(ctx context.Context, candidate model.NegotiationCandidate) (*model.Resume, error) {
	var resume model.Resume

	resumeID := strings.TrimSpace(candidate.ResumeID)
	target := strings.TrimSpace(candidate.ResumeAPIURL)

	var status int
	var err error
	switch {
	case strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://"):
		status, err = s.client.GetJSONURL(ctx, target, nil, &resume)
	case strings.HasPrefix(target, "/"):
		status, err = s.client.GetJSON(ctx, target, nil, &resume)
	default:
		status, err = s.client.GetJSON(ctx, "/resumes/"+resumeID, nil, &resume)
	}
	if err != nil {
		if apiErr, ok := err.(*hhclient.APIError); ok {
			switch apiErr.Type {
			case "no_available_service":
				return nil, fmt.Errorf("resume %s requires paid access: %w", resumeID, err)
			case "cant_view_contacts":
				s.logger.Warn("cant view contacts for resume", "resume_id", resumeID)
			}
		}
		return nil, fmt.Errorf("fetch resume %s failed: status=%d err=%w", resumeID, status, err)
	}
	return &resume, nil
}

func (s *Service) DownloadOriginal(ctx context.Context, fs model.FileSystem, outputDir string, resume *model.Resume) (string, bool, error) {
	if resume == nil || resume.Download == nil {
		return "", false, fmt.Errorf("resume has no download links")
	}
	if err := fs.MkdirAll(outputDir, 0o755); err != nil {
		return "", false, fmt.Errorf("create originals dir: %w", err)
	}

	downloadURL := ""
	ext := ""
	if resume.Download.PDF != nil && strings.TrimSpace(resume.Download.PDF.URL) != "" {
		downloadURL = strings.TrimSpace(resume.Download.PDF.URL)
		ext = "pdf"
	} else if resume.Download.RTF != nil && strings.TrimSpace(resume.Download.RTF.URL) != "" {
		downloadURL = strings.TrimSpace(resume.Download.RTF.URL)
		ext = "rtf"
	}
	if downloadURL == "" {
		return "", false, fmt.Errorf("resume %s has no downloadable original", resume.ID)
	}

	content, status, err := s.client.Download(ctx, downloadURL)
	if err != nil {
		return "", false, fmt.Errorf("download original resume %s failed: status=%d err=%w", resume.ID, status, err)
	}

	baseName := sanitizeFilePart(resume.FIO())
	if baseName == "" || baseName == "[ФИО НЕДОСТУПНО]" {
		baseName = "candidate"
	}
	fileName := fmt.Sprintf("%s_%s.%s", baseName, resume.ID, ext)
	filePath := filepath.Join(outputDir, fileName)
	if err := fs.WriteFile(filePath, content, 0o644); err != nil {
		return "", false, fmt.Errorf("save original resume file: %w", err)
	}
	return filePath, true, nil
}

func FormatContacts(contacts []model.Contact) []string {
	return formatContacts(contacts)
}

func sanitizeFilePart(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return v
	}
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	return replacer.Replace(v)
}
