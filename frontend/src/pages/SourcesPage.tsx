import { FormEvent, useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import {
  clearSourcesHistory,
  createFilesystemRAGExport,
  createFullSourceTXTExport,
  createTelegramChannelDocumentSource,
  createSource,
  createTelegramMessageLinkSource,
  createYouTubeSource,
  downloadYouTubeAudioSource,
  exportDownloadURL,
  getSources,
  importJSONFile,
  scanFilesystemDirectory,
  syncSource,
} from "../api/client";
import StatusBadge from "../components/StatusBadge";
import type { FilesystemRAGExportResult, FilesystemScanResult, Source } from "../types";

function formatBytes(sizeBytes: number): string {
  if (!Number.isFinite(sizeBytes) || sizeBytes < 1024) {
    return `${sizeBytes} B`;
  }
  if (sizeBytes < 1024 * 1024) {
    return `${(sizeBytes / 1024).toFixed(1)} KB`;
  }
  if (sizeBytes < 1024 * 1024 * 1024) {
    return `${(sizeBytes / (1024 * 1024)).toFixed(1)} MB`;
  }
  return `${(sizeBytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

function displaySourceURL(source: Source): string {
  if (source.source_type === "telegram_channel_document" && source.external_id) {
    return source.external_id;
  }
  return source.url;
}

export default function SourcesPage() {
  const [sources, setSources] = useState<Source[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [syncingID, setSyncingID] = useState<string | null>(null);
  const [downloadingID, setDownloadingID] = useState<string | null>(null);
  const [exportingID, setExportingID] = useState<string | null>(null);

  const [telegramForm, setTelegramForm] = useState({ url: "", username: "" });
  const [telegramChannelDocumentForm, setTelegramChannelDocumentForm] = useState({ title: "", url: "" });
  const [telegramLinksForm, setTelegramLinksForm] = useState({ title: "", messageLinks: "" });
  const [youtubeForm, setYouTubeForm] = useState({ url: "" });
  const [filesystemForm, setFilesystemForm] = useState({ path: "" });
  const [jsonImportFile, setJsonImportFile] = useState<File | null>(null);

  const [creatingTelegram, setCreatingTelegram] = useState(false);
  const [creatingTelegramChannelDocument, setCreatingTelegramChannelDocument] = useState(false);
  const [creatingTelegramLinks, setCreatingTelegramLinks] = useState(false);
  const [creatingJSONImport, setCreatingJSONImport] = useState(false);
  const [creatingYouTube, setCreatingYouTube] = useState(false);
  const [scanningFilesystem, setScanningFilesystem] = useState(false);
  const [exportingFilesystemRAG, setExportingFilesystemRAG] = useState(false);
  const [clearingHistory, setClearingHistory] = useState(false);
  const [filesystemScan, setFilesystemScan] = useState<FilesystemScanResult | null>(null);
  const [lastFilesystemRAGExport, setLastFilesystemRAGExport] = useState<FilesystemRAGExportResult | null>(null);
  const [lastJSONImport, setLastJSONImport] = useState<{
    sourceID: string;
    importedCount: number;
    processedCount: number;
    duplicateCount: number;
    trashCount: number;
    chunkCount: number;
  } | null>(null);
  const jsonFileInputRef = useRef<HTMLInputElement | null>(null);

  async function loadSources(options?: { silent?: boolean }) {
    const silent = Boolean(options?.silent);
    try {
      if (!silent) {
        setLoading(true);
      }
      const data = await getSources();
      setSources(Array.isArray(data) ? data : []);
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
    void loadSources();
  }, []);

  useEffect(() => {
    if (!sources.some((item) => item.status === "running")) {
      return;
    }
    const timer = window.setInterval(() => {
      void loadSources({ silent: true });
    }, 3000);
    return () => window.clearInterval(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sources]);

  async function onTelegramSubmit(event: FormEvent) {
    event.preventDefault();
    try {
      setCreatingTelegram(true);
      await createSource({
        url: telegramForm.url || undefined,
        username: telegramForm.username || undefined,
      });
      setTelegramForm({ url: "", username: "" });
      await loadSources();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setCreatingTelegram(false);
    }
  }

  async function onYouTubeSubmit(event: FormEvent) {
    event.preventDefault();
    try {
      setCreatingYouTube(true);
      await createYouTubeSource({
        url: youtubeForm.url,
      });
      setYouTubeForm({ url: "" });
      await loadSources();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setCreatingYouTube(false);
    }
  }

  async function onTelegramLinksSubmit(event: FormEvent) {
    event.preventDefault();
    const messageLinks = telegramLinksForm.messageLinks
      .split(/\r?\n/)
      .map((item) => item.trim())
      .filter(Boolean);
    if (messageLinks.length === 0) {
      setError("Add at least one Telegram message link.");
      return;
    }

    try {
      setCreatingTelegramLinks(true);
      await createTelegramMessageLinkSource({
        title: telegramLinksForm.title || undefined,
        message_links: messageLinks,
      });
      setTelegramLinksForm({ title: "", messageLinks: "" });
      setError(null);
      await loadSources();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setCreatingTelegramLinks(false);
    }
  }

  async function onTelegramChannelDocumentSubmit(event: FormEvent) {
    event.preventDefault();
    if (!telegramChannelDocumentForm.url.trim()) {
      setError("Add a Telegram channel URL.");
      return;
    }

    try {
      setCreatingTelegramChannelDocument(true);
      await createTelegramChannelDocumentSource({
        title: telegramChannelDocumentForm.title || undefined,
        url: telegramChannelDocumentForm.url.trim(),
      });
      setTelegramChannelDocumentForm({ title: "", url: "" });
      setError(null);
      await loadSources();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setCreatingTelegramChannelDocument(false);
    }
  }

  async function onFilesystemSubmit(event: FormEvent) {
    event.preventDefault();
    try {
      setScanningFilesystem(true);
      const result = await scanFilesystemDirectory({
        path: filesystemForm.path,
      });
      setFilesystemScan(result);
      setLastFilesystemRAGExport(null);
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setScanningFilesystem(false);
    }
  }

  async function onFilesystemExportRAG() {
    const path = filesystemForm.path.trim() || filesystemScan?.root_path?.trim() || "";
    if (!path) {
      setError("Run filesystem scan first or provide a folder path.");
      return;
    }

    try {
      setExportingFilesystemRAG(true);
      const result = await createFilesystemRAGExport({
        path,
        include_skipped: true,
      });
      setLastFilesystemRAGExport(result);
      setError(null);
      window.open(exportDownloadURL(result.export_id), "_blank", "noopener,noreferrer");
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setExportingFilesystemRAG(false);
    }
  }

  async function onJSONImportSubmit(event: FormEvent) {
    event.preventDefault();
    if (!jsonImportFile) {
      setError("Choose a JSON file to upload.");
      return;
    }

    try {
      setCreatingJSONImport(true);
      const result = await importJSONFile({
        file: jsonImportFile,
      });
      setJsonImportFile(null);
      setLastJSONImport({
        sourceID: result.source.id,
        importedCount: result.imported_count,
        processedCount: result.processed_count,
        duplicateCount: result.duplicate_count,
        trashCount: result.trash_count,
        chunkCount: result.chunk_count,
      });
      if (jsonFileInputRef.current) {
        jsonFileInputRef.current.value = "";
      }
      setError(null);
      await loadSources();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setCreatingJSONImport(false);
    }
  }

  async function onSync(sourceID: string) {
    try {
      setSyncingID(sourceID);
      await syncSource(sourceID, { full_resync: true });
      await loadSources();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSyncingID(null);
    }
  }

  async function onDownloadAudio(sourceID: string) {
    try {
      setDownloadingID(sourceID);
      await downloadYouTubeAudioSource(sourceID);
      await loadSources();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setDownloadingID(null);
    }
  }

  async function onExportTXT(sourceID: string) {
    try {
      setExportingID(sourceID);
      const result = await createFullSourceTXTExport(sourceID);
      window.open(exportDownloadURL(result.export_id), "_blank", "noopener,noreferrer");
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setExportingID(null);
    }
  }

  async function onClearHistory() {
    if (!window.confirm("Clear source history? This will delete all saved sources and their parsed data.")) {
      return;
    }

    try {
      setClearingHistory(true);
      await clearSourcesHistory();
      setLastJSONImport(null);
      setError(null);
      await loadSources();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setClearingHistory(false);
    }
  }

  return (
    <section>
      <div className="card">
        <h2>Add Telegram Source</h2>
        <form className="grid-form" onSubmit={onTelegramSubmit}>
          <label>
            Telegram URL
            <input
              type="text"
              placeholder="https://t.me/somechannel"
              value={telegramForm.url}
              onChange={(e) => setTelegramForm((prev) => ({ ...prev, url: e.target.value }))}
            />
          </label>
          <label>
            Username
            <input
              type="text"
              placeholder="@somechannel"
              value={telegramForm.username}
              onChange={(e) => setTelegramForm((prev) => ({ ...prev, username: e.target.value }))}
            />
          </label>
          <button className="btn primary" type="submit" disabled={creatingTelegram}>
            {creatingTelegram ? "Creating..." : "Add Telegram source"}
          </button>
        </form>
      </div>

      <div className="card">
        <h2>Add YouTube Source</h2>
        <form className="grid-form" onSubmit={onYouTubeSubmit}>
          <label>
            YouTube URL
            <input
              type="text"
              placeholder="https://www.youtube.com/watch?v=..."
              value={youtubeForm.url}
              onChange={(e) => setYouTubeForm((prev) => ({ ...prev, url: e.target.value }))}
              required
            />
          </label>
          <button className="btn primary" type="submit" disabled={creatingYouTube}>
            {creatingYouTube ? "Creating..." : "Add YouTube source"}
          </button>
        </form>
      </div>

      <div className="card">
        <h2>Add Telegram Channel As One RAG Document</h2>
        <form className="grid-form" onSubmit={onTelegramChannelDocumentSubmit}>
          <label>
            Title
            <input
              type="text"
              placeholder="Closed channel full document"
              value={telegramChannelDocumentForm.title}
              onChange={(e) => setTelegramChannelDocumentForm((prev) => ({ ...prev, title: e.target.value }))}
            />
          </label>
          <label>
            Channel URL
            <input
              type="text"
              placeholder="https://t.me/+inviteHash or https://t.me/channelname"
              value={telegramChannelDocumentForm.url}
              onChange={(e) => setTelegramChannelDocumentForm((prev) => ({ ...prev, url: e.target.value }))}
              required
            />
          </label>
          <button className="btn primary" type="submit" disabled={creatingTelegramChannelDocument}>
            {creatingTelegramChannelDocument ? "Creating..." : "Add Telegram channel document"}
          </button>
        </form>
        <p className="muted">
          This mode pulls the channel history, auto-generates per-message links, and then merges cleaned message text
          into one RAG document. For private channels, use the same MTProto account that already has access.
        </p>
      </div>

      <div className="card">
        <h2>Add Telegram Source By Message Links</h2>
        <form className="grid-form" onSubmit={onTelegramLinksSubmit}>
          <label>
            Title
            <input
              type="text"
              placeholder="Closed channel selection"
              value={telegramLinksForm.title}
              onChange={(e) => setTelegramLinksForm((prev) => ({ ...prev, title: e.target.value }))}
            />
          </label>
          <label>
            Message links
            <textarea
              placeholder={"https://t.me/c/1941234567/10\nhttps://t.me/c/1941234567/11"}
              value={telegramLinksForm.messageLinks}
              onChange={(e) => setTelegramLinksForm((prev) => ({ ...prev, messageLinks: e.target.value }))}
              rows={5}
              required
            />
          </label>
          <button className="btn primary" type="submit" disabled={creatingTelegramLinks}>
            {creatingTelegramLinks ? "Creating..." : "Add Telegram message-link source"}
          </button>
        </form>
        <p className="muted">
          Use one Telegram post link per line. All links must belong to the same channel. Private channel links in the{" "}
          <code>t.me/c/...</code> format require an authorized MTProto session.
        </p>
      </div>

      <div className="card">
        <h2>Scan Local Folder</h2>
        <form className="grid-form" onSubmit={onFilesystemSubmit}>
          <label>
            Folder path on backend host
            <input
              type="text"
              placeholder="/Users/gg/data/archive-drop"
              value={filesystemForm.path}
              onChange={(e) => setFilesystemForm({ path: e.target.value })}
              required
            />
          </label>
          <button className="btn primary" type="submit" disabled={scanningFilesystem}>
            {scanningFilesystem ? "Scanning..." : "Scan folders + archives"}
          </button>
        </form>
        <p className="muted">
          The API process must have direct access to this path. Nested folders are scanned recursively. Supported
          archives are extracted automatically and their files are merged into one flat manifest.
        </p>
        {filesystemScan ? (
          <>
            <div className="stats">
              <article>
                <h3>Total files</h3>
                <p>{filesystemScan.total_files}</p>
              </article>
              <article>
                <h3>Regular files</h3>
                <p>{filesystemScan.regular_files}</p>
              </article>
              <article>
                <h3>Files from archives</h3>
                <p>{filesystemScan.extracted_files}</p>
              </article>
              <article>
                <h3>Archives processed</h3>
                <p>{filesystemScan.archives_processed}</p>
              </article>
            </div>
            <p className="muted">
              Root: <code>{filesystemScan.root_path}</code>
            </p>
            <p className="muted">
              Supported: <code>{filesystemScan.supported_archive_extensions.join(", ")}</code>
            </p>
            <div className="inline-actions">
              <button className="btn" onClick={() => void onFilesystemExportRAG()} disabled={exportingFilesystemRAG}>
                {exportingFilesystemRAG ? "Preparing TXT..." : "Export filesystem to TXT RAG"}
              </button>
              {lastFilesystemRAGExport ? (
                <a className="btn link" href={exportDownloadURL(lastFilesystemRAGExport.export_id)}>
                  Download latest TXT RAG
                </a>
              ) : null}
            </div>
            {lastFilesystemRAGExport ? (
              <p className="muted">
                Exported {lastFilesystemRAGExport.exported_files} of {lastFilesystemRAGExport.scanned_files} files;
                skipped {lastFilesystemRAGExport.skipped_files}; written documents {lastFilesystemRAGExport.row_count}.
              </p>
            ) : null}
            {filesystemScan.skipped_archives.length > 0 ? (
              <div className="error-box">
                <strong>Skipped archives</strong>
                <ul className="plain-list">
                  {filesystemScan.skipped_archives.map((item) => (
                    <li key={`${item.path}-${item.reason}`}>
                      <code>{item.path}</code>: {item.reason}
                    </li>
                  ))}
                </ul>
              </div>
            ) : null}
            <div className="table-scroll">
              <table>
                <thead>
                  <tr>
                    <th>Logical path</th>
                    <th>Origin</th>
                    <th>Size</th>
                    <th>Resolved path</th>
                  </tr>
                </thead>
                <tbody>
                  {filesystemScan.files.map((item) => (
                    <tr key={`${item.logical_path}-${item.resolved_path}`}>
                      <td className="mono-cell">{item.logical_path}</td>
                      <td>{item.origin}</td>
                      <td>{formatBytes(item.size_bytes)}</td>
                      <td className="mono-cell">{item.resolved_path}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </>
        ) : null}
      </div>

      <div className="card">
        <h2>Import JSON</h2>
        <form className="grid-form" onSubmit={onJSONImportSubmit}>
          <label>
            JSON file
            <input
              ref={jsonFileInputRef}
              type="file"
              accept=".json,application/json"
              onChange={(e) => setJsonImportFile(e.target.files?.[0] ?? null)}
              required
            />
          </label>
          <button className="btn primary" type="submit" disabled={creatingJSONImport}>
            {creatingJSONImport ? "Uploading..." : "Upload JSON"}
          </button>
        </form>
        {lastJSONImport ? (
          <p>
            Imported {lastJSONImport.importedCount} records, processed {lastJSONImport.processedCount}, duplicates{" "}
            {lastJSONImport.duplicateCount}, trash {lastJSONImport.trashCount}, chunks {lastJSONImport.chunkCount}.{" "}
            <Link className="btn link" to={`/sources/${lastJSONImport.sourceID}`}>
              Open imported source
            </Link>
          </p>
        ) : null}
      </div>

      <div className="card">
        <div className="card-header">
          <h2>Sources</h2>
          <div className="inline-actions">
            <button className="btn" onClick={() => void loadSources()} disabled={loading || clearingHistory}>
              Refresh
            </button>
            <button className="btn" onClick={() => void onClearHistory()} disabled={loading || clearingHistory}>
              {clearingHistory ? "Clearing..." : "Clear history"}
            </button>
          </div>
        </div>

        {error && <p className="error">{error}</p>}

        {loading ? (
          <p>Loading...</p>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Source</th>
                <th>Status</th>
                <th>Type</th>
                <th>Provider</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {sources.map((source) => {
                const isTelegramSource = source.source_type === "telegram_public_channel";
                const isTelegramChannelDocumentSource = source.source_type === "telegram_channel_document";
                const isTelegramLinkSource = source.source_type === "telegram_message_links";
                const isYouTubeSource = source.source_type === "youtube_video";
                const isJSONUploadSource = source.source_type === "json_upload";

                return (
                  <tr key={source.id}>
                      <td>
                        <div className="cell-title">
                          <Link to={`/sources/${source.id}`}>{source.title || source.username || source.url}</Link>
                          <small>{displaySourceURL(source)}</small>
                        </div>
                      </td>
                    <td>
                      <StatusBadge status={source.status} />
                    </td>
                    <td>{source.source_type}</td>
                    <td>{source.provider || "-"}</td>
                    <td>
                      {isTelegramSource || isTelegramLinkSource || isTelegramChannelDocumentSource ? (
                        <>
                          <button className="btn" onClick={() => void onSync(source.id)} disabled={syncingID === source.id}>
                            {syncingID === source.id
                              ? "Syncing..."
                              : isTelegramLinkSource
                                ? "Fetch message links"
                                : isTelegramChannelDocumentSource
                                  ? "Build channel document"
                                : "Sync full history"}
                          </button>
                          <button
                            className="btn"
                            onClick={() => void onExportTXT(source.id)}
                            disabled={exportingID === source.id}
                            style={{ marginLeft: "0.4rem" }}
                          >
                            {exportingID === source.id ? "Preparing TXT..." : "Export TXT RAG"}
                          </button>
                        </>
                      ) : isYouTubeSource ? (
                        <>
                          <button
                            className="btn"
                            onClick={() => void onDownloadAudio(source.id)}
                            disabled={downloadingID === source.id || source.status === "running"}
                          >
                            {downloadingID === source.id ? "Running..." : "Download + Transcribe"}
                          </button>
                          <Link className="btn" to={`/sources/${source.id}`} style={{ marginLeft: "0.4rem" }}>
                            Open
                          </Link>
                        </>
                      ) : isJSONUploadSource ? (
                        <>
                          <button
                            className="btn"
                            onClick={() => void onExportTXT(source.id)}
                            disabled={exportingID === source.id}
                          >
                            {exportingID === source.id ? "Preparing TXT..." : "Export TXT RAG"}
                          </button>
                          <Link className="btn" to={`/sources/${source.id}`} style={{ marginLeft: "0.4rem" }}>
                            Open
                          </Link>
                        </>
                      ) : (
                        <Link className="btn" to={`/sources/${source.id}`}>
                          Open
                        </Link>
                      )}
                    </td>
                  </tr>
                );
              })}
              {sources.length === 0 && (
                <tr>
                  <td colSpan={5}>No sources yet.</td>
                </tr>
              )}
            </tbody>
          </table>
        )}
      </div>
    </section>
  );
}
