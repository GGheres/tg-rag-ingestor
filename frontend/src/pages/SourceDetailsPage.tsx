import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import {
  createFullSourceTXTExport,
  exportDownloadURL,
  documentDownloadURL,
  downloadYouTubeAudioSource,
  getDocument,
  getDocuments,
  getRawMessages,
  getSource,
  getTelegramMessageLinks,
  transcribeYouTubeAudioSource,
  getYouTubeSource,
  getYouTubeSourceAudio,
  syncSource,
  youTubeAudioDownloadURL,
} from "../api/client";
import StatusBadge from "../components/StatusBadge";
import type {
  Chunk,
  Document,
  RawMessage,
  Source,
  SourceStats,
  TelegramMessageLink,
  YouTubeAudioArtifact,
} from "../types";

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" ? (value as Record<string, unknown>) : {};
}

function asStringMap(value: unknown): Record<string, string> {
  const data = asRecord(value);
  const out: Record<string, string> = {};
  for (const [key, raw] of Object.entries(data)) {
    if (typeof raw === "string" && raw.trim() !== "") {
      out[key] = raw;
    }
  }
  return out;
}

function asNumber(value: unknown): number | null {
  if (typeof value === "number" && Number.isFinite(value)) {
    return value;
  }
  if (typeof value === "string" && value.trim() !== "") {
    const parsed = Number(value);
    if (Number.isFinite(parsed)) {
      return parsed;
    }
  }
  return null;
}

function formatSeconds(seconds: number | null): string {
  if (seconds === null || !Number.isFinite(seconds)) {
    return "-";
  }
  if (seconds < 60) {
    return `${seconds.toFixed(1)}s`;
  }
  const mins = Math.floor(seconds / 60);
  const secs = seconds % 60;
  return `${mins}m ${secs.toFixed(1)}s`;
}

function displaySourceURL(source: Source): string {
  if (source.source_type === "telegram_channel_document" && source.external_id) {
    return source.external_id;
  }
  return source.url;
}

export default function SourceDetailsPage() {
  const { id = "" } = useParams();

  const [source, setSource] = useState<Source | null>(null);
  const [stats, setStats] = useState<SourceStats | null>(null);
  const [audioArtifact, setAudioArtifact] = useState<YouTubeAudioArtifact | null>(null);

  const [rawMessages, setRawMessages] = useState<RawMessage[]>([]);
  const [messageLinks, setMessageLinks] = useState<TelegramMessageLink[]>([]);
  const [documents, setDocuments] = useState<Document[]>([]);
  const [selectedDocument, setSelectedDocument] = useState<Document | null>(null);
  const [chunks, setChunks] = useState<Chunk[]>([]);

  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [syncing, setSyncing] = useState(false);
  const [downloadingAudio, setDownloadingAudio] = useState(false);
  const [transcribingAudio, setTranscribingAudio] = useState(false);
  const [exportingTXT, setExportingTXT] = useState(false);
  const [lastExport, setLastExport] = useState<{ id: string; rowCount: number } | null>(null);

  async function loadAll(options?: { silent?: boolean }) {
    const silent = Boolean(options?.silent);
    try {
      if (!silent) {
        setLoading(true);
      }

      const genericSource = await getSource(id);
      const sourceType = genericSource.source.source_type;

      if (sourceType === "youtube_video") {
        const [sourceResp, audioResp, docsResp] = await Promise.all([getYouTubeSource(id), getYouTubeSourceAudio(id), getDocuments(id, 50)]);
        setSource(sourceResp.source);
        setStats(sourceResp.stats);
        setAudioArtifact(audioResp.artifact ?? null);
        setRawMessages([]);
        setMessageLinks([]);
        setDocuments(docsResp);
        if (docsResp.length > 0) {
          const detail = await getDocument(docsResp[0].id);
          setSelectedDocument(detail.document);
          setChunks(detail.chunks);
        } else {
          setSelectedDocument(null);
          setChunks([]);
        }
      } else {
        const [sourceResp, rawResp, docsResp, linkResp] = await Promise.all([
          getSource(id),
          getRawMessages(id, 50),
          getDocuments(id, 50),
          sourceType === "telegram_message_links" || sourceType === "telegram_channel_document"
            ? getTelegramMessageLinks(id)
            : Promise.resolve([]),
        ]);
        setSource(sourceResp.source);
        setStats(sourceResp.stats);
        setRawMessages(rawResp);
        setMessageLinks(linkResp);
        setDocuments(docsResp);
        setAudioArtifact(null);

        if (docsResp.length > 0) {
          const detail = await getDocument(docsResp[0].id);
          setSelectedDocument(detail.document);
          setChunks(detail.chunks);
        } else {
          setSelectedDocument(null);
          setChunks([]);
        }
      }

      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      if (!silent) {
        setLoading(false);
      }
    }
  }

  useEffect(() => {
    void loadAll();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);

  useEffect(() => {
    if (!source || source.status !== "running") {
      return;
    }
    const timer = window.setInterval(() => {
      void loadAll({ silent: true });
    }, 3000);
    return () => window.clearInterval(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [source?.id, source?.status]);

  async function onSelectDocument(documentID: string) {
    try {
      const detail = await getDocument(documentID);
      setSelectedDocument(detail.document);
      setChunks(detail.chunks);
    } catch (err) {
      setError((err as Error).message);
    }
  }

  async function onSync() {
    try {
      setSyncing(true);
      await syncSource(id, { full_resync: true });
      await loadAll();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSyncing(false);
    }
  }

  async function onDownloadAudio() {
    try {
      setDownloadingAudio(true);
      await downloadYouTubeAudioSource(id);
      await loadAll();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setDownloadingAudio(false);
    }
  }

  async function onTranscribeAudio() {
    try {
      setTranscribingAudio(true);
      await transcribeYouTubeAudioSource(id);
      await loadAll();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setTranscribingAudio(false);
    }
  }

  async function onExportTXT() {
    try {
      setExportingTXT(true);
      const result = await createFullSourceTXTExport(id);
      setLastExport({ id: result.export_id, rowCount: result.row_count });
      setError(null);
      window.open(exportDownloadURL(result.export_id), "_blank", "noopener,noreferrer");
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setExportingTXT(false);
    }
  }

  if (loading) {
    return <p>Loading source details...</p>;
  }

  if (!source) {
    return <p>Source not found.</p>;
  }

  const isYouTube = source.source_type === "youtube_video";
  const isTelegram = source.source_type === "telegram_public_channel";
  const isTelegramChannelDocument = source.source_type === "telegram_channel_document";
  const isTelegramMessageLinks = source.source_type === "telegram_message_links";
  const isJSONUpload = source.source_type === "json_upload";
  const audioRaw = asRecord(audioArtifact?.raw_json);
  const downloadMetadata = asRecord(audioRaw.download_metadata);
  const transcriptionMetadata = asRecord(audioRaw.transcription_metadata);
  const timingMetadata = asRecord(audioRaw.timing_seconds);
  const speakerRoles = asStringMap(audioRaw.speaker_roles);
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
    source.status !== "running" &&
    !downloadingAudio &&
    !transcribingAudio;
  const canExportRAG = !isYouTube || audioArtifact?.audio_status === "transcribed" || (stats?.documents ?? 0) > 0;

  return (
    <section className="details-grid">
      {error && <p className="error">{error}</p>}

      <div className="card">
        <div className="card-header">
          <h2>{source.title || source.username || source.url}</h2>
          <div className="inline-actions">
            {isYouTube ? (
              <>
                <button className="btn primary" onClick={() => void onDownloadAudio()} disabled={downloadingAudio || source.status === "running"}>
                  {downloadingAudio ? "Queuing audio download..." : source.status === "running" ? "Download running..." : "Download + Transcribe"}
                </button>
                {audioArtifact?.audio_file_path ? (
                  <button className="btn" onClick={() => void onTranscribeAudio()} disabled={!canTranscribeExisting}>
                    {transcribingAudio ? "Queuing transcription..." : "Transcribe saved audio"}
                  </button>
                ) : null}
                <button className="btn" onClick={() => void onExportTXT()} disabled={exportingTXT || !canExportRAG}>
                  {exportingTXT ? "Preparing TXT..." : "Export TXT RAG"}
                </button>
              </>
            ) : isTelegram || isTelegramMessageLinks || isTelegramChannelDocument ? (
              <>
                <button className="btn primary" onClick={() => void onSync()} disabled={syncing}>
                  {syncing
                    ? isTelegramMessageLinks || isTelegramChannelDocument
                      ? "Fetching message links..."
                      : "Syncing full history..."
                    : isTelegramMessageLinks
                      ? "Fetch message links"
                      : isTelegramChannelDocument
                        ? "Build channel document"
                      : "Sync full history"}
                </button>
                <button className="btn" onClick={() => void onExportTXT()} disabled={exportingTXT}>
                  {exportingTXT ? "Preparing TXT..." : "Export full TXT RAG"}
                </button>
              </>
            ) : (
              <button className="btn" onClick={() => void onExportTXT()} disabled={exportingTXT || !canExportRAG}>
                {exportingTXT ? "Preparing TXT..." : "Export TXT RAG"}
              </button>
            )}
          </div>
        </div>

        <p>{displaySourceURL(source)}</p>
        <p>
          Source type: <strong>{source.source_type}</strong> | Provider: <strong>{source.provider || "-"}</strong>
          {source.external_id ? (
            <>
              {" "}| External ID: <strong>{source.external_id}</strong>
            </>
          ) : null}
        </p>
        <StatusBadge status={source.status} />
        {source.last_error && <p className="error">{source.last_error}</p>}
        {lastExport && (
          <p>
            Last export ready ({lastExport.rowCount} rows):{" "}
            <a className="btn link" href={exportDownloadURL(lastExport.id)}>
              Download TXT RAG
            </a>
          </p>
        )}

        <div className="stats">
          <article>
            <h3>Raw messages</h3>
            <p>{stats?.raw_messages ?? 0}</p>
          </article>
          <article>
            <h3>Documents</h3>
            <p>{stats?.documents ?? 0}</p>
          </article>
          <article>
            <h3>Chunks</h3>
            <p>{stats?.chunks ?? 0}</p>
          </article>
        </div>
      </div>

      {(isTelegramMessageLinks || isTelegramChannelDocument) && (
        <div className="card">
          <h2>Configured Message Links</h2>
          <div className="list-scroll">
            {messageLinks.map((item) => (
              <article className="list-item" key={item.id}>
                <header>
                  <strong>#{item.link_order}</strong>
                  <small>Message {item.telegram_message_id}</small>
                </header>
                <p>
                  <a href={item.canonical_url} target="_blank" rel="noreferrer">
                    {item.canonical_url}
                  </a>
                </p>
                {item.original_url !== item.canonical_url ? <small>{item.original_url}</small> : null}
              </article>
            ))}
            {messageLinks.length === 0 && <p>No configured message links.</p>}
          </div>
        </div>
      )}

      {isYouTube && (
        <div className="card">
          <h2>YouTube Audio + Transcription</h2>
          {audioArtifact ? (
            <>
              <p>
                Audio status: <StatusBadge status={audioArtifact.audio_status} />
              </p>
              {audioArtifact.audio_file_path ? (
                <p>
                  Audio file: <a className="btn link" href={youTubeAudioDownloadURL(audioArtifact.audio_file_path)}>Download saved audio</a>
                </p>
              ) : (
                <p>No downloaded audio yet.</p>
              )}
              {audioArtifact.error_text && <p className="error">{audioArtifact.error_text}</p>}
              {(downloadElapsed !== null || transcribeElapsed !== null || totalElapsed !== null || backendElapsed !== null) && (
                <>
                  <h3>Timing</h3>
                  <p>
                    Download: <strong>{formatSeconds(downloadElapsed)}</strong> | Transcription:{" "}
                    <strong>{formatSeconds(transcribeElapsed)}</strong> | Total: <strong>{formatSeconds(totalElapsed)}</strong>
                  </p>
                  {backendElapsed !== null && (
                    <p>
                      Last backend stage: <strong>{formatSeconds(backendElapsed)}</strong>
                    </p>
                  )}
                </>
              )}
              {downloadMetadata ? <pre>{JSON.stringify(downloadMetadata, null, 2)}</pre> : null}
              {Object.keys(speakerRoles).length > 0 ? (
                <>
                  <h3>Speaker Roles</h3>
                  <div className="list-scroll">
                    {Object.entries(speakerRoles).map(([speaker, role]) => (
                      <article key={speaker} className="list-item">
                        <header>
                          <strong>{speaker}</strong>
                          <small>{role}</small>
                        </header>
                      </article>
                    ))}
                  </div>
                </>
              ) : null}
              {ragText ? (
                <>
                  <h3>RAG Text Preview</h3>
                  <pre className="cleaned-preview">{ragText}</pre>
                </>
              ) : null}
            </>
          ) : (
            <p>No audio artifact yet. Click <strong>Download + Transcribe</strong>.</p>
          )}
        </div>
      )}

      {!isYouTube && (
        <div className="card">
          <h2>{isJSONUpload ? "Imported Raw Records" : "Recent Raw Messages"}</h2>
          <div className="list-scroll">
            {rawMessages.map((item) => (
              <article key={item.id} className="list-item">
                <header>
                  <strong>{isJSONUpload ? `Record #${item.telegram_message_id}` : `#${item.telegram_message_id}`}</strong>
                  <small>{item.posted_at ? new Date(item.posted_at).toLocaleString() : "-"}</small>
                </header>
                <p>{item.text_raw || item.caption_raw || "(empty)"}</p>
              </article>
            ))}
            {rawMessages.length === 0 && <p>No raw messages yet.</p>}
          </div>
        </div>
      )}

      <div className="card">
        <h2>Documents</h2>
        <div className="list-scroll">
          {documents.map((item) => (
            <article key={item.id} className="list-item">
              <header>
                <button className="btn link" onClick={() => void onSelectDocument(item.id)}>
                  {item.external_doc_id}
                </button>
                <StatusBadge status={item.is_duplicate ? "duplicate" : "primary"} />
              </header>
              <p>{item.text_clean.slice(0, 180)}...</p>
            </article>
          ))}
          {documents.length === 0 && <p>No documents yet.</p>}
        </div>
      </div>

      <div className="card">
        <h2>Selected Document & Chunks</h2>
        {selectedDocument ? (
          <>
            <div className="card-header">
              <p>
                <strong>{selectedDocument.external_doc_id}</strong>
              </p>
              <div className="inline-actions">
                <a className="btn" href={documentDownloadURL(selectedDocument.id, "txt")}>Download TXT</a>
                <a className="btn" href={documentDownloadURL(selectedDocument.id, "json")}>Download JSON</a>
              </div>
            </div>
            <p>{selectedDocument.text_clean}</p>
            <hr />
            <h3>Chunks</h3>
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
              {chunks.length === 0 && <p>No chunks for this document.</p>}
            </div>
          </>
        ) : (
          <p>Select a document to inspect chunks.</p>
        )}
      </div>
    </section>
  );
}
