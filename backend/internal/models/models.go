package models

import "time"

type Source struct {
	ID                 string     `json:"id"`
	SourceType         string     `json:"source_type"`
	Provider           *string    `json:"provider,omitempty"`
	ExternalID         *string    `json:"external_id,omitempty"`
	TelegramChannelID  *int64     `json:"telegram_channel_id,omitempty"`
	TelegramAccessHash *int64     `json:"telegram_access_hash,omitempty"`
	Username           *string    `json:"username,omitempty"`
	Title              *string    `json:"title,omitempty"`
	URL                string     `json:"url"`
	Status             string     `json:"status"`
	LastMessageID      *int64     `json:"last_message_id,omitempty"`
	LastSyncedAt       *time.Time `json:"last_synced_at,omitempty"`
	LastError          *string    `json:"last_error,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type RawMessage struct {
	ID                string     `json:"id"`
	SourceID          string     `json:"source_id"`
	TelegramMessageID int64      `json:"telegram_message_id"`
	GroupedID         *int64     `json:"grouped_id,omitempty"`
	PostedAt          *time.Time `json:"posted_at,omitempty"`
	EditedAt          *time.Time `json:"edited_at,omitempty"`
	TextRaw           *string    `json:"text_raw,omitempty"`
	CaptionRaw        *string    `json:"caption_raw,omitempty"`
	RawJSON           string     `json:"raw_json"`
	MediaJSON         *string    `json:"media_json,omitempty"`
	EntitiesJSON      *string    `json:"entities_json,omitempty"`
	ReactionsJSON     *string    `json:"reactions_json,omitempty"`
	ViewsCount        *int       `json:"views_count,omitempty"`
	ForwardsCount     *int       `json:"forwards_count,omitempty"`
	ReplyToMessageID  *int64     `json:"reply_to_message_id,omitempty"`
	ForwardFromName   *string    `json:"forward_from_name,omitempty"`
	ForwardFromChat   *string    `json:"forward_from_chat,omitempty"`
	HasMedia          bool       `json:"has_media"`
	FetchJobID        *string    `json:"fetch_job_id,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type Document struct {
	ID            string         `json:"id"`
	SourceID      string         `json:"source_id"`
	RawMessageID  *string        `json:"raw_message_id,omitempty"`
	ExternalDocID string         `json:"external_doc_id"`
	Title         *string        `json:"title,omitempty"`
	TextClean     string         `json:"text_clean"`
	TextOriginal  *string        `json:"text_original,omitempty"`
	Summary       *string        `json:"summary,omitempty"`
	LanguageCode  *string        `json:"language_code,omitempty"`
	ContentHash   string         `json:"content_hash"`
	Simhash       *string        `json:"simhash,omitempty"`
	DedupeGroup   *string        `json:"dedupe_group,omitempty"`
	QualityScore  *float64       `json:"quality_score,omitempty"`
	IsDuplicate   bool           `json:"is_duplicate"`
	DuplicateOf   *string        `json:"duplicate_of,omitempty"`
	IsForward     bool           `json:"is_forward"`
	ForwardSource *string        `json:"forward_source,omitempty"`
	Tags          []string       `json:"tags"`
	Links         []string       `json:"links"`
	Metadata      map[string]any `json:"metadata"`
	PublishedAt   *time.Time     `json:"published_at,omitempty"`
	IndexedAt     *time.Time     `json:"indexed_at,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type ParsedPost struct {
	DocumentID        string         `json:"document_id"`
	SourceID          string         `json:"source_id"`
	RawMessageID      string         `json:"raw_message_id"`
	ExternalDocID     string         `json:"external_doc_id"`
	ChannelUsername   string         `json:"channel_username"`
	ChannelTitle      *string        `json:"channel_title,omitempty"`
	ChannelURL        string         `json:"channel_url"`
	PostURL           string         `json:"post_url"`
	TelegramMessageID int64          `json:"telegram_message_id"`
	PublishedAt       *time.Time     `json:"published_at,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	TextClean         string         `json:"text_clean"`
	TextOriginal      *string        `json:"text_original,omitempty"`
	LanguageCode      *string        `json:"language_code,omitempty"`
	Hashtags          []string       `json:"hashtags"`
	Mentions          []string       `json:"mentions"`
	Links             []string       `json:"links"`
	IsDuplicate       bool           `json:"is_duplicate"`
	IsForward         bool           `json:"is_forward"`
	QualityScore      *float64       `json:"quality_score,omitempty"`
	Metadata          map[string]any `json:"metadata"`
}

type Chunk struct {
	ID              string         `json:"id"`
	DocumentID      string         `json:"document_id"`
	ChunkIndex      int            `json:"chunk_index"`
	Text            string         `json:"text"`
	TokenCount      *int           `json:"token_count,omitempty"`
	CharCount       *int           `json:"char_count,omitempty"`
	Metadata        map[string]any `json:"metadata"`
	EmbeddingStatus string         `json:"embedding_status"`
	EmbeddingModel  *string        `json:"embedding_model,omitempty"`
	EmbeddingError  *string        `json:"embedding_error,omitempty"`
	QdrantPointID   *string        `json:"qdrant_point_id,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type Job struct {
	ID            string         `json:"id"`
	JobType       string         `json:"job_type"`
	SourceID      *string        `json:"source_id,omitempty"`
	Status        string         `json:"status"`
	Payload       map[string]any `json:"payload"`
	ProgressTotal int            `json:"progress_total"`
	ProgressDone  int            `json:"progress_done"`
	ResultJSON    map[string]any `json:"result_json,omitempty"`
	ErrorText     *string        `json:"error_text,omitempty"`
	StartedAt     *time.Time     `json:"started_at,omitempty"`
	FinishedAt    *time.Time     `json:"finished_at,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
}

type Export struct {
	ID         string     `json:"id"`
	ExportType string     `json:"export_type"`
	Status     string     `json:"status"`
	SourceID   *string    `json:"source_id,omitempty"`
	FilePath   *string    `json:"file_path,omitempty"`
	RowCount   int        `json:"row_count"`
	ErrorText  *string    `json:"error_text,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

type SourceStats struct {
	RawMessages int `json:"raw_messages"`
	Documents   int `json:"documents"`
	Chunks      int `json:"chunks"`
}

type YouTubeAudioArtifact struct {
	ID            string         `json:"id"`
	SourceID      string         `json:"source_id"`
	Provider      string         `json:"provider"`
	VideoID       string         `json:"video_id"`
	AudioFilePath *string        `json:"audio_file_path,omitempty"`
	AudioStatus   string         `json:"audio_status"`
	RawJSON       map[string]any `json:"raw_json"`
	ErrorText     *string        `json:"error_text,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}
