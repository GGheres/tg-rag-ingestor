import { useState } from "react";
import {
  createSource,
  createTelegramChannelDocumentSource,
  createTelegramMessageLinkSource,
} from "../../api/client";
import { TELEGRAM_MODES, type TelegramMode, type ImportResult } from "./types";

type Props = {
  onResult: (result: ImportResult) => void;
};

export default function TelegramImportForm({ onResult }: Props) {
  const [mode, setMode] = useState<TelegramMode>("channel-document");
  const [channel, setChannel] = useState("");
  const [title, setTitle] = useState("");
  const [messageLinks, setMessageLinks] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const canSubmit = () => {
    if (loading) return false;
    if (mode === "message-links") return messageLinks.trim().length > 0;
    return channel.trim().length > 0;
  };

  const handleSubmit = async () => {
    setError(null);
    setLoading(true);

    const now = new Date().toISOString();
    const resultBase = {
      id: crypto.randomUUID(),
      method: "telegram" as const,
      telegramMode: mode,
      startedAt: now,
    };

    try {
      if (mode === "channel-document") {
        const source = await createTelegramChannelDocumentSource({
          title: title.trim() || undefined,
          url: channel.trim(),
        });
        onResult({
          ...resultBase,
          title: source.title || source.url,
          url: source.url,
          status: "success",
          itemLabel: "document",
          itemCount: 1,
          finishedAt: new Date().toISOString(),
          summary: "Channel imported as a single RAG document",
        });
      } else if (mode === "regular-source") {
        const source = await createSource({
          url: channel.trim(),
          username: channel.trim().replace(/^@/, "").replace(/^https?:\/\/t\.me\//, ""),
        });
        onResult({
          ...resultBase,
          title: source.title || source.url,
          url: source.url,
          status: "success",
          itemLabel: "source",
          itemCount: 1,
          finishedAt: new Date().toISOString(),
          summary: "Source created. Use sync to fetch messages.",
        });
      } else {
        const links = messageLinks
          .split("\n")
          .map((l) => l.trim())
          .filter(Boolean);
        const source = await createTelegramMessageLinkSource({
          title: title.trim() || undefined,
          message_links: links,
        });
        onResult({
          ...resultBase,
          title: source.title || "Message Links Import",
          status: "success",
          itemLabel: "messages",
          itemCount: links.length,
          finishedAt: new Date().toISOString(),
          summary: `${links.length} message links imported`,
        });
      }
      setChannel("");
      setTitle("");
      setMessageLinks("");
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Unknown error";
      setError(msg);
      onResult({
        ...resultBase,
        title: title.trim() || channel.trim() || "Telegram Import",
        status: "error",
        itemLabel: mode === "message-links" ? "messages" : "source",
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
      <div className="telegram-modes">
        {TELEGRAM_MODES.map((m) => (
          <label key={m.id} className={`telegram-mode-option ${mode === m.id ? "active" : ""}`}>
            <input
              type="radio"
              name="telegram-mode"
              value={m.id}
              checked={mode === m.id}
              onChange={() => setMode(m.id)}
            />
            <span className="telegram-mode-content">
              <span className="telegram-mode-label">{m.label}</span>
              <span className="telegram-mode-desc">{m.description}</span>
            </span>
          </label>
        ))}
      </div>

      <div className="import-form-fields">
        {mode !== "message-links" && (
          <label>
            Channel username or link
            <input
              type="text"
              value={channel}
              onChange={(e) => setChannel(e.target.value)}
              placeholder="@channel_name or https://t.me/channel_name"
              disabled={loading}
            />
          </label>
        )}

        {mode === "message-links" && (
          <label>
            Message links (one per line)
            <textarea
              value={messageLinks}
              onChange={(e) => setMessageLinks(e.target.value)}
              placeholder={"https://t.me/channel/123\nhttps://t.me/channel/456\nhttps://t.me/channel/789"}
              rows={5}
              disabled={loading}
            />
          </label>
        )}

        <label>
          Source title (optional)
          <input
            type="text"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="Custom name for this import"
            disabled={loading}
          />
        </label>
      </div>

      {error && <div className="import-inline-error">{error}</div>}

      <div className="import-form-actions">
        <button
          className="btn primary"
          disabled={!canSubmit()}
          onClick={handleSubmit}
          type="button"
        >
          {loading ? "Importing..." : "Import from Telegram"}
        </button>
      </div>
    </div>
  );
}
