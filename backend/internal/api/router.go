package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"tg-rag-ingestor/backend/internal/export"
	"tg-rag-ingestor/backend/internal/filescan"
	"tg-rag-ingestor/backend/internal/ingestion"
	"tg-rag-ingestor/backend/internal/model"
	"tg-rag-ingestor/backend/internal/storage"
	"tg-rag-ingestor/backend/internal/youtube"
)

func NewRouter(
	repo *storage.Repository,
	ingestionService *ingestion.Service,
	youtubeService *youtube.Service,
	exportService *export.Service,
	fileScanService *filescan.Service,
	hhConfig model.HHConfig,
	logger *slog.Logger,
	allowedOrigin string,
) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(requestLogger(logger))
	r.Use(corsMiddleware(allowedOrigin))

	h := NewHandler(repo, ingestionService, youtubeService, exportService, fileScanService, hhConfig, logger)

	r.Get("/health", h.Health)

	r.Route("/api", func(r chi.Router) {
		r.Get("/sources", h.ListSources)
		r.Delete("/sources", h.DeleteAllSources)
		r.Post("/sources", h.CreateSource)
		r.Post("/sources/telegram-channel-document", h.CreateTelegramChannelDocumentSource)
		r.Post("/sources/telegram-message-links", h.CreateTelegramMessageLinkSource)
		r.Post("/imports/json", h.ImportJSONFile)
		r.Post("/filesystem/scan", h.ScanFilesystemDirectory)
		r.Post("/filesystem/export-rag", h.ExportFilesystemRAG)
		r.Get("/sources/{id}", h.GetSource)
		r.Post("/sources/{id}/sync", h.SyncSource)
		r.Get("/sources/{id}/raw-messages", h.ListRawMessages)
		r.Get("/sources/{id}/message-links", h.ListTelegramMessageLinks)
		r.Get("/sources/{id}/documents", h.ListDocuments)
		r.Get("/youtube/sources", h.ListYouTubeSources)
		r.Post("/youtube/sources", h.CreateYouTubeSource)
		r.Get("/youtube/sources/{id}", h.GetYouTubeSource)
		r.Get("/youtube/sources/{id}/audio", h.GetYouTubeSourceAudio)
		r.Post("/youtube/sources/{id}/download-audio", h.DownloadYouTubeAudio)
		r.Post("/youtube/sources/{id}/transcribe-audio", h.TranscribeYouTubeAudio)
		r.Post("/audio/upload-and-transcribe", h.UploadAndTranscribeAudio)
		r.Post("/posts/parsed", h.ListParsedPosts)
		r.Get("/documents/{id}", h.GetDocument)
		r.Get("/documents/{id}/download", h.DownloadDocument)
		r.Post("/preview/clean", h.PreviewClean)
		r.Post("/exports/jsonl", h.CreateJSONLExport)
		r.Get("/exports", h.ListExports)
		r.Get("/exports/{id}/download", h.DownloadExport)
		r.Get("/jobs", h.ListJobs)

		// HeadHunter resume extraction
		r.Get("/hh/config", h.HHGetConfig)
		r.Get("/hh/vacancies", h.HHListVacancies)
		r.Get("/hh/access-status", h.HHGetAccessStatus)
		r.Get("/hh/payable-actions", h.HHGetPayableActions)
		r.Get("/hh/method-access", h.HHGetMethodAccess)
		r.Get("/hh/resume-limits", h.HHGetResumeLimits)
		r.Post("/hh/public-vacancies/import", h.HHImportPublicVacancies)
		r.Post("/hh/global-resumes/import", h.HHImportGlobalResumes)
		r.Get("/hh/oauth/callback", h.HHOAuthCallback)
		r.Post("/hh/oauth/exchange", h.HHExchangeCode)
		r.Post("/hh/extract", h.HHStartExtraction)
		r.Get("/hh/extractions", h.HHListExtractions)
		r.Get("/hh/extractions/{extractionID}", h.HHGetExtraction)
		r.Get("/hh/extractions/{extractionID}/files/{filename}", h.HHDownloadFile)
	})

	return r
}
