import { FormEvent, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { createSource, getSources, importJSONSource, syncSource } from "../api/client";
import StatusBadge from "../components/StatusBadge";
import type { Source } from "../types";

export default function SourcesPage() {
  const [sources, setSources] = useState<Source[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [syncingID, setSyncingID] = useState<string | null>(null);
  const [form, setForm] = useState({ url: "", username: "", title: "" });
  const [creating, setCreating] = useState(false);
  const [importFileKey, setImportFileKey] = useState(0);
  const [importFile, setImportFile] = useState<File | null>(null);
  const [importTitle, setImportTitle] = useState("");
  const [importing, setImporting] = useState(false);
  const [importSummary, setImportSummary] = useState<string | null>(null);

  async function loadSources() {
    try {
      setLoading(true);
      const data = await getSources();
      setSources(data);
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void loadSources();
  }, []);

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    try {
      setCreating(true);
      await createSource({
        url: form.url || undefined,
        username: form.username || undefined,
        title: form.title || undefined,
      });
      setForm({ url: "", username: "", title: "" });
      await loadSources();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setCreating(false);
    }
  }

  async function handleSync(id: string) {
    try {
      setSyncingID(id);
      await syncSource(id, { full_resync: true });
      await loadSources();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSyncingID(null);
    }
  }

  async function onImportSubmit(event: FormEvent) {
    event.preventDefault();
    if (!importFile) {
      setError("Select a JSON file to import.");
      return;
    }
    try {
      setImporting(true);
      const formData = new FormData();
      formData.append("file", importFile);
      if (importTitle.trim() !== "") {
        formData.append("title", importTitle.trim());
      }
      const result = await importJSONSource(formData);
      setImportSummary(
        `Imported ${result.imported_count} records. Processed: ${result.processed_count}, duplicates: ${result.duplicate_count}, trash: ${result.trash_count}.`
      );
      setImportFile(null);
      setImportTitle("");
      setImportFileKey((prev) => prev + 1);
      setError(null);
      await loadSources();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setImporting(false);
    }
  }

  return (
    <section>
      <div className="card">
        <h2>Add Source</h2>
        <form className="grid-form" onSubmit={onSubmit}>
          <label>
            Telegram URL
            <input
              type="text"
              placeholder="https://t.me/somechannel"
              value={form.url}
              onChange={(e) => setForm((prev) => ({ ...prev, url: e.target.value }))}
            />
          </label>
          <label>
            Username
            <input
              type="text"
              placeholder="@somechannel"
              value={form.username}
              onChange={(e) => setForm((prev) => ({ ...prev, username: e.target.value }))}
            />
          </label>
          <label>
            Title (optional)
            <input
              type="text"
              placeholder="Display title"
              value={form.title}
              onChange={(e) => setForm((prev) => ({ ...prev, title: e.target.value }))}
            />
          </label>
          <button className="btn primary" type="submit" disabled={creating}>
            {creating ? "Creating..." : "Add source"}
          </button>
        </form>
      </div>

      <div className="card">
        <h2>Import JSON For Cleaning</h2>
        <form className="grid-form" onSubmit={onImportSubmit}>
          <label>
            JSON file
            <input
              key={importFileKey}
              type="file"
              accept="application/json,.json"
              onChange={(e) => setImportFile(e.target.files?.[0] ?? null)}
            />
          </label>
          <label>
            Title (optional)
            <input
              type="text"
              placeholder="Imported dataset title"
              value={importTitle}
              onChange={(e) => setImportTitle(e.target.value)}
            />
          </label>
          <button className="btn primary" type="submit" disabled={importing}>
            {importing ? "Importing..." : "Import and clean"}
          </button>
        </form>
        {importSummary && <p>{importSummary}</p>}
      </div>

      <div className="card">
        <div className="card-header">
          <h2>Sources</h2>
          <button className="btn" onClick={() => void loadSources()} disabled={loading}>
            Refresh
          </button>
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
                <th>Last message</th>
                <th>Last sync</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {sources.map((source) => (
                <tr key={source.id}>
                  <td>
                    <div className="cell-title">
                      <Link to={`/sources/${source.id}`}>{source.title || source.username || source.url}</Link>
                      <small>{source.url}</small>
                    </div>
                  </td>
                  <td>
                    <StatusBadge status={source.status} />
                  </td>
                  <td>{source.last_message_id ?? "-"}</td>
                  <td>{source.last_synced_at ? new Date(source.last_synced_at).toLocaleString() : "-"}</td>
                  <td>
                    <button
                      className="btn"
                      onClick={() => void handleSync(source.id)}
                      disabled={syncingID === source.id || source.source_type !== "telegram_public_channel"}
                    >
                      {source.source_type === "telegram_public_channel"
                        ? syncingID === source.id
                          ? "Syncing..."
                          : "Sync full history"
                        : "Imported"}
                    </button>
                  </td>
                </tr>
              ))}
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
