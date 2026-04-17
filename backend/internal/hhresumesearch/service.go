package hhresumesearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/sync/errgroup"

	"tg-rag-ingestor/backend/internal/auth"
	"tg-rag-ingestor/backend/internal/hhclient"
	"tg-rag-ingestor/backend/internal/ingestion"
	"tg-rag-ingestor/backend/internal/model"
	"tg-rag-ingestor/backend/internal/models"
	"tg-rag-ingestor/backend/internal/resumes"
)

const (
	searchPerPage         = 20
	defaultMaxImportItems = 100
	maxImportItemsCap     = 500
)

type Service struct {
	client      *hhclient.Client
	resumes     *resumes.Service
	ingestion   *ingestion.Service
	logger      *slog.Logger
	concurrency int
}

type ImportRequest struct {
	Text             string
	Area             string
	ProfessionalRole string
	MaxItems         int
	SourceName       string
	Title            string
}

type FetchStats struct {
	PagesVisited    int  `json:"pages_visited"`
	RequestsMade    int  `json:"requests_made"`
	ResumesFound    int  `json:"resumes_found"`
	ResumesFetched  int  `json:"resumes_fetched"`
	ReachedMaxItems bool `json:"reached_max_items"`
}

type ImportResult struct {
	Source          models.Source          `json:"source"`
	ImportedCount   int                    `json:"imported_count"`
	ProcessedCount  int                    `json:"processed_count"`
	DuplicateCount  int                    `json:"duplicate_count"`
	TrashCount      int                    `json:"trash_count"`
	ChunkCount      int                    `json:"chunk_count"`
	FetchStats      FetchStats             `json:"fetch_stats"`
	GeneratedTitle  string                 `json:"generated_title"`
	GeneratedSource string                 `json:"generated_source_name"`
	SearchURL       string                 `json:"search_url"`
	Filters         map[string]string      `json:"filters"`
	RawPayloadSize  int                    `json:"raw_payload_size"`
	ImportMetadata  map[string]interface{} `json:"import_metadata"`
}

type resumeSearchPage struct {
	Items        []resumeSearchItem `json:"items"`
	Found        int                `json:"found"`
	Pages        int                `json:"pages"`
	Page         int                `json:"page"`
	PerPage      int                `json:"per_page"`
	AlternateURL string             `json:"alternate_url"`
}

type resumeSearchItem struct {
	ID              string          `json:"id"`
	Title           string          `json:"title"`
	URL             string          `json:"url"`
	AlternateURL    string          `json:"alternate_url"`
	FirstName       string          `json:"first_name"`
	LastName        string          `json:"last_name"`
	MiddleName      string          `json:"middle_name"`
	UpdatedAt       string          `json:"updated_at"`
	CreatedAt       string          `json:"created_at"`
	Area            *model.NamedRef `json:"area"`
	CanViewFullInfo bool            `json:"can_view_full_info"`
}

func New(cfg model.HHConfig, ingestionService *ingestion.Service, logger *slog.Logger) *Service {
	cfg = cfg.WithDefaults()
	authManager := auth.NewManager(cfg, logger)
	client := hhclient.New(cfg, authManager, logger)
	return &Service{
		client:      client,
		resumes:     resumes.New(client, logger),
		ingestion:   ingestionService,
		logger:      logger,
		concurrency: maxInt(1, cfg.Concurrency),
	}
}

func (s *Service) Import(ctx context.Context, req ImportRequest) (*ImportResult, error) {
	if s.ingestion == nil {
		return nil, fmt.Errorf("ingestion service is not configured")
	}

	text := strings.TrimSpace(req.Text)
	if text == "" {
		return nil, errors.New("resume search query is required")
	}

	maxItems := req.MaxItems
	if maxItems <= 0 {
		maxItems = defaultMaxImportItems
	}
	if maxItems > maxImportItemsCap {
		maxItems = maxImportItemsCap
	}

	page0, err := s.fetchPage(ctx, resumeSearchFilters{
		Text:             text,
		Area:             strings.TrimSpace(req.Area),
		ProfessionalRole: strings.TrimSpace(req.ProfessionalRole),
	}, 0)
	if err != nil {
		return nil, err
	}

	stats := FetchStats{
		PagesVisited: 1,
		RequestsMade: 1,
		ResumesFound: page0.Found,
	}
	searchURL := strings.TrimSpace(page0.AlternateURL)

	collected := make([]resumeSearchItem, 0, minInt(maxItems, len(page0.Items)))
	seen := make(map[string]struct{}, maxItems)
	appendItems := func(items []resumeSearchItem) {
		for _, item := range items {
			if len(collected) >= maxItems {
				stats.ReachedMaxItems = true
				return
			}
			id := strings.TrimSpace(item.ID)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			collected = append(collected, item)
		}
	}

	appendItems(page0.Items)
	totalPages := page0.Pages
	if totalPages <= 0 {
		totalPages = 1
	}
	for page := 1; page < totalPages && len(collected) < maxItems; page++ {
		nextPage, err := s.fetchPage(ctx, resumeSearchFilters{
			Text:             text,
			Area:             strings.TrimSpace(req.Area),
			ProfessionalRole: strings.TrimSpace(req.ProfessionalRole),
		}, page)
		if err != nil {
			return nil, err
		}
		stats.PagesVisited++
		stats.RequestsMade++
		if searchURL == "" && nextPage.AlternateURL != "" {
			searchURL = strings.TrimSpace(nextPage.AlternateURL)
		}
		appendItems(nextPage.Items)
	}

	if len(collected) == 0 {
		return nil, fmt.Errorf("no resumes matched the selected filters")
	}

	fullResumes, fetchErr := s.fetchFullResumes(ctx, collected)
	if fetchErr != nil {
		return nil, fetchErr
	}
	stats.ResumesFetched = len(fullResumes)
	if len(fullResumes) == 0 {
		return nil, fmt.Errorf("resume search returned items but full resume fetch produced no results")
	}

	payload, err := json.Marshal(fullResumes)
	if err != nil {
		return nil, fmt.Errorf("marshal import payload: %w", err)
	}

	title := buildTitle(req.Title, text)
	sourceName := buildSourceName(req.SourceName, text)
	importRes, err := s.ingestion.ImportJSON(ctx, ingestion.ImportJSONInput{
		Filename:   "hh_global_resumes.json",
		SourceName: sourceName,
		Title:      title,
		Payload:    payload,
	})
	if err != nil {
		return nil, err
	}

	return &ImportResult{
		Source:          importRes.Source,
		ImportedCount:   importRes.ImportedCount,
		ProcessedCount:  importRes.ProcessedCount,
		DuplicateCount:  importRes.DuplicateCount,
		TrashCount:      importRes.TrashCount,
		ChunkCount:      importRes.ChunkCount,
		FetchStats:      stats,
		GeneratedTitle:  title,
		GeneratedSource: sourceName,
		SearchURL:       searchURL,
		Filters: map[string]string{
			"text":              text,
			"area":              strings.TrimSpace(req.Area),
			"professional_role": strings.TrimSpace(req.ProfessionalRole),
		},
		RawPayloadSize: len(payload),
		ImportMetadata: map[string]interface{}{
			"max_items": maxItems,
		},
	}, nil
}

type resumeSearchFilters struct {
	Text             string
	Area             string
	ProfessionalRole string
}

func (s *Service) fetchPage(ctx context.Context, filters resumeSearchFilters, page int) (*resumeSearchPage, error) {
	query := url.Values{}
	query.Set("text", filters.Text)
	query.Set("per_page", strconv.Itoa(searchPerPage))
	query.Set("page", strconv.Itoa(page))
	if filters.Area != "" {
		query.Set("area", filters.Area)
	}
	if filters.ProfessionalRole != "" {
		query.Set("professional_role", filters.ProfessionalRole)
	}

	var out resumeSearchPage
	status, err := s.client.GetJSON(ctx, "/resumes", query, &out)
	if err != nil {
		return nil, wrapSearchError(status, err)
	}
	return &out, nil
}

func (s *Service) fetchFullResumes(ctx context.Context, items []resumeSearchItem) ([]model.Resume, error) {
	results := make([]*model.Resume, len(items))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(s.concurrency)

	for idx := range items {
		idx := idx
		group.Go(func() error {
			candidate := model.NegotiationCandidate{
				ResumeID:     strings.TrimSpace(items[idx].ID),
				ResumeAPIURL: strings.TrimSpace(items[idx].URL),
			}
			item, err := s.resumes.FetchFull(groupCtx, candidate)
			if err != nil {
				return err
			}
			results[idx] = item
			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return nil, wrapSearchError(0, err)
	}

	out := make([]model.Resume, 0, len(results))
	for _, item := range results {
		if item == nil {
			continue
		}
		out = append(out, *item)
	}
	return out, nil
}

func wrapSearchError(status int, err error) error {
	if err == nil {
		return nil
	}
	var apiErr *hhclient.APIError
	if errors.As(err, &apiErr) {
		if apiErr.Type == "api_access_payment" || apiErr.Value == "action_must_be_payed" {
			return fmt.Errorf("global resume search requires paid HH employer access: %w", err)
		}
		if apiErr.Type == "forbidden" {
			return fmt.Errorf("HH denied access to resume search for the current employer token: %w", err)
		}
	}
	if status > 0 {
		return fmt.Errorf("hh resume search failed: status=%d err=%w", status, err)
	}
	return fmt.Errorf("hh resume search failed: %w", err)
}

func buildTitle(rawTitle, text string) string {
	if strings.TrimSpace(rawTitle) != "" {
		return strings.TrimSpace(rawTitle)
	}
	return "HH global resumes: " + text
}

func buildSourceName(rawName, text string) string {
	if strings.TrimSpace(rawName) != "" {
		return strings.TrimSpace(rawName)
	}
	base := strings.ToLower(strings.TrimSpace(text))
	base = strings.ReplaceAll(base, " ", "_")
	base = strings.ReplaceAll(base, "/", "_")
	base = strings.ReplaceAll(base, "\\", "_")
	if base == "" {
		base = "global"
	}
	return "hh_global_resumes_" + base
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
