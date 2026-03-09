import { FormEvent, useEffect, useState } from "react";
import { createExport, exportDownloadURL, getExports, getSources } from "../api/client";
import StatusBadge from "../components/StatusBadge";
import type { ExportRecord, Source } from "../types";

export default function ExportsPage() {
  const [exports, setExports] = useState<ExportRecord[]>([]);
  const [sources, setSources] = useState<Source[]>([]);
  const [sourceID, setSourceID] = useState("");
  const [mode, setMode] = useState<"documents" | "chunks">("documents");
  const [format, setFormat] = useState<"jsonl" | "txt_rag">("jsonl");
  const [includeDuplicates, setIncludeDuplicates] = useState(false);
  const [includeTrash, setIncludeTrash] = useState(false);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function loadAll() {
    try {
      setLoading(true);
      const [exportsData, sourcesData] = await Promise.all([getExports(), getSources()]);
      setExports(exportsData);
      setSources(sourcesData);
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void loadAll();
  }, []);

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    try {
      setCreating(true);
      await createExport({
        source_id: sourceID || undefined,
        mode,
        format,
        include_duplicates: includeDuplicates,
        include_trash: includeTrash,
      });
      await loadAll();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setCreating(false);
    }
  }

  return (
    <section>
      <div className="card">
        <h2>Create Export</h2>
        <form className="grid-form" onSubmit={onSubmit}>
          <label>
            Source
            <select value={sourceID} onChange={(e) => setSourceID(e.target.value)}>
              <option value="">All sources</option>
              {sources.map((source) => (
                <option key={source.id} value={source.id}>
                  {source.title || source.username || source.url}
                </option>
              ))}
            </select>
          </label>
          <label>
            Mode
            <select
              value={mode}
              onChange={(e) => setMode(e.target.value as "documents" | "chunks")}
              disabled={format === "txt_rag"}
            >
              <option value="documents">Documents</option>
              <option value="chunks">Chunks</option>
            </select>
          </label>
          <label>
            Format
            <select
              value={format}
              onChange={(e) => {
                const next = e.target.value as "jsonl" | "txt_rag";
                setFormat(next);
                if (next === "txt_rag") {
                  setMode("documents");
                }
              }}
            >
              <option value="jsonl">JSONL</option>
              <option value="txt_rag">TXT (RAG markup)</option>
            </select>
          </label>
          <label>
            Include duplicates
            <select
              value={includeDuplicates ? "yes" : "no"}
              onChange={(e) => setIncludeDuplicates(e.target.value === "yes")}
            >
              <option value="no">No</option>
              <option value="yes">Yes</option>
            </select>
          </label>
          <label>
            Include trash
            <select value={includeTrash ? "yes" : "no"} onChange={(e) => setIncludeTrash(e.target.value === "yes")}>
              <option value="no">No</option>
              <option value="yes">Yes</option>
            </select>
          </label>
          <button className="btn primary" type="submit" disabled={creating}>
            {creating ? "Exporting..." : "Create export"}
          </button>
        </form>
      </div>

      <div className="card">
        <div className="card-header">
          <h2>Exports</h2>
          <button className="btn" onClick={() => void loadAll()} disabled={loading}>
            Refresh
          </button>
        </div>
        {error && <p className="error">{error}</p>}
        {loading ? (
          <p>Loading exports...</p>
        ) : (
          <table>
            <thead>
              <tr>
                <th>ID</th>
                <th>Type</th>
                <th>Status</th>
                <th>Rows</th>
                <th>File</th>
                <th>Download</th>
                <th>Created</th>
              </tr>
            </thead>
            <tbody>
              {exports.map((item) => (
                <tr key={item.id}>
                  <td>{item.id.slice(0, 8)}...</td>
                  <td>{item.export_type}</td>
                  <td>
                    <StatusBadge status={item.status} />
                  </td>
                  <td>{item.row_count}</td>
                  <td>{item.file_path || "-"}</td>
                  <td>
                    {item.status === "succeeded" ? (
                      <a className="btn" href={exportDownloadURL(item.id)}>
                        Download
                      </a>
                    ) : (
                      "-"
                    )}
                  </td>
                  <td>{new Date(item.created_at).toLocaleString()}</td>
                </tr>
              ))}
              {exports.length === 0 && (
                <tr>
                  <td colSpan={7}>No exports yet.</td>
                </tr>
              )}
            </tbody>
          </table>
        )}
      </div>
    </section>
  );
}
