package service

import (
	"context"

	"tg-rag-ingestor/backend/internal/model"
)

type NegotiationsFetcher interface {
	FetchByVacancy(ctx context.Context, vacancyID string) ([]model.NegotiationCandidate, error)
}

type ResumeProvider interface {
	FetchFull(ctx context.Context, resumeID string) (*model.Resume, error)
	DownloadOriginal(ctx context.Context, fs model.FileSystem, outputDir string, resume *model.Resume) (string, bool, error)
}

type ResumeExporter interface {
	WriteManifest(manifest *model.RunManifest, outputDir string) (string, error)
	WriteCombinedMarkdown(resumeItems []*model.Resume, vacancyID, outputDir string) (string, error)
	WriteCombinedHTML(resumeItems []*model.Resume, vacancyID, outputDir string) (string, error)
	WriteCombinedPDF(ctx context.Context, htmlPath, outputDir string) (string, error)
}
