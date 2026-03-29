package processing

import (
	"context"
	"fmt"
	"strings"
	"time"

	"tg-rag-ingestor/backend/internal/chunking"
	"tg-rag-ingestor/backend/internal/cleaning"
	"tg-rag-ingestor/backend/internal/models"
	"tg-rag-ingestor/backend/internal/storage"
)

func (s *Service) ProcessTelegramMessageLinkDocument(
	ctx context.Context,
	source models.Source,
	rawMessages []models.RawMessage,
	links []models.TelegramMessageLink,
) (ProcessResult, error) {
	if len(rawMessages) == 0 {
		if err := s.repo.DeleteDocumentByExternalDocID(ctx, buildTelegramMessageLinkExternalDocID(source)); err != nil {
			return ProcessResult{}, err
		}
		return ProcessResult{IsTrash: true}, nil
	}
	if len(rawMessages) != len(links) {
		return ProcessResult{}, fmt.Errorf("raw message count %d does not match link count %d", len(rawMessages), len(links))
	}

	externalDocID := buildTelegramMessageLinkExternalDocID(source)

	cleanBlocks := make([]string, 0, len(rawMessages))
	originalBlocks := make([]string, 0, len(rawMessages))
	contentLinks := make([]string, 0)
	hashtags := make([]string, 0)
	mentions := make([]string, 0)
	messageIDs := make([]int64, 0, len(rawMessages))
	messageURLs := make([]string, 0, len(rawMessages))
	skippedMessageIDs := make([]int64, 0)
	var publishedAt *time.Time
	var primaryRawMessageID *string
	isForward := false
	forwardSource := ""

	for idx, raw := range rawMessages {
		link := links[idx]
		textRaw := deref(raw.TextRaw)
		captionRaw := deref(raw.CaptionRaw)
		normalized := cleaning.NormalizeTextBody(textRaw, captionRaw)
		messageContentLinks := cleaning.ExtractLinks(normalized)
		normalized = cleaning.NormalizeText(cleaning.RemoveLinks(normalized))
		if cleaning.IsTrash(normalized) {
			skippedMessageIDs = append(skippedMessageIDs, raw.TelegramMessageID)
			continue
		}

		cleanBlocks = append(cleanBlocks, normalized)
		messageIDs = append(messageIDs, raw.TelegramMessageID)
		messageURLs = append(messageURLs, link.CanonicalURL)
		contentLinks = appendUniqueStrings(contentLinks, messageContentLinks...)
		hashtags = appendUniqueStrings(hashtags, cleaning.ExtractHashtags(normalized)...)
		mentions = appendUniqueStrings(mentions, cleaning.ExtractMentions(normalized)...)
		if primaryRawMessageID == nil {
			value := raw.ID
			primaryRawMessageID = &value
		}

		original := strings.TrimSpace(strings.Join([]string{textRaw, captionRaw}, "\n\n"))
		if original != "" {
			originalBlocks = append(originalBlocks, original)
		}

		if raw.PostedAt != nil && (publishedAt == nil || raw.PostedAt.Before(*publishedAt)) {
			publishedAt = raw.PostedAt
		}

		if raw.ForwardFromName != nil || raw.ForwardFromChat != nil {
			isForward = true
			if forwardSource == "" {
				forwardSource = strings.TrimSpace(deref(raw.ForwardFromChat))
				if forwardSource == "" {
					forwardSource = strings.TrimSpace(deref(raw.ForwardFromName))
				}
			}
		}
	}

	if len(cleanBlocks) == 0 {
		if err := s.repo.DeleteDocumentByExternalDocID(ctx, externalDocID); err != nil {
			return ProcessResult{}, err
		}
		return ProcessResult{
			IsTrash:          true,
			IncludedMessages: 0,
		}, nil
	}

	textClean := strings.Join(cleanBlocks, "\n\n")
	textOriginal := strings.Join(originalBlocks, "\n\n")
	contentHash := cleaning.ContentHash(textClean)
	simhash := cleaning.SimhashPlaceholder(textClean)
	quality := qualityScore(textClean)
	language := detectLanguage(textClean)

	metadata := map[string]any{
		"source":              "telegram",
		"source_subtype":      "message_link_bundle",
		"channel_username":    deref(source.Username),
		"channel_title":       deref(source.Title),
		"channel_id":          source.TelegramChannelID,
		"url":                 messageURLs[0],
		"message_ids":         messageIDs,
		"message_urls":        messageURLs,
		"message_count":       len(messageIDs),
		"input_link_count":    len(links),
		"hashtags":            hashtags,
		"mentions":            mentions,
		"is_forward":          isForward,
		"skipped_message_ids": skippedMessageIDs,
	}

	doc, err := s.repo.SaveDocument(ctx, storage.SaveDocumentInput{
		SourceID:      source.ID,
		RawMessageID:  primaryRawMessageID,
		ExternalDocID: externalDocID,
		Title:         source.Title,
		TextClean:     textClean,
		TextOriginal:  ptrIfNotEmpty(textOriginal),
		LanguageCode:  &language,
		ContentHash:   contentHash,
		Simhash:       &simhash,
		DedupeGroup:   ptrIfNotEmpty(contentHash[:12]),
		QualityScore:  &quality,
		IsDuplicate:   false,
		DuplicateOf:   nil,
		IsForward:     isForward,
		ForwardSource: ptrIfNotEmpty(forwardSource),
		Tags:          hashtags,
		Links:         contentLinks,
		Metadata:      metadata,
		PublishedAt:   publishedAt,
	})
	if err != nil {
		return ProcessResult{}, err
	}

	if err := s.repo.DeleteChunksByDocumentID(ctx, doc.ID); err != nil {
		return ProcessResult{}, err
	}

	chunks := chunking.SplitWithConfig(textClean, s.chunkCfg)
	if err := s.repo.InsertChunks(ctx, doc.ID, source.ID, nil, chunks); err != nil {
		return ProcessResult{}, err
	}

	return ProcessResult{
		DocumentID:       doc.ID,
		IsDuplicate:      false,
		ChunkCount:       len(chunks),
		IncludedMessages: len(cleanBlocks),
	}, nil
}

func buildTelegramMessageLinkExternalDocID(source models.Source) string {
	return "tg-links:" + source.ID
}

func appendUniqueStrings(dst []string, values ...string) []string {
	if len(values) == 0 {
		return dst
	}
	seen := make(map[string]struct{}, len(dst))
	for _, value := range dst {
		if strings.TrimSpace(value) != "" {
			seen[strings.TrimSpace(value)] = struct{}{}
		}
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		dst = append(dst, value)
	}
	return dst
}
