import { FormEvent, useEffect, useMemo, useState } from "react";
import {
  createFullSourceTXTExport,
  createYouTubeSource,
  documentDownloadURL,
  downloadYouTubeAudioSource,
  exportDownloadURL,
  getDocument,
  getDocuments,
  getYouTubeSource,
  getYouTubeSourceAudio,
  listYouTubeSources,
  transcribeYouTubeAudioSource,
  youTubeAudioDownloadURL,
} from "../../api/client";
import StatusBadge from "../../components/StatusBadge";
import type { Chunk, Document, Source, SourceStats, YouTubeAudioArtifact } from "../../types";
import type { ImportResult } from "./types";

type Props = {
  onResult: (result: ImportResult) => void;
};

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" ? (value as Record<string, unknown>) : {};
}

function asStringMap(value: unknown): Record<string, string> {
  const data = asRecord(value);
  const out: Record<string, string> = {};
  for (const [key, raw] of Object.entries(data)) {
    if (typeof raw === "string" && raw.trim() !== "") out[key] = raw;
  }
  return out;
}

function asNumber(value: unknown): number | null {
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value === "string" && value.trim() !== "") {
    const parsed = Number(value);
    if (Number.isFinite(parsed)) return parsed;
  }
  return null;
}

function formatSeconds(seconds: number | null): string {
  if (seconds === null || !Number.isFinite(seconds)) return "-";
  if (seconds < 60) return `${seconds.toFixed(1)}s`;
  const mins = Math.floor(seconds / 60);
  const secs = seconds % 60;
  return `${mins}m ${secs.toFixed(1)}s`;
}

function formatDate(value?: string | null): string {
  if (!value) return "-";
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return value;
  return parsed.toLocaleString();
}

export default function YouTubeImportForm({ onResult }: Props) {
  const [url, setUrl] = useState("");
  const [title, setTitle] = useState("");
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Sources list
  const [sources, setSources] = useState<Source[]>([]);
  const [selectedID, setSelectedID] = useState<string | null>(null);

  // Selected source details
  const [sourceDetail, setSourceDetail] = useState<Source | null>(null);
  const [stats, setStats] = useState<SourceStats | null>(null);
  const [audioArtifact, setAudioArtifact] = useState<YouTubeAudioArtifact | null>(null);
  const [documents, setDocuments] = useState<Document[]>([]);
  const [selectedDocument, setSelectedDocument] = useState<Document | null>(null);
  const [chunks, setChunks] = useState<Chunk[]>([]);

  // Action states
  const [downloadingAudio, setDownloadingAudio] = useState(false);
  const [transcribingAudio, setTranscribingAudio] = useState(false);
  const [exportingTXT, setExportingTXT] = useState(false);
  const [lastExport, setLastExport] = useState<{ id: string; rowCount: number } | null>(null);

  async function loadSources(options?: { silent?: boolean }) {
    try {
      if (!options?.silent) setLoading(true);
      const list = await listYouTubeSources();
      setSources(list);
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      if (!options?.silent) setLoading(false);
    }
  }

  async function loadDetails(id: string) {
    try {
      const [srcResp, audioResp, docsResp] = await Promise.all([
        getYouTubeSource(id),
        getYouTubeSourceAudio(id),
        getDocuments(id, 50),
      ]);
      setSourceDetail(srcResp.source);
      setStats(srcResp.stats);
      setAudioArtifact(audioResp.artifact ?? null);
      setDocuments(docsResp);
      if (docsResp.length > 0) {
        const detail = await getDocument(docsResp[0].id);
        setSelectedDocument(detail.document);
        setChunks(detail.chunks);
      } else {
        setSelectedDocument(null);
        setChunks([]);
      }
    } catch (err) {
      setError((err as Error).message);
    }
  }

  useEffect(() => {
    void loadSources();
  }, []);

  useEffect(() => {
    if (!selectedID) {
      setSourceDetail(null);
      setStats(null);
      setAudioArtifact(null);
      setDocuments([]);
      setSelectedDocument(null);
      setChunks([]);
      return;
    }
    void loadDetails(selectedID);
  }, [selectedID]);

  // Auto-refresh when source is running
  useEffect(() => {
    if (!sourceDetail || sourceDetail.status !== "running") return;
    const timer = window.setInterval(() => {
      void loadSources({ silent: true });
      if (selectedID) void loadDetails(selectedID);
    }, 3000);
    return () => window.clearInterval(timer);
  }, [sourceDetail?.id, sourceDetail?.status, selectedID]);

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!url.trim()) return;
    setError(null);
    setSubmitting(true);
    const now = new Date().toISOString();
    try {
      const source = await createYouTubeSource({ url: url.trim() });
      onResult({
        id: crypto.randomUUID(),
        method: "youtube",
        title: title.trim() || source.title || source.url,
        url: source.url,
        status: "success",
        itemLabel: "videos",
        itemCount: 1,
        startedAt: now,
        finishedAt: new Date().toISOString(),
        summary: "YouTube source created",
      });
      setUrl("");
      setTitle("");
      await loadSources({ silent: true });
      setSelectedID(source.id);
    } catch (err) {
      const msg = (err as Error).message;
      setError(msg);
      onResult({
        id: crypto.randomUUID(),
        method: "youtube",
        title: title.trim() || url.trim(),
        url: url.trim(),
        status: "error",
        itemLabel: "videos",
        itemCount: 0,
        startedAt: now,
        finishedAt: new Date().toISOString(),
        error: msg,
      });
    } finally {
      setSubmitting(false);
    }
  }

  async function onDownloadAudio() {
    if (!selectedID) return;
    try {
      setDownloadingAudio(true);
      await downloadYouTubeAudioSource(selectedID);
      await loadDetails(selectedID);
      await loadSources({ silent: true });
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setDownloadingAudio(false);
    }
  }

  async function onTranscribeAudio() {
    if (!selectedID) return;
    try {
      setTranscribingAudio(true);
      await transcribeYouTubeAudioSource(selectedID);
      await loadDetails(selectedID);
      await loadSources({ silent: true });
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setTranscribingAudio(false);
    }
  }

  async function onExportTXT() {
    if (!selectedID) return;
    try {
      setExportingTXT(true);
      const result = await createFullSourceTXTExport(selectedID);
      setLastExport({ id: result.export_id, rowCount: result.row_count });
      setError(null);
      window.open(exportDownloadURL(result.export_id), "_blank", "noopener,noreferrer");
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setExportingTXT(false);
    }
  }

  async function onSelectDocument(docID: string) {
    try {
      const detail = await getDocument(docID);
      setSelectedDocument(detail.document);
      setChunks(detail.chunks);
    } catch (err) {
      setError((err as Error).message);
    }
  }

  // Audio metadata
  const audioRaw = useMemo(() => asRecord(audioArtifact?.raw_json), [audioArtifact?.raw_json]);
  const downloadMetadata = useMemo(() => asRecord(audioRaw.download_metadata), [audioRaw]);
  const transcriptionMetadata = useMemo(() => asRecord(audioRaw.transcription_metadata), [audioRaw]);
  const timingMetadata = useMemo(() => asRecord(audioRaw.timing_seconds), [audioRaw]);
  const speakerRoles = useMemo(() => asStringMap(audioRaw.speaker_roles), [audioRaw]);
  const ragText = typeof audioRaw.full_text_rag === "string" ? audioRaw.full_text_rag : "";

  const downloadElapsed =
    asNumber(timingMetadata.download_elapsed_seconds) ?? asNumber(downloadMetadata.elapsed_seconds);
  const transcribeElapsed =
    asNumber(timingMetadata.transcription_elapsed_seconds) ?? asNumber(transcriptionMetadata.elapsed_seconds);
  const totalElapsed =
    asNumber(timingMetadata.total_elapsed_seconds) ??
    (downloadElapsed !== null && transcribeElapsed !== null ? downloadElapsed + transcribeElapsed : null);
  const backendElapsed = asNumber(timingMetadata.backend_pipeline_elapsed_seconds);

  const canTranscribeExisting =
    Boolean(audioArtifact?.audio_file_path) &&
    sourceDetail?.status !== "running" &&
    !downloadingAudio &&
    !transcribingAudio;
  const canExportRAG =
    audioArtifact?.audio_status === "transcribed" || (stats?.documents ?? 0) > 0;

  if (loading) {
    return (
      <div className="import-form">
        <p className="muted">Loading YouTube sources...</p>
      </div>
    );
  }

  return (
    <div className="import-form hh-import-form">
      {error && <div className="import-inline-error">{error}</div>}

      {/* Add new source */}
      <div className="hh-section">
        <div className="hh-section-header">
          <h4>Add YouTube Source</h4>
        </div>
        <form className="import-form-fields" onSubmit={onSubmit}>
          <label>
            YouTube URL
            <input
              type="url"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder="Video, channel, or playlist URL"
              disabled={submitting}
            />
            <span className="field-hint">Supports individual videos, channels, and playlists</span>
          </label>
          <label>
            Source title (optional)
            <input
              type="text"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="Custom name for this source"
              disabled={submitting}
            />
          </label>
          <div className="import-form-actions" style={{ borderTop: "none", paddingTop: 0 }}>
            <button className="btn primary" type="submit" disabled={submitting || !url.trim()}>
              {submitting ? "Importing..." : "Import from YouTube"}
            </button>
          </div>
        </form>
      </div>

      {/* Sources list */}
      {sources.length > 0 && (
        <div className="hh-section">
          <div className="hh-section-header">
            <h4>YouTube Sources</h4>
            <button className="btn" type="button" onClick={() => void loadSources()}>
              Refresh
            </button>
          </div>
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>Title / URL</th>
                  <th>Status</th>
                  <th>Created</th>
                </tr>
              </thead>
              <tbody>
                {sources.map((s) => (
                  <tr
                    key={s.id}
                    className={selectedID === s.id ? "row-selected" : ""}
                  >
                    <td>
                      <button className="btn link" onClick={() => setSelectedID(s.id)}>
                        {s.title || s.external_id || s.url}
                      </button>
                    </td>
                    <td>
                      <StatusBadge status={s.status} />
                    </td>
                    <td>{formatDate(s.created_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Source Details */}
      {selectedID && sourceDetail && (
        <div className="hh-section">
          <div className="hh-section-header">
            <h4>{sourceDetail.title || sourceDetail.url}</h4>
            <div className="hh-auth-status">
              <StatusBadge status={sourceDetail.status} />
            </div>
          </div>

          {/* Actions */}
          <div className="yt-actions">
            <button
              className="btn primary"
              type="button"
              onClick={() => void onDownloadAudio()}
              disabled={downloadingAudio || sourceDetail.status === "running"}
            >
              {downloadingAudio
                ? "Queuing download..."
                : sourceDetail.status === "running"
                ? "Download running..."
                : "Download + Transcribe"}
            </button>
            {audioArtifact?.audio_file_path && (
              <button
                className="btn"
                type="button"
                onClick={() => void onTranscribeAudio()}
                disabled={!canTranscribeExisting}
              >
                {transcribingAudio ? "Queuing transcription..." : "Transcribe saved audio"}
              </button>
            )}
            <button
              className="btn"
              type="button"
              onClick={() => void onExportTXT()}
              disabled={exportingTXT || !canExportRAG}
            >
              {exportingTXT ? "Preparing TXT..." : "Export TXT RAG"}
            </button>
          </div>

          {/* Stats */}
          {stats && (
            <div className="yt-stats-row">
              <span>Documents: <strong>{stats.documents}</strong></span>
              <span>Chunks: <strong>{stats.chunks}</strong></span>
              <span>Raw messages: <strong>{stats.raw_messages}</strong></span>
            </div>
          )}

          {sourceDetail.last_error && (
            <div className="import-inline-error">{sourceDetail.last_error}</div>
          )}

          {lastExport && (
            <p className="muted">
              Export ready ({lastExport.rowCount} rows):{" "}
              <a className="btn link" href={exportDownloadURL(lastExport.id)}>
                Download TXT RAG
              </a>
            </p>
          )}

          {/* Audio artifact */}
          {audioArtifact ? (
            <div className="yt-audio-section">
              <h4>Audio + Transcription</h4>
              <p>
                Audio status: <StatusBadge status={audioArtifact.audio_status} />
              </p>
              {audioArtifact.audio_file_path && (
                <p>
                  Audio file:{" "}
                  <a className="btn link" href={youTubeAudioDownloadURL(audioArtifact.audio_file_path)}>
                    Download saved audio
                  </a>
                </p>
              )}
              {audioArtifact.error_text && (
                <div className="import-inline-error">{audioArtifact.error_text}</div>
              )}

              {(downloadElapsed !== null || transcribeElapsed !== null || totalElapsed !== null) && (
                <div className="yt-stats-row">
                  <span>Download: <strong>{formatSeconds(downloadElapsed)}</strong></span>
                  <span>Transcription: <strong>{formatSeconds(transcribeElapsed)}</strong></span>
                  <span>Total: <strong>{formatSeconds(totalElapsed)}</strong></span>
                  {backendElapsed !== null && (
                    <span>Backend stage: <strong>{formatSeconds(backendElapsed)}</strong></span>
                  )}
                </div>
              )}

              {Object.keys(downloadMetadata).length > 0 && (
                <details>
                  <summary>Download metadata</summary>
                  <pre>{JSON.stringify(downloadMetadata, null, 2)}</pre>
                </details>
              )}

              {Object.keys(speakerRoles).length > 0 && (
                <details open>
                  <summary>Speaker Roles ({Object.keys(speakerRoles).length})</summary>
                  <div className="yt-speaker-list">
                    {Object.entries(speakerRoles).map(([speaker, role]) => (
                      <span key={speaker} className="yt-speaker-tag">
                        <strong>{speaker}</strong>: {role}
                      </span>
                    ))}
                  </div>
                </details>
              )}

              {ragText && (
                <details>
                  <summary>RAG Text Preview</summary>
                  <pre className="cleaned-preview">{ragText}</pre>
                </details>
              )}
            </div>
          ) : (
            <p className="muted">No audio artifact yet. Click <strong>Download + Transcribe</strong>.</p>
          )}

          {/* Documents */}
          {documents.length > 0 && (
            <details>
              <summary>Documents ({documents.length})</summary>
              <div className="list-scroll">
                {documents.map((doc) => (
                  <article key={doc.id} className="list-item">
                    <header>
                      <button className="btn link" onClick={() => void onSelectDocument(doc.id)}>
                        {doc.external_doc_id}
                      </button>
                      <StatusBadge status={doc.is_duplicate ? "duplicate" : "primary"} />
                    </header>
                    <p>{doc.text_clean.slice(0, 180)}...</p>
                  </article>
                ))}
              </div>
            </details>
          )}

          {/* Selected Document + Chunks */}
          {selectedDocument && (
            <details>
              <summary>
                Document: {selectedDocument.external_doc_id} ({chunks.length} chunks)
              </summary>
              <div style={{ display: "flex", gap: "0.5rem", margin: "0.5rem 0" }}>
                <a className="btn" href={documentDownloadURL(selectedDocument.id, "txt")}>
                  Download TXT
                </a>
                <a className="btn" href={documentDownloadURL(selectedDocument.id, "json")}>
                  Download JSON
                </a>
              </div>
              <pre className="cleaned-preview">{selectedDocument.text_clean}</pre>
              {chunks.length > 0 && (
                <div className="list-scroll">
                  {chunks.map((chunk) => (
                    <article key={chunk.id} className="list-item">
                      <header>
                        <strong>Chunk #{chunk.chunk_index}</strong>
                        <small>{chunk.char_count ?? 0} chars</small>
                      </header>
                      <p>{chunk.text}</p>
                    </article>
                  ))}
                </div>
              )}
            </details>
          )}
        </div>
      )}
    </div>
  );
}
