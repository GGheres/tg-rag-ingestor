package ingestion

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"tg-rag-ingestor/backend/internal/cleaning"
	"tg-rag-ingestor/backend/internal/models"
	"tg-rag-ingestor/backend/internal/storage"
	"tg-rag-ingestor/backend/internal/telegram"
)

type CreateTelegramMessageLinkSourceInput struct {
	Title        *string
	MessageLinks []string
}

type CreateTelegramChannelDocumentSourceInput struct {
	Title *string
	URL   string
}

func (s *Service) CreateTelegramMessageLinkSource(ctx context.Context, input CreateTelegramMessageLinkSourceInput) (models.Source, error) {
	links, err := normalizeTelegramMessageLinks(input.MessageLinks)
	if err != nil {
		return models.Source{}, err
	}

	first := links[0]
	sourceURL := buildTelegramMessageLinkSourceURL(first.IdentityKey(), links)
	sourceTitle := buildTelegramMessageLinkSourceTitle(input.Title, first, len(links))

	createLinks := make([]storage.CreateTelegramMessageLinkInput, 0, len(links))
	for idx, link := range links {
		linkCopy := link
		createLinks = append(createLinks, storage.CreateTelegramMessageLinkInput{
			LinkOrder:         idx + 1,
			OriginalURL:       linkCopy.OriginalURL,
			CanonicalURL:      linkCopy.CanonicalURL,
			TelegramChannelID: linkCopy.ChannelID,
			Username:          ptrIfNotEmpty(linkCopy.Username),
			TelegramMessageID: linkCopy.MessageID,
		})
	}

	return s.repo.CreateTelegramMessageLinkSource(ctx, storage.CreateSourceInput{
		SourceType: telegram.MessageLinkSourceType,
		Username:   ptrIfNotEmpty(first.Username),
		Title:      ptrIfNotEmpty(sourceTitle),
		URL:        sourceURL,
	}, createLinks)
}

func (s *Service) CreateTelegramChannelDocumentSource(ctx context.Context, input CreateTelegramChannelDocumentSourceInput) (models.Source, error) {
	channelLink, err := telegram.ParseChannelLink(telegram.ResolveInput{URL: input.URL})
	if err != nil {
		return models.Source{}, err
	}
	sourceURL := buildTelegramChannelDocumentSourceURL(channelLink.NormalizedURL)

	sourceTitle := strings.TrimSpace(stringOrEmpty(input.Title))
	if sourceTitle == "" {
		if channelLink.Username != "" {
			sourceTitle = "Telegram channel document @" + channelLink.Username
		} else {
			sourceTitle = "Telegram channel document"
		}
	}

	existing, err := s.repo.GetSourceByURL(ctx, channelLink.NormalizedURL)
	if err == nil {
		if existing.SourceType == telegram.ChannelDocumentSourceType {
			return existing, nil
		}
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return models.Source{}, err
	}

	existing, err = s.repo.GetSourceByURL(ctx, sourceURL)
	if err == nil {
		if existing.SourceType != telegram.ChannelDocumentSourceType {
			return models.Source{}, fmt.Errorf("source with this url already exists as type %s", existing.SourceType)
		}
		return existing, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return models.Source{}, err
	}

	source, err := s.repo.CreateSource(ctx, storage.CreateSourceInput{
		SourceType: telegram.ChannelDocumentSourceType,
		ExternalID: ptrIfNotEmpty(channelLink.NormalizedURL),
		Username:   ptrIfNotEmpty(channelLink.Username),
		Title:      ptrIfNotEmpty(sourceTitle),
		URL:        sourceURL,
	})
	if err != nil {
		if isSourceURLConflict(err) {
			existing, lookupErr := s.repo.GetSourceByURL(ctx, sourceURL)
			if lookupErr == nil {
				if existing.SourceType != telegram.ChannelDocumentSourceType {
					return models.Source{}, fmt.Errorf("source with this url already exists as type %s", existing.SourceType)
				}
				return existing, nil
			}
		}
		return models.Source{}, err
	}
	return source, nil
}

func (s *Service) syncMessageLinkSourceWithJob(ctx context.Context, source models.Source, jobID string) (syncCounters, error) {
	links, err := s.repo.ListTelegramMessageLinksBySource(ctx, source.ID)
	if err != nil {
		return syncCounters{}, fmt.Errorf("load source message links: %w", err)
	}
	if len(links) == 0 {
		return syncCounters{}, fmt.Errorf("source %s has no telegram message links", source.ID)
	}

	channelRef, err := s.resolveMessageLinkChannel(ctx, links[0])
	if err != nil {
		return syncCounters{}, fmt.Errorf("resolve linked channel: %w", err)
	}

	resolvedUsername := channelRef.Username
	if strings.TrimSpace(resolvedUsername) == "" && links[0].Username != nil {
		resolvedUsername = strings.TrimSpace(*links[0].Username)
	}
	resolvedTitle := strings.TrimSpace(channelRef.Title)
	if resolvedTitle == "" {
		resolvedTitle = strings.TrimSpace(stringOrEmpty(source.Title))
	}
	if err := s.repo.UpdateSourceResolvedChannel(
		ctx,
		source.ID,
		channelRef.ID,
		channelRef.AccessHash,
		resolvedUsername,
		resolvedTitle,
		source.URL,
	); err != nil {
		return syncCounters{}, fmt.Errorf("update source resolved channel: %w", err)
	}
	source.TelegramChannelID = channelRef.ID
	source.TelegramAccessHash = channelRef.AccessHash
	if strings.TrimSpace(resolvedUsername) != "" {
		source.Username = &resolvedUsername
	}
	if strings.TrimSpace(resolvedTitle) != "" {
		source.Title = &resolvedTitle
	}

	messageIDs := make([]int64, 0, len(links))
	for _, link := range links {
		messageIDs = append(messageIDs, link.TelegramMessageID)
	}

	messages, err := s.collector.FetchMessages(ctx, channelRef, messageIDs)
	if err != nil {
		return syncCounters{}, fmt.Errorf("fetch linked messages: %w", err)
	}
	if len(messages) != len(links) {
		return syncCounters{}, fmt.Errorf("collector returned %d messages for %d links", len(messages), len(links))
	}

	counters := syncCounters{
		PagesFetched:    1,
		MessagesFetched: len(messages),
	}

	rawMessages := make([]models.RawMessage, 0, len(messages))
	var maxMessageID *int64

	for idx, msg := range messages {
		link := links[idx]
		jobIDValue := jobID
		rawJSON := cloneMap(msg.RawJSON)
		if rawJSON == nil {
			rawJSON = map[string]any{}
		}
		rawJSON["source_link"] = map[string]any{
			"link_order":    link.LinkOrder,
			"original_url":  link.OriginalURL,
			"canonical_url": link.CanonicalURL,
		}
		rawJSON["url"] = link.CanonicalURL

		rawMessage, err := s.repo.UpsertRawMessage(ctx, storage.UpsertRawMessageInput{
			SourceID:          source.ID,
			TelegramMessageID: msg.MessageID,
			GroupedID:         msg.GroupedID,
			PostedAt:          msg.PostedAt,
			EditedAt:          msg.EditedAt,
			TextRaw:           msg.TextRaw,
			CaptionRaw:        msg.CaptionRaw,
			RawJSON:           rawJSON,
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
		rawMessages = append(rawMessages, rawMessage)

		if maxMessageID == nil || msg.MessageID > *maxMessageID {
			value := msg.MessageID
			maxMessageID = &value
		}
	}

	processResult, err := s.processor.ProcessTelegramMessageLinkDocument(ctx, source, rawMessages, links)
	if err != nil {
		return syncCounters{}, fmt.Errorf("process linked messages: %w", err)
	}

	counters.TrashCount = len(rawMessages) - processResult.IncludedMessages
	if !processResult.IsTrash {
		counters.ProcessedCount = 1
		counters.ChunkCount = processResult.ChunkCount
	}

	if err := s.repo.UpdateJobProgress(ctx, jobID, counters.MessagesFetched, counters.MessagesFetched); err != nil {
		return syncCounters{}, err
	}

	result := map[string]any{
		"pages_fetched":     counters.PagesFetched,
		"messages_fetched":  counters.MessagesFetched,
		"processed_count":   counters.ProcessedCount,
		"duplicate_count":   counters.DuplicateCount,
		"trash_count":       counters.TrashCount,
		"chunk_count":       counters.ChunkCount,
		"included_messages": processResult.IncludedMessages,
	}
	if err := s.repo.CompleteJob(ctx, jobID, result); err != nil {
		return syncCounters{}, err
	}
	if err := s.repo.MarkSourceSyncSuccess(ctx, source.ID, maxMessageID); err != nil {
		return syncCounters{}, err
	}

	return counters, nil
}

func (s *Service) syncChannelDocumentSourceWithJob(
	ctx context.Context,
	source models.Source,
	jobID string,
	batchSize int,
	maxMessages int,
) (syncCounters, error) {
	if batchSize <= 0 {
		batchSize = s.defaultBatch
	}
	if batchSize > 100 {
		batchSize = 100
	}

	channelRef, err := s.collector.ResolveChannel(ctx, telegram.ResolveInput{
		URL:      telegramChannelDocumentResolveURL(source),
		Username: stringOrEmpty(source.Username),
	})
	if err != nil {
		return syncCounters{}, fmt.Errorf("resolve channel: %w", err)
	}

	resolvedUsername := strings.TrimSpace(channelRef.Username)
	resolvedTitle := strings.TrimSpace(channelRef.Title)
	if resolvedTitle == "" {
		resolvedTitle = strings.TrimSpace(stringOrEmpty(source.Title))
	}
	if err := s.repo.UpdateSourceResolvedChannel(
		ctx,
		source.ID,
		channelRef.ID,
		channelRef.AccessHash,
		resolvedUsername,
		resolvedTitle,
		source.URL,
	); err != nil {
		return syncCounters{}, fmt.Errorf("update source resolved channel: %w", err)
	}
	source.TelegramChannelID = channelRef.ID
	source.TelegramAccessHash = channelRef.AccessHash
	if resolvedUsername != "" {
		source.Username = &resolvedUsername
	}
	if resolvedTitle != "" {
		source.Title = &resolvedTitle
	}

	counters := syncCounters{}
	cursor := (*string)(nil)
	collected := make([]telegram.Message, 0)
	var maxMessageID *int64

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
			Limit:  pageLimit,
			Cursor: cursor,
		})
		if err != nil {
			return syncCounters{}, fmt.Errorf("fetch history: %w", err)
		}
		if len(page.Messages) == 0 {
			break
		}

		counters.PagesFetched++
		collected = append(collected, page.Messages...)
		counters.MessagesFetched += len(page.Messages)

		for _, msg := range page.Messages {
			if maxMessageID == nil || msg.MessageID > *maxMessageID {
				value := msg.MessageID
				maxMessageID = &value
			}
		}

		if err := s.repo.UpdateJobProgress(ctx, jobID, counters.MessagesFetched, maxInt(counters.MessagesFetched, maxMessages)); err != nil {
			return syncCounters{}, err
		}
		if page.NextCursor == nil {
			break
		}
		cursor = page.NextCursor
	}

	sort.Slice(collected, func(i, j int) bool {
		return collected[i].MessageID < collected[j].MessageID
	})

	linkInputs := make([]storage.CreateTelegramMessageLinkInput, 0, len(collected))
	processLinks := make([]models.TelegramMessageLink, 0, len(collected))
	rawMessages := make([]models.RawMessage, 0, len(collected))

	for idx, msg := range collected {
		linkURL := buildChannelMessageURL(channelRef, msg.MessageID)
		linkOrder := idx + 1
		linkInputs = append(linkInputs, storage.CreateTelegramMessageLinkInput{
			LinkOrder:         linkOrder,
			OriginalURL:       linkURL,
			CanonicalURL:      linkURL,
			TelegramChannelID: channelRef.ID,
			Username:          ptrIfNotEmpty(channelRef.Username),
			TelegramMessageID: msg.MessageID,
		})
		processLinks = append(processLinks, models.TelegramMessageLink{
			SourceID:          source.ID,
			LinkOrder:         linkOrder,
			OriginalURL:       linkURL,
			CanonicalURL:      linkURL,
			TelegramChannelID: channelRef.ID,
			Username:          ptrIfNotEmpty(channelRef.Username),
			TelegramMessageID: msg.MessageID,
		})

		jobIDValue := jobID
		rawJSON := cloneMap(msg.RawJSON)
		if rawJSON == nil {
			rawJSON = map[string]any{}
		}
		rawJSON["source_link"] = map[string]any{
			"link_order":    linkOrder,
			"original_url":  linkURL,
			"canonical_url": linkURL,
		}
		rawJSON["url"] = linkURL

		rawMessage, err := s.repo.UpsertRawMessage(ctx, storage.UpsertRawMessageInput{
			SourceID:          source.ID,
			TelegramMessageID: msg.MessageID,
			GroupedID:         msg.GroupedID,
			PostedAt:          msg.PostedAt,
			EditedAt:          msg.EditedAt,
			TextRaw:           msg.TextRaw,
			CaptionRaw:        msg.CaptionRaw,
			RawJSON:           rawJSON,
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
		rawMessages = append(rawMessages, rawMessage)
	}

	if err := s.repo.ReplaceTelegramMessageLinksBySource(ctx, source.ID, linkInputs); err != nil {
		return syncCounters{}, fmt.Errorf("replace generated message links: %w", err)
	}

	processResult, err := s.processor.ProcessTelegramMessageLinkDocument(ctx, source, rawMessages, processLinks)
	if err != nil {
		return syncCounters{}, fmt.Errorf("process channel document: %w", err)
	}

	counters.TrashCount = len(rawMessages) - processResult.IncludedMessages
	if !processResult.IsTrash {
		counters.ProcessedCount = 1
		counters.ChunkCount = processResult.ChunkCount
	}

	if err := s.repo.UpdateJobProgress(ctx, jobID, counters.MessagesFetched, counters.MessagesFetched); err != nil {
		return syncCounters{}, err
	}
	result := map[string]any{
		"pages_fetched":     counters.PagesFetched,
		"messages_fetched":  counters.MessagesFetched,
		"processed_count":   counters.ProcessedCount,
		"duplicate_count":   counters.DuplicateCount,
		"trash_count":       counters.TrashCount,
		"chunk_count":       counters.ChunkCount,
		"included_messages": processResult.IncludedMessages,
	}
	if err := s.repo.CompleteJob(ctx, jobID, result); err != nil {
		return syncCounters{}, err
	}
	if err := s.repo.MarkSourceSyncSuccess(ctx, source.ID, maxMessageID); err != nil {
		return syncCounters{}, err
	}

	return counters, nil
}

func (s *Service) resolveMessageLinkChannel(ctx context.Context, link models.TelegramMessageLink) (telegram.ChannelRef, error) {
	if link.TelegramChannelID != nil && *link.TelegramChannelID > 0 {
		return s.collector.ResolveChannelByID(ctx, *link.TelegramChannelID)
	}

	return s.collector.ResolveChannel(ctx, telegram.ResolveInput{
		URL:      link.CanonicalURL,
		Username: stringOrEmpty(link.Username),
	})
}

func normalizeTelegramMessageLinks(rawLinks []string) ([]telegram.MessageLink, error) {
	flattened := make([]string, 0, len(rawLinks))
	for _, raw := range rawLinks {
		for _, line := range strings.Split(raw, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				flattened = append(flattened, line)
			}
		}
	}
	if len(flattened) == 0 {
		return nil, fmt.Errorf("message_links must contain at least one Telegram message link")
	}

	parsed := make([]telegram.MessageLink, 0, len(flattened))
	seen := make(map[string]struct{}, len(flattened))
	var identityKey string

	for _, raw := range flattened {
		link, err := telegram.ParseMessageLink(raw)
		if err != nil {
			return nil, err
		}
		if identityKey == "" {
			identityKey = link.IdentityKey()
		} else if link.IdentityKey() != identityKey {
			return nil, fmt.Errorf("all message links must belong to the same Telegram channel")
		}
		if _, ok := seen[link.CanonicalURL]; ok {
			continue
		}
		seen[link.CanonicalURL] = struct{}{}
		parsed = append(parsed, link)
	}

	if len(parsed) == 0 {
		return nil, fmt.Errorf("message_links must contain at least one unique Telegram message link")
	}
	return parsed, nil
}

func buildTelegramMessageLinkSourceURL(identityKey string, links []telegram.MessageLink) string {
	canonical := make([]string, 0, len(links))
	for _, link := range links {
		canonical = append(canonical, link.CanonicalURL)
	}
	hash := cleaning.ContentHash(strings.Join(canonical, "\n"))
	replacer := strings.NewReplacer(":", "-", "/", "-", "@", "")
	return "telegram-links://" + replacer.Replace(identityKey) + "/" + hash[:16]
}

func buildTelegramMessageLinkSourceTitle(rawTitle *string, first telegram.MessageLink, count int) string {
	if title := strings.TrimSpace(stringOrEmpty(rawTitle)); title != "" {
		return title
	}
	if first.Username != "" {
		return fmt.Sprintf("Telegram message links @%s (%d)", first.Username, count)
	}
	if first.ChannelID != nil {
		return fmt.Sprintf("Telegram private message links %d (%d)", *first.ChannelID, count)
	}
	return fmt.Sprintf("Telegram message links (%d)", count)
}

func buildChannelMessageURL(channel telegram.ChannelRef, messageID int64) string {
	if strings.TrimSpace(channel.Username) != "" {
		return telegram.MessageURL(channel.Username, messageID)
	}
	if channel.ID != nil {
		return telegram.PrivateMessageURL(*channel.ID, messageID)
	}
	return ""
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func buildTelegramChannelDocumentSourceURL(channelURL string) string {
	hash := cleaning.ContentHash(strings.TrimSpace(channelURL))
	return "telegram-channel-document://" + hash[:24]
}

func telegramChannelDocumentResolveURL(source models.Source) string {
	if value := strings.TrimSpace(stringOrEmpty(source.ExternalID)); value != "" {
		return value
	}
	return source.URL
}

func isSourceURLConflict(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && pgErr.ConstraintName == "sources_url_key"
}
