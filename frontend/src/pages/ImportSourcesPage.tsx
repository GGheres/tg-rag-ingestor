import { useState } from "react";
import ImportMethodTabs from "../features/import-sources/ImportMethodTabs";
import TelegramImportForm from "../features/import-sources/TelegramImportForm";
import YouTubeImportForm from "../features/import-sources/YouTubeImportForm";
import AudioUploadImportForm from "../features/import-sources/AudioUploadImportForm";
import LocalFolderImportForm from "../features/import-sources/LocalFolderImportForm";
import JsonImportForm from "../features/import-sources/JsonImportForm";
import HHResumeImportForm from "../features/import-sources/HHResumeImportForm";
import ImportResultsList from "../features/import-sources/ImportResultsList";
import type { ImportMethod, ImportResult } from "../features/import-sources/types";
import { MOCK_RESULTS } from "../features/import-sources/types";

const USE_MOCK = false;

export default function ImportSourcesPage() {
  const [activeMethod, setActiveMethod] = useState<ImportMethod>("telegram");
  const [results, setResults] = useState<ImportResult[]>(USE_MOCK ? MOCK_RESULTS : []);

  const addResult = (result: ImportResult) => {
    setResults((prev) => [result, ...prev]);
  };

  const clearResults = () => setResults([]);

  return (
    <div className="import-page">
      <section className="card import-card">
        <ImportMethodTabs active={activeMethod} onChange={setActiveMethod} />

        <div className="import-form-container">
          {activeMethod === "telegram" && <TelegramImportForm onResult={addResult} />}
          {activeMethod === "youtube" && <YouTubeImportForm onResult={addResult} />}
          {activeMethod === "audio-upload" && <AudioUploadImportForm onResult={addResult} />}
          {activeMethod === "local-folder" && <LocalFolderImportForm onResult={addResult} />}
          {activeMethod === "json" && <JsonImportForm onResult={addResult} />}
          {activeMethod === "hh-resumes" && <HHResumeImportForm onResult={addResult} />}
        </div>
      </section>

      <section className="import-results-section">
        <ImportResultsList results={results} onClear={clearResults} />
      </section>
    </div>
  );
}
