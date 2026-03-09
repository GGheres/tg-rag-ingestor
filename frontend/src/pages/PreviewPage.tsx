import { useEffect, useMemo, useState } from "react";
import { getParsedPosts, getSources } from "../api/client";
import type { ParsedPost, Source } from "../types";

const DEFAULT_LIMIT_PER_SOURCE = 200;

export default function PreviewPage() {
  const [sources, setSources] = useState<Source[]>([]);
  const [selectedSourceIDs, setSelectedSourceIDs] = useState<string[]>([]);
  const [limitPerSource, setLimitPerSource] = useState(DEFAULT_LIMIT_PER_SOURCE);
  const [includeDuplicates, setIncludeDuplicates] = useState(false);
  const [posts, setPosts] = useState<ParsedPost[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const sourceLabelByID = useMemo(() => {
    const out = new Map<string, string>();
    for (const source of sources) {
      out.set(source.id, source.title || source.username || source.url);
    }
    return out;
  }, [sources]);

  async function loadPreview(nextSourceIDs: string[], refreshSources = false) {
    try {
      setLoading(true);

      const sourceList = refreshSources || sources.length === 0 ? await getSources() : sources;
      if (refreshSources || sources.length === 0) {
        setSources(sourceList);
      }

      const availableSourceIDSet = new Set(sourceList.map((item) => item.id));
      const normalizedSelection = nextSourceIDs.filter((id) => availableSourceIDSet.has(id));

      if (normalizedSelection.length === 0) {
        setPosts([]);
        setSelectedSourceIDs([]);
        setError(null);
        return;
      }

      const items = await getParsedPosts({
        source_ids: normalizedSelection,
        limit_per_source: limitPerSource,
        include_duplicates: includeDuplicates,
      });

      setPosts(items);
      setSelectedSourceIDs(normalizedSelection);
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    async function bootstrap() {
      const sourceList = await getSources();
      const allSourceIDs = sourceList.map((item) => item.id);
      setSources(sourceList);
      setSelectedSourceIDs(allSourceIDs);
      if (allSourceIDs.length > 0) {
        await loadPreview(allSourceIDs, false);
      }
    }

    void bootstrap().catch((err) => setError((err as Error).message));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  function toggleSourceSelection(sourceID: string) {
    setSelectedSourceIDs((prev) =>
      prev.includes(sourceID) ? prev.filter((item) => item !== sourceID) : [...prev, sourceID]
    );
  }

  return (
    <section className="card">
      <div className="card-header">
        <h2>Parsed Telegram Posts</h2>
        <button className="btn" onClick={() => void loadPreview(selectedSourceIDs, true)} disabled={loading}>
          {loading ? "Refreshing..." : "Refresh sources"}
        </button>
      </div>
      <p>Parse and preview text information from selected Telegram channels.</p>

      <div className="preview-toolbar">
        <label>
          Limit per channel
          <input
            type="number"
            min={1}
            max={2000}
            value={limitPerSource}
            onChange={(event) => setLimitPerSource(Number(event.target.value) || DEFAULT_LIMIT_PER_SOURCE)}
            disabled={loading}
          />
        </label>
        <label className="checkbox-label">
          <input
            type="checkbox"
            checked={includeDuplicates}
            onChange={(event) => setIncludeDuplicates(event.target.checked)}
            disabled={loading}
          />
          Include duplicates
        </label>
      </div>

      <div className="selection-actions">
        <button
          className="btn"
          onClick={() => setSelectedSourceIDs(sources.map((source) => source.id))}
          disabled={loading || sources.length === 0}
        >
          Select all
        </button>
        <button className="btn" onClick={() => setSelectedSourceIDs([])} disabled={loading || sources.length === 0}>
          Clear selection
        </button>
        <button className="btn primary" onClick={() => void loadPreview(selectedSourceIDs, false)} disabled={loading}>
          {loading ? "Loading..." : "Load parsed posts"}
        </button>
      </div>

      <div className="source-select-grid">
        {sources.map((source) => (
          <label key={source.id} className="source-checkbox">
            <input
              type="checkbox"
              checked={selectedSourceIDs.includes(source.id)}
              onChange={() => toggleSourceSelection(source.id)}
              disabled={loading}
            />
            <span>{source.title || source.username || source.url}</span>
          </label>
        ))}
        {sources.length === 0 && <p>No sources found. Add channels on the Sources page first.</p>}
      </div>

      {error && <p className="error">{error}</p>}
      <p className="preview-meta">
        Selected channels: {selectedSourceIDs.length} • Parsed posts: {posts.length}
      </p>

      <div className="list-scroll">
        {posts.map((post) => {
          const hashtags = Array.isArray(post.hashtags) ? post.hashtags : [];
          const mentions = Array.isArray(post.mentions) ? post.mentions : [];
          const links = Array.isArray(post.links) ? post.links : [];

          return (
            <article key={post.document_id} className="list-item">
              <header>
                <div className="cell-title">
                  <strong>{post.external_doc_id}</strong>
                  <small>
                    {sourceLabelByID.get(post.source_id) ?? post.source_id} • #{post.telegram_message_id} •{" "}
                    {new Date(post.published_at ?? post.created_at).toLocaleString()}
                  </small>
                </div>
              </header>

              <p className="preview-submeta">
                <a href={post.post_url || post.channel_url} target="_blank" rel="noreferrer">
                  {post.post_url || post.channel_url}
                </a>
              </p>
              <p className="preview-submeta">
                Hashtags: {hashtags.length > 0 ? hashtags.join(", ") : "-"} | Mentions:{" "}
                {mentions.length > 0 ? mentions.join(", ") : "-"} | Links: {links.length > 0 ? links.join(", ") : "-"}
              </p>
              <pre className="cleaned-preview">{post.text_clean || "(empty after cleaning)"}</pre>
            </article>
          );
        })}
        {posts.length === 0 && !loading && <p>No parsed posts found for selected channels.</p>}
      </div>
    </section>
  );
}
