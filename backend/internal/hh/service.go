package hh

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sync/errgroup"
)

// Service is the main orchestrator for HH resume extraction.
type Service struct {
	cfg          Config
	client       *Client
	negotiations *NegotiationsService
	resumes      *ResumeService
	logger       *slog.Logger
}

func NewService(cfg Config, logger *slog.Logger) *Service {
	client := NewClient(cfg, logger)
	return &Service{
		cfg:          cfg,
		client:       client,
		negotiations: NewNegotiationsService(client, logger),
		resumes:      NewResumeService(client, logger),
		logger:       logger,
	}
}

// Run executes the full pipeline: fetch negotiations -> resumes -> export.
func (s *Service) Run(ctx context.Context) error {
	s.logger.Info("starting HH resume extraction",
		"vacancy_id", s.cfg.VacancyID,
		"dry_run", s.cfg.DryRun,
		"output_dir", s.cfg.OutputDir,
		"concurrency", s.cfg.Concurrency,
	)

	// Prepare output directories.
	originalsDir := filepath.Join(s.cfg.OutputDir, "originals")
	if !s.cfg.DryRun {
		if err := os.MkdirAll(originalsDir, 0755); err != nil {
			return fmt.Errorf("create output dirs: %w", err)
		}
	}

	// Step 1: Fetch all negotiations.
	items, err := s.negotiations.FetchAllByVacancy(ctx, s.cfg.VacancyID)
	if err != nil {
		return fmt.Errorf("fetch negotiations: %w", err)
	}

	if len(items) == 0 {
		s.logger.Warn("no candidates found for vacancy", "vacancy_id", s.cfg.VacancyID)
		return nil
	}

	manifest := &Manifest{
		VacancyID:  s.cfg.VacancyID,
		TotalFound: len(items),
		DryRun:     s.cfg.DryRun,
	}

	if s.cfg.DryRun {
		s.logger.Info("DRY RUN — would process candidates", "count", len(items))
		for _, item := range items {
			manifest.Candidates = append(manifest.Candidates, CandidateResult{
				CandidateID: item.ID,
				ResumeID:    item.Resume.ID,
				FIO:         item.Resume.FIO(),
				Status:      "skipped",
			})
		}
		manifest.Processed = len(items)
		if _, err := WriteManifest(manifest, s.cfg.OutputDir); err != nil {
			// In dry-run, output dir might not exist, log and continue.
			s.logger.Warn("could not write manifest in dry-run", "error", err)
		}
		return nil
	}

	// Step 2: Fetch full resumes concurrently.
	type resumeResult struct {
		item   NegotiationItem
		resume *Resume
		err    error
	}

	concurrency := s.cfg.Concurrency
	if concurrency <= 0 {
		concurrency = 3
	}

	results := make([]resumeResult, len(items))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)

	for i, item := range items {
		i, item := i, item
		g.Go(func() error {
			resume, err := s.resumes.FetchFull(gctx, item.Resume.ID)
			results[i] = resumeResult{item: item, resume: resume, err: err}
			return nil // Never fail the group — partial success.
		})
	}
	_ = g.Wait()

	// Step 3: Download originals concurrently.
	var (
		successResumes []*Resume
		mu             sync.Mutex
	)

	g2, gctx2 := errgroup.WithContext(ctx)
	g2.SetLimit(concurrency)

	for i := range results {
		i := i
		r := &results[i]

		cr := CandidateResult{
			CandidateID: r.item.ID,
			ResumeID:    r.item.Resume.ID,
			FIO:         r.item.Resume.FIO(),
		}

		if r.err != nil {
			cr.Status = "error"
			cr.ErrorMessage = r.err.Error()
			s.logger.Error("failed to fetch resume",
				"resume_id", r.item.Resume.ID,
				"fio", cr.FIO,
				"error", r.err,
			)
			manifest.Candidates = append(manifest.Candidates, cr)
			manifest.Failed++
			continue
		}

		// Update FIO from full resume (more complete).
		cr.FIO = r.resume.FIO()

		mu.Lock()
		successResumes = append(successResumes, r.resume)
		mu.Unlock()

		cr.Status = "ok"
		cr.IncludedInCombined = true

		// Download original in background.
		idx := len(manifest.Candidates)
		manifest.Candidates = append(manifest.Candidates, cr)

		g2.Go(func() error {
			filePath, _, dlErr := s.resumes.DownloadOriginal(gctx2, results[i].resume, originalsDir)
			mu.Lock()
			defer mu.Unlock()
			if dlErr != nil {
				s.logger.Warn("could not download original",
					"resume_id", results[i].resume.ID,
					"error", dlErr,
				)
				manifest.Candidates[idx].DownloadedOriginal = false
				manifest.Candidates[idx].Status = "partial"
			} else {
				manifest.Candidates[idx].DownloadedOriginal = true
				manifest.Candidates[idx].OriginalFile = filepath.Base(filePath)
			}
			return nil
		})
	}
	_ = g2.Wait()

	manifest.Processed = len(items)
	manifest.Succeeded = len(successResumes)
	manifest.Failed = manifest.Processed - manifest.Succeeded

	// Step 4: Generate combined files.
	if len(successResumes) > 0 {
		txtPath, err := GenerateCombinedText(successResumes, s.cfg.VacancyID, s.cfg.OutputDir)
		if err != nil {
			s.logger.Error("failed to generate combined text", "error", err)
		} else {
			s.logger.Info("combined text generated", "path", txtPath)
		}

		htmlPath, err := GenerateCombinedHTML(successResumes, s.cfg.VacancyID, s.cfg.OutputDir)
		if err != nil {
			s.logger.Error("failed to generate combined HTML", "error", err)
		} else {
			s.logger.Info("combined HTML generated", "path", htmlPath)
		}

		// Step 5: Optional PDF export.
		if s.cfg.ExportPDF && htmlPath != "" {
			pdfPath, err := GeneratePDF(ctx, htmlPath, s.cfg.OutputDir, s.logger)
			if err != nil {
				s.logger.Warn("PDF export failed (non-critical)", "error", err)
			} else {
				s.logger.Info("combined PDF generated", "path", pdfPath)
			}
		}
	} else {
		s.logger.Warn("no resumes fetched successfully, skipping export")
	}

	// Step 6: Write manifest.
	manifestPath, err := WriteManifest(manifest, s.cfg.OutputDir)
	if err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	s.logger.Info("manifest written", "path", manifestPath)

	s.logger.Info("extraction complete",
		"total", manifest.TotalFound,
		"succeeded", manifest.Succeeded,
		"failed", manifest.Failed,
	)

	return nil
}
