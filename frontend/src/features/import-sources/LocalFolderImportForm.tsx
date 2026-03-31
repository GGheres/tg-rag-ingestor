import { useState } from "react";
import { scanFilesystemDirectory, createFilesystemRAGExport } from "../../api/client";
import type { ImportResult } from "./types";
import type { FilesystemScanResult } from "../../types";

type Props = {
  onResult: (result: ImportResult) => void;
};

export default function LocalFolderImportForm({ onResult }: Props) {
  const [folderPath, setFolderPath] = useState("");
  const [title, setTitle] = useState("");
  const [recursive, setRecursive] = useState(true);
  const [fileTypeFilter, setFileTypeFilter] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [scanResult, setScanResult] = useState<FilesystemScanResult | null>(null);

  const canSubmit = !loading && folderPath.trim().length > 0;

  const handleScan = async () => {
    setError(null);
    setLoading(true);
    setScanResult(null);

    try {
      const result = await scanFilesystemDirectory({ path: folderPath.trim() });
      setScanResult(result);
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Unknown error";
      setError(msg);
    } finally {
      setLoading(false);
    }
  };

  const handleExport = async () => {
    setError(null);
    setLoading(true);

    const now = new Date().toISOString();
    const resultBase = {
      id: crypto.randomUUID(),
      method: "local-folder" as const,
      startedAt: now,
    };

    try {
      const result = await createFilesystemRAGExport({ path: folderPath.trim() });
      onResult({
        ...resultBase,
        title: title.trim() || folderPath.trim(),
        path: folderPath.trim(),
        status: result.skipped_files > 0 ? "partial" : "success",
        itemLabel: "files",
        itemCount: result.exported_files,
        finishedAt: new Date().toISOString(),
        summary: `${result.exported_files} files exported, ${result.skipped_files} skipped, ${result.row_count} rows`,
      });
      setFolderPath("");
      setTitle("");
      setScanResult(null);
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Unknown error";
      setError(msg);
      onResult({
        ...resultBase,
        title: title.trim() || folderPath.trim(),
        path: folderPath.trim(),
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
          Folder path
          <input
            type="text"
            value={folderPath}
            onChange={(e) => setFolderPath(e.target.value)}
            placeholder="/path/to/your/documents"
            disabled={loading}
          />
        </label>

        <div className="import-form-row">
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

          <label>
            File type filter (optional)
            <input
              type="text"
              value={fileTypeFilter}
              onChange={(e) => setFileTypeFilter(e.target.value)}
              placeholder=".txt, .md, .pdf"
              disabled={loading}
            />
            <span className="field-hint">Comma-separated extensions</span>
          </label>
        </div>

        <label className="checkbox-label">
          <input
            type="checkbox"
            checked={recursive}
            onChange={(e) => setRecursive(e.target.checked)}
            disabled={loading}
          />
          Scan subdirectories recursively
        </label>
      </div>

      {scanResult && (
        <div className="scan-preview">
          <div className="scan-preview-header">
            <strong>Scan Results</strong>
            <span className="muted">{scanResult.root_path}</span>
          </div>
          <div className="scan-preview-stats">
            <span>{scanResult.total_files} files found</span>
            <span>{scanResult.regular_files} regular</span>
            <span>{scanResult.extracted_files} from archives</span>
            {scanResult.skipped_archives.length > 0 && (
              <span className="warn-text">{scanResult.skipped_archives.length} archives skipped</span>
            )}
          </div>
        </div>
      )}

      {error && <div className="import-inline-error">{error}</div>}

      <div className="import-form-actions">
        <button
          className="btn"
          disabled={!canSubmit}
          onClick={handleScan}
          type="button"
        >
          {loading && !scanResult ? "Scanning..." : "Preview Scan"}
        </button>
        <button
          className="btn primary"
          disabled={!canSubmit}
          onClick={handleExport}
          type="button"
        >
          {loading && scanResult ? "Exporting..." : "Import Folder"}
        </button>
      </div>
    </div>
  );
}
