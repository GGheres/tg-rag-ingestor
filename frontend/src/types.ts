export type Source = {
  id: string;
  source_type: string;
  telegram_channel_id?: number | null;
  telegram_access_hash?: number | null;
  username?: string | null;
  title?: string | null;
  url: string;
  status: string;
  last_message_id?: number | null;
  last_synced_at?: string | null;
  last_error?: string | null;
  created_at: string;
  updated_at: string;
};

export type SourceStats = {
  raw_messages: number;
  documents: number;
  chunks: number;
};

export type RawMessage = {
  id: string;
  source_id: string;
  telegram_message_id: number;
  posted_at?: string | null;
  text_raw?: string | null;
  caption_raw?: string | null;
  has_media: boolean;
  created_at: string;
};

export type Document = {
  id: string;
  source_id: string;
  raw_message_id: string;
  external_doc_id: string;
  text_clean: string;
  text_original?: string | null;
  language_code?: string | null;
  content_hash: string;
  quality_score?: number | null;
  is_duplicate: boolean;
  duplicate_of?: string | null;
  tags: string[];
  links: string[];
  metadata: Record<string, unknown>;
  published_at?: string | null;
  created_at: string;
};

export type ParsedPost = {
  document_id: string;
  source_id: string;
  raw_message_id: string;
  external_doc_id: string;
  channel_username: string;
  channel_title?: string | null;
  channel_url: string;
  post_url: string;
  telegram_message_id: number;
  published_at?: string | null;
  created_at: string;
  text_clean: string;
  text_original?: string | null;
  language_code?: string | null;
  hashtags: string[];
  mentions: string[];
  links: string[];
  is_duplicate: boolean;
  is_forward: boolean;
  quality_score?: number | null;
  metadata: Record<string, unknown>;
};

export type Chunk = {
  id: string;
  document_id: string;
  chunk_index: number;
  text: string;
  token_count?: number | null;
  char_count?: number | null;
  embedding_status: string;
};

export type Job = {
  id: string;
  job_type: string;
  source_id?: string | null;
  status: string;
  payload: Record<string, unknown>;
  progress_total: number;
  progress_done: number;
  result_json?: Record<string, unknown>;
  error_text?: string | null;
  created_at: string;
  started_at?: string | null;
  finished_at?: string | null;
};

export type ExportRecord = {
  id: string;
  export_type: string;
  status: string;
  source_id?: string | null;
  file_path?: string | null;
  row_count: number;
  error_text?: string | null;
  created_at: string;
  finished_at?: string | null;
};

export type PreviewResult = {
  cleaned: string;
  is_trash: boolean;
  links: string[];
  hashtags: string[];
  mentions: string[];
  chunks: Array<{
    index: number;
    text: string;
    char_count: number;
    token_count: number;
  }>;
};

export type ImportJSONResult = {
  source: Source;
  imported_count: number;
  processed_count: number;
  duplicate_count: number;
  trash_count: number;
  chunk_count: number;
};
