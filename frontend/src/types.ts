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

export type TelegramMessageLink = {
  id: string;
  source_id: string;
  link_order: number;
  original_url: string;
  canonical_url: string;
  telegram_channel_id?: number | null;
  username?: string | null;
  telegram_message_id: number;
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

export type FilesystemRAGExportResult = {
  export_id: string;
  file_path: string;
  row_count: number;
  scanned_files: number;
  exported_files: number;
  skipped_files: number;
};

export type HHConfigStatus = {
  configured: boolean;
  has_access_token: boolean;
  has_refresh_token: boolean;
  user_agent: string;
  output_dir: string;
  redirect_uri: string;
  base_url: string;
  oauth_authorize_url: string;
};

export type HHOAuthExchangeResponse = {
  access_token: string;
  refresh_token: string;
  token_type: string;
  expires_in: number;
  note: string;
  env_path: string;
};

export type HHManagerAccount = {
  id: string;
  employer_id: string;
  employer_name: string;
  is_current: boolean;
  is_primary: boolean;
};

export type HHVacancyListItem = {
  id: string;
  name: string;
  status: string;
  manager_account_id: string;
  employer_id: string;
  employer_name: string;
  alternate_url?: string;
  published_at?: string;
  archived_at?: string;
  responses?: number;
  views?: number;
};

export type HHVacancyCatalog = {
  accounts: HHManagerAccount[];
  current_account_id: string;
  vacancies: HHVacancyListItem[];
};

export type HHCandidateManifest = {
  candidate_id: string;
  resume_id: string;
  fio: string;
  status: string;
  downloaded_original: boolean;
  included_in_combined: boolean;
  error_message?: string;
  original_file?: string;
};

export type HHRunManifest = {
  vacancy_id: string;
  provider: string;
  created_at: string;
  total_found: number;
  processed: number;
  succeeded: number;
  failed: number;
  dry_run: boolean;
  candidates: HHCandidateManifest[];
};

export type HHExtractionListItem = {
  id: string;
  vacancy_id: string;
  status: string;
  created_at: string;
  provider?: string;
  total_found: number;
  succeeded: number;
  failed: number;
  dry_run: boolean;
  files: string[];
};

export type HHExtractionDetails = {
  extraction_id: string;
  status: string;
  files?: string[];
  manifest?: HHRunManifest;
};

export type HHPublicVacancyImportResponse = {
  source: Source;
  imported_count: number;
  processed_count: number;
  duplicate_count: number;
  trash_count: number;
  chunk_count: number;
  generated_title: string;
  generated_source_name: string;
  search_url: string;
  search_window: {
    date_from: string;
    date_to: string;
  };
  filters: {
    text?: string;
    area?: string;
    professional_role?: string;
  };
  raw_payload_size: number;
  fetch_stats: {
    windows_visited: number;
    requests_made: number;
    vacancies_fetched: number;
    truncated_windows: number;
    reached_max_items: boolean;
  };
};
