package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"tg-rag-ingestor/backend/internal/chunking"
	"tg-rag-ingestor/backend/internal/cleaning"
	"tg-rag-ingestor/backend/internal/export"
	"tg-rag-ingestor/backend/internal/filescan"
	"tg-rag-ingestor/backend/internal/ingestion"
	"tg-rag-ingestor/backend/internal/naming"
	"tg-rag-ingestor/backend/internal/storage"
	"tg-rag-ingestor/backend/internal/telegram"
	"tg-rag-ingestor/backend/internal/youtube"
)

type Handler struct {
	repo             *storage.Repository
	ingestionService *ingestion.Service
	youtubeService   *youtube.Service
	exportService    *export.Service
	fileScanService  *filescan.Service
}

func NewHandler(
	repo *storage.Repository,
	ingestionService *ingestion.Service,
	youtubeService *youtube.Service,
	exportService *export.Service,
	fileScanService *filescan.Service,
) *Handler {
	return &Handler{
		repo:             repo,
		ingestionService: ingestionService,
		youtubeService:   youtubeService,
		exportService:    exportService,
		fileScanService:  fileScanService,
	}
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	status := map[string]any{"ok": true}
	if err := h.repo.Ping(r.Context()); err != nil {
		status["ok"] = false
		status["postgres"] = err.Error()
		writeJSON(w, http.StatusServiceUnavailable, status)
		return
	}
	status["postgres"] = "ok"
	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) ListSources(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListSources(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed_to_list_sources", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) CreateSource(w http.ResponseWriter, r *http.Request) {
	type request struct {
		URL      string  `json:"url"`
		Username string  `json:"username"`
		Title    *string `json:"title"`
	}
	var req request
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	username, normalizedURL, err := telegram.ResolveUsername(telegram.ResolveInput{
		URL:      req.URL,
		Username: req.Username,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_source", err.Error())
		return
	}

	sourceType := "telegram_public_channel"
	source, err := h.repo.CreateSource(r.Context(), storage.CreateSourceInput{
		SourceType: sourceType,
		Username:   ptrIfNotEmpty(username),
		Title:      req.Title,
		URL:        normalizedURL,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed_to_create_source", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, source)
}

func (h *Handler) ListYouTubeSources(w http.ResponseWriter, r *http.Request) {
	items, err := h.youtubeService.ListYouTubeSources(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed_to_list_youtube_sources", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) CreateYouTubeSource(w http.ResponseWriter, r *http.Request) {
	type request struct {
		URL   string  `json:"url"`
		Title *string `json:"title"`
	}
	var req request
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	source, err := h.youtubeService.CreateYouTubeSource(r.Context(), youtube.CreateYouTubeSourceInput{
		URL:   req.URL,
		Title: req.Title,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed_to_create_youtube_source", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, source)
}

func (h *Handler) GetYouTubeSource(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "id")
	source, err := h.youtubeService.GetYouTubeSource(r.Context(), sourceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "source_not_found", "source not found")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_youtube_source", err.Error())
		return
	}

	stats, err := h.repo.GetSourceStats(r.Context(), sourceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed_to_get_source_stats", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"source": source,
		"stats":  stats,
	})
}

func (h *Handler) GetYouTubeSourceAudio(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "id")
	result, err := h.youtubeService.GetSourceAudio(r.Context(), sourceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "source_not_found", "source not found")
			return
		}
		writeError(w, http.StatusBadRequest, "failed_to_get_youtube_audio", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) DownloadYouTubeAudio(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "id")
	source, err := h.youtubeService.GetYouTubeSource(r.Context(), sourceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "source_not_found", "source not found")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_youtube_source", err.Error())
		return
	}
	if strings.EqualFold(source.Status, "running") {
		writeError(w, http.StatusConflict, "youtube_download_running", "audio download is already running for this source")
		return
	}

	go func(sourceID string) {
		if _, err := h.youtubeService.DownloadSourceAudio(context.Background(), sourceID); err != nil {
			log.Printf("youtube audio download failed source_id=%s: %v", sourceID, err)
		}
	}(sourceID)

	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":    "queued",
		"source_id": sourceID,
		"stage":     "download_audio",
	})
}

func (h *Handler) TranscribeYouTubeAudio(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "id")
	source, err := h.youtubeService.GetYouTubeSource(r.Context(), sourceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "source_not_found", "source not found")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_youtube_source", err.Error())
		return
	}
	if strings.EqualFold(source.Status, "running") {
		writeError(w, http.StatusConflict, "youtube_transcription_running", "another job is already running for this source")
		return
	}
	audioDetails, err := h.youtubeService.GetSourceAudio(r.Context(), sourceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed_to_get_youtube_audio", err.Error())
		return
	}
	if audioDetails.Artifact == nil || audioDetails.Artifact.AudioFilePath == nil || strings.TrimSpace(*audioDetails.Artifact.AudioFilePath) == "" {
		writeError(w, http.StatusConflict, "youtube_audio_not_downloaded", "download audio first, then run transcription")
		return
	}

	go func(sourceID string) {
		if _, err := h.youtubeService.TranscribeSourceAudio(context.Background(), sourceID); err != nil {
			log.Printf("youtube transcription failed source_id=%s: %v", sourceID, err)
		}
	}(sourceID)

	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":    "queued",
		"source_id": sourceID,
		"stage":     "transcribe_audio",
	})
}

func (h *Handler) ImportJSONFile(w http.ResponseWriter, r *http.Request) {
	const maxUploadBytes = int64(100 << 20) // 100MB

	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_multipart_form", err.Error())
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing_file", "form field 'file' is required")
		return
	}
	defer file.Close()

	payload, err := io.ReadAll(io.LimitReader(file, maxUploadBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed_to_read_file", err.Error())
		return
	}
	if int64(len(payload)) > maxUploadBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", "max file size is 100MB")
		return
	}

	result, err := h.ingestionService.ImportJSON(r.Context(), ingestion.ImportJSONInput{
		Filename:   strings.TrimSpace(header.Filename),
		SourceName: strings.TrimSpace(r.FormValue("source_name")),
		Title:      strings.TrimSpace(r.FormValue("title")),
		Payload:    payload,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "import_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, result)
}

func (h *Handler) ScanFilesystemDirectory(w http.ResponseWriter, r *http.Request) {
	if h.fileScanService == nil {
		writeError(w, http.StatusServiceUnavailable, "filesystem_scan_unavailable", "filesystem scan service is not configured")
		return
	}

	type request struct {
		Path string `json:"path"`
	}
	var req request
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	result, err := h.fileScanService.ScanDirectory(r.Context(), filescan.ScanDirectoryInput{
		Path: req.Path,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "filesystem_scan_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) GetSource(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "id")
	source, err := h.repo.GetSource(r.Context(), sourceID)
	if err != nil {
		handleNotFound(w, err, "source")
		return
	}
	stats, err := h.repo.GetSourceStats(r.Context(), sourceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed_to_get_source_stats", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"source": source,
		"stats":  stats,
	})
}

func (h *Handler) SyncSource(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "id")
	type request struct {
		Limit       int  `json:"limit"`
		BatchSize   int  `json:"batch_size"`
		MaxMessages int  `json:"max_messages"`
		FullResync  bool `json:"full_resync"`
	}
	var req request
	if err := decodeJSON(r, &req); err != nil && !errors.Is(err, errEmptyBody) {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.BatchSize <= 0 && req.Limit > 0 {
		req.BatchSize = req.Limit
	}

	result, err := h.ingestionService.SyncSource(r.Context(), sourceID, ingestion.SyncOptions{
		BatchSize:   req.BatchSize,
		MaxMessages: req.MaxMessages,
		FullResync:  req.FullResync,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "sync_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ListRawMessages(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "id")
	limit := parseIntQuery(r, "limit", 50)
	items, err := h.repo.ListRawMessagesBySource(r.Context(), sourceID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed_to_list_raw_messages", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) ListDocuments(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "id")
	limit := parseIntQuery(r, "limit", 100)
	items, err := h.repo.ListDocumentsBySource(r.Context(), sourceID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed_to_list_documents", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) ListParsedPosts(w http.ResponseWriter, r *http.Request) {
	type request struct {
		SourceIDs         []string `json:"source_ids"`
		LimitPerSource    int      `json:"limit_per_source"`
		IncludeDuplicates bool     `json:"include_duplicates"`
	}

	var req request
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	sourceIDs := make([]string, 0, len(req.SourceIDs))
	for _, sourceID := range req.SourceIDs {
		trimmed := strings.TrimSpace(sourceID)
		if trimmed == "" {
			continue
		}
		sourceIDs = append(sourceIDs, trimmed)
	}
	if len(sourceIDs) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_source_ids", "source_ids must contain at least one source id")
		return
	}

	limitPerSource := req.LimitPerSource
	if limitPerSource <= 0 {
		limitPerSource = 100
	}
	if limitPerSource > 2000 {
		limitPerSource = 2000
	}

	items, err := h.repo.ListParsedPostsBySources(r.Context(), sourceIDs, limitPerSource, req.IncludeDuplicates)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed_to_list_parsed_posts", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) GetDocument(w http.ResponseWriter, r *http.Request) {
	documentID := chi.URLParam(r, "id")
	doc, err := h.repo.GetDocumentByID(r.Context(), documentID)
	if err != nil {
		handleNotFound(w, err, "document")
		return
	}
	chunks, err := h.repo.ListChunksByDocumentID(r.Context(), documentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed_to_list_chunks", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"document": doc,
		"chunks":   chunks,
	})
}

func (h *Handler) DownloadDocument(w http.ResponseWriter, r *http.Request) {
	documentID := chi.URLParam(r, "id")
	doc, err := h.repo.GetDocumentByID(r.Context(), documentID)
	if err != nil {
		handleNotFound(w, err, "document")
		return
	}
	source, sourceErr := h.repo.GetSource(r.Context(), doc.SourceID)
	chunks, err := h.repo.ListChunksByDocumentID(r.Context(), documentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed_to_list_chunks", err.Error())
		return
	}

	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "txt"
	}
	filename := naming.SanitizeFilename(doc.ExternalDocID)
	if sourceErr == nil {
		filename = naming.DocumentFilename(source, doc)
	}
	if filename == "" {
		filename = "document_" + naming.SanitizeFilename(documentID)
	}

	switch format {
	case "json":
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.json"`, filename))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"document": doc,
			"chunks":   chunks,
		})
	case "txt":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.txt"`, filename))
		var builder strings.Builder
		builder.WriteString(doc.TextClean)
		if len(chunks) > 0 {
			builder.WriteString("\n\n--- Chunks ---\n")
			for _, chunk := range chunks {
				builder.WriteString("\n[Chunk ")
				builder.WriteString(strconv.Itoa(chunk.ChunkIndex))
				builder.WriteString("]\n")
				builder.WriteString(chunk.Text)
				builder.WriteString("\n")
			}
		}
		_, _ = io.WriteString(w, builder.String())
	default:
		writeError(w, http.StatusBadRequest, "invalid_format", "supported formats: txt, json")
	}
}

func (h *Handler) PreviewClean(w http.ResponseWriter, r *http.Request) {
	type request struct {
		Text string `json:"text"`
	}
	var req request
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	cleaned := cleaning.NormalizeText(req.Text)
	writeJSON(w, http.StatusOK, map[string]any{
		"cleaned":  cleaned,
		"is_trash": cleaning.IsTrash(cleaned),
		"links":    cleaning.ExtractLinks(cleaned),
		"hashtags": cleaning.ExtractHashtags(cleaned),
		"mentions": cleaning.ExtractMentions(cleaned),
		"chunks":   chunking.SplitWithConfig(cleaned, chunking.DefaultConfig()),
	})
}

func (h *Handler) CreateJSONLExport(w http.ResponseWriter, r *http.Request) {
	type request struct {
		SourceID          *string `json:"source_id"`
		Mode              string  `json:"mode"`
		Format            string  `json:"format"`
		IncludeDuplicates bool    `json:"include_duplicates"`
		IncludeTrash      bool    `json:"include_trash"`
	}
	var req request
	if err := decodeJSON(r, &req); err != nil && !errors.Is(err, errEmptyBody) {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	result, err := h.exportService.ExportJSONL(r.Context(), export.JSONLRequest{
		SourceID:          req.SourceID,
		Mode:              req.Mode,
		Format:            req.Format,
		IncludeDuplicates: req.IncludeDuplicates,
		IncludeTrash:      req.IncludeTrash,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "export_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (h *Handler) ListExports(w http.ResponseWriter, r *http.Request) {
	limit := parseIntQuery(r, "limit", 100)
	items, err := h.repo.ListExports(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed_to_list_exports", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) DownloadExport(w http.ResponseWriter, r *http.Request) {
	exportID := chi.URLParam(r, "id")
	item, err := h.repo.GetExportByID(r.Context(), exportID)
	if err != nil {
		handleNotFound(w, err, "export")
		return
	}
	if item.Status != "succeeded" || item.FilePath == nil || strings.TrimSpace(*item.FilePath) == "" {
		writeError(w, http.StatusBadRequest, "export_not_ready", "export is not ready for download")
		return
	}

	filePath := strings.TrimSpace(*item.FilePath)
	file, err := os.Open(filePath)
	if err != nil {
		writeError(w, http.StatusNotFound, "export_file_not_found", err.Error())
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "export_file_stat_failed", err.Error())
		return
	}

	filename := filepath.Base(filePath)
	if filename == "" || filename == "." || filename == "/" {
		extension := ".jsonl"
		if strings.HasPrefix(item.ExportType, "txt_") {
			extension = ".txt"
		}
		filename = "export_" + naming.SanitizeFilename(item.ID) + extension
	}

	contentType := "application/octet-stream"
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".jsonl", ".ndjson":
		contentType = "application/x-ndjson"
	case ".txt":
		contentType = "text/plain; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	http.ServeContent(w, r, filename, info.ModTime(), file)
}

func (h *Handler) ListJobs(w http.ResponseWriter, r *http.Request) {
	limit := parseIntQuery(r, "limit", 100)
	items, err := h.repo.ListJobs(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed_to_list_jobs", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func handleNotFound(w http.ResponseWriter, err error, name string) {
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, name+"_not_found", fmt.Sprintf("%s not found", name))
		return
	}
	writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
}

var errEmptyBody = errors.New("empty request body")

func decodeJSON(r *http.Request, target any) error {
	if r.Body == nil {
		return errEmptyBody
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if strings.Contains(err.Error(), "EOF") {
			return errEmptyBody
		}
		return err
	}
	return nil
}

func parseIntQuery(r *http.Request, key string, fallback int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

func ptrIfNotEmpty(v string) *string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return &v
}
