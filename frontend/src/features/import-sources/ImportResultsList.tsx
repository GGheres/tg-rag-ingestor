import type { ImportResult } from "./types";
import ImportResultCard from "./ImportResultCard";

type Props = {
  results: ImportResult[];
  onClear: () => void;
};

export default function ImportResultsList({ results, onClear }: Props) {
  if (results.length === 0) {
    return (
      <div className="import-results-empty">
        <div className="import-results-empty-icon">&#8681;</div>
        <h3>No imports yet</h3>
        <p>Select an import method above and add your first source. Results will appear here.</p>
      </div>
    );
  }

  const running = results.filter((r) => r.status === "running").length;
  const succeeded = results.filter((r) => r.status === "success").length;
  const failed = results.filter((r) => r.status === "error").length;

  return (
    <div className="import-results">
      <div className="import-results-header">
        <div>
          <h3>Import History</h3>
          <div className="import-results-summary">
            <span>{results.length} total</span>
            {running > 0 && <span className="warn-text">{running} running</span>}
            {succeeded > 0 && <span className="success-text">{succeeded} succeeded</span>}
            {failed > 0 && <span className="error">{failed} failed</span>}
          </div>
        </div>
        <button className="btn" onClick={onClear} type="button">
          Clear History
        </button>
      </div>

      <div className="import-results-list">
        {results.map((result) => (
          <ImportResultCard key={result.id} result={result} />
        ))}
      </div>
    </div>
  );
}
