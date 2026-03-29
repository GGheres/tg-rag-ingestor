package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"tg-rag-ingestor/backend/internal/export"
	"tg-rag-ingestor/backend/internal/model"
	hhservice "tg-rag-ingestor/backend/internal/service"
)

func (h *Handler) newHHService(managerAccountID string) *hhservice.HHExtractionService {
	cfg := h.hhConfig
	cfg.ManagerAccountID = strings.TrimSpace(managerAccountID)
	return hhservice.NewHHExtractionService(cfg, model.OSFileSystem{}, h.logger)
}

func (h *Handler) newHHVacancyCatalogService() *hhservice.HHVacancyCatalogService {
	return hhservice.NewHHVacancyCatalogService(h.hhConfig, h.logger)
}

// HHGetConfig returns current HH configuration status without secrets.
func (h *Handler) HHGetConfig(w http.ResponseWriter, r *http.Request) {
	svc := h.newHHService("")
	configured := h.hhConfig.ClientID != "" && h.hhConfig.UserAgent != ""
	writeJSON(w, http.StatusOK, map[string]any{
		"configured":          configured,
		"has_access_token":    strings.TrimSpace(h.hhConfig.AccessToken) != "",
		"has_refresh_token":   strings.TrimSpace(h.hhConfig.RefreshToken) != "",
		"user_agent":          h.hhConfig.UserAgent,
		"output_dir":          h.hhConfig.OutputDir,
		"redirect_uri":        h.hhConfig.RedirectURI,
		"base_url":            h.hhConfig.BaseURL,
		"oauth_authorize_url": svc.AuthorizationURL(),
	})
}

// HHExchangeCode exchanges OAuth code and returns access+refresh tokens.
// Tokens are returned to UI and not persisted server-side.
func (h *Handler) HHExchangeCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Code) == "" {
		writeError(w, http.StatusBadRequest, "missing_code", "code is required")
		return
	}
	svc := h.newHHService("")
	token, err := svc.ExchangeCode(r.Context(), req.Code)
	if err != nil {
		writeError(w, http.StatusBadRequest, "oauth_exchange_failed", err.Error())
		return
	}

	envPath, err := findEnvFilePath()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "env_file_not_found", err.Error())
		return
	}
	if err := persistEnvValues(envPath, map[string]string{
		"HH_ACCESS_TOKEN":  token.AccessToken,
		"HH_REFRESH_TOKEN": token.RefreshToken,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "persist_tokens_failed", err.Error())
		return
	}

	h.hhConfig.AccessToken = token.AccessToken
	h.hhConfig.RefreshToken = token.RefreshToken
	_ = os.Setenv("HH_ACCESS_TOKEN", token.AccessToken)
	_ = os.Setenv("HH_REFRESH_TOKEN", token.RefreshToken)

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  token.AccessToken,
		"refresh_token": token.RefreshToken,
		"token_type":    token.TokenType,
		"expires_in":    token.ExpiresIn,
		"note":          "Saved HH_ACCESS_TOKEN and HH_REFRESH_TOKEN to .env automatically.",
		"env_path":      envPath,
	})
}

// HHListVacancies returns available vacancies from HH grouped across manager accounts.
func (h *Handler) HHListVacancies(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(h.hhConfig.AccessToken) == "" && strings.TrimSpace(h.hhConfig.RefreshToken) == "" {
		writeError(w, http.StatusBadRequest, "hh_not_configured", "configure HH_ACCESS_TOKEN or HH_REFRESH_TOKEN in .env")
		return
	}

	catalog, err := h.newHHVacancyCatalogService().List(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, "hh_list_vacancies_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, catalog)
}

// HHStartExtraction starts async extraction job for vacancy.
func (h *Handler) HHStartExtraction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		VacancyID        string `json:"vacancy_id"`
		ManagerAccountID string `json:"manager_account_id"`
		DryRun           bool   `json:"dry_run"`
		ExportPDF        bool   `json:"export_pdf"`
		SaveOriginals    bool   `json:"save_originals"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.VacancyID) == "" {
		writeError(w, http.StatusBadRequest, "missing_vacancy_id", "vacancy_id is required")
		return
	}
	if strings.TrimSpace(h.hhConfig.AccessToken) == "" && strings.TrimSpace(h.hhConfig.RefreshToken) == "" {
		writeError(w, http.StatusBadRequest, "hh_not_configured", "configure HH_ACCESS_TOKEN or HH_REFRESH_TOKEN in .env")
		return
	}

	extractionID := fmt.Sprintf("%s_%d", strings.TrimSpace(req.VacancyID), time.Now().Unix())
	outputDir := filepath.Join(h.hhConfig.OutputDir, extractionID)
	svc := h.newHHService(req.ManagerAccountID)

	go func() {
		request := model.ExtractionRequest{
			VacancyID:        strings.TrimSpace(req.VacancyID),
			ManagerAccountID: strings.TrimSpace(req.ManagerAccountID),
			DryRun:           req.DryRun,
			ExportPDF:        req.ExportPDF,
			SaveOriginals:    req.SaveOriginals,
			OutputDir:        outputDir,
		}
		if _, _, err := svc.Run(context.Background(), request); err != nil {
			h.logger.Error("hh extraction failed", "extraction_id", extractionID, "manager_account_id", strings.TrimSpace(req.ManagerAccountID), "error", err)
		}
	}()

	writeJSON(w, http.StatusAccepted, map[string]any{
		"extraction_id":      extractionID,
		"vacancy_id":         strings.TrimSpace(req.VacancyID),
		"manager_account_id": strings.TrimSpace(req.ManagerAccountID),
		"status":             "running",
		"output_dir":         outputDir,
	})
}

// HHListExtractions returns extraction directories and manifest status.
func (h *Handler) HHListExtractions(w http.ResponseWriter, r *http.Request) {
	baseDir := h.hhConfig.OutputDir
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		writeError(w, http.StatusInternalServerError, "read_dir_failed", err.Error())
		return
	}

	type extractionRow struct {
		ID         string   `json:"id"`
		VacancyID  string   `json:"vacancy_id"`
		Status     string   `json:"status"`
		CreatedAt  string   `json:"created_at"`
		Provider   string   `json:"provider"`
		TotalFound int      `json:"total_found"`
		Succeeded  int      `json:"succeeded"`
		Failed     int      `json:"failed"`
		DryRun     bool     `json:"dry_run"`
		Files      []string `json:"files"`
	}

	rows := make([]extractionRow, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		row := extractionRow{
			ID:     entry.Name(),
			Status: "running",
		}
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) > 0 {
			row.VacancyID = parts[0]
		}

		manifestPath := filepath.Join(baseDir, entry.Name(), "manifest.json")
		if data, readErr := os.ReadFile(manifestPath); readErr == nil {
			var manifest model.RunManifest
			if json.Unmarshal(data, &manifest) == nil {
				row.Status = "done"
				if manifest.DryRun {
					row.Status = "dry_run"
				}
				row.CreatedAt = manifest.CreatedAt.Format(time.RFC3339)
				row.Provider = manifest.Provider
				row.TotalFound = manifest.TotalFound
				row.Succeeded = manifest.Succeeded
				row.Failed = manifest.Failed
				row.DryRun = manifest.DryRun
			}
		}

		extractionDir := filepath.Join(baseDir, entry.Name())
		files, _ := export.CollectExtractionFiles(model.OSFileSystem{}, extractionDir)
		row.Files = files
		rows = append(rows, row)
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].ID > rows[j].ID })
	writeJSON(w, http.StatusOK, rows)
}

// HHGetExtraction returns manifest for extraction or running status.
func (h *Handler) HHGetExtraction(w http.ResponseWriter, r *http.Request) {
	extractionID := chi.URLParam(r, "extractionID")
	manifestPath := filepath.Join(h.hhConfig.OutputDir, extractionID, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			extractionDir := filepath.Join(h.hhConfig.OutputDir, extractionID)
			if _, statErr := os.Stat(extractionDir); statErr == nil {
				writeJSON(w, http.StatusOK, map[string]any{
					"extraction_id": extractionID,
					"status":        "running",
				})
				return
			}
			writeError(w, http.StatusNotFound, "not_found", "extraction not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "read_failed", err.Error())
		return
	}
	var manifest model.RunManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		writeError(w, http.StatusInternalServerError, "parse_manifest_failed", err.Error())
		return
	}
	files, _ := export.CollectExtractionFiles(model.OSFileSystem{}, filepath.Join(h.hhConfig.OutputDir, extractionID))
	writeJSON(w, http.StatusOK, map[string]any{
		"extraction_id": extractionID,
		"status":        "done",
		"files":         files,
		"manifest":      manifest,
	})
}

// HHDownloadFile serves extraction file.
func (h *Handler) HHDownloadFile(w http.ResponseWriter, r *http.Request) {
	extractionID := chi.URLParam(r, "extractionID")
	filename := chi.URLParam(r, "filename")
	if strings.Contains(filename, "..") {
		writeError(w, http.StatusBadRequest, "invalid_filename", "invalid filename")
		return
	}

	subdir := r.URL.Query().Get("subdir")
	var filePath string
	if subdir == "originals" {
		filePath = filepath.Join(h.hhConfig.OutputDir, extractionID, "originals", filename)
	} else {
		filePath = filepath.Join(h.hhConfig.OutputDir, extractionID, filename)
	}

	f, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "file_not_found", "file not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "open_failed", err.Error())
		return
	}
	defer f.Close()

	stat, _ := f.Stat()
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case ".json":
		w.Header().Set("Content-Type", "application/json")
	case ".md":
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	case ".pdf":
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	case ".rtf":
		w.Header().Set("Content-Type", "application/rtf")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	}

	http.ServeContent(w, r, filename, stat.ModTime(), f)
}
