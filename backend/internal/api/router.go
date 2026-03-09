package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"tg-rag-ingestor/backend/internal/export"
	"tg-rag-ingestor/backend/internal/ingestion"
	"tg-rag-ingestor/backend/internal/storage"
)

func NewRouter(
	repo *storage.Repository,
	ingestionService *ingestion.Service,
	exportService *export.Service,
	logger *slog.Logger,
	allowedOrigin string,
) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(requestLogger(logger))
	r.Use(corsMiddleware(allowedOrigin))

	h := NewHandler(repo, ingestionService, exportService)

	r.Get("/health", h.Health)

	r.Route("/api", func(r chi.Router) {
		r.Get("/sources", h.ListSources)
		r.Post("/sources", h.CreateSource)
		r.Post("/imports/json", h.ImportJSONFile)
		r.Get("/sources/{id}", h.GetSource)
		r.Post("/sources/{id}/sync", h.SyncSource)
		r.Get("/sources/{id}/raw-messages", h.ListRawMessages)
		r.Get("/sources/{id}/documents", h.ListDocuments)
		r.Post("/posts/parsed", h.ListParsedPosts)
		r.Get("/documents/{id}", h.GetDocument)
		r.Get("/documents/{id}/download", h.DownloadDocument)
		r.Post("/preview/clean", h.PreviewClean)
		r.Post("/exports/jsonl", h.CreateJSONLExport)
		r.Get("/exports", h.ListExports)
		r.Get("/exports/{id}/download", h.DownloadExport)
		r.Get("/jobs", h.ListJobs)
	})

	return r
}
