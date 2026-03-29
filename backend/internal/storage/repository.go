package storage

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"tg-rag-ingestor/backend/internal/chunking"
	"tg-rag-ingestor/backend/internal/models"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

type CreateSourceInput struct {
	SourceType string
	Provider   *string
	ExternalID *string
	Username   *string
	Title      *string
	URL        string
}

type CreateTelegramMessageLinkInput struct {
	LinkOrder         int
	OriginalURL       string
	CanonicalURL      string
	TelegramChannelID *int64
	Username          *string
	TelegramMessageID int64
}

func (r *Repository) CreateSource(ctx context.Context, input CreateSourceInput) (models.Source, error) {
	var source models.Source
	err := r.pool.QueryRow(ctx, `
		insert into sources (source_type, provider, external_id, username, title, url, status)
		values ($1, $2, $3, $4, $5, $6, 'active')
		returning id, source_type, provider, external_id, telegram_channel_id, telegram_access_hash, username, title, url, status,
		          last_message_id, last_synced_at, last_error, created_at, updated_at
	`, input.SourceType, input.Provider, input.ExternalID, input.Username, input.Title, input.URL).Scan(
		&source.ID,
		&source.SourceType,
		&source.Provider,
		&source.ExternalID,
		&source.TelegramChannelID,
		&source.TelegramAccessHash,
		&source.Username,
		&source.Title,
		&source.URL,
		&source.Status,
		&source.LastMessageID,
		&source.LastSyncedAt,
		&source.LastError,
		&source.CreatedAt,
		&source.UpdatedAt,
	)
	return source, err
}

func (r *Repository) CreateTelegramMessageLinkSource(
	ctx context.Context,
	sourceInput CreateSourceInput,
	links []CreateTelegramMessageLinkInput,
) (models.Source, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return models.Source{}, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var source models.Source
	err = tx.QueryRow(ctx, `
		insert into sources (source_type, provider, external_id, username, title, url, status)
		values ($1, $2, $3, $4, $5, $6, 'active')
		returning id, source_type, provider, external_id, telegram_channel_id, telegram_access_hash, username, title, url, status,
		          last_message_id, last_synced_at, last_error, created_at, updated_at
	`, sourceInput.SourceType, sourceInput.Provider, sourceInput.ExternalID, sourceInput.Username, sourceInput.Title, sourceInput.URL).Scan(
		&source.ID,
		&source.SourceType,
		&source.Provider,
		&source.ExternalID,
		&source.TelegramChannelID,
		&source.TelegramAccessHash,
		&source.Username,
		&source.Title,
		&source.URL,
		&source.Status,
		&source.LastMessageID,
		&source.LastSyncedAt,
		&source.LastError,
		&source.CreatedAt,
		&source.UpdatedAt,
	)
	if err != nil {
		return models.Source{}, err
	}

	for _, link := range links {
		if _, err := tx.Exec(ctx, `
			insert into telegram_message_links (
				source_id, link_order, original_url, canonical_url, telegram_channel_id, username, telegram_message_id
			)
			values ($1, $2, $3, $4, $5, $6, $7)
		`, source.ID, link.LinkOrder, link.OriginalURL, link.CanonicalURL, link.TelegramChannelID, link.Username, link.TelegramMessageID); err != nil {
			return models.Source{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return models.Source{}, err
	}
	return source, nil
}

func (r *Repository) ReplaceTelegramMessageLinksBySource(
	ctx context.Context,
	sourceID string,
	links []CreateTelegramMessageLinkInput,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if _, err := tx.Exec(ctx, `delete from telegram_message_links where source_id = $1`, sourceID); err != nil {
		return err
	}
	for _, link := range links {
		if _, err := tx.Exec(ctx, `
			insert into telegram_message_links (
				source_id, link_order, original_url, canonical_url, telegram_channel_id, username, telegram_message_id
			)
			values ($1, $2, $3, $4, $5, $6, $7)
		`, sourceID, link.LinkOrder, link.OriginalURL, link.CanonicalURL, link.TelegramChannelID, link.Username, link.TelegramMessageID); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (r *Repository) ListSources(ctx context.Context) ([]models.Source, error) {
	rows, err := r.pool.Query(ctx, `
		select id, source_type, provider, external_id, telegram_channel_id, telegram_access_hash, username, title, url, status,
		       last_message_id, last_synced_at, last_error, created_at, updated_at
		from sources
		order by created_at desc
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.Source, 0)
	for rows.Next() {
		var source models.Source
		if err := rows.Scan(
			&source.ID,
			&source.SourceType,
			&source.Provider,
			&source.ExternalID,
			&source.TelegramChannelID,
			&source.TelegramAccessHash,
			&source.Username,
			&source.Title,
			&source.URL,
			&source.Status,
			&source.LastMessageID,
			&source.LastSyncedAt,
			&source.LastError,
			&source.CreatedAt,
			&source.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, source)
	}

	return out, rows.Err()
}

func (r *Repository) DeleteAllSources(ctx context.Context) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var deletedCount int64
	if err := tx.QueryRow(ctx, `select count(*) from sources`).Scan(&deletedCount); err != nil {
		return 0, err
	}

	if _, err := tx.Exec(ctx, `
		truncate table
			telegram_message_links,
			youtube_audio_artifacts,
			chunks,
			documents,
			raw_messages,
			exports,
			jobs,
			sources
		restart identity cascade
	`); err != nil {
		return 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return deletedCount, nil
}

func (r *Repository) GetSource(ctx context.Context, sourceID string) (models.Source, error) {
	var source models.Source
	err := r.pool.QueryRow(ctx, `
		select id, source_type, provider, external_id, telegram_channel_id, telegram_access_hash, username, title, url, status,
		       last_message_id, last_synced_at, last_error, created_at, updated_at
		from sources
		where id = $1
	`, sourceID).Scan(
		&source.ID,
		&source.SourceType,
		&source.Provider,
		&source.ExternalID,
		&source.TelegramChannelID,
		&source.TelegramAccessHash,
		&source.Username,
		&source.Title,
		&source.URL,
		&source.Status,
		&source.LastMessageID,
		&source.LastSyncedAt,
		&source.LastError,
		&source.CreatedAt,
		&source.UpdatedAt,
	)
	return source, err
}

func (r *Repository) GetSourceByURL(ctx context.Context, sourceURL string) (models.Source, error) {
	row := r.pool.QueryRow(ctx, `
		select id, source_type, provider, external_id, telegram_channel_id, telegram_access_hash, username, title, url, status,
		       last_message_id, last_synced_at, last_error, created_at, updated_at
		from sources
		where url = $1
	`, sourceURL)

	return scanSource(row)
}

func (r *Repository) UpdateSourceResolvedChannel(ctx context.Context, sourceID string, channelID, accessHash *int64, username, title, url string) error {
	_, err := r.pool.Exec(ctx, `
		update sources
		set telegram_channel_id = $2,
		    telegram_access_hash = $3,
		    username = $4,
		    title = $5,
		    url = $6,
		    updated_at = now()
		where id = $1
	`, sourceID, channelID, accessHash, username, title, url)
	return err
}

func (r *Repository) MarkSourceSyncSuccess(ctx context.Context, sourceID string, lastMessageID *int64) error {
	_, err := r.pool.Exec(ctx, `
		update sources
		set status = 'active',
		    last_message_id = coalesce($2, last_message_id),
		    last_synced_at = now(),
		    last_error = null,
		    updated_at = now()
		where id = $1
	`, sourceID, lastMessageID)
	return err
}

func (r *Repository) MarkSourceSyncError(ctx context.Context, sourceID string, errText string) error {
	_, err := r.pool.Exec(ctx, `
		update sources
		set status = 'error',
		    last_error = $2,
		    updated_at = now()
		where id = $1
	`, sourceID, errText)
	return err
}

func (r *Repository) GetSourceStats(ctx context.Context, sourceID string) (models.SourceStats, error) {
	var stats models.SourceStats
	err := r.pool.QueryRow(ctx, `
		select
			(select count(*) from raw_messages where source_id = $1),
			(select count(*) from documents where source_id = $1),
			(select count(*)
			 from chunks c
			 join documents d on d.id = c.document_id
			 where d.source_id = $1)
	`, sourceID).Scan(&stats.RawMessages, &stats.Documents, &stats.Chunks)
	return stats, err
}

type UpsertRawMessageInput struct {
	SourceID          string
	TelegramMessageID int64
	GroupedID         *int64
	PostedAt          *time.Time
	EditedAt          *time.Time
	TextRaw           string
	CaptionRaw        string
	RawJSON           map[string]any
	MediaJSON         map[string]any
	EntitiesJSON      map[string]any
	ReactionsJSON     map[string]any
	ViewsCount        *int
	ForwardsCount     *int
	ReplyToMessageID  *int64
	ForwardFromName   *string
	ForwardFromChat   *string
	HasMedia          bool
	FetchJobID        *string
}

func (r *Repository) UpsertRawMessage(ctx context.Context, input UpsertRawMessageInput) (models.RawMessage, error) {
	input.TextRaw = sanitizeUTF8String(input.TextRaw)
	input.CaptionRaw = sanitizeUTF8String(input.CaptionRaw)
	input.ForwardFromName = sanitizeNullableUTF8String(input.ForwardFromName)
	input.ForwardFromChat = sanitizeNullableUTF8String(input.ForwardFromChat)

	rawJSON, err := marshalJSON(input.RawJSON)
	if err != nil {
		return models.RawMessage{}, err
	}
	mediaJSON, err := nullableJSON(input.MediaJSON)
	if err != nil {
		return models.RawMessage{}, err
	}
	entitiesJSON, err := nullableJSON(input.EntitiesJSON)
	if err != nil {
		return models.RawMessage{}, err
	}
	reactionsJSON, err := nullableJSON(input.ReactionsJSON)
	if err != nil {
		return models.RawMessage{}, err
	}

	textRaw := nullableString(input.TextRaw)
	captionRaw := nullableString(input.CaptionRaw)

	var out models.RawMessage
	var rawData []byte
	var mediaData []byte
	var entitiesData []byte
	var reactionsData []byte
	err = r.pool.QueryRow(ctx, `
		insert into raw_messages (
			source_id, telegram_message_id, grouped_id, posted_at, edited_at, text_raw, caption_raw,
			raw_json, media_json, entities_json, reactions_json, views_count, forwards_count,
			reply_to_message_id, forward_from_name, forward_from_chat, has_media, fetch_job_id
		)
		values (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13,
			$14, $15, $16, $17, $18
		)
		on conflict (source_id, telegram_message_id) do update
		set grouped_id = excluded.grouped_id,
		    posted_at = excluded.posted_at,
		    edited_at = excluded.edited_at,
		    text_raw = excluded.text_raw,
		    caption_raw = excluded.caption_raw,
		    raw_json = excluded.raw_json,
		    media_json = excluded.media_json,
		    entities_json = excluded.entities_json,
		    reactions_json = excluded.reactions_json,
		    views_count = excluded.views_count,
		    forwards_count = excluded.forwards_count,
		    reply_to_message_id = excluded.reply_to_message_id,
		    forward_from_name = excluded.forward_from_name,
		    forward_from_chat = excluded.forward_from_chat,
		    has_media = excluded.has_media,
		    fetch_job_id = excluded.fetch_job_id,
		    updated_at = now()
		returning id, source_id, telegram_message_id, grouped_id, posted_at, edited_at, text_raw, caption_raw,
		          raw_json, media_json, entities_json, reactions_json, views_count, forwards_count,
		          reply_to_message_id, forward_from_name, forward_from_chat, has_media, fetch_job_id,
		          created_at, updated_at
	`,
		input.SourceID,
		input.TelegramMessageID,
		input.GroupedID,
		input.PostedAt,
		input.EditedAt,
		textRaw,
		captionRaw,
		rawJSON,
		mediaJSON,
		entitiesJSON,
		reactionsJSON,
		input.ViewsCount,
		input.ForwardsCount,
		input.ReplyToMessageID,
		input.ForwardFromName,
		input.ForwardFromChat,
		input.HasMedia,
		input.FetchJobID,
	).Scan(
		&out.ID,
		&out.SourceID,
		&out.TelegramMessageID,
		&out.GroupedID,
		&out.PostedAt,
		&out.EditedAt,
		&out.TextRaw,
		&out.CaptionRaw,
		&rawData,
		&mediaData,
		&entitiesData,
		&reactionsData,
		&out.ViewsCount,
		&out.ForwardsCount,
		&out.ReplyToMessageID,
		&out.ForwardFromName,
		&out.ForwardFromChat,
		&out.HasMedia,
		&out.FetchJobID,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if err != nil {
		return models.RawMessage{}, err
	}

	out.RawJSON = string(rawData)
	if len(mediaData) > 0 {
		v := string(mediaData)
		out.MediaJSON = &v
	}
	if len(entitiesData) > 0 {
		v := string(entitiesData)
		out.EntitiesJSON = &v
	}
	if len(reactionsData) > 0 {
		v := string(reactionsData)
		out.ReactionsJSON = &v
	}
	return out, nil
}

func (r *Repository) ListRawMessagesBySource(ctx context.Context, sourceID string, limit int) ([]models.RawMessage, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := r.pool.Query(ctx, `
		select id, source_id, telegram_message_id, grouped_id, posted_at, edited_at, text_raw, caption_raw,
		       raw_json, media_json, entities_json, reactions_json, views_count, forwards_count,
		       reply_to_message_id, forward_from_name, forward_from_chat, has_media, fetch_job_id,
		       created_at, updated_at
		from raw_messages
		where source_id = $1
		order by telegram_message_id desc
		limit $2
	`, sourceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.RawMessage, 0, limit)
	for rows.Next() {
		item, err := scanRawMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) ListTelegramMessageLinksBySource(ctx context.Context, sourceID string) ([]models.TelegramMessageLink, error) {
	rows, err := r.pool.Query(ctx, `
		select id, source_id, link_order, original_url, canonical_url, telegram_channel_id, username, telegram_message_id, created_at
		from telegram_message_links
		where source_id = $1
		order by link_order asc, created_at asc
	`, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.TelegramMessageLink, 0)
	for rows.Next() {
		var item models.TelegramMessageLink
		if err := rows.Scan(
			&item.ID,
			&item.SourceID,
			&item.LinkOrder,
			&item.OriginalURL,
			&item.CanonicalURL,
			&item.TelegramChannelID,
			&item.Username,
			&item.TelegramMessageID,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) FindCanonicalDocumentByHash(ctx context.Context, sourceID, contentHash, externalDocID string) (*models.Document, error) {
	var doc models.Document
	var tagsRaw []byte
	var linksRaw []byte
	var metadataRaw []byte

	err := r.pool.QueryRow(ctx, `
		select id, source_id, raw_message_id, external_doc_id, title, text_clean, text_original, summary,
		       language_code, content_hash, simhash, dedupe_group, quality_score, is_duplicate, duplicate_of,
		       is_forward, forward_source, tags, links, metadata, published_at, indexed_at, created_at, updated_at
		from documents
		where source_id = $1
		  and content_hash = $2
		  and external_doc_id <> $3
		  and is_duplicate = false
		order by created_at asc
		limit 1
	`, sourceID, contentHash, externalDocID).Scan(
		&doc.ID,
		&doc.SourceID,
		&doc.RawMessageID,
		&doc.ExternalDocID,
		&doc.Title,
		&doc.TextClean,
		&doc.TextOriginal,
		&doc.Summary,
		&doc.LanguageCode,
		&doc.ContentHash,
		&doc.Simhash,
		&doc.DedupeGroup,
		&doc.QualityScore,
		&doc.IsDuplicate,
		&doc.DuplicateOf,
		&doc.IsForward,
		&doc.ForwardSource,
		&tagsRaw,
		&linksRaw,
		&metadataRaw,
		&doc.PublishedAt,
		&doc.IndexedAt,
		&doc.CreatedAt,
		&doc.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	doc.Tags = decodeStringArray(tagsRaw)
	doc.Links = decodeStringArray(linksRaw)
	doc.Metadata = decodeMap(metadataRaw)
	return &doc, nil
}

type SaveDocumentInput struct {
	SourceID      string
	RawMessageID  *string
	ExternalDocID string
	Title         *string
	TextClean     string
	TextOriginal  *string
	Summary       *string
	LanguageCode  *string
	ContentHash   string
	Simhash       *string
	DedupeGroup   *string
	QualityScore  *float64
	IsDuplicate   bool
	DuplicateOf   *string
	IsForward     bool
	ForwardSource *string
	Tags          []string
	Links         []string
	Metadata      map[string]any
	PublishedAt   *time.Time
}

func (r *Repository) SaveDocument(ctx context.Context, input SaveDocumentInput) (models.Document, error) {
	input.ExternalDocID = sanitizeUTF8String(input.ExternalDocID)
	input.Title = sanitizeNullableUTF8String(input.Title)
	input.TextClean = sanitizeUTF8String(input.TextClean)
	input.TextOriginal = sanitizeNullableUTF8String(input.TextOriginal)
	input.Summary = sanitizeNullableUTF8String(input.Summary)
	input.LanguageCode = sanitizeNullableUTF8String(input.LanguageCode)
	input.ContentHash = sanitizeUTF8String(input.ContentHash)
	input.Simhash = sanitizeNullableUTF8String(input.Simhash)
	input.DedupeGroup = sanitizeNullableUTF8String(input.DedupeGroup)
	input.ForwardSource = sanitizeNullableUTF8String(input.ForwardSource)
	input.Tags = sanitizeUTF8Strings(input.Tags)
	input.Links = sanitizeUTF8Strings(input.Links)

	tagsJSON, err := marshalJSON(input.Tags)
	if err != nil {
		return models.Document{}, err
	}
	linksJSON, err := marshalJSON(input.Links)
	if err != nil {
		return models.Document{}, err
	}
	metadataJSON, err := marshalJSON(input.Metadata)
	if err != nil {
		return models.Document{}, err
	}

	var out models.Document
	var tagsRaw []byte
	var linksRaw []byte
	var metadataRaw []byte
	err = r.pool.QueryRow(ctx, `
		insert into documents (
			source_id, raw_message_id, external_doc_id, title, text_clean, text_original, summary,
			language_code, content_hash, simhash, dedupe_group, quality_score, is_duplicate, duplicate_of,
			is_forward, forward_source, tags, links, metadata, published_at
		)
		values (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13, $14,
			$15, $16, $17, $18, $19, $20
		)
		on conflict (external_doc_id) do update
		set source_id = excluded.source_id,
		    raw_message_id = excluded.raw_message_id,
		    title = excluded.title,
		    text_clean = excluded.text_clean,
		    text_original = excluded.text_original,
		    summary = excluded.summary,
		    language_code = excluded.language_code,
		    content_hash = excluded.content_hash,
		    simhash = excluded.simhash,
		    dedupe_group = excluded.dedupe_group,
		    quality_score = excluded.quality_score,
		    is_duplicate = excluded.is_duplicate,
		    duplicate_of = excluded.duplicate_of,
		    is_forward = excluded.is_forward,
		    forward_source = excluded.forward_source,
		    tags = excluded.tags,
		    links = excluded.links,
		    metadata = excluded.metadata,
		    published_at = excluded.published_at,
		    updated_at = now()
		returning id, source_id, raw_message_id, external_doc_id, title, text_clean, text_original, summary,
		          language_code, content_hash, simhash, dedupe_group, quality_score, is_duplicate, duplicate_of,
		          is_forward, forward_source, tags, links, metadata, published_at, indexed_at, created_at, updated_at
	`,
		input.SourceID,
		input.RawMessageID,
		input.ExternalDocID,
		input.Title,
		input.TextClean,
		input.TextOriginal,
		input.Summary,
		input.LanguageCode,
		input.ContentHash,
		input.Simhash,
		input.DedupeGroup,
		input.QualityScore,
		input.IsDuplicate,
		input.DuplicateOf,
		input.IsForward,
		input.ForwardSource,
		tagsJSON,
		linksJSON,
		metadataJSON,
		input.PublishedAt,
	).Scan(
		&out.ID,
		&out.SourceID,
		&out.RawMessageID,
		&out.ExternalDocID,
		&out.Title,
		&out.TextClean,
		&out.TextOriginal,
		&out.Summary,
		&out.LanguageCode,
		&out.ContentHash,
		&out.Simhash,
		&out.DedupeGroup,
		&out.QualityScore,
		&out.IsDuplicate,
		&out.DuplicateOf,
		&out.IsForward,
		&out.ForwardSource,
		&tagsRaw,
		&linksRaw,
		&metadataRaw,
		&out.PublishedAt,
		&out.IndexedAt,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if err != nil {
		return models.Document{}, err
	}

	out.Tags = decodeStringArray(tagsRaw)
	out.Links = decodeStringArray(linksRaw)
	out.Metadata = decodeMap(metadataRaw)
	return out, nil
}

func (r *Repository) DeleteChunksByDocumentID(ctx context.Context, documentID string) error {
	_, err := r.pool.Exec(ctx, `delete from chunks where document_id = $1`, documentID)
	return err
}

func (r *Repository) DeleteDocumentByExternalDocID(ctx context.Context, externalDocID string) error {
	_, err := r.pool.Exec(ctx, `delete from documents where external_doc_id = $1`, externalDocID)
	return err
}

func (r *Repository) InsertChunks(ctx context.Context, documentID string, sourceID string, rawMessageID *string, chunks []chunking.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}
	for _, item := range chunks {
		item.Text = sanitizeUTF8String(item.Text)
		metadata := map[string]any{
			"source_id":      sourceID,
			"raw_message_id": rawMessageID,
			"chunk_index":    item.Index,
		}
		for key, value := range item.Metadata {
			metadata[key] = value
		}

		metadataJSON, err := marshalJSON(metadata)
		if err != nil {
			return err
		}
		charCount := item.CharCount
		tokenCount := item.TokenCount
		if _, err := r.pool.Exec(ctx, `
			insert into chunks (document_id, chunk_index, text, token_count, char_count, metadata, embedding_status)
			values ($1, $2, $3, $4, $5, $6, 'pending')
			on conflict (document_id, chunk_index) do update
			set text = excluded.text,
			    token_count = excluded.token_count,
			    char_count = excluded.char_count,
			    metadata = excluded.metadata,
			    updated_at = now()
		`, documentID, item.Index, item.Text, tokenCount, charCount, metadataJSON); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ListDocumentsBySource(ctx context.Context, sourceID string, limit int) ([]models.Document, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := r.pool.Query(ctx, `
		select id, source_id, raw_message_id, external_doc_id, title, text_clean, text_original, summary,
		       language_code, content_hash, simhash, dedupe_group, quality_score, is_duplicate, duplicate_of,
		       is_forward, forward_source, tags, links, metadata, published_at, indexed_at, created_at, updated_at
		from documents
		where source_id = $1
		order by created_at desc
		limit $2
	`, sourceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.Document, 0, limit)
	for rows.Next() {
		item, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) ListParsedPostsBySources(
	ctx context.Context,
	sourceIDs []string,
	limitPerSource int,
	includeDuplicates bool,
) ([]models.ParsedPost, error) {
	if len(sourceIDs) == 0 {
		return []models.ParsedPost{}, nil
	}
	if limitPerSource <= 0 {
		limitPerSource = 100
	}
	if limitPerSource > 2000 {
		limitPerSource = 2000
	}

	rows, err := r.pool.Query(ctx, `
		with ranked as (
			select d.id,
			       d.source_id,
			       d.raw_message_id,
			       d.external_doc_id,
			       d.text_clean,
			       d.text_original,
			       d.language_code,
			       d.tags,
			       d.links,
			       d.metadata,
			       d.is_duplicate,
			       d.is_forward,
			       d.quality_score,
			       d.published_at,
			       d.created_at,
			       coalesce(s.username, '') as channel_username,
			       s.title as channel_title,
			       coalesce(s.url, '') as channel_url,
			       coalesce(d.metadata->>'url', '') as post_url,
			       rm.telegram_message_id,
			       row_number() over (
			           partition by d.source_id
			           order by coalesce(d.published_at, d.created_at) desc, d.created_at desc
			       ) as rn
			from documents d
			join sources s on s.id = d.source_id
			join raw_messages rm on rm.id = d.raw_message_id
			where d.source_id::text = any($1::text[])
			  and ($2::boolean or d.is_duplicate = false)
		)
		select id,
		       source_id,
		       raw_message_id,
		       external_doc_id,
		       text_clean,
		       text_original,
		       language_code,
		       tags,
		       links,
		       metadata,
		       is_duplicate,
		       is_forward,
		       quality_score,
		       published_at,
		       created_at,
		       channel_username,
		       channel_title,
		       channel_url,
		       post_url,
		       telegram_message_id
		from ranked
		where rn <= $3
		order by coalesce(published_at, created_at) desc, created_at desc
	`, sourceIDs, includeDuplicates, limitPerSource)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.ParsedPost, 0, len(sourceIDs)*limitPerSource)
	for rows.Next() {
		var item models.ParsedPost
		var tagsRaw []byte
		var linksRaw []byte
		var metadataRaw []byte
		if err := rows.Scan(
			&item.DocumentID,
			&item.SourceID,
			&item.RawMessageID,
			&item.ExternalDocID,
			&item.TextClean,
			&item.TextOriginal,
			&item.LanguageCode,
			&tagsRaw,
			&linksRaw,
			&metadataRaw,
			&item.IsDuplicate,
			&item.IsForward,
			&item.QualityScore,
			&item.PublishedAt,
			&item.CreatedAt,
			&item.ChannelUsername,
			&item.ChannelTitle,
			&item.ChannelURL,
			&item.PostURL,
			&item.TelegramMessageID,
		); err != nil {
			return nil, err
		}
		item.Hashtags = decodeStringArray(tagsRaw)
		item.Links = decodeStringArray(linksRaw)
		item.Metadata = decodeMap(metadataRaw)
		item.Mentions = extractStringSliceFromMap(item.Metadata, "mentions")
		if item.Hashtags == nil {
			item.Hashtags = []string{}
		}
		if item.Mentions == nil {
			item.Mentions = []string{}
		}
		if item.Links == nil {
			item.Links = []string{}
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) GetDocumentByID(ctx context.Context, documentID string) (models.Document, error) {
	row := r.pool.QueryRow(ctx, `
		select id, source_id, raw_message_id, external_doc_id, title, text_clean, text_original, summary,
		       language_code, content_hash, simhash, dedupe_group, quality_score, is_duplicate, duplicate_of,
		       is_forward, forward_source, tags, links, metadata, published_at, indexed_at, created_at, updated_at
		from documents
		where id = $1
	`, documentID)

	return scanDocumentRow(row)
}

func (r *Repository) ListChunksByDocumentID(ctx context.Context, documentID string) ([]models.Chunk, error) {
	rows, err := r.pool.Query(ctx, `
		select id, document_id, chunk_index, text, token_count, char_count, metadata,
		       embedding_status, embedding_model, embedding_error, qdrant_point_id, created_at, updated_at
		from chunks
		where document_id = $1
		order by chunk_index asc
	`, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.Chunk
	for rows.Next() {
		var chunk models.Chunk
		var metadataRaw []byte
		if err := rows.Scan(
			&chunk.ID,
			&chunk.DocumentID,
			&chunk.ChunkIndex,
			&chunk.Text,
			&chunk.TokenCount,
			&chunk.CharCount,
			&metadataRaw,
			&chunk.EmbeddingStatus,
			&chunk.EmbeddingModel,
			&chunk.EmbeddingError,
			&chunk.QdrantPointID,
			&chunk.CreatedAt,
			&chunk.UpdatedAt,
		); err != nil {
			return nil, err
		}
		chunk.Metadata = decodeMap(metadataRaw)
		out = append(out, chunk)
	}
	return out, rows.Err()
}

type CreateJobInput struct {
	JobType       string
	SourceID      *string
	Status        string
	Payload       map[string]any
	ProgressTotal int
}

func (r *Repository) CreateJob(ctx context.Context, input CreateJobInput) (models.Job, error) {
	payloadJSON, err := marshalJSON(input.Payload)
	if err != nil {
		return models.Job{}, err
	}

	var job models.Job
	var payloadRaw []byte
	var resultRaw []byte
	err = r.pool.QueryRow(ctx, `
		insert into jobs (job_type, source_id, status, payload, progress_total, progress_done)
		values ($1, $2, $3, $4, $5, 0)
		returning id, job_type, source_id, status, payload, progress_total, progress_done, result_json,
		          error_text, started_at, finished_at, created_at
	`, input.JobType, input.SourceID, input.Status, payloadJSON, input.ProgressTotal).Scan(
		&job.ID,
		&job.JobType,
		&job.SourceID,
		&job.Status,
		&payloadRaw,
		&job.ProgressTotal,
		&job.ProgressDone,
		&resultRaw,
		&job.ErrorText,
		&job.StartedAt,
		&job.FinishedAt,
		&job.CreatedAt,
	)
	if err != nil {
		return models.Job{}, err
	}
	job.Payload = decodeMap(payloadRaw)
	job.ResultJSON = decodeMap(resultRaw)
	return job, nil
}

func (r *Repository) UpdateJobRunning(ctx context.Context, jobID string) error {
	_, err := r.pool.Exec(ctx, `
		update jobs
		set status = 'running',
		    started_at = coalesce(started_at, now())
		where id = $1
	`, jobID)
	return err
}

func (r *Repository) UpdateJobProgress(ctx context.Context, jobID string, done, total int) error {
	_, err := r.pool.Exec(ctx, `
		update jobs
		set progress_done = $2,
		    progress_total = $3
		where id = $1
	`, jobID, done, total)
	return err
}

func (r *Repository) CompleteJob(ctx context.Context, jobID string, result map[string]any) error {
	resultJSON, err := marshalJSON(result)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		update jobs
		set status = 'succeeded',
		    result_json = $2,
		    finished_at = now()
		where id = $1
	`, jobID, resultJSON)
	return err
}

func (r *Repository) FailJob(ctx context.Context, jobID string, errText string, result map[string]any) error {
	resultJSON, err := marshalJSON(result)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		update jobs
		set status = 'failed',
		    error_text = $2,
		    result_json = $3,
		    finished_at = now()
		where id = $1
	`, jobID, errText, resultJSON)
	return err
}

func (r *Repository) ListJobs(ctx context.Context, limit int) ([]models.Job, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		select id, job_type, source_id, status, payload, progress_total, progress_done, result_json,
		       error_text, started_at, finished_at, created_at
		from jobs
		order by created_at desc
		limit $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.Job
	for rows.Next() {
		var job models.Job
		var payloadRaw []byte
		var resultRaw []byte
		if err := rows.Scan(
			&job.ID,
			&job.JobType,
			&job.SourceID,
			&job.Status,
			&payloadRaw,
			&job.ProgressTotal,
			&job.ProgressDone,
			&resultRaw,
			&job.ErrorText,
			&job.StartedAt,
			&job.FinishedAt,
			&job.CreatedAt,
		); err != nil {
			return nil, err
		}
		job.Payload = decodeMap(payloadRaw)
		job.ResultJSON = decodeMap(resultRaw)
		out = append(out, job)
	}
	return out, rows.Err()
}

func (r *Repository) FailActiveJobsBySourceID(ctx context.Context, sourceID string, errText string) (int64, error) {
	resultJSON, err := marshalJSON(map[string]any{
		"error": errText,
	})
	if err != nil {
		return 0, err
	}

	tag, err := r.pool.Exec(ctx, `
		update jobs
		set status = 'failed',
		    error_text = $2,
		    result_json = $3,
		    finished_at = now()
		where source_id = $1
		  and status in ('queued', 'running')
	`, sourceID, errText, resultJSON)
	if err != nil {
		return 0, err
	}

	return tag.RowsAffected(), nil
}

func (r *Repository) DeleteJobsBySourceID(ctx context.Context, sourceID string) (int64, error) {
	tag, err := r.pool.Exec(ctx, `delete from jobs where source_id = $1`, sourceID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

type CreateExportInput struct {
	ExportType string
	SourceID   *string
	Status     string
}

func (r *Repository) CreateExport(ctx context.Context, input CreateExportInput) (models.Export, error) {
	var out models.Export
	err := r.pool.QueryRow(ctx, `
		insert into exports (export_type, source_id, status)
		values ($1, $2, $3)
		returning id, export_type, status, source_id, file_path, row_count, error_text, created_at, finished_at
	`, input.ExportType, input.SourceID, input.Status).Scan(
		&out.ID,
		&out.ExportType,
		&out.Status,
		&out.SourceID,
		&out.FilePath,
		&out.RowCount,
		&out.ErrorText,
		&out.CreatedAt,
		&out.FinishedAt,
	)
	return out, err
}

func (r *Repository) CompleteExport(ctx context.Context, exportID string, filePath string, rowCount int) error {
	_, err := r.pool.Exec(ctx, `
		update exports
		set status = 'succeeded',
		    file_path = $2,
		    row_count = $3,
		    finished_at = now()
		where id = $1
	`, exportID, filePath, rowCount)
	return err
}

func (r *Repository) FailExport(ctx context.Context, exportID string, errText string) error {
	_, err := r.pool.Exec(ctx, `
		update exports
		set status = 'failed',
		    error_text = $2,
		    finished_at = now()
		where id = $1
	`, exportID, errText)
	return err
}

func (r *Repository) ListExports(ctx context.Context, limit int) ([]models.Export, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		select id, export_type, status, source_id, file_path, row_count, error_text, created_at, finished_at
		from exports
		order by created_at desc
		limit $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.Export
	for rows.Next() {
		var item models.Export
		if err := rows.Scan(
			&item.ID,
			&item.ExportType,
			&item.Status,
			&item.SourceID,
			&item.FilePath,
			&item.RowCount,
			&item.ErrorText,
			&item.CreatedAt,
			&item.FinishedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) GetExportByID(ctx context.Context, exportID string) (models.Export, error) {
	var item models.Export
	err := r.pool.QueryRow(ctx, `
		select id, export_type, status, source_id, file_path, row_count, error_text, created_at, finished_at
		from exports
		where id = $1
	`, exportID).Scan(
		&item.ID,
		&item.ExportType,
		&item.Status,
		&item.SourceID,
		&item.FilePath,
		&item.RowCount,
		&item.ErrorText,
		&item.CreatedAt,
		&item.FinishedAt,
	)
	return item, err
}

type ExportDocumentRow struct {
	DocID           string
	Text            string
	ChannelUsername string
	MessageID       int64
	URL             string
	PublishedAt     *time.Time
	LanguageCode    *string
	IsForward       bool
	QualityScore    *float64
	Metadata        map[string]any
}

func (r *Repository) ListExportDocuments(ctx context.Context, sourceID *string, includeDuplicates bool) ([]ExportDocumentRow, error) {
	query := `
		select d.external_doc_id,
		       d.text_clean,
		       coalesce(s.username, d.metadata->>'channel_username', ''),
		       coalesce(rm.telegram_message_id, 0),
		       coalesce(d.metadata->>'url', s.url, ''),
		       d.published_at,
		       d.language_code,
		       d.is_forward,
		       d.quality_score,
		       d.metadata
		from documents d
		join sources s on s.id = d.source_id
		left join raw_messages rm on rm.id = d.raw_message_id
		where 1=1
	`
	args := []any{}
	if !includeDuplicates {
		query += ` and d.is_duplicate = false`
	}
	if sourceID != nil {
		query += ` and d.source_id = $1`
		args = append(args, *sourceID)
	}
	query += ` order by d.created_at asc`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ExportDocumentRow
	for rows.Next() {
		var item ExportDocumentRow
		var metadataRaw []byte
		if err := rows.Scan(
			&item.DocID,
			&item.Text,
			&item.ChannelUsername,
			&item.MessageID,
			&item.URL,
			&item.PublishedAt,
			&item.LanguageCode,
			&item.IsForward,
			&item.QualityScore,
			&metadataRaw,
		); err != nil {
			return nil, err
		}
		item.Metadata = decodeMap(metadataRaw)
		out = append(out, item)
	}
	return out, rows.Err()
}

type ExportChunkRow struct {
	DocID           string
	ChunkIndex      int
	Text            string
	ChannelUsername string
	MessageID       int64
	URL             string
	PublishedAt     *time.Time
	LanguageCode    *string
	Metadata        map[string]any
}

func (r *Repository) ListExportChunks(ctx context.Context, sourceID *string, includeDuplicates bool) ([]ExportChunkRow, error) {
	query := `
		select d.external_doc_id,
		       c.chunk_index,
		       c.text,
		       coalesce(s.username, d.metadata->>'channel_username', ''),
		       coalesce(rm.telegram_message_id, 0),
		       coalesce(d.metadata->>'url', s.url, ''),
		       d.published_at,
		       d.language_code,
		       c.metadata
		from chunks c
		join documents d on d.id = c.document_id
		join sources s on s.id = d.source_id
		left join raw_messages rm on rm.id = d.raw_message_id
		where 1=1
	`
	args := []any{}
	if !includeDuplicates {
		query += ` and d.is_duplicate = false`
	}
	if sourceID != nil {
		query += ` and d.source_id = $1`
		args = append(args, *sourceID)
	}
	query += ` order by d.created_at asc, c.chunk_index asc`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ExportChunkRow
	for rows.Next() {
		var item ExportChunkRow
		var metadataRaw []byte
		if err := rows.Scan(
			&item.DocID,
			&item.ChunkIndex,
			&item.Text,
			&item.ChannelUsername,
			&item.MessageID,
			&item.URL,
			&item.PublishedAt,
			&item.LanguageCode,
			&metadataRaw,
		); err != nil {
			return nil, err
		}
		item.Metadata = decodeMap(metadataRaw)
		out = append(out, item)
	}
	return out, rows.Err()
}

type ExportTrashRawRow struct {
	SourceID        string
	ChannelUsername string
	MessageID       int64
	TextRaw         string
	CaptionRaw      string
	PublishedAt     *time.Time
	IsForward       bool
	SourceURL       string
	RawJSON         map[string]any
}

func (r *Repository) ListExportTrashRawMessages(ctx context.Context, sourceID *string) ([]ExportTrashRawRow, error) {
	query := `
		select rm.source_id::text,
		       coalesce(s.username, ''),
		       rm.telegram_message_id,
		       coalesce(rm.text_raw, ''),
		       coalesce(rm.caption_raw, ''),
		       rm.posted_at,
		       (rm.forward_from_name is not null or rm.forward_from_chat is not null),
		       coalesce(s.url, ''),
		       rm.raw_json
		from raw_messages rm
		join sources s on s.id = rm.source_id
		left join documents d on d.raw_message_id = rm.id
		where d.id is null
		  and (coalesce(rm.text_raw, '') <> '' or coalesce(rm.caption_raw, '') <> '')
	`
	args := []any{}
	if sourceID != nil {
		query += ` and rm.source_id = $1`
		args = append(args, *sourceID)
	}
	query += ` order by rm.created_at asc`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ExportTrashRawRow, 0)
	for rows.Next() {
		var item ExportTrashRawRow
		var rawJSON []byte
		if err := rows.Scan(
			&item.SourceID,
			&item.ChannelUsername,
			&item.MessageID,
			&item.TextRaw,
			&item.CaptionRaw,
			&item.PublishedAt,
			&item.IsForward,
			&item.SourceURL,
			&rawJSON,
		); err != nil {
			return nil, err
		}
		item.RawJSON = decodeMap(rawJSON)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) ListSourcesByType(ctx context.Context, sourceType string) ([]models.Source, error) {
	rows, err := r.pool.Query(ctx, `
		select id, source_type, provider, external_id, telegram_channel_id, telegram_access_hash, username, title, url, status,
		       last_message_id, last_synced_at, last_error, created_at, updated_at
		from sources
		where source_type = $1
		order by created_at desc
	`, sourceType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.Source, 0)
	for rows.Next() {
		item, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) UpdateSourceStatus(ctx context.Context, sourceID string, status string, lastError *string) error {
	_, err := r.pool.Exec(ctx, `
		update sources
		set status = $2,
		    last_error = $3,
		    updated_at = now()
		where id = $1
	`, sourceID, status, lastError)
	return err
}

func (r *Repository) UpdateSourceTitleIfEmpty(ctx context.Context, sourceID string, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil
	}

	_, err := r.pool.Exec(ctx, `
		update sources
		set title = $2,
		    updated_at = now()
		where id = $1
		  and (title is null or btrim(title) = '')
	`, sourceID, title)
	return err
}

func (r *Repository) CompleteJobDone(ctx context.Context, jobID string, result map[string]any) error {
	resultJSON, err := marshalJSON(result)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		update jobs
		set status = 'done',
		    result_json = $2,
		    finished_at = now()
		where id = $1
	`, jobID, resultJSON)
	return err
}

type SaveYouTubeAudioArtifactInput struct {
	SourceID      string
	Provider      string
	VideoID       string
	AudioFilePath *string
	AudioStatus   string
	RawJSON       map[string]any
	ErrorText     *string
}

func (r *Repository) SaveYouTubeAudioArtifact(ctx context.Context, input SaveYouTubeAudioArtifactInput) (models.YouTubeAudioArtifact, error) {
	input.Provider = sanitizeUTF8String(input.Provider)
	input.VideoID = sanitizeUTF8String(input.VideoID)
	input.AudioFilePath = sanitizeNullableUTF8String(input.AudioFilePath)
	input.AudioStatus = sanitizeUTF8String(input.AudioStatus)
	input.ErrorText = sanitizeNullableUTF8String(input.ErrorText)

	rawJSON, err := marshalJSON(input.RawJSON)
	if err != nil {
		return models.YouTubeAudioArtifact{}, err
	}

	var out models.YouTubeAudioArtifact
	var rawJSONOut []byte
	err = r.pool.QueryRow(ctx, `
		insert into youtube_audio_artifacts (
			source_id, provider, video_id, audio_file_path, audio_status, raw_json, error_text
		)
		values ($1, $2, $3, $4, $5, $6, $7)
		on conflict (source_id) do update
		set provider = excluded.provider,
		    video_id = excluded.video_id,
		    audio_file_path = excluded.audio_file_path,
		    audio_status = excluded.audio_status,
		    raw_json = excluded.raw_json,
		    error_text = excluded.error_text,
		    updated_at = now()
		returning id, source_id, provider, video_id, audio_file_path, audio_status, raw_json,
		          error_text, created_at, updated_at
	`,
		input.SourceID,
		input.Provider,
		input.VideoID,
		input.AudioFilePath,
		input.AudioStatus,
		rawJSON,
		input.ErrorText,
	).Scan(
		&out.ID,
		&out.SourceID,
		&out.Provider,
		&out.VideoID,
		&out.AudioFilePath,
		&out.AudioStatus,
		&rawJSONOut,
		&out.ErrorText,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if err != nil {
		return models.YouTubeAudioArtifact{}, err
	}
	out.RawJSON = decodeMap(rawJSONOut)
	return out, nil
}

func (r *Repository) GetYouTubeAudioArtifactBySource(ctx context.Context, sourceID string) (models.YouTubeAudioArtifact, error) {
	var out models.YouTubeAudioArtifact
	var rawJSON []byte
	err := r.pool.QueryRow(ctx, `
		select id, source_id, provider, video_id, audio_file_path, audio_status, raw_json,
		       error_text, created_at, updated_at
		from youtube_audio_artifacts
		where source_id = $1
		order by updated_at desc
		limit 1
	`, sourceID).Scan(
		&out.ID,
		&out.SourceID,
		&out.Provider,
		&out.VideoID,
		&out.AudioFilePath,
		&out.AudioStatus,
		&rawJSON,
		&out.ErrorText,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if err != nil {
		return models.YouTubeAudioArtifact{}, err
	}
	out.RawJSON = decodeMap(rawJSON)
	return out, nil
}

func (r *Repository) ListChunksBySourceID(ctx context.Context, sourceID string, limit int) ([]models.Chunk, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := r.pool.Query(ctx, `
		select c.id, c.document_id, c.chunk_index, c.text, c.token_count, c.char_count, c.metadata,
		       c.embedding_status, c.embedding_model, c.embedding_error, c.qdrant_point_id, c.created_at, c.updated_at
		from chunks c
		join documents d on d.id = c.document_id
		where d.source_id = $1
		order by d.created_at desc, c.chunk_index asc
		limit $2
	`, sourceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.Chunk, 0, limit)
	for rows.Next() {
		var chunk models.Chunk
		var metadataRaw []byte
		if err := rows.Scan(
			&chunk.ID,
			&chunk.DocumentID,
			&chunk.ChunkIndex,
			&chunk.Text,
			&chunk.TokenCount,
			&chunk.CharCount,
			&metadataRaw,
			&chunk.EmbeddingStatus,
			&chunk.EmbeddingModel,
			&chunk.EmbeddingError,
			&chunk.QdrantPointID,
			&chunk.CreatedAt,
			&chunk.UpdatedAt,
		); err != nil {
			return nil, err
		}
		chunk.Metadata = decodeMap(metadataRaw)
		out = append(out, chunk)
	}
	return out, rows.Err()
}

func scanSource(row pgx.Row) (models.Source, error) {
	var source models.Source
	err := row.Scan(
		&source.ID,
		&source.SourceType,
		&source.Provider,
		&source.ExternalID,
		&source.TelegramChannelID,
		&source.TelegramAccessHash,
		&source.Username,
		&source.Title,
		&source.URL,
		&source.Status,
		&source.LastMessageID,
		&source.LastSyncedAt,
		&source.LastError,
		&source.CreatedAt,
		&source.UpdatedAt,
	)
	if err != nil {
		return models.Source{}, err
	}
	return source, nil
}

func scanRawMessage(rows pgx.Row) (models.RawMessage, error) {
	var out models.RawMessage
	var rawData []byte
	var mediaData []byte
	var entitiesData []byte
	var reactionsData []byte
	err := rows.Scan(
		&out.ID,
		&out.SourceID,
		&out.TelegramMessageID,
		&out.GroupedID,
		&out.PostedAt,
		&out.EditedAt,
		&out.TextRaw,
		&out.CaptionRaw,
		&rawData,
		&mediaData,
		&entitiesData,
		&reactionsData,
		&out.ViewsCount,
		&out.ForwardsCount,
		&out.ReplyToMessageID,
		&out.ForwardFromName,
		&out.ForwardFromChat,
		&out.HasMedia,
		&out.FetchJobID,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if err != nil {
		return models.RawMessage{}, err
	}
	out.RawJSON = string(rawData)
	if len(mediaData) > 0 {
		v := string(mediaData)
		out.MediaJSON = &v
	}
	if len(entitiesData) > 0 {
		v := string(entitiesData)
		out.EntitiesJSON = &v
	}
	if len(reactionsData) > 0 {
		v := string(reactionsData)
		out.ReactionsJSON = &v
	}
	return out, nil
}

func scanDocument(rows pgx.Row) (models.Document, error) {
	var out models.Document
	var tagsRaw []byte
	var linksRaw []byte
	var metadataRaw []byte
	err := rows.Scan(
		&out.ID,
		&out.SourceID,
		&out.RawMessageID,
		&out.ExternalDocID,
		&out.Title,
		&out.TextClean,
		&out.TextOriginal,
		&out.Summary,
		&out.LanguageCode,
		&out.ContentHash,
		&out.Simhash,
		&out.DedupeGroup,
		&out.QualityScore,
		&out.IsDuplicate,
		&out.DuplicateOf,
		&out.IsForward,
		&out.ForwardSource,
		&tagsRaw,
		&linksRaw,
		&metadataRaw,
		&out.PublishedAt,
		&out.IndexedAt,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if err != nil {
		return models.Document{}, err
	}
	out.Tags = decodeStringArray(tagsRaw)
	out.Links = decodeStringArray(linksRaw)
	out.Metadata = decodeMap(metadataRaw)
	return out, nil
}

func scanDocumentRow(row pgx.Row) (models.Document, error) {
	return scanDocument(row)
}

func marshalJSON(value any) ([]byte, error) {
	if value == nil {
		return []byte(`{}`), nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func nullableJSON(value any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	return json.Marshal(value)
}

func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func decodeMap(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func decodeStringArray(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	out := []string{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func extractStringSliceFromMap(input map[string]any, key string) []string {
	if len(input) == 0 {
		return nil
	}
	raw, ok := input[key]
	if !ok {
		return nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		str, ok := value.(string)
		if !ok || str == "" {
			continue
		}
		out = append(out, str)
	}
	return out
}

func sanitizeUTF8String(value string) string {
	if value == "" {
		return value
	}
	return strings.ToValidUTF8(value, "")
}

func sanitizeNullableUTF8String(value *string) *string {
	if value == nil {
		return nil
	}
	clean := sanitizeUTF8String(*value)
	if clean == "" {
		return nil
	}
	return &clean
}

func sanitizeUTF8Strings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		clean := sanitizeUTF8String(value)
		if clean == "" {
			continue
		}
		out = append(out, clean)
	}
	return out
}
