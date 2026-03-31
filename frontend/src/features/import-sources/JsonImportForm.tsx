import { useState, useRef } from "react";
import { importJSONFile } from "../../api/client";
import type { ImportResult } from "./types";

type Props = {
  onResult: (result: ImportResult) => void;
};

type ValidationState = "idle" | "valid" | "invalid";

export default function JsonImportForm({ onResult }: Props) {
  const [jsonText, setJsonText] = useState("");
  const [title, setTitle] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [validation, setValidation] = useState<ValidationState>("idle");
  const [validationMsg, setValidationMsg] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const validateJson = (text: string) => {
    if (!text.trim()) {
      setValidation("idle");
      setValidationMsg("");
      return;
    }
    try {
      const parsed = JSON.parse(text);
      const isArray = Array.isArray(parsed);
      const count = isArray ? parsed.length : Object.keys(parsed).length;
      setValidation("valid");
      setValidationMsg(
        isArray ? `Valid JSON array with ${count} items` : `Valid JSON object with ${count} keys`
      );
    } catch {
      setValidation("invalid");
      setValidationMsg("Invalid JSON syntax");
    }
  };

  const handleTextChange = (text: string) => {
    setJsonText(text);
    validateJson(text);
  };

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const selected = e.target.files?.[0] ?? null;
    setFile(selected);
    if (selected) {
      setJsonText("");
      setValidation("idle");
      setValidationMsg("");
    }
  };

  const canSubmit = !loading && (file !== null || (jsonText.trim().length > 0 && validation === "valid"));

  const handleSubmit = async () => {
    setError(null);
    setLoading(true);

    const now = new Date().toISOString();
    const resultBase = {
      id: crypto.randomUUID(),
      method: "json" as const,
      startedAt: now,
    };

    let uploadFile: File;
    if (file) {
      uploadFile = file;
    } else {
      uploadFile = new File([jsonText], "import.json", { type: "application/json" });
    }

    try {
      const result = await importJSONFile({ file: uploadFile });
      onResult({
        ...resultBase,
        title: title.trim() || file?.name || "JSON Import",
        status: result.trash_count > 0 ? "partial" : "success",
        itemLabel: "records",
        itemCount: result.imported_count,
        finishedAt: new Date().toISOString(),
        summary: `${result.imported_count} imported, ${result.processed_count} processed, ${result.duplicate_count} duplicates, ${result.chunk_count} chunks`,
      });
      setJsonText("");
      setTitle("");
      setFile(null);
      setValidation("idle");
      setValidationMsg("");
      if (fileInputRef.current) fileInputRef.current.value = "";
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Unknown error";
      setError(msg);
      onResult({
        ...resultBase,
        title: title.trim() || file?.name || "JSON Import",
        status: "error",
        itemLabel: "records",
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
          Upload JSON file
          <input
            ref={fileInputRef}
            type="file"
            accept=".json,application/json"
            onChange={handleFileChange}
            disabled={loading}
          />
        </label>

        <div className="import-divider">
          <span>or paste JSON directly</span>
        </div>

        <label>
          JSON content
          <textarea
            value={jsonText}
            onChange={(e) => handleTextChange(e.target.value)}
            placeholder='[{"text": "document content", "metadata": {...}}]'
            rows={6}
            disabled={loading || file !== null}
            className={
              validation === "valid"
                ? "validation-valid"
                : validation === "invalid"
                ? "validation-invalid"
                : ""
            }
          />
          {validationMsg && (
            <span className={`field-hint ${validation === "invalid" ? "error" : "success-text"}`}>
              {validationMsg}
            </span>
          )}
        </label>

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
          disabled={!canSubmit}
          onClick={handleSubmit}
          type="button"
        >
          {loading ? "Importing..." : "Import JSON"}
        </button>
      </div>
    </div>
  );
}
