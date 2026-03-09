package processing

import (
	"context"
	"encoding/json"
	"strings"

	"tg-rag-ingestor/backend/internal/chunking"
	"tg-rag-ingestor/backend/internal/cleaning"
	"tg-rag-ingestor/backend/internal/models"
	"tg-rag-ingestor/backend/internal/storage"
	"tg-rag-ingestor/backend/internal/telegram"
)

type Service struct {
	repo     *storage.Repository
	chunkCfg chunking.Config
}

func NewService(repo *storage.Repository, chunkCfg chunking.Config) *Service {
	return &Service{
		repo:     repo,
		chunkCfg: chunkCfg,
	}
}

type ProcessResult struct {
	DocumentID  string
	IsTrash     bool
	IsDuplicate bool
	ChunkCount  int
}

func (s *Service) ProcessRawMessage(ctx context.Context, source models.Source, raw models.RawMessage) (ProcessResult, error) {
	textRaw := deref(raw.TextRaw)
	captionRaw := deref(raw.CaptionRaw)
	normalized := cleaning.NormalizeTextBody(textRaw, captionRaw)
	links := cleaning.ExtractLinks(normalized)
	normalized = cleaning.NormalizeText(cleaning.RemoveLinks(normalized))
	if cleaning.IsTrash(normalized) {
		return ProcessResult{IsTrash: true}, nil
	}

	originalText := strings.TrimSpace(strings.Join([]string{textRaw, captionRaw}, "\n\n"))
	hashtags := cleaning.ExtractHashtags(normalized)
	mentions := cleaning.ExtractMentions(normalized)
	contentHash := cleaning.ContentHash(normalized)
	simhash := cleaning.SimhashPlaceholder(normalized)
	quality := qualityScore(normalized)

	externalDocID := buildExternalDocID(source, raw.TelegramMessageID)
	canonical, err := s.repo.FindCanonicalDocumentByHash(ctx, source.ID, contentHash, externalDocID)
	if err != nil {
		return ProcessResult{}, err
	}

	isDuplicate := canonical != nil
	var duplicateOf *string
	if canonical != nil {
		duplicateOf = &canonical.ID
	}

	sourceName := "telegram"
	sourceSubtype := "public_channel_post"
	metadataURL := telegram.MessageURL(deref(source.Username), raw.TelegramMessageID)
	if source.SourceType == "json_upload" {
		sourceName = "json_upload"
		sourceSubtype = "uploaded_json_post"
		if importedURL := extractURLFromRawJSON(raw.RawJSON); importedURL != "" {
			metadataURL = importedURL
		} else if strings.TrimSpace(source.URL) != "" {
			metadataURL = strings.TrimSpace(source.URL)
		}
	}

	metadata := map[string]any{
		"source":              sourceName,
		"source_subtype":      sourceSubtype,
		"channel_username":    deref(source.Username),
		"message_id":          raw.TelegramMessageID,
		"url":                 metadataURL,
		"hashtags":            hashtags,
		"mentions":            mentions,
		"is_forward":          raw.ForwardFromName != nil || raw.ForwardFromChat != nil,
		"reply_to_message_id": raw.ReplyToMessageID,
	}

	language := detectLanguage(normalized)
	simhashPtr := &simhash

	doc, err := s.repo.SaveDocument(ctx, storage.SaveDocumentInput{
		SourceID:      source.ID,
		RawMessageID:  raw.ID,
		ExternalDocID: externalDocID,
		Title:         source.Title,
		TextClean:     normalized,
		TextOriginal:  ptrIfNotEmpty(originalText),
		LanguageCode:  &language,
		ContentHash:   contentHash,
		Simhash:       simhashPtr,
		DedupeGroup:   ptrIfNotEmpty(contentHash[:12]),
		QualityScore:  &quality,
		IsDuplicate:   isDuplicate,
		DuplicateOf:   duplicateOf,
		IsForward:     raw.ForwardFromName != nil || raw.ForwardFromChat != nil,
		ForwardSource: firstNonNil(raw.ForwardFromChat, raw.ForwardFromName),
		Tags:          hashtags,
		Links:         links,
		Metadata:      metadata,
		PublishedAt:   raw.PostedAt,
	})
	if err != nil {
		return ProcessResult{}, err
	}

	if err := s.repo.DeleteChunksByDocumentID(ctx, doc.ID); err != nil {
		return ProcessResult{}, err
	}

	if doc.IsDuplicate {
		return ProcessResult{
			DocumentID:  doc.ID,
			IsDuplicate: true,
			ChunkCount:  0,
		}, nil
	}

	chunks := chunking.SplitWithConfig(normalized, s.chunkCfg)
	if err := s.repo.InsertChunks(ctx, doc.ID, source.ID, raw.ID, chunks); err != nil {
		return ProcessResult{}, err
	}

	return ProcessResult{
		DocumentID:  doc.ID,
		IsDuplicate: false,
		ChunkCount:  len(chunks),
	}, nil
}

func buildExternalDocID(source models.Source, messageID int64) string {
	username := deref(source.Username)
	if username == "" {
		username = source.ID
	}
	prefix := "tg"
	if source.SourceType == "json_upload" {
		prefix = "json"
	}
	return prefix + ":" + username + ":" + int64ToString(messageID)
}

func detectLanguage(input string) string {
	for _, r := range input {
		if r >= 'а' && r <= 'я' {
			return "ru"
		}
		if r >= 'А' && r <= 'Я' {
			return "ru"
		}
	}
	return "en"
}

func qualityScore(input string) float64 {
	length := len(strings.Fields(input))
	if length <= 8 {
		return 0.45
	}
	if length <= 20 {
		return 0.72
	}
	if length <= 50 {
		return 0.86
	}
	return 0.92
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func ptrIfNotEmpty(v string) *string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return &v
}

func firstNonNil(values ...*string) *string {
	for _, value := range values {
		if value != nil && strings.TrimSpace(*value) != "" {
			return value
		}
	}
	return nil
}

func int64ToString(v int64) string {
	if v == 0 {
		return "0"
	}
	negative := v < 0
	if negative {
		v = -v
	}
	buf := make([]byte, 0, 20)
	for v > 0 {
		buf = append(buf, byte('0'+(v%10)))
		v /= 10
	}
	if negative {
		buf = append(buf, '-')
	}
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}

func extractURLFromRawJSON(rawJSON string) string {
	if strings.TrimSpace(rawJSON) == "" {
		return ""
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &parsed); err != nil {
		return ""
	}
	for _, key := range []string{"url", "link", "source_url"} {
		raw, ok := parsed[key]
		if !ok || raw == nil {
			continue
		}
		if value, ok := raw.(string); ok {
			value = strings.TrimSpace(value)
			if value != "" {
				return value
			}
		}
	}
	return ""
}
