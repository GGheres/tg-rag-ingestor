import { documentDownloadURL } from "../../api/client";
import type { ImportResult } from "./types";

const METHOD_LABELS: Record<string, string> = {
  telegram: "Telegram",
  youtube: "YouTube",
  "audio-upload": "Audio to Text",
  "local-folder": "Local Folder",
  json: "JSON",
  "hh-resumes": "HH Resumes",
};

const TELEGRAM_MODE_LABELS: Record<string, string> = {
  "channel-document": "Channel as Document",
  "regular-source": "Channel Source",
  "message-links": "Message Links",
};

const STATUS_CLASSES: Record<string, string> = {
  pending: "",
  running: "warn",
  success: "success",
  partial: "warn",
  error: "danger",
};

const STATUS_LABELS: Record<string, string> = {
  pending: "Pending",
  running: "Running",
  success: "Success",
  partial: "Partial",
  error: "Error",
};

function formatTime(iso: string) {
  return new Date(iso).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

type Props = {
  result: ImportResult;
};

export default function ImportResultCard({ result }: Props) {
  const methodLabel = METHOD_LABELS[result.method] || result.method;
  const modeLabel = result.telegramMode ? TELEGRAM_MODE_LABELS[result.telegramMode] : null;
  const statusClass = STATUS_CLASSES[result.status] || "";

  return (
    <div className={`import-result-card ${result.status === "running" ? "is-running" : ""}`}>
      <div className="import-result-header">
        <div className="import-result-meta">
          <span className="import-result-type">
            {methodLabel}
            {modeLabel && <span className="import-result-mode"> / {modeLabel}</span>}
          </span>
          <span className={`status ${statusClass}`}>{STATUS_LABELS[result.status]}</span>
        </div>
        <h4 className="import-result-title">{result.title}</h4>
        {(result.url || result.path) && (
          <span className="import-result-location">{result.url || result.path}</span>
        )}
      </div>

      <div className="import-result-body">
        <div className="import-result-stats">
          {result.itemCount !== undefined && (
            <span className="import-result-stat">
              <strong>{result.itemCount}</strong> {result.itemLabel}
            </span>
          )}
          <span className="import-result-stat">
            {formatTime(result.startedAt)}
          </span>
          {result.finishedAt && (
            <span className="import-result-stat">
              {durationLabel(result.startedAt, result.finishedAt)}
            </span>
          )}
        </div>

        {result.summary && <p className="import-result-summary">{result.summary}</p>}
        {result.error && <p className="import-result-error">{result.error}</p>}
        {result.documentId && (
          <div className="import-result-actions">
            <a
              href={documentDownloadURL(result.documentId, "txt")}
              className="btn small"
              target="_blank"
              rel="noopener noreferrer"
            >
              TXT
            </a>
            <a
              href={documentDownloadURL(result.documentId, "chunks")}
              className="btn small"
              target="_blank"
              rel="noopener noreferrer"
            >
              Chunks
            </a>
            <a
              href={documentDownloadURL(result.documentId, "json")}
              className="btn small"
              target="_blank"
              rel="noopener noreferrer"
            >
              JSON
            </a>
          </div>
        )}
      </div>

      {result.status === "running" && <div className="import-result-progress" />}
    </div>
  );
}

function durationLabel(start: string, end: string): string {
  const ms = new Date(end).getTime() - new Date(start).getTime();
  if (ms < 1000) return "<1s";
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const remaining = seconds % 60;
  return `${minutes}m ${remaining}s`;
}
