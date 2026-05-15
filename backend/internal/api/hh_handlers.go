package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"tg-rag-ingestor/backend/internal/export"
	"tg-rag-ingestor/backend/internal/hhaccess"
	"tg-rag-ingestor/backend/internal/hhpublic"
	"tg-rag-ingestor/backend/internal/hhresumesearch"
	"tg-rag-ingestor/backend/internal/hhtokens"
	"tg-rag-ingestor/backend/internal/model"
	hhservice "tg-rag-ingestor/backend/internal/service"
)

type hhTokenPersistError struct {
	err error
}

func (e *hhTokenPersistError) Error() string {
	return e.err.Error()
}

func (e *hhTokenPersistError) Unwrap() error {
	return e.err
}

func (h *Handler) hhConfigSnapshot() model.HHConfig {
	h.hhMu.RLock()
	cfg := h.hhConfig
	h.hhMu.RUnlock()
	cfg.OnTokenRefresh = h.persistRefreshedHHTokens
	return cfg
}

func (h *Handler) saveHHTokens(accessToken, refreshToken string) (string, error) {
	envPath, err := hhtokens.PersistHHTokens(accessToken, refreshToken)
	if err != nil {
		return "", err
	}

	h.hhMu.Lock()
	if strings.TrimSpace(accessToken) != "" {
		h.hhConfig.AccessToken = strings.TrimSpace(accessToken)
	}
	if strings.TrimSpace(refreshToken) != "" {
		h.hhConfig.RefreshToken = strings.TrimSpace(refreshToken)
	}
	h.hhMu.Unlock()

	if strings.TrimSpace(accessToken) != "" {
		_ = os.Setenv("HH_ACCESS_TOKEN", strings.TrimSpace(accessToken))
	}
	if strings.TrimSpace(refreshToken) != "" {
		_ = os.Setenv("HH_REFRESH_TOKEN", strings.TrimSpace(refreshToken))
	}
	return envPath, nil
}

func (h *Handler) persistRefreshedHHTokens(ctx context.Context, token model.TokenResponse) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	envPath, err := h.saveHHTokens(token.AccessToken, token.RefreshToken)
	if err != nil {
		return err
	}
	h.logger.Info("hh refreshed oauth tokens persisted", "env_path", envPath)
	return nil
}

func hasHHToken(cfg model.HHConfig) bool {
	return strings.TrimSpace(cfg.AccessToken) != "" || strings.TrimSpace(cfg.RefreshToken) != ""
}

func (h *Handler) newHHService(managerAccountID string) *hhservice.HHExtractionService {
	cfg := h.hhConfigSnapshot()
	cfg.ManagerAccountID = strings.TrimSpace(managerAccountID)
	return hhservice.NewHHExtractionService(cfg, model.OSFileSystem{}, h.logger)
}

func (h *Handler) newHHVacancyCatalogService() *hhservice.HHVacancyCatalogService {
	return hhservice.NewHHVacancyCatalogService(h.hhConfigSnapshot(), h.logger)
}

func (h *Handler) newHHPublicVacancyService() *hhpublic.Service {
	return hhpublic.New(h.hhConfigSnapshot(), h.ingestionService, h.logger)
}

func (h *Handler) newHHAccessService() *hhaccess.Service {
	return hhaccess.New(h.hhConfigSnapshot(), h.logger)
}

func (h *Handler) newHHResumeSearchService() *hhresumesearch.Service {
	return hhresumesearch.New(h.hhConfigSnapshot(), h.ingestionService, h.logger)
}

// HHGetConfig returns current HH configuration status without secrets.
func (h *Handler) HHGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := h.hhConfigSnapshot()
	svc := h.newHHService("")
	configured := cfg.ClientID != "" && cfg.ClientSecret != "" && cfg.UserAgent != ""
	writeJSON(w, http.StatusOK, map[string]any{
		"configured":          configured,
		"has_access_token":    strings.TrimSpace(cfg.AccessToken) != "",
		"has_refresh_token":   strings.TrimSpace(cfg.RefreshToken) != "",
		"user_agent":          cfg.UserAgent,
		"output_dir":          cfg.OutputDir,
		"redirect_uri":        cfg.RedirectURI,
		"base_url":            cfg.BaseURL,
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
	token, envPath, err := h.exchangeAndSaveHHCode(r.Context(), req.Code)
	if err != nil {
		var persistErr *hhTokenPersistError
		if errors.As(err, &persistErr) {
			writeError(w, http.StatusInternalServerError, "persist_tokens_failed", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "oauth_exchange_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  token.AccessToken,
		"refresh_token": token.RefreshToken,
		"token_type":    token.TokenType,
		"expires_in":    token.ExpiresIn,
		"note":          "Saved HH_ACCESS_TOKEN and HH_REFRESH_TOKEN to .env automatically.",
		"env_path":      envPath,
	})
}

func (h *Handler) HHOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if oauthErr := strings.TrimSpace(r.URL.Query().Get("error")); oauthErr != "" {
		description := strings.TrimSpace(r.URL.Query().Get("error_description"))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, "<!doctype html><title>HH OAuth failed</title><h1>HH OAuth failed</h1><p>%s</p><p>%s</p>",
			html.EscapeString(oauthErr),
			html.EscapeString(description),
		)
		return
	}

	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		writeError(w, http.StatusBadRequest, "missing_code", "code query parameter is required")
		return
	}

	_, envPath, err := h.exchangeAndSaveHHCode(r.Context(), code)
	if err != nil {
		status := http.StatusBadRequest
		var persistErr *hhTokenPersistError
		if errors.As(err, &persistErr) {
			status = http.StatusInternalServerError
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w, "<!doctype html><title>HH OAuth failed</title><h1>HH OAuth failed</h1><p>%s</p>",
			html.EscapeString(err.Error()),
		)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, "<!doctype html><title>HH OAuth saved</title><h1>HH OAuth tokens saved</h1><p>Tokens were saved to %s. You can close this tab and return to the app.</p>",
		html.EscapeString(envPath),
	)
}

func (h *Handler) exchangeAndSaveHHCode(ctx context.Context, code string) (*model.TokenResponse, string, error) {
	svc := h.newHHService("")
	token, err := svc.ExchangeCode(ctx, code)
	if err != nil {
		return nil, "", err
	}
	envPath, err := h.saveHHTokens(token.AccessToken, token.RefreshToken)
	if err != nil {
		return nil, "", &hhTokenPersistError{err: err}
	}
	return token, envPath, nil
}

// HHListVacancies returns available vacancies from HH grouped across manager accounts.
func (h *Handler) HHListVacancies(w http.ResponseWriter, r *http.Request) {
	cfg := h.hhConfigSnapshot()
	if !hasHHToken(cfg) {
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

// HHGetAccessStatus returns current employer HH paid-access status, method groups, and resume limits.
func (h *Handler) HHGetAccessStatus(w http.ResponseWriter, r *http.Request) {
	cfg := h.hhConfigSnapshot()
	if !hasHHToken(cfg) {
		writeError(w, http.StatusBadRequest, "hh_not_configured", "configure HH_ACCESS_TOKEN or HH_REFRESH_TOKEN in .env")
		return
	}

	result, err := h.newHHAccessService().GetStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, "hh_access_status_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// HHGetPayableActions returns active paid HH API services for the current employer.
func (h *Handler) HHGetPayableActions(w http.ResponseWriter, r *http.Request) {
	cfg := h.hhConfigSnapshot()
	if !hasHHToken(cfg) {
		writeError(w, http.StatusBadRequest, "hh_not_configured", "configure HH_ACCESS_TOKEN or HH_REFRESH_TOKEN in .env")
		return
	}

	accessSvc := h.newHHAccessService()
	user, err := accessSvc.GetCurrentUser(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, "hh_current_user_failed", err.Error())
		return
	}
	if user.Employer == nil || strings.TrimSpace(user.Employer.ID) == "" {
		writeError(w, http.StatusBadRequest, "hh_employer_context_missing", "current hh token has no employer context")
		return
	}
	result, err := accessSvc.GetPayableActions(r.Context(), user.Employer.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "hh_payable_actions_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// HHGetMethodAccess returns paid-method access groups for the current employer manager.
func (h *Handler) HHGetMethodAccess(w http.ResponseWriter, r *http.Request) {
	cfg := h.hhConfigSnapshot()
	if !hasHHToken(cfg) {
		writeError(w, http.StatusBadRequest, "hh_not_configured", "configure HH_ACCESS_TOKEN or HH_REFRESH_TOKEN in .env")
		return
	}

	accessSvc := h.newHHAccessService()
	user, err := accessSvc.GetCurrentUser(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, "hh_current_user_failed", err.Error())
		return
	}
	if user.Employer == nil || strings.TrimSpace(user.Employer.ID) == "" || user.Manager == nil || strings.TrimSpace(user.Manager.ID) == "" {
		writeError(w, http.StatusBadRequest, "hh_manager_context_missing", "current hh token has no employer/manager context")
		return
	}
	result, err := accessSvc.GetMethodAccess(r.Context(), user.Employer.ID, user.Manager.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "hh_method_access_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// HHGetResumeLimits returns the current manager resume-view daily limits.
func (h *Handler) HHGetResumeLimits(w http.ResponseWriter, r *http.Request) {
	cfg := h.hhConfigSnapshot()
	if !hasHHToken(cfg) {
		writeError(w, http.StatusBadRequest, "hh_not_configured", "configure HH_ACCESS_TOKEN or HH_REFRESH_TOKEN in .env")
		return
	}

	accessSvc := h.newHHAccessService()
	user, err := accessSvc.GetCurrentUser(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, "hh_current_user_failed", err.Error())
		return
	}
	if user.Employer == nil || strings.TrimSpace(user.Employer.ID) == "" || user.Manager == nil || strings.TrimSpace(user.Manager.ID) == "" {
		writeError(w, http.StatusBadRequest, "hh_manager_context_missing", "current hh token has no employer/manager context")
		return
	}
	result, err := accessSvc.GetResumeLimits(r.Context(), user.Employer.ID, user.Manager.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "hh_resume_limits_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// HHImportPublicVacancies imports public HH vacancies into the generic JSON ingestion flow.
func (h *Handler) HHImportPublicVacancies(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text             string `json:"text"`
		Area             string `json:"area"`
		ProfessionalRole string `json:"professional_role"`
		DateFrom         string `json:"date_from"`
		DateTo           string `json:"date_to"`
		MaxItems         int    `json:"max_items"`
		SourceName       string `json:"source_name"`
		Title            string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	result, err := h.newHHPublicVacancyService().Import(r.Context(), hhpublic.ImportRequest{
		Text:             req.Text,
		Area:             req.Area,
		ProfessionalRole: req.ProfessionalRole,
		DateFrom:         req.DateFrom,
		DateTo:           req.DateTo,
		MaxItems:         req.MaxItems,
		SourceName:       req.SourceName,
		Title:            req.Title,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "hh_public_import_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

// HHImportGlobalResumes imports globally searched HH resumes into the generic JSON ingestion flow.
func (h *Handler) HHImportGlobalResumes(w http.ResponseWriter, r *http.Request) {
	cfg := h.hhConfigSnapshot()
	if !hasHHToken(cfg) {
		writeError(w, http.StatusBadRequest, "hh_not_configured", "configure HH_ACCESS_TOKEN or HH_REFRESH_TOKEN in .env")
		return
	}

	var req struct {
		Text             string `json:"text"`
		Area             string `json:"area"`
		ProfessionalRole string `json:"professional_role"`
		MaxItems         int    `json:"max_items"`
		SourceName       string `json:"source_name"`
		Title            string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	result, err := h.newHHResumeSearchService().Import(r.Context(), hhresumesearch.ImportRequest{
		Text:             req.Text,
		Area:             req.Area,
		ProfessionalRole: req.ProfessionalRole,
		MaxItems:         req.MaxItems,
		SourceName:       req.SourceName,
		Title:            req.Title,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "hh_resume_search_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

// HHStartExtraction starts async extraction job for vacancy.
func (h *Handler) HHStartExtraction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		VacancyID        string `json:"vacancy_id"`
		ManagerAccountID string `json:"manager_account_id"`
		DryRun           bool   `json:"dry_run"`
		ExportPDF        bool   `json:"export_pdf"`
		SaveOriginals    *bool  `json:"save_originals"`
		CoverLetterOnly  bool   `json:"cover_letter_only"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.VacancyID) == "" {
		writeError(w, http.StatusBadRequest, "missing_vacancy_id", "vacancy_id is required")
		return
	}
	cfg := h.hhConfigSnapshot()
	if !hasHHToken(cfg) {
		writeError(w, http.StatusBadRequest, "hh_not_configured", "configure HH_ACCESS_TOKEN or HH_REFRESH_TOKEN in .env")
		return
	}

	extractionID := fmt.Sprintf("%s_%d", strings.TrimSpace(req.VacancyID), time.Now().Unix())
	outputDir := filepath.Join(cfg.OutputDir, extractionID)
	svc := h.newHHService(req.ManagerAccountID)
	saveOriginals := resolveOptionalBool(req.SaveOriginals, cfg.SaveOriginalsDefault)

	go func() {
		request := model.ExtractionRequest{
			VacancyID:        strings.TrimSpace(req.VacancyID),
			ManagerAccountID: strings.TrimSpace(req.ManagerAccountID),
			DryRun:           req.DryRun,
			ExportPDF:        req.ExportPDF,
			SaveOriginals:    saveOriginals,
			CoverLetterOnly:  req.CoverLetterOnly,
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
	cfg := h.hhConfigSnapshot()
	baseDir := cfg.OutputDir
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
	cfg := h.hhConfigSnapshot()
	extractionID := chi.URLParam(r, "extractionID")
	manifestPath := filepath.Join(cfg.OutputDir, extractionID, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			extractionDir := filepath.Join(cfg.OutputDir, extractionID)
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
	files, _ := export.CollectExtractionFiles(model.OSFileSystem{}, filepath.Join(cfg.OutputDir, extractionID))
	writeJSON(w, http.StatusOK, map[string]any{
		"extraction_id": extractionID,
		"status":        "done",
		"files":         files,
		"manifest":      manifest,
	})
}

// HHDownloadFile serves extraction file.
func (h *Handler) HHDownloadFile(w http.ResponseWriter, r *http.Request) {
	cfg := h.hhConfigSnapshot()
	extractionID := chi.URLParam(r, "extractionID")
	filename := chi.URLParam(r, "filename")
	if strings.Contains(filename, "..") {
		writeError(w, http.StatusBadRequest, "invalid_filename", "invalid filename")
		return
	}

	subdir := r.URL.Query().Get("subdir")
	var filePath string
	if subdir == "originals" {
		filePath = filepath.Join(cfg.OutputDir, extractionID, "originals", filename)
	} else {
		filePath = filepath.Join(cfg.OutputDir, extractionID, filename)
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

func resolveOptionalBool(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
