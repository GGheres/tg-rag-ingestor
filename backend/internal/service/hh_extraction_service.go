package service

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"

	"golang.org/x/sync/errgroup"

	"tg-rag-ingestor/backend/internal/auth"
	"tg-rag-ingestor/backend/internal/export"
	"tg-rag-ingestor/backend/internal/hhclient"
	"tg-rag-ingestor/backend/internal/model"
	"tg-rag-ingestor/backend/internal/negotiations"
	"tg-rag-ingestor/backend/internal/resumes"
)

const hhProvider = "headhunter_employer_api"

type HHExtractionService struct {
	cfg          model.HHConfig
	fs           model.FileSystem
	logger       *slog.Logger
	auth         *auth.Manager
	client       *hhclient.Client
	negotiations *negotiations.Service
	resumes      *resumes.Service
	exporter     *export.HHExporter
}

func NewHHExtractionService(cfg model.HHConfig, fs model.FileSystem, logger *slog.Logger) *HHExtractionService {
	cfg = cfg.WithDefaults()
	authManager := auth.NewManager(cfg, logger)
	client := hhclient.New(cfg, authManager, logger)
	return &HHExtractionService{
		cfg:          cfg,
		fs:           fs,
		logger:       logger,
		auth:         authManager,
		client:       client,
		negotiations: negotiations.New(client, logger),
		resumes:      resumes.New(client, logger),
		exporter:     export.NewHHExporter(fs, logger),
	}
}

func (s *HHExtractionService) AuthorizationURL() string {
	return s.auth.AuthorizationURL()
}

func (s *HHExtractionService) ExchangeCode(ctx context.Context, code string) (*model.TokenResponse, error) {
	return s.auth.ExchangeCode(ctx, code)
}

func (s *HHExtractionService) Run(ctx context.Context, request model.ExtractionRequest) (*model.RunManifest, []string, error) {
	request.OutputDir = ensureOutputDir(request.OutputDir, s.cfg.OutputDir)
	request.SaveOriginals = request.SaveOriginals || s.cfg.SaveOriginalsDefault
	request.SaveAsMarkdown = true

	if err := s.fs.MkdirAll(request.OutputDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create extraction output dir: %w", err)
	}
	originalsDir := filepath.Join(request.OutputDir, "originals")
	if request.SaveOriginals && !request.DryRun {
		if err := s.fs.MkdirAll(originalsDir, 0o755); err != nil {
			return nil, nil, fmt.Errorf("create originals dir: %w", err)
		}
	}

	candidates, err := s.negotiations.FetchByVacancy(ctx, request.VacancyID)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch negotiations failed: %w", err)
	}

	manifest := &model.RunManifest{
		VacancyID:  request.VacancyID,
		Provider:   hhProvider,
		TotalFound: len(candidates),
		DryRun:     request.DryRun,
		Candidates: make([]model.CandidateManifest, 0, len(candidates)),
	}

	if len(candidates) == 0 {
		_, writeErr := s.exporter.WriteManifest(manifest, request.OutputDir)
		if writeErr != nil {
			return nil, nil, writeErr
		}
		files, _ := export.CollectExtractionFiles(s.fs, request.OutputDir)
		return manifest, files, nil
	}

	if request.DryRun {
		for _, candidate := range candidates {
			manifest.Candidates = append(manifest.Candidates, model.CandidateManifest{
				CandidateID: candidate.CandidateID,
				ResumeID:    candidate.ResumeID,
				FIO:         candidate.FIO,
				Status:      "skipped",
			})
		}
		manifest.Processed = len(candidates)
		manifest.Succeeded = len(candidates)
		if _, writeErr := s.exporter.WriteManifest(manifest, request.OutputDir); writeErr != nil {
			return nil, nil, writeErr
		}
		files, _ := export.CollectExtractionFiles(s.fs, request.OutputDir)
		return manifest, files, nil
	}

	type resumeResult struct {
		candidate model.NegotiationCandidate
		resume    *model.Resume
		err       error
	}

	resumeResults := make([]resumeResult, len(candidates))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(s.cfg.Concurrency)
	for idx, candidate := range candidates {
		idx := idx
		candidate := candidate
		group.Go(func() error {
			resumeItem, fetchErr := s.resumes.FetchFull(groupCtx, candidate.ResumeID)
			resumeResults[idx] = resumeResult{
				candidate: candidate,
				resume:    resumeItem,
				err:       fetchErr,
			}
			return nil
		})
	}
	_ = group.Wait()

	successResumes := make([]*model.Resume, 0, len(candidates))
	type downloadTask struct {
		manifestIndex int
		resume        *model.Resume
	}
	downloadTasks := make([]downloadTask, 0, len(candidates))

	for _, result := range resumeResults {
		row := model.CandidateManifest{
			CandidateID: result.candidate.CandidateID,
			ResumeID:    result.candidate.ResumeID,
			FIO:         result.candidate.FIO,
		}
		if result.err != nil {
			row.Status = "error"
			row.ErrorMessage = result.err.Error()
			row.IncludedInCombined = false
			manifest.Candidates = append(manifest.Candidates, row)
			continue
		}

		row.Status = "ok"
		row.IncludedInCombined = true
		if result.resume != nil {
			row.FIO = result.resume.FIO()
			result.resume.CoverLetter = result.candidate.CoverLetter
		}
		successResumes = append(successResumes, result.resume)
		manifest.Candidates = append(manifest.Candidates, row)

		if request.SaveOriginals {
			downloadTasks = append(downloadTasks, downloadTask{
				manifestIndex: len(manifest.Candidates) - 1,
				resume:        result.resume,
			})
		}
	}

	if request.SaveOriginals && len(downloadTasks) > 0 {
		var mu sync.Mutex
		dlGroup, dlCtx := errgroup.WithContext(ctx)
		dlGroup.SetLimit(s.cfg.Concurrency)
		for _, task := range downloadTasks {
			task := task
			dlGroup.Go(func() error {
				filePath, downloaded, dlErr := s.resumes.DownloadOriginal(dlCtx, s.fs, originalsDir, task.resume)
				mu.Lock()
				defer mu.Unlock()
				if dlErr != nil {
					manifest.Candidates[task.manifestIndex].Status = "partial"
					manifest.Candidates[task.manifestIndex].DownloadedOriginal = false
					manifest.Candidates[task.manifestIndex].ErrorMessage = dlErr.Error()
					return nil
				}
				manifest.Candidates[task.manifestIndex].DownloadedOriginal = downloaded
				manifest.Candidates[task.manifestIndex].OriginalFile = filepath.Base(filePath)
				return nil
			})
		}
		_ = dlGroup.Wait()
	}

	manifest.Processed = len(candidates)
	for _, item := range manifest.Candidates {
		switch item.Status {
		case "ok", "partial":
			manifest.Succeeded++
		default:
			manifest.Failed++
		}
	}

	if len(successResumes) > 0 {
		if _, mdErr := s.exporter.WriteCombinedMarkdown(successResumes, request.VacancyID, request.OutputDir); mdErr != nil {
			s.logger.Warn("failed to create combined markdown", "error", mdErr)
		}
		htmlPath, htmlErr := s.exporter.WriteCombinedHTML(successResumes, request.VacancyID, request.OutputDir)
		if htmlErr != nil {
			s.logger.Warn("failed to create combined html", "error", htmlErr)
		} else if request.ExportPDF {
			if _, pdfErr := s.exporter.WriteCombinedPDF(ctx, htmlPath, request.OutputDir); pdfErr != nil {
				s.logger.Warn("failed to create combined pdf", "error", pdfErr)
			}
		}
	}

	if _, err := s.exporter.WriteManifest(manifest, request.OutputDir); err != nil {
		return nil, nil, err
	}

	files, _ := export.CollectExtractionFiles(s.fs, request.OutputDir)
	return manifest, files, nil
}

func ensureOutputDir(requested, fallback string) string {
	if requested != "" {
		return requested
	}
	if fallback != "" {
		return fallback
	}
	return model.DefaultHHOutputDir
}
