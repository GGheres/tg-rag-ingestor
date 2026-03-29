package hh

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
)

// ResumeService fetches full resumes and downloads original files.
type ResumeService struct {
	client *Client
	logger *slog.Logger
}

func NewResumeService(client *Client, logger *slog.Logger) *ResumeService {
	return &ResumeService{client: client, logger: logger}
}

// FetchFull fetches the complete resume by ID.
func (s *ResumeService) FetchFull(ctx context.Context, resumeID string) (*Resume, error) {
	s.logger.Debug("fetching full resume", "resume_id", resumeID)

	body, status, err := s.client.Get(ctx, "/resumes/"+resumeID, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch resume %s: %w", resumeID, err)
	}

	if status != http.StatusOK {
		apiErr := ParseAPIError(body)
		if status == http.StatusForbidden {
			if apiErr.HasErrorType("no_available_service") {
				return nil, fmt.Errorf("resume %s: requires paid access (no_available_service)", resumeID)
			}
			if apiErr.HasErrorType("cant_view_contacts") {
				// Resume accessible but contacts hidden — not a fatal error.
				s.logger.Warn("contacts hidden for resume", "resume_id", resumeID)
			}
			return nil, fmt.Errorf("resume %s: forbidden (err=%+v)", resumeID, apiErr)
		}
		if status == http.StatusNotFound {
			return nil, fmt.Errorf("resume %s: not found", resumeID)
		}
		return nil, fmt.Errorf("resume %s: status=%d err=%+v", resumeID, status, apiErr)
	}

	resume, err := ParseResume(body)
	if err != nil {
		return nil, err
	}

	return resume, nil
}

// DownloadOriginal downloads the PDF or RTF original of a resume.
// Returns the local file path and format, or an error.
func (s *ResumeService) DownloadOriginal(ctx context.Context, resume *Resume, outputDir string) (string, string, error) {
	if resume.Download == nil {
		return "", "", fmt.Errorf("no download links available for resume %s", resume.ID)
	}

	// Prefer PDF over RTF.
	var dlURL, ext string
	if resume.Download.PDF != nil && resume.Download.PDF.URL != "" {
		dlURL = resume.Download.PDF.URL
		ext = "pdf"
	} else if resume.Download.RTF != nil && resume.Download.RTF.URL != "" {
		dlURL = resume.Download.RTF.URL
		ext = "rtf"
	} else {
		return "", "", fmt.Errorf("no PDF or RTF download URL for resume %s", resume.ID)
	}

	s.logger.Debug("downloading original", "resume_id", resume.ID, "format", ext)

	data, err := s.client.DownloadFile(ctx, dlURL)
	if err != nil {
		apiErr := ParseAPIError(data)
		if apiErr.HasErrorType("no_available_service") {
			return "", "", fmt.Errorf("resume %s: download requires paid service", resume.ID)
		}
		return "", "", fmt.Errorf("download resume %s: %w", resume.ID, err)
	}

	filename := fmt.Sprintf("%s_%s.%s", resume.LastName, resume.ID, ext)
	filePath := filepath.Join(outputDir, filename)

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return "", "", fmt.Errorf("save file %s: %w", filePath, err)
	}

	s.logger.Info("downloaded original", "resume_id", resume.ID, "path", filePath, "size", len(data))
	return filePath, ext, nil
}
