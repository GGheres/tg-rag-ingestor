package ingestion

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tg-rag-ingestor/backend/internal/models"
	"tg-rag-ingestor/backend/internal/processing"
	"tg-rag-ingestor/backend/internal/storage"
	"tg-rag-ingestor/backend/internal/telegram"
)

type Service struct {
	repo         *storage.Repository
	collector    telegram.Collector
	processor    *processing.Service
	defaultBatch int
}

func NewService(repo *storage.Repository, collector telegram.Collector, processor *processing.Service, defaultBatch int) *Service {
	if defaultBatch <= 0 {
		defaultBatch = 100
	}
	return &Service{
		repo:         repo,
		collector:    collector,
		processor:    processor,
		defaultBatch: defaultBatch,
	}
}

type SyncResult struct {
	Job             models.Job `json:"job"`
	PagesFetched    int        `json:"pages_fetched"`
	MessagesFetched int        `json:"messages_fetched"`
	ProcessedCount  int        `json:"processed_count"`
	DuplicateCount  int        `json:"duplicate_count"`
	TrashCount      int        `json:"trash_count"`
	ChunkCount      int        `json:"chunk_count"`
}

type SyncOptions struct {
	BatchSize   int
	MaxMessages int
	FullResync  bool
}

type ImportJSONInput struct {
	Filename   string
	SourceName string
	Title      string
	Payload    []byte
}

type ImportJSONResult struct {
	Source         models.Source `json:"source"`
	ImportedCount  int           `json:"imported_count"`
	ProcessedCount int           `json:"processed_count"`
	DuplicateCount int           `json:"duplicate_count"`
	TrashCount     int           `json:"trash_count"`
	ChunkCount     int           `json:"chunk_count"`
}

func (s *Service) SyncSource(ctx context.Context, sourceID string, options SyncOptions) (SyncResult, error) {
	source, err := s.repo.GetSource(ctx, sourceID)
	if err != nil {
		return SyncResult{}, err
	}
	if source.SourceType != "telegram_public_channel" {
		return SyncResult{}, fmt.Errorf("sync is supported only for telegram_public_channel sources")
	}

	batchSize := options.BatchSize
	if batchSize <= 0 {
		batchSize = s.defaultBatch
	}
	maxMessages := options.MaxMessages
	if maxMessages < 0 {
		maxMessages = 0
	}

	payload := map[string]any{
		"batch_size":   batchSize,
		"max_messages": maxMessages,
		"full_resync":  options.FullResync,
	}
	job, err := s.repo.CreateJob(ctx, storage.CreateJobInput{
		JobType:       "sync_source",
		SourceID:      &sourceID,
		Status:        "queued",
		Payload:       payload,
		ProgressTotal: 0,
	})
	if err != nil {
		return SyncResult{}, err
	}

	if err := s.repo.UpdateJobRunning(ctx, job.ID); err != nil {
		return SyncResult{}, err
	}

	result, syncErr := s.syncSourceWithJob(ctx, source, job.ID, batchSize, maxMessages, options.FullResync)
	if syncErr != nil {
		_ = s.repo.FailJob(ctx, job.ID, syncErr.Error(), map[string]any{
			"error": syncErr.Error(),
		})
		_ = s.repo.MarkSourceSyncError(ctx, source.ID, syncErr.Error())
		return SyncResult{}, syncErr
	}

	job.Status = "succeeded"
	return SyncResult{
		Job:             job,
		PagesFetched:    result.PagesFetched,
		MessagesFetched: result.MessagesFetched,
		ProcessedCount:  result.ProcessedCount,
		DuplicateCount:  result.DuplicateCount,
		TrashCount:      result.TrashCount,
		ChunkCount:      result.ChunkCount,
	}, nil
}

func (s *Service) ImportJSON(ctx context.Context, input ImportJSONInput) (ImportJSONResult, error) {
	records, err := parseImportedJSONRecords(input.Payload)
	if err != nil {
		return ImportJSONResult{}, err
	}
	if len(records) == 0 {
		return ImportJSONResult{}, fmt.Errorf("no importable records found in json")
	}

	sourceName := buildImportSourceName(input.SourceName, input.Title, input.Filename)
	sourceURL := fmt.Sprintf("json://upload/%s/%d", sourceName, time.Now().UTC().UnixNano())

	sourceTitle := strings.TrimSpace(input.Title)
	if sourceTitle == "" {
		sourceTitle = strings.TrimSpace(input.SourceName)
	}
	if sourceTitle == "" {
		sourceTitle = strings.TrimSpace(strings.TrimSuffix(filepath.Base(input.Filename), filepath.Ext(input.Filename)))
	}

	source, err := s.repo.CreateSource(ctx, storage.CreateSourceInput{
		SourceType: "json_upload",
		Username:   ptrIfNotEmpty(sourceName),
		Title:      ptrIfNotEmpty(sourceTitle),
		URL:        sourceURL,
	})
	if err != nil {
		return ImportJSONResult{}, fmt.Errorf("create import source: %w", err)
	}

	counters := syncCounters{}
	usedIDs := make(map[int64]struct{}, len(records))
	nextAutoID := int64(1)
	var maxMessageID *int64

	for idx, record := range records {
		messageID := record.MessageID
		if messageID <= 0 {
			messageID = nextAutoID
		}
		for {
			if _, exists := usedIDs[messageID]; !exists {
				break
			}
			messageID++
		}
		usedIDs[messageID] = struct{}{}
		if messageID >= nextAutoID {
			nextAutoID = messageID + 1
		}

		rawJSON := cloneMap(record.RawJSON)
		if rawJSON == nil {
			rawJSON = map[string]any{}
		}
		rawJSON["import_index"] = idx
		rawJSON["source_file"] = input.Filename
		if strings.TrimSpace(record.URL) != "" {
			rawJSON["url"] = strings.TrimSpace(record.URL)
		}

		rawMessage, err := s.repo.UpsertRawMessage(ctx, storage.UpsertRawMessageInput{
			SourceID:          source.ID,
			TelegramMessageID: messageID,
			PostedAt:          record.PostedAt,
			TextRaw:           record.Text,
			CaptionRaw:        record.Caption,
			RawJSON:           rawJSON,
			HasMedia:          strings.TrimSpace(record.Caption) != "",
		})
		if err != nil {
			return ImportJSONResult{}, fmt.Errorf("upsert imported message %d: %w", messageID, err)
		}

		processResult, err := s.processor.ProcessRawMessage(ctx, source, rawMessage)
		if err != nil {
			return ImportJSONResult{}, fmt.Errorf("process imported message %d: %w", messageID, err)
		}

		counters.MessagesFetched++
		if processResult.IsTrash {
			counters.TrashCount++
		} else {
			counters.ProcessedCount++
			if processResult.IsDuplicate {
				counters.DuplicateCount++
			}
			counters.ChunkCount += processResult.ChunkCount
		}

		if maxMessageID == nil || messageID > *maxMessageID {
			value := messageID
			maxMessageID = &value
		}
	}

	if err := s.repo.MarkSourceSyncSuccess(ctx, source.ID, maxMessageID); err != nil {
		return ImportJSONResult{}, err
	}

	return ImportJSONResult{
		Source:         source,
		ImportedCount:  counters.MessagesFetched,
		ProcessedCount: counters.ProcessedCount,
		DuplicateCount: counters.DuplicateCount,
		TrashCount:     counters.TrashCount,
		ChunkCount:     counters.ChunkCount,
	}, nil
}

type syncCounters struct {
	PagesFetched    int
	MessagesFetched int
	ProcessedCount  int
	DuplicateCount  int
	TrashCount      int
	ChunkCount      int
}

func (s *Service) syncSourceWithJob(ctx context.Context, source models.Source, jobID string, batchSize int, maxMessages int, fullResync bool) (syncCounters, error) {
	username := stringOrEmpty(source.Username)
	channelRef, err := s.collector.ResolveChannel(ctx, telegram.ResolveInput{
		URL:      source.URL,
		Username: username,
	})
	if err != nil {
		return syncCounters{}, fmt.Errorf("resolve channel: %w", err)
	}

	if err := s.repo.UpdateSourceResolvedChannel(ctx, source.ID, channelRef.ID, channelRef.AccessHash, channelRef.Username, channelRef.Title, channelRef.URL); err != nil {
		return syncCounters{}, fmt.Errorf("update source resolved channel: %w", err)
	}

	counters := syncCounters{}
	var maxMessageID *int64
	minMessageID := source.LastMessageID
	if fullResync {
		minMessageID = nil
	}
	var cursor *string
	for {
		if maxMessages > 0 && counters.MessagesFetched >= maxMessages {
			break
		}

		remaining := maxMessages - counters.MessagesFetched
		pageLimit := batchSize
		if maxMessages > 0 && remaining < pageLimit {
			pageLimit = remaining
		}

		page, err := s.collector.FetchChannelHistory(ctx, channelRef, telegram.FetchOptions{
			Limit:          pageLimit,
			AfterMessageID: minMessageID,
			Cursor:         cursor,
		})
		if err != nil {
			return syncCounters{}, fmt.Errorf("fetch history: %w", err)
		}
		if len(page.Messages) == 0 {
			break
		}
		counters.PagesFetched++

		for _, msg := range page.Messages {
			jobIDValue := jobID
			rawMessage, err := s.repo.UpsertRawMessage(ctx, storage.UpsertRawMessageInput{
				SourceID:          source.ID,
				TelegramMessageID: msg.MessageID,
				GroupedID:         msg.GroupedID,
				PostedAt:          msg.PostedAt,
				EditedAt:          msg.EditedAt,
				TextRaw:           msg.TextRaw,
				CaptionRaw:        msg.CaptionRaw,
				RawJSON:           msg.RawJSON,
				MediaJSON:         msg.MediaJSON,
				EntitiesJSON:      msg.EntitiesJSON,
				ReactionsJSON:     msg.ReactionsJSON,
				ViewsCount:        msg.ViewsCount,
				ForwardsCount:     msg.ForwardsCount,
				ReplyToMessageID:  msg.ReplyToMessageID,
				ForwardFromName:   msg.ForwardFromName,
				ForwardFromChat:   msg.ForwardFromChat,
				HasMedia:          msg.HasMedia,
				FetchJobID:        &jobIDValue,
			})
			if err != nil {
				return syncCounters{}, fmt.Errorf("upsert raw message %d: %w", msg.MessageID, err)
			}

			processResult, err := s.processor.ProcessRawMessage(ctx, source, rawMessage)
			if err != nil {
				return syncCounters{}, fmt.Errorf("process raw message %d: %w", msg.MessageID, err)
			}

			if processResult.IsTrash {
				counters.TrashCount++
			} else {
				counters.ProcessedCount++
				if processResult.IsDuplicate {
					counters.DuplicateCount++
				}
				counters.ChunkCount += processResult.ChunkCount
			}

			counters.MessagesFetched++

			if maxMessageID == nil || msg.MessageID > *maxMessageID {
				value := msg.MessageID
				maxMessageID = &value
			}

			if maxMessages > 0 && counters.MessagesFetched >= maxMessages {
				break
			}
		}

		progressTotal := counters.MessagesFetched
		if maxMessages > 0 {
			progressTotal = maxMessages
		}
		if err := s.repo.UpdateJobProgress(ctx, jobID, counters.MessagesFetched, progressTotal); err != nil {
			return syncCounters{}, err
		}

		if page.NextCursor == nil {
			break
		}
		if cursor != nil && *cursor == *page.NextCursor {
			break
		}
		cursor = page.NextCursor
	}

	if err := s.repo.UpdateJobProgress(ctx, jobID, counters.MessagesFetched, counters.MessagesFetched); err != nil {
		return syncCounters{}, err
	}

	result := map[string]any{
		"pages_fetched":    counters.PagesFetched,
		"messages_fetched": counters.MessagesFetched,
		"processed_count":  counters.ProcessedCount,
		"duplicate_count":  counters.DuplicateCount,
		"trash_count":      counters.TrashCount,
		"chunk_count":      counters.ChunkCount,
	}
	if err := s.repo.CompleteJob(ctx, jobID, result); err != nil {
		return syncCounters{}, err
	}

	if err := s.repo.MarkSourceSyncSuccess(ctx, source.ID, maxMessageID); err != nil {
		return syncCounters{}, err
	}

	return counters, nil
}

func stringOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func ptrIfNotEmpty(v string) *string {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

type importedJSONRecord struct {
	MessageID int64
	Text      string
	Caption   string
	PostedAt  *time.Time
	URL       string
	RawJSON   map[string]any
}

func parseImportedJSONRecords(payload []byte) ([]importedJSONRecord, error) {
	if len(strings.TrimSpace(string(payload))) == 0 {
		return nil, fmt.Errorf("json payload is empty")
	}

	var root any
	if err := json.Unmarshal(payload, &root); err != nil {
		return nil, fmt.Errorf("invalid json file: %w", err)
	}

	items, err := extractImportItems(root)
	if err != nil {
		return nil, err
	}

	records := make([]importedJSONRecord, 0, len(items))
	for _, item := range items {
		record, ok := parseImportedItem(item)
		if !ok {
			continue
		}
		records = append(records, record)
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("json does not contain supported records")
	}
	return records, nil
}

func extractImportItems(root any) ([]any, error) {
	switch value := root.(type) {
	case []any:
		return value, nil
	case map[string]any:
		for _, key := range []string{"items", "messages", "posts", "data", "result"} {
			raw, ok := value[key]
			if !ok {
				continue
			}
			items, ok := raw.([]any)
			if ok {
				return items, nil
			}
		}
		return nil, fmt.Errorf("json object must contain one of: items, messages, posts, data")
	default:
		return nil, fmt.Errorf("json root must be an array or object")
	}
}

func parseImportedItem(item any) (importedJSONRecord, bool) {
	switch value := item.(type) {
	case string:
		text := strings.TrimSpace(value)
		if text == "" {
			return importedJSONRecord{}, false
		}
		return importedJSONRecord{
			Text:    text,
			RawJSON: map[string]any{"text": text},
		}, true
	case map[string]any:
		text := firstNonEmptyText(value, "text", "content", "message", "body")
		caption := firstNonEmptyText(value, "caption", "description", "summary")
		if text == "" && caption == "" {
			return importedJSONRecord{}, false
		}
		messageID := firstInt64(value, "message_id", "post_id", "id")
		url := firstNonEmptyString(value, "url", "link", "source_url")
		postedAt := firstTime(value, "published_at", "date", "timestamp", "created_at")
		return importedJSONRecord{
			MessageID: messageID,
			Text:      text,
			Caption:   caption,
			PostedAt:  postedAt,
			URL:       url,
			RawJSON:   cloneMap(value),
		}, true
	default:
		return importedJSONRecord{}, false
	}
}

func firstNonEmptyString(input map[string]any, keys ...string) string {
	for _, key := range keys {
		raw, ok := input[key]
		if !ok || raw == nil {
			continue
		}
		switch value := raw.(type) {
		case string:
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				return trimmed
			}
		case float64:
			return strings.TrimSpace(strconv.FormatInt(int64(value), 10))
		case int64:
			return strings.TrimSpace(strconv.FormatInt(value, 10))
		}
	}
	return ""
}

func firstNonEmptyText(input map[string]any, keys ...string) string {
	for _, key := range keys {
		raw, ok := input[key]
		if !ok || raw == nil {
			continue
		}
		value := strings.TrimSpace(extractAnyText(raw))
		if value != "" {
			return value
		}
	}
	return ""
}

func extractAnyText(raw any) string {
	switch value := raw.(type) {
	case string:
		return value
	case []any:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			part := strings.TrimSpace(extractAnyText(item))
			if part == "" {
				continue
			}
			parts = append(parts, part)
		}
		return strings.Join(parts, "")
	case map[string]any:
		for _, key := range []string{"text", "value", "content", "body", "message", "caption", "title"} {
			rawValue, ok := value[key]
			if !ok || rawValue == nil {
				continue
			}
			extracted := strings.TrimSpace(extractAnyText(rawValue))
			if extracted != "" {
				return extracted
			}
		}
		for _, key := range []string{"url", "href"} {
			rawValue, ok := value[key]
			if !ok || rawValue == nil {
				continue
			}
			if url, ok := rawValue.(string); ok {
				url = strings.TrimSpace(url)
				if url != "" {
					return url
				}
			}
		}
		return ""
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
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

func firstInt64(input map[string]any, keys ...string) int64 {
	for _, key := range keys {
		raw, ok := input[key]
		if !ok || raw == nil {
			continue
		}
		switch value := raw.(type) {
		case float64:
			return int64(value)
		case int64:
			return value
		case string:
			parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if err == nil {
				return parsed
			}
		}
	}
	return 0
}

func firstTime(input map[string]any, keys ...string) *time.Time {
	for _, key := range keys {
		raw, ok := input[key]
		if !ok || raw == nil {
			continue
		}
		parsed := parseAnyTime(raw)
		if parsed != nil {
			return parsed
		}
	}
	return nil
}

func parseAnyTime(raw any) *time.Time {
	switch value := raw.(type) {
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return nil
		}
		layouts := []string{
			time.RFC3339Nano,
			time.RFC3339,
			"2006-01-02 15:04:05",
			"2006-01-02",
		}
		for _, layout := range layouts {
			if parsed, err := time.Parse(layout, trimmed); err == nil {
				utc := parsed.UTC()
				return &utc
			}
		}
		if unix, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
			parsed := time.Unix(unix, 0).UTC()
			return &parsed
		}
	case float64:
		parsed := time.Unix(int64(value), 0).UTC()
		return &parsed
	}
	return nil
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

func buildImportSourceName(sourceName, title, filename string) string {
	base := strings.TrimSpace(sourceName)
	if base == "" {
		base = strings.TrimSpace(title)
	}
	if base == "" {
		base = strings.TrimSpace(strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)))
	}
	if base == "" {
		base = "json_upload"
	}

	base = strings.ToLower(base)
	var builder strings.Builder
	for _, r := range base {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			continue
		}
		builder.WriteRune('_')
	}
	out := strings.Trim(builder.String(), "_")
	for strings.Contains(out, "__") {
		out = strings.ReplaceAll(out, "__", "_")
	}
	if out == "" {
		out = "json_upload"
	}
	if len(out) > 48 {
		out = out[:48]
	}
	return out
}
