import type {
  Chunk,
  Document,
  ExportRecord,
  ImportJSONResult,
  Job,
  PreviewResult,
  ParsedPost,
  RawMessage,
  Source,
  SourceStats,
} from "../types";

const API_URL = "http://localhost:8080";

type APIError = {
  error?: {
    code?: string;
    message?: string;
  };
};

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_URL}${path}`, init);
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as APIError;
    throw new Error(body.error?.message || `Request failed with ${res.status}`);
  }
  return (await res.json()) as T;
}

export function getHealth() {
  return request<{ ok: boolean; postgres: string }>("/health");
}

export function getSources() {
  return request<Source[]>("/api/sources");
}

export function createSource(payload: { url?: string; username?: string; title?: string }) {
  return request<Source>("/api/sources", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function importJSONSource(formData: FormData) {
  return request<ImportJSONResult>("/api/imports/json", {
    method: "POST",
    body: formData,
  });
}

export function getSource(id: string) {
  return request<{ source: Source; stats: SourceStats }>(`/api/sources/${id}`);
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

export function getRawMessages(sourceID: string, limit = 100) {
  return request<RawMessage[]>(`/api/sources/${sourceID}/raw-messages?limit=${limit}`);
}

export function getDocuments(sourceID: string, limit = 100) {
  return request<Document[]>(`/api/sources/${sourceID}/documents?limit=${limit}`);
}

export function getParsedPosts(payload: {
  source_ids: string[];
  limit_per_source?: number;
  include_duplicates?: boolean;
}) {
  return request<ParsedPost[]>("/api/posts/parsed", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function getDocument(id: string) {
  return request<{ document: Document; chunks: Chunk[] }>(`/api/documents/${id}`);
}

export function previewClean(text: string) {
  return request<PreviewResult>("/api/preview/clean", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ text }),
  });
}

export function createExport(payload: {
  source_id?: string;
  mode?: "documents" | "chunks";
  format?: "jsonl" | "txt_rag";
  include_duplicates?: boolean;
  include_trash?: boolean;
}) {
  return request<{ export_id: string; file_path: string; row_count: number }>("/api/exports/jsonl", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function getExports() {
  return request<ExportRecord[]>("/api/exports");
}

export function getJobs() {
  return request<Job[]>("/api/jobs");
}

export function documentDownloadURL(id: string, format: "txt" | "json" = "txt") {
  return `${API_URL}/api/documents/${id}/download?format=${format}`;
}

export function exportDownloadURL(id: string) {
  return `${API_URL}/api/exports/${id}/download`;
}
