import type {
  Chunk,
  Document,
  FilesystemScanResult,
  RawMessage,
  Source,
  SourceStats,
  YouTubeAudioDetails,
} from "../types";

const API_URL = "http://localhost:18080";
const PY_SERVICE_URL = "http://localhost:8090";

type APIError = {
  error?: {
    code?: string;
    stage?: string;
    message?: string;
  };
};

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_URL}${path}`, init);
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

export function createSource(payload: { url?: string; username?: string }) {
  return request<Source>("/api/sources", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
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
  return `${API_URL}/api/exports/${exportID}/download`;
}

export function documentDownloadURL(id: string, format: "txt" | "json" = "txt") {
  return `${API_URL}/api/documents/${id}/download?format=${format}`;
}

export function youTubeAudioDownloadURL(audioFilePath: string) {
  return `${PY_SERVICE_URL}/api/download-youtube-audio-file?path=${encodeURIComponent(audioFilePath)}`;
}
