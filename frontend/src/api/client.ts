import type {
  Chunk,
  Document,
  FilesystemRAGExportResult,
  FilesystemScanResult,
  HHConfigStatus,
  HHVacancyCatalog,
  HHExtractionDetails,
  HHExtractionListItem,
  HHOAuthExchangeResponse,
  HHPublicVacancyImportResponse,
  RawMessage,
  Source,
  SourceStats,
  TelegramMessageLink,
  YouTubeAudioDetails,
} from "../types";

const API_URL = normalizeBaseURL(import.meta.env.VITE_API_URL);
const PY_SERVICE_URL = normalizeBaseURL(import.meta.env.VITE_PY_SERVICE_URL);

type APIError = {
  error?: {
    code?: string;
    stage?: string;
    message?: string;
  };
};

function normalizeBaseURL(value?: string): string {
  if (!value) {
    return "";
  }
  return value.replace(/\/+$/, "");
}

function buildURL(baseURL: string, path: string): string {
  if (!path.startsWith("/")) {
    return `${baseURL}/${path}`;
  }
  return `${baseURL}${path}`;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(buildURL(API_URL, path), init);
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as APIError;
    const code = body.error?.code?.trim();
    const stage = body.error?.stage?.trim();
    const message = body.error?.message?.trim();
    const parts = [stage, code].filter((item): item is string => Boolean(item));
    if (parts.length > 0 && message) {
      throw new Error(`${parts.join("/")} - ${message}`);
    }
    if (code && message) {
      throw new Error(`${code}: ${message}`);
    }
    if (message) {
      throw new Error(message);
    }
    throw new Error(`HTTP ${res.status} ${res.statusText}`);
  }
  return (await res.json()) as T;
}

export function getSources() {
  return request<Source[]>("/api/sources");
}

export function clearSourcesHistory() {
  return request<{ deleted_count: number }>("/api/sources", {
    method: "DELETE",
  });
}

export function createSource(payload: { url?: string; username?: string }) {
  return request<Source>("/api/sources", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function createTelegramMessageLinkSource(payload: { title?: string; message_links: string[] }) {
  return request<Source>("/api/sources/telegram-message-links", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function createTelegramChannelDocumentSource(payload: { title?: string; url: string }) {
  return request<Source>("/api/sources/telegram-channel-document", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function listYouTubeSources() {
  return request<Source[]>("/api/youtube/sources");
}

export function createYouTubeSource(payload: { url: string }) {
  return request<Source>("/api/youtube/sources", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function importJSONFile(payload: { file: File }) {
  const formData = new FormData();
  formData.set("file", payload.file);
  return request<{
    source: Source;
    imported_count: number;
    processed_count: number;
    duplicate_count: number;
    trash_count: number;
    chunk_count: number;
  }>("/api/imports/json", {
    method: "POST",
    body: formData,
  });
}

export function scanFilesystemDirectory(payload: { path: string }) {
  return request<FilesystemScanResult>("/api/filesystem/scan", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function createFilesystemRAGExport(payload: { path: string; include_skipped?: boolean }) {
  return request<FilesystemRAGExportResult>("/api/filesystem/export-rag", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function getSource(id: string) {
  return request<{ source: Source; stats: SourceStats }>(`/api/sources/${id}`);
}

export function getYouTubeSource(id: string) {
  return request<{ source: Source; stats: SourceStats }>(`/api/youtube/sources/${id}`);
}

export function getYouTubeSourceAudio(id: string) {
  return request<YouTubeAudioDetails>(`/api/youtube/sources/${id}/audio`);
}

export function syncSource(
  id: string,
  options?: {
    batch_size?: number;
    max_messages?: number;
    limit?: number;
    full_resync?: boolean;
  }
) {
  return request<{
    pages_fetched: number;
    messages_fetched: number;
    processed_count: number;
    duplicate_count: number;
    trash_count: number;
    chunk_count: number;
  }>(`/api/sources/${id}/sync`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(options ?? {}),
  });
}

export function downloadYouTubeAudioSource(id: string) {
  return request<{
    status: "queued";
    source_id: string;
    stage: "download_audio";
  }>(`/api/youtube/sources/${id}/download-audio`, {
    method: "POST",
  });
}

export function transcribeYouTubeAudioSource(id: string) {
  return request<{
    status: "queued";
    source_id: string;
    stage: "transcribe_audio";
  }>(`/api/youtube/sources/${id}/transcribe-audio`, {
    method: "POST",
  });
}

export function getRawMessages(sourceID: string, limit = 100) {
  return request<RawMessage[]>(`/api/sources/${sourceID}/raw-messages?limit=${limit}`);
}

export function getTelegramMessageLinks(sourceID: string) {
  return request<TelegramMessageLink[]>(`/api/sources/${sourceID}/message-links`);
}

export function getDocuments(sourceID: string, limit = 100) {
  return request<Document[]>(`/api/sources/${sourceID}/documents?limit=${limit}`);
}

export function getDocument(id: string) {
  return request<{ document: Document; chunks: Chunk[] }>(`/api/documents/${id}`);
}

export function createFullSourceTXTExport(sourceID: string) {
  return request<{
    export_id: string;
    file_path: string;
    row_count: number;
  }>("/api/exports/jsonl", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      source_id: sourceID,
      mode: "documents",
      format: "txt_rag",
      include_duplicates: true,
      include_trash: true,
    }),
  });
}

export function exportDownloadURL(exportID: string) {
  return buildURL(API_URL, `/api/exports/${exportID}/download`);
}

export function documentDownloadURL(id: string, format: "txt" | "chunks" | "json" = "txt") {
  return `${buildURL(API_URL, `/api/documents/${id}/download`)}?format=${format}`;
}

export function youTubeAudioDownloadURL(audioFilePath: string) {
  const downloadBase = PY_SERVICE_URL
    ? buildURL(PY_SERVICE_URL, "/api/download-youtube-audio-file")
    : "/youtube-audio-files";
  return `${downloadBase}?path=${encodeURIComponent(audioFilePath)}`;
}

export function uploadAndTranscribeAudio(payload: {
  file: File;
  title?: string;
  language?: string;
  speakers?: number;
}) {
  const formData = new FormData();
  formData.set("file", payload.file);
  if (payload.title) formData.set("title", payload.title);
  if (payload.language) formData.set("language", payload.language);
  if (payload.speakers && payload.speakers >= 1) formData.set("speakers", String(payload.speakers));
  return request<{
    source: Source;
    artifact: unknown;
    document_id: string;
    chunk_count: number;
    speaker_roles: Record<string, string>;
  }>("/api/audio/upload-and-transcribe", {
    method: "POST",
    body: formData,
  });
}

export function getHHConfig() {
  return request<HHConfigStatus>("/api/hh/config");
}

export function listHHVacancies() {
  return request<HHVacancyCatalog>("/api/hh/vacancies");
}

export function exchangeHHOAuthCode(payload: { code: string }) {
  return request<HHOAuthExchangeResponse>("/api/hh/oauth/exchange", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function startHHExtraction(payload: {
  vacancy_id: string;
  manager_account_id?: string;
  dry_run?: boolean;
  export_pdf?: boolean;
  save_originals?: boolean;
  cover_letter_only?: boolean;
}) {
  return request<{
    extraction_id: string;
    vacancy_id: string;
    manager_account_id?: string;
    status: string;
    output_dir: string;
  }>("/api/hh/extract", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function importHHPublicVacancies(payload: {
  text?: string;
  area?: string;
  professional_role?: string;
  date_from?: string;
  date_to?: string;
  max_items?: number;
  source_name?: string;
  title?: string;
}) {
  return request<HHPublicVacancyImportResponse>("/api/hh/public-vacancies/import", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function listHHExtractions() {
  return request<HHExtractionListItem[]>("/api/hh/extractions");
}

export function getHHExtraction(extractionID: string) {
  return request<HHExtractionDetails>(`/api/hh/extractions/${extractionID}`);
}

export function hhExtractionFileURL(extractionID: string, filename: string) {
  const encodedFileName = encodeURIComponent(filename);
  if (filename.startsWith("originals/")) {
    const base = filename.slice("originals/".length);
    return `${API_URL}/api/hh/extractions/${extractionID}/files/${encodeURIComponent(base)}?subdir=originals`;
  }
  return `${API_URL}/api/hh/extractions/${extractionID}/files/${encodedFileName}`;
}
