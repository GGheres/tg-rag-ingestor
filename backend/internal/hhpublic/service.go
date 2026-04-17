package hhpublic

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"tg-rag-ingestor/backend/internal/auth"
	"tg-rag-ingestor/backend/internal/hhclient"
	"tg-rag-ingestor/backend/internal/ingestion"
	"tg-rag-ingestor/backend/internal/model"
	"tg-rag-ingestor/backend/internal/models"
)

const (
	maxSearchResultsPerWindow = 2000
	searchPerPage             = 100
	defaultMaxImportItems     = 500
	maxImportItemsCap         = 5000
	minWindowSize             = time.Second
)

type Service struct {
	client    *hhclient.Client
	ingestion *ingestion.Service
	logger    *slog.Logger
}

type ImportRequest struct {
	Text             string
	Area             string
	ProfessionalRole string
	DateFrom         string
	DateTo           string
	MaxItems         int
	SourceName       string
	Title            string
}

type FetchStats struct {
	WindowsVisited   int  `json:"windows_visited"`
	RequestsMade     int  `json:"requests_made"`
	VacanciesFetched int  `json:"vacancies_fetched"`
	TruncatedWindows int  `json:"truncated_windows"`
	ReachedMaxItems  bool `json:"reached_max_items"`
}

type ImportResult struct {
	Source          models.Source           `json:"source"`
	ImportedCount   int                     `json:"imported_count"`
	ProcessedCount  int                     `json:"processed_count"`
	DuplicateCount  int                     `json:"duplicate_count"`
	TrashCount      int                     `json:"trash_count"`
	ChunkCount      int                     `json:"chunk_count"`
	FetchStats      FetchStats              `json:"fetch_stats"`
	GeneratedTitle  string                  `json:"generated_title"`
	GeneratedSource string                  `json:"generated_source_name"`
	SearchURL       string                  `json:"search_url"`
	SearchWindow    map[string]string       `json:"search_window"`
	Filters         map[string]string       `json:"filters"`
	RawPayloadSize  int                     `json:"raw_payload_size"`
	ImportMetadata  map[string]interface{}  `json:"import_metadata"`
}

type vacancySearchPage struct {
	Items        []map[string]any `json:"items"`
	Found        int              `json:"found"`
	Pages        int              `json:"pages"`
	Page         int              `json:"page"`
	PerPage      int              `json:"per_page"`
	AlternateURL string           `json:"alternate_url"`
}

type searchFilters struct {
	Text             string
	Area             string
	ProfessionalRole string
}

type searchWindow struct {
	From time.Time
	To   time.Time
}

type collectState struct {
	ctx        context.Context
	filters    searchFilters
	maxItems   int
	stats      FetchStats
	seen       map[string]struct{}
	items      []map[string]any
	searchURL  string
}

func New(cfg model.HHConfig, ingestionService *ingestion.Service, logger *slog.Logger) *Service {
	cfg = cfg.WithDefaults()
	// Public vacancy search does not need employer OAuth. Keep requests anonymous
	// so stale tokens do not affect public-search availability.
	cfg.AccessToken = ""
	cfg.RefreshToken = ""
	cfg.ManagerAccountID = ""

	authManager := auth.NewManager(cfg, logger)
	return &Service{
		client:    hhclient.New(cfg, authManager, logger),
		ingestion: ingestionService,
		logger:    logger,
	}
}

func (s *Service) Import(ctx context.Context, req ImportRequest) (*ImportResult, error) {
	if s.ingestion == nil {
		return nil, fmt.Errorf("ingestion service is not configured")
	}

	filters := searchFilters{
		Text:             strings.TrimSpace(req.Text),
		Area:             strings.TrimSpace(req.Area),
		ProfessionalRole: strings.TrimSpace(req.ProfessionalRole),
	}
	window, err := normalizeWindow(req.DateFrom, req.DateTo)
	if err != nil {
		return nil, err
	}

	maxItems := req.MaxItems
	if maxItems <= 0 {
		maxItems = defaultMaxImportItems
	}
	if maxItems > maxImportItemsCap {
		maxItems = maxImportItemsCap
	}

	state := &collectState{
		ctx:      ctx,
		filters:  filters,
		maxItems: maxItems,
		seen:     make(map[string]struct{}),
		items:    make([]map[string]any, 0, maxItems),
	}
	if err := s.collectWindow(state, window); err != nil {
		return nil, err
	}
	if len(state.items) == 0 {
		return nil, fmt.Errorf("no public vacancies matched the selected filters")
	}

	payload, err := json.Marshal(state.items)
	if err != nil {
		return nil, fmt.Errorf("marshal import payload: %w", err)
	}

	title := buildTitle(req.Title, filters, window)
	sourceName := buildSourceName(req.SourceName, filters)
	importRes, err := s.ingestion.ImportJSON(ctx, ingestion.ImportJSONInput{
		Filename:   "hh_public_vacancies.json",
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
		FetchStats:      state.stats,
		GeneratedTitle:  title,
		GeneratedSource: sourceName,
		SearchURL:       state.searchURL,
		SearchWindow: map[string]string{
			"date_from": formatHHTime(window.From),
			"date_to":   formatHHTime(window.To),
		},
		Filters: map[string]string{
			"text":              filters.Text,
			"area":              filters.Area,
			"professional_role": filters.ProfessionalRole,
		},
		RawPayloadSize: len(payload),
		ImportMetadata: map[string]interface{}{
			"max_items": maxItems,
		},
	}, nil
}

func (s *Service) collectWindow(state *collectState, window searchWindow) error {
	if len(state.items) >= state.maxItems {
		state.stats.ReachedMaxItems = true
		return nil
	}

	probe, err := s.fetchPage(state.ctx, state.filters, window, 1, 0)
	if err != nil {
		return err
	}
	state.stats.RequestsMade++
	state.stats.WindowsVisited++
	if probe.AlternateURL != "" && state.searchURL == "" {
		state.searchURL = probe.AlternateURL
	}
	if probe.Found == 0 {
		return nil
	}

	remaining := state.maxItems - len(state.items)
	if probe.Found <= maxSearchResultsPerWindow || window.To.Sub(window.From) <= minWindowSize {
		if probe.Found > maxSearchResultsPerWindow {
			state.stats.TruncatedWindows++
			s.logger.Warn("hh public vacancies window exceeded hard limit",
				"date_from", formatHHTime(window.From),
				"date_to", formatHHTime(window.To),
				"found", probe.Found,
			)
		}
		return s.fetchWindowItems(state, window, minInt(remaining, minInt(probe.Found, maxSearchResultsPerWindow)))
	}

	left, right, ok := splitWindow(window)
	if !ok {
		state.stats.TruncatedWindows++
		return s.fetchWindowItems(state, window, minInt(remaining, maxSearchResultsPerWindow))
	}

	// Walk newer half first to keep import focused on the latest vacancies when max_items is small.
	if err := s.collectWindow(state, right); err != nil {
		return err
	}
	if len(state.items) >= state.maxItems {
		state.stats.ReachedMaxItems = true
		return nil
	}
	return s.collectWindow(state, left)
}

func (s *Service) fetchWindowItems(state *collectState, window searchWindow, limit int) error {
	if limit <= 0 {
		return nil
	}

	pages := (limit + searchPerPage - 1) / searchPerPage
	if pages > maxSearchResultsPerWindow/searchPerPage {
		pages = maxSearchResultsPerWindow / searchPerPage
	}

	for page := 0; page < pages; page++ {
		if len(state.items) >= state.maxItems {
			state.stats.ReachedMaxItems = true
			return nil
		}

		resp, err := s.fetchPage(state.ctx, state.filters, window, searchPerPage, page)
		if err != nil {
			return err
		}
		state.stats.RequestsMade++
		if resp.AlternateURL != "" && state.searchURL == "" {
			state.searchURL = resp.AlternateURL
		}

		for _, item := range resp.Items {
			if len(state.items) >= state.maxItems {
				state.stats.ReachedMaxItems = true
				return nil
			}
			id := vacancyID(item)
			if id == "" {
				continue
			}
			if _, exists := state.seen[id]; exists {
				continue
			}
			state.seen[id] = struct{}{}
			state.items = append(state.items, buildImportItem(item))
			state.stats.VacanciesFetched++
		}

		if len(resp.Items) < searchPerPage {
			return nil
		}
	}
	return nil
}

func (s *Service) fetchPage(ctx context.Context, filters searchFilters, window searchWindow, perPage, page int) (*vacancySearchPage, error) {
	query := url.Values{}
	if filters.Text != "" {
		query.Set("text", filters.Text)
	}
	if filters.Area != "" {
		query.Set("area", filters.Area)
	}
	if filters.ProfessionalRole != "" {
		query.Set("professional_role", filters.ProfessionalRole)
	}
	query.Set("date_from", formatHHTime(window.From))
	query.Set("date_to", formatHHTime(window.To))
	query.Set("order_by", "publication_time")
	query.Set("per_page", strconv.Itoa(perPage))
	query.Set("page", strconv.Itoa(page))

	var out vacancySearchPage
	status, err := s.client.GetJSON(ctx, "/vacancies", query, &out)
	if err != nil {
		return nil, fmt.Errorf("fetch public vacancies page %d failed: status=%d err=%w", page, status, err)
	}
	return &out, nil
}

func buildImportItem(item map[string]any) map[string]any {
	out := cloneMap(item)
	if out == nil {
		out = map[string]any{}
	}

	title := strings.TrimSpace(stringValue(item["name"]))
	out["text"] = buildVacancyText(item)
	out["caption"] = title
	out["source_url"] = firstNonEmpty(
		stringValue(item["alternate_url"]),
		stringValue(item["url"]),
	)
	out["provider"] = "hh_public_vacancies"
	out["imported_at"] = time.Now().UTC().Format(time.RFC3339)
	return out
}

func buildVacancyText(item map[string]any) string {
	lines := make([]string, 0, 16)
	appendLine := func(label, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		lines = append(lines, fmt.Sprintf("%s: %s", label, value))
	}

	appendLine("VACANCY", stringValue(item["name"]))
	appendLine("EMPLOYER", nestedString(item, "employer", "name"))
	appendLine("AREA", nestedString(item, "area", "name"))
	appendLine("PROFESSIONAL_ROLES", joinNamedList(item["professional_roles"]))
	appendLine("EXPERIENCE", nestedString(item, "experience", "name"))
	appendLine("EMPLOYMENT_FORM", nestedString(item, "employment_form", "name"))
	appendLine("TYPE", nestedString(item, "type", "name"))
	appendLine("SALARY", salaryText(item["salary"]))
	appendLine("ADDRESS", addressText(item["address"]))
	appendLine("PUBLISHED_AT", stringValue(item["published_at"]))
	appendLine("ALTERNATE_URL", firstNonEmpty(stringValue(item["alternate_url"]), stringValue(item["url"])))
	appendLine("REQUIREMENTS", nestedString(item, "snippet", "requirement"))
	appendLine("RESPONSIBILITIES", nestedString(item, "snippet", "responsibility"))
	return strings.Join(lines, "\n")
}

func buildTitle(rawTitle string, filters searchFilters, window searchWindow) string {
	if trimmed := strings.TrimSpace(rawTitle); trimmed != "" {
		return trimmed
	}
	parts := []string{"HH Public Vacancies"}
	if filters.Text != "" {
		parts = append(parts, filters.Text)
	}
	if filters.Area != "" {
		parts = append(parts, "area "+filters.Area)
	}
	if filters.ProfessionalRole != "" {
		parts = append(parts, "role "+filters.ProfessionalRole)
	}
	parts = append(parts, window.From.Format("2006-01-02")+".."+window.To.Format("2006-01-02"))
	return strings.Join(parts, " | ")
}

func buildSourceName(rawName string, filters searchFilters) string {
	if trimmed := strings.TrimSpace(rawName); trimmed != "" {
		return trimmed
	}
	base := "hh_public_vacancies"
	if filters.Text != "" {
		return base + "_" + filters.Text
	}
	return base
}

func normalizeWindow(rawFrom, rawTo string) (searchWindow, error) {
	now := time.Now()
	var from time.Time
	var to time.Time
	var err error

	if strings.TrimSpace(rawFrom) == "" && strings.TrimSpace(rawTo) == "" {
		to = endOfDay(now)
		from = startOfDay(now)
	} else {
		if strings.TrimSpace(rawFrom) != "" {
			from, err = parseDateBoundary(rawFrom, false)
			if err != nil {
				return searchWindow{}, fmt.Errorf("invalid date_from: %w", err)
			}
		}
		if strings.TrimSpace(rawTo) != "" {
			to, err = parseDateBoundary(rawTo, true)
			if err != nil {
				return searchWindow{}, fmt.Errorf("invalid date_to: %w", err)
			}
		}
		if from.IsZero() {
			from = startOfDay(to)
		}
		if to.IsZero() {
			to = endOfDay(from)
		}
	}

	if from.After(to) {
		return searchWindow{}, fmt.Errorf("date_from must be before date_to")
	}
	return searchWindow{From: from, To: to}, nil
}

func parseDateBoundary(raw string, inclusiveEnd bool) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02",
	}
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, raw)
		if err != nil {
			continue
		}
		if layout == "2006-01-02" {
			if inclusiveEnd {
				return endOfDay(parsed), nil
			}
			return startOfDay(parsed), nil
		}
		return parsed, nil
	}
	return time.Time{}, fmt.Errorf("expected RFC3339 or YYYY-MM-DD")
}

func startOfDay(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
}

func endOfDay(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 23, 59, 59, 0, value.Location())
}

func splitWindow(window searchWindow) (searchWindow, searchWindow, bool) {
	if window.To.Sub(window.From) <= minWindowSize {
		return searchWindow{}, searchWindow{}, false
	}
	mid := window.From.Add(window.To.Sub(window.From) / 2).Truncate(time.Second)
	if !mid.After(window.From) {
		mid = window.From.Add(time.Second)
	}
	if !mid.Before(window.To) {
		return searchWindow{}, searchWindow{}, false
	}
	left := searchWindow{From: window.From, To: mid}
	rightStart := mid.Add(time.Second)
	if rightStart.After(window.To) {
		return searchWindow{}, searchWindow{}, false
	}
	right := searchWindow{From: rightStart, To: window.To}
	return left, right, true
}

func formatHHTime(value time.Time) string {
	return value.Format("2006-01-02T15:04:05-0700")
}

func vacancyID(item map[string]any) string {
	return strings.TrimSpace(stringValue(item["id"]))
}

func nestedString(item map[string]any, keys ...string) string {
	if len(keys) == 0 {
		return ""
	}
	var current any = item
	for _, key := range keys {
		mapped, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = mapped[key]
	}
	return strings.TrimSpace(stringValue(current))
}

func joinNamedList(raw any) string {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		part := strings.TrimSpace(stringValueFromMap(item, "name"))
		if part == "" {
			continue
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ", ")
}

func salaryText(raw any) string {
	mapped, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	from := strings.TrimSpace(stringValue(mapped["from"]))
	to := strings.TrimSpace(stringValue(mapped["to"]))
	currency := strings.TrimSpace(stringValue(mapped["currency"]))
	gross := strings.TrimSpace(stringValue(mapped["gross"]))
	switch {
	case from != "" && to != "":
		return strings.TrimSpace(from + " - " + to + " " + currency + " gross=" + gross)
	case from != "":
		return strings.TrimSpace("from " + from + " " + currency + " gross=" + gross)
	case to != "":
		return strings.TrimSpace("to " + to + " " + currency + " gross=" + gross)
	default:
		return ""
	}
}

func addressText(raw any) string {
	mapped, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	return firstNonEmpty(
		stringValue(mapped["raw"]),
		strings.TrimSpace(strings.Join([]string{
			stringValue(mapped["city"]),
			stringValue(mapped["street"]),
			stringValue(mapped["building"]),
		}, ", ")),
	)
}

func stringValueFromMap(raw any, key string) string {
	mapped, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	return stringValue(mapped[key])
}

func stringValue(raw any) string {
	switch value := raw.(type) {
	case string:
		return value
	case float64:
		return strconv.FormatInt(int64(value), 10)
	case int:
		return strconv.Itoa(value)
	case int64:
		return strconv.FormatInt(value, 10)
	case bool:
		if value {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
