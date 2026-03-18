export type Source = {
  id: string;
  source_type: string;
  provider?: string | null;
  external_id?: string | null;
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
  raw_message_id?: string | null;
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

export type Chunk = {
  id: string;
  document_id: string;
  chunk_index: number;
  text: string;
  token_count?: number | null;
  char_count?: number | null;
  embedding_status: string;
};

export type YouTubeAudioArtifact = {
  id: string;
  source_id: string;
  provider: string;
  video_id: string;
  audio_file_path?: string | null;
  audio_status: string;
  raw_json: Record<string, unknown>;
  error_text?: string | null;
  created_at: string;
  updated_at: string;
};

export type YouTubeAudioDetails = {
  source: Source;
  artifact?: YouTubeAudioArtifact;
};

export type FilesystemSkippedArchive = {
  path: string;
  reason: string;
};

export type FilesystemScannedFile = {
  name: string;
  logical_path: string;
  resolved_path: string;
  size_bytes: number;
  origin: string;
  archive_path?: string | null;
  archive_member_path?: string | null;
};

export type FilesystemScanResult = {
  root_path: string;
  scanned_at: string;
  total_files: number;
  regular_files: number;
  extracted_files: number;
  archives_processed: number;
  supported_archive_extensions: string[];
  unsupported_archive_formats: string[];
  skipped_archives: FilesystemSkippedArchive[];
  files: FilesystemScannedFile[];
};
