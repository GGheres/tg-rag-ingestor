import { useState } from "react";
import { createYouTubeSource } from "../../api/client";
import type { ImportResult } from "./types";

type Props = {
  onResult: (result: ImportResult) => void;
};

export default function YouTubeImportForm({ onResult }: Props) {
  const [url, setUrl] = useState("");
  const [title, setTitle] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const canSubmit = !loading && url.trim().length > 0;

  const handleSubmit = async () => {
    setError(null);
    setLoading(true);

    const now = new Date().toISOString();
    const resultBase = {
      id: crypto.randomUUID(),
      method: "youtube" as const,
      startedAt: now,
    };

    try {
      const source = await createYouTubeSource({ url: url.trim() });
      onResult({
        ...resultBase,
        title: title.trim() || source.title || source.url,
        url: source.url,
        status: "success",
        itemLabel: "videos",
        itemCount: 1,
        finishedAt: new Date().toISOString(),
        summary: "YouTube source created successfully",
      });
      setUrl("");
      setTitle("");
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Unknown error";
      setError(msg);
      onResult({
        ...resultBase,
        title: title.trim() || url.trim(),
        url: url.trim(),
        status: "error",
        itemLabel: "videos",
        itemCount: 0,
        finishedAt: new Date().toISOString(),
        error: msg,
      });
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="import-form">
      <div className="import-form-fields">
        <label>
          YouTube URL
          <input
            type="url"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="Video, channel, or playlist URL"
            disabled={loading}
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
            disabled={loading}
          />
        </label>
      </div>

      {error && <div className="import-inline-error">{error}</div>}

      <div className="import-form-actions">
        <button
          className="btn primary"
          disabled={!canSubmit}
          onClick={handleSubmit}
          type="button"
        >
          {loading ? "Importing..." : "Import from YouTube"}
        </button>
      </div>
    </div>
  );
}
