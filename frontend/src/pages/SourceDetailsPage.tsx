import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { documentDownloadURL, getDocument, getDocuments, getRawMessages, getSource, syncSource } from "../api/client";
import StatusBadge from "../components/StatusBadge";
import type { Chunk, Document, RawMessage, Source, SourceStats } from "../types";

export default function SourceDetailsPage() {
  const { id = "" } = useParams();
  const [source, setSource] = useState<Source | null>(null);
  const [stats, setStats] = useState<SourceStats | null>(null);
  const [rawMessages, setRawMessages] = useState<RawMessage[]>([]);
  const [documents, setDocuments] = useState<Document[]>([]);
  const [selectedDocument, setSelectedDocument] = useState<Document | null>(null);
  const [chunks, setChunks] = useState<Chunk[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [syncing, setSyncing] = useState(false);

  async function loadAll() {
    try {
      setLoading(true);
      const [sourceResp, rawResp, docsResp] = await Promise.all([
        getSource(id),
        getRawMessages(id, 50),
        getDocuments(id, 50),
      ]);
      setSource(sourceResp.source);
      setStats(sourceResp.stats);
      setRawMessages(rawResp);
      setDocuments(docsResp);
      if (docsResp.length > 0) {
        const detail = await getDocument(docsResp[0].id);
        setSelectedDocument(detail.document);
        setChunks(detail.chunks);
      } else {
        setSelectedDocument(null);
        setChunks([]);
      }
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void loadAll();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);

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

  if (loading) {
    return <p>Loading source details...</p>;
  }

  if (!source) {
    return <p>Source not found.</p>;
  }

  return (
    <section className="details-grid">
      {error && <p className="error">{error}</p>}

      <div className="card">
        <div className="card-header">
          <h2>{source.title || source.username || source.url}</h2>
          <button
            className="btn primary"
            onClick={() => void onSync()}
            disabled={syncing || source.source_type !== "telegram_public_channel"}
          >
            {source.source_type === "telegram_public_channel"
              ? syncing
                ? "Syncing full history..."
                : "Sync full history"
              : "Imported source"}
          </button>
        </div>
        <p>{source.url}</p>
        <StatusBadge status={source.status} />
        {source.last_error && <p className="error">{source.last_error}</p>}
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

      <div className="card">
        <h2>Recent Raw Messages</h2>
        <div className="list-scroll">
          {rawMessages.map((item) => (
            <article key={item.id} className="list-item">
              <header>
                <strong>#{item.telegram_message_id}</strong>
                <small>{item.posted_at ? new Date(item.posted_at).toLocaleString() : "-"}</small>
              </header>
              <p>{item.text_raw || item.caption_raw || "(empty)"}</p>
            </article>
          ))}
          {rawMessages.length === 0 && <p>No raw messages yet.</p>}
        </div>
      </div>

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
                <a className="btn" href={documentDownloadURL(selectedDocument.id, "txt")}>
                  Download TXT
                </a>
                <a className="btn" href={documentDownloadURL(selectedDocument.id, "json")}>
                  Download JSON
                </a>
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
