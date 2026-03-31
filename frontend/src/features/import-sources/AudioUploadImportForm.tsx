import { useState, useRef } from "react";
import { uploadAndTranscribeAudio } from "../../api/client";
import type { ImportResult } from "./types";

type Props = {
  onResult: (result: ImportResult) => void;
};

export default function AudioUploadImportForm({ onResult }: Props) {
  const [file, setFile] = useState<File | null>(null);
  const [title, setTitle] = useState("");
  const [language, setLanguage] = useState("");
  const [speakers, setSpeakers] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const canSubmit = !loading && file !== null;

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const selected = e.target.files?.[0] ?? null;
    setFile(selected);
    setError(null);
  };

  const handleSubmit = async () => {
    if (!file) return;
    setError(null);
    setLoading(true);

    const now = new Date().toISOString();
    const resultBase = {
      id: crypto.randomUUID(),
      method: "audio-upload" as const,
      startedAt: now,
    };

    try {
      const result = await uploadAndTranscribeAudio({
        file,
        title: title.trim() || undefined,
        language: language.trim() || undefined,
        speakers: speakers.trim() ? parseInt(speakers.trim(), 10) : undefined,
      });

      const speakerCount = Object.keys(result.speaker_roles || {}).length;
      onResult({
        ...resultBase,
        title: title.trim() || file.name,
        status: "success",
        itemLabel: "speakers",
        itemCount: speakerCount,
        finishedAt: new Date().toISOString(),
        summary: `Transcribed with ${speakerCount} speaker(s), ${result.chunk_count} chunks created`,
        documentId: result.document_id,
      });

      setFile(null);
      setTitle("");
      setLanguage("");
      setSpeakers("");
      if (fileInputRef.current) fileInputRef.current.value = "";
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Unknown error";
      setError(msg);
      onResult({
        ...resultBase,
        title: title.trim() || file.name,
        status: "error",
        itemLabel: "files",
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
          Audio file
          <input
            ref={fileInputRef}
            type="file"
            accept=".wav,.mp3,.m4a,.webm,.opus,.ogg,.flac,.mp4"
            onChange={handleFileChange}
            disabled={loading}
          />
          <span className="field-hint">
            Supported: WAV, MP3, M4A, WEBM, OPUS, OGG, FLAC, MP4
          </span>
        </label>

        <label>
          Title (optional)
          <input
            type="text"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="Custom name for this transcription"
            disabled={loading}
          />
        </label>

        <label>
          Number of speakers (optional)
          <input
            type="number"
            min={1}
            max={20}
            value={speakers}
            onChange={(e) => setSpeakers(e.target.value)}
            placeholder="e.g. 2"
            disabled={loading}
          />
          <span className="field-hint">
            Specify for better diarization. Leave empty for auto-detect.
          </span>
        </label>

        <label>
          Language (optional)
          <input
            type="text"
            value={language}
            onChange={(e) => setLanguage(e.target.value)}
            placeholder="e.g. ru, en, de (auto-detect if empty)"
            disabled={loading}
          />
          <span className="field-hint">
            Leave empty for automatic language detection.
          </span>
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
          {loading ? "Transcribing..." : "Upload & Transcribe"}
        </button>
      </div>
    </div>
  );
}
