import { FormEvent, useEffect, useMemo, useState } from "react";
import {
  exchangeHHOAuthCode,
  getHHConfig,
  getHHExtraction,
  hhExtractionFileURL,
  listHHExtractions,
  listHHVacancies,
  startHHExtraction,
} from "../../api/client";
import type {
  HHConfigStatus,
  HHExtractionDetails,
  HHExtractionListItem,
  HHOAuthExchangeResponse,
  HHVacancyCatalog,
  HHVacancyListItem,
} from "../../types";
import StatusBadge from "../../components/StatusBadge";
import type { ImportResult } from "./types";

type Props = {
  onResult: (result: ImportResult) => void;
};

function formatDate(value?: string): string {
  if (!value) return "-";
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return value;
  return parsed.toLocaleString();
}

function findOAuthCode(value: string): string | null {
  const trimmed = value.trim();
  if (!trimmed) return null;

  if (/^https?:\/\//i.test(trimmed)) {
    try {
      const url = new URL(trimmed);
      const code = url.searchParams.get("code");
      if (code?.trim()) return code.trim();
    } catch {
      // fall through
    }
  }

  const queryStart = trimmed.indexOf("?");
  const queryLikeValue =
    queryStart >= 0
      ? trimmed.slice(queryStart + 1)
      : trimmed.startsWith("?") || trimmed.includes("=")
      ? trimmed
      : "";
  if (!queryLikeValue) return null;

  const params = new URLSearchParams(queryLikeValue.replace(/^[?#]/, ""));
  const code = params.get("code");
  if (code?.trim()) return code.trim();

  return null;
}

function clearOAuthCodeFromURL() {
  const url = new URL(window.location.href);
  if (!url.searchParams.has("code")) return;
  url.searchParams.delete("code");
  const nextURL = `${url.pathname}${url.search}${url.hash}`;
  window.history.replaceState({}, document.title, nextURL);
}

export default function HHResumeImportForm({ onResult }: Props) {
  const [config, setConfig] = useState<HHConfigStatus | null>(null);
  const [items, setItems] = useState<HHExtractionListItem[]>([]);
  const [vacancyCatalog, setVacancyCatalog] = useState<HHVacancyCatalog | null>(null);
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [details, setDetails] = useState<HHExtractionDetails | null>(null);
  const [oauthResult, setOauthResult] = useState<HHOAuthExchangeResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [authSubmitting, setAuthSubmitting] = useState(false);
  const [vacancyLoading, setVacancyLoading] = useState(false);

  const [form, setForm] = useState({
    vacancyID: "",
    managerAccountID: "",
    dryRun: false,
    exportPDF: false,
    saveOriginals: true,
    coverLetterOnly: false,
  });
  const [oauthCode, setOauthCode] = useState("");
  const [showReauth, setShowReauth] = useState(false);

  async function loadConfigAndExtractions(options?: { silent?: boolean }) {
    const silent = Boolean(options?.silent);
    try {
      if (!silent) setLoading(true);
      const [cfgResp, listResp] = await Promise.all([getHHConfig(), listHHExtractions()]);
      setConfig(cfgResp);
      setItems(listResp);
      if (!selectedID && listResp.length > 0) setSelectedID(listResp[0].id);
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      if (!silent) setLoading(false);
    }
  }

  useEffect(() => {
    void loadConfigAndExtractions();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    const codeFromURL = findOAuthCode(window.location.href);
    if (codeFromURL) setOauthCode((current) => current || codeFromURL);
  }, []);

  useEffect(() => {
    const hasRunning = items.some((item) => item.status === "running");
    if (!hasRunning) return;
    const timer = window.setInterval(() => {
      void loadConfigAndExtractions({ silent: true });
      if (selectedID) void loadDetails(selectedID);
    }, 3000);
    return () => window.clearInterval(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [items, selectedID]);

  async function loadDetails(extractionID: string) {
    try {
      const detail = await getHHExtraction(extractionID);
      setDetails(detail);
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    }
  }

  useEffect(() => {
    if (!selectedID) {
      setDetails(null);
      return;
    }
    void loadDetails(selectedID);
  }, [selectedID]);

  async function onStartExtraction(event: FormEvent) {
    event.preventDefault();
    if (!form.vacancyID.trim()) {
      setError("vacancy_id is required");
      return;
    }

    const now = new Date().toISOString();
    try {
      setSubmitting(true);
      await startHHExtraction({
        vacancy_id: form.vacancyID.trim(),
        manager_account_id: form.managerAccountID.trim() || undefined,
        dry_run: form.dryRun,
        export_pdf: form.exportPDF,
        save_originals: form.saveOriginals,
        cover_letter_only: form.coverLetterOnly,
      });

      onResult({
        id: crypto.randomUUID(),
        method: "hh-resumes",
        title: `Vacancy #${form.vacancyID.trim()}`,
        status: "running",
        itemLabel: "resumes",
        startedAt: now,
        summary: `Extraction started${form.dryRun ? " (dry run)" : ""}`,
      });

      setForm((prev) => ({ ...prev, vacancyID: "" }));
      await loadConfigAndExtractions();
      setError(null);
    } catch (err) {
      setError((err as Error).message);
      onResult({
        id: crypto.randomUUID(),
        method: "hh-resumes",
        title: `Vacancy #${form.vacancyID.trim()}`,
        status: "error",
        itemLabel: "resumes",
        itemCount: 0,
        startedAt: now,
        finishedAt: new Date().toISOString(),
        error: (err as Error).message,
      });
    } finally {
      setSubmitting(false);
    }
  }

  async function onLoadVacancies() {
    try {
      setVacancyLoading(true);
      const catalog = await listHHVacancies();
      setVacancyCatalog(catalog);
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setVacancyLoading(false);
    }
  }

  async function onExchangeCode(event: FormEvent) {
    event.preventDefault();
    const normalizedCode = findOAuthCode(oauthCode) || oauthCode.trim();
    if (!normalizedCode) {
      setError("OAuth code is required");
      return;
    }
    try {
      setAuthSubmitting(true);
      const res = await exchangeHHOAuthCode({ code: normalizedCode });
      setOauthResult(res);
      setOauthCode("");
      clearOAuthCodeFromURL();
      setShowReauth(false);
      setError(null);
      await loadConfigAndExtractions({ silent: true });
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setAuthSubmitting(false);
    }
  }

  const selectedFiles = useMemo(() => {
    if (!selectedID) return [];
    if (details?.files && details.files.length > 0) return details.files;
    const row = items.find((item) => item.id === selectedID);
    return row?.files ?? [];
  }, [details?.files, items, selectedID]);

  const vacancyOptions = useMemo(
    () => vacancyCatalog?.vacancies ?? [],
    [vacancyCatalog?.vacancies]
  );

  if (loading) {
    return (
      <div className="import-form">
        <p className="muted">Loading HeadHunter integration status...</p>
      </div>
    );
  }

  return (
    <div className="import-form hh-import-form">
      {error && <div className="import-inline-error">{error}</div>}

      {/* OAuth Section */}
      <div className="hh-section">
        <div className="hh-section-header">
          <h4>OAuth2 Authentication</h4>
          <div className="hh-auth-status">
            <span className={`status ${config?.has_access_token ? "success" : "danger"}`}>
              {config?.has_access_token ? "Connected" : "No token"}
            </span>
            {config?.has_access_token && (
              <button
                className="btn"
                type="button"
                onClick={() => setShowReauth((v) => !v)}
              >
                {showReauth ? "Hide" : "Re-authenticate"}
              </button>
            )}
          </div>
        </div>

        {(!config?.has_access_token || showReauth) && (
          <div className="hh-auth-block">
            <div className="hh-auth-info">
              <p className="muted">
                Redirect URI: <code>{config?.redirect_uri || "-"}</code>
              </p>
              {config?.oauth_authorize_url && (
                <a
                  href={config.oauth_authorize_url}
                  target="_blank"
                  rel="noreferrer"
                  className="btn"
                >
                  Open HH OAuth page
                </a>
              )}
            </div>
            <form className="hh-oauth-form" onSubmit={onExchangeCode}>
              <label>
                OAuth code from redirect URL
                <input
                  type="text"
                  placeholder="paste ?code=..."
                  value={oauthCode}
                  onChange={(e) => setOauthCode(e.target.value)}
                />
              </label>
              <button className="btn" type="submit" disabled={authSubmitting}>
                {authSubmitting ? "Exchanging..." : "Exchange code"}
              </button>
            </form>
            {oauthResult && (
              <pre>
                {`HH_ACCESS_TOKEN=${oauthResult.access_token}\nHH_REFRESH_TOKEN=${oauthResult.refresh_token}\nexpires_in=${oauthResult.expires_in}`}
              </pre>
            )}
          </div>
        )}
      </div>

      {/* Extraction Form */}
      <div className="hh-section">
        <div className="hh-section-header">
          <h4>Run Extraction</h4>
          <button
            className="btn"
            type="button"
            onClick={() => void onLoadVacancies()}
            disabled={vacancyLoading || (!config?.has_access_token && !config?.has_refresh_token)}
          >
            {vacancyLoading ? "Loading..." : "Load vacancies"}
          </button>
        </div>

        {vacancyCatalog && (
          <p className="muted" style={{ margin: "0 0 0.5rem" }}>
            {vacancyCatalog.vacancies.length} vacancies loaded from{" "}
            {vacancyCatalog.accounts.length} account(s)
          </p>
        )}

        <form className="import-form-fields" onSubmit={onStartExtraction}>
          {vacancyOptions.length > 0 && (
            <label>
              Choose vacancy
              <select
                value={
                  form.managerAccountID && form.vacancyID
                    ? `${form.managerAccountID}::${form.vacancyID}`
                    : ""
                }
                onChange={(e) => {
                  const selected = vacancyOptions.find(
                    (item) => `${item.manager_account_id}::${item.id}` === e.target.value
                  );
                  if (!selected) {
                    setForm((prev) => ({ ...prev, vacancyID: "", managerAccountID: "" }));
                    return;
                  }
                  setForm((prev) => ({
                    ...prev,
                    vacancyID: selected.id,
                    managerAccountID: selected.manager_account_id,
                  }));
                }}
              >
                <option value="">Select vacancy</option>
                {vacancyOptions.map((item: HHVacancyListItem) => (
                  <option
                    key={`${item.manager_account_id}:${item.status}:${item.id}`}
                    value={`${item.manager_account_id}::${item.id}`}
                  >
                    [{item.status}] {item.name} (#{item.id}) - {item.employer_name}
                  </option>
                ))}
              </select>
            </label>
          )}

          <label>
            Vacancy ID
            <input
              type="text"
              placeholder="12345678"
              value={form.vacancyID}
              onChange={(e) =>
                setForm((prev) => ({
                  ...prev,
                  vacancyID: e.target.value,
                  managerAccountID: vacancyOptions.some((item) => item.id === e.target.value)
                    ? prev.managerAccountID
                    : "",
                }))
              }
              required
            />
          </label>

          <div className="hh-checkboxes">
            <label className="checkbox-label">
              <input
                type="checkbox"
                checked={form.dryRun}
                onChange={(e) => setForm((prev) => ({ ...prev, dryRun: e.target.checked }))}
              />
              Dry run
            </label>
            <label className="checkbox-label">
              <input
                type="checkbox"
                checked={form.exportPDF}
                onChange={(e) => setForm((prev) => ({ ...prev, exportPDF: e.target.checked }))}
              />
              Export combined PDF
            </label>
            <label className="checkbox-label">
              <input
                type="checkbox"
                checked={form.saveOriginals}
                onChange={(e) =>
                  setForm((prev) => ({ ...prev, saveOriginals: e.target.checked }))
                }
              />
              Save original PDFs/RTF
            </label>
            <label className="checkbox-label">
              <input
                type="checkbox"
                checked={form.coverLetterOnly}
                onChange={(e) =>
                  setForm((prev) => ({ ...prev, coverLetterOnly: e.target.checked }))
                }
              />
              Только с сопроводительным письмом
            </label>
          </div>

          <div className="import-form-actions" style={{ borderTop: "none", paddingTop: 0 }}>
            <button className="btn primary" type="submit" disabled={submitting}>
              {submitting ? "Starting..." : "Start extraction"}
            </button>
          </div>
        </form>
      </div>

      {/* Extractions History */}
      {items.length > 0 && (
        <div className="hh-section">
          <div className="hh-section-header">
            <h4>Extractions</h4>
            <button className="btn" onClick={() => void loadConfigAndExtractions()}>
              Refresh
            </button>
          </div>
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>ID</th>
                  <th>Vacancy</th>
                  <th>Status</th>
                  <th>Created</th>
                  <th>Found</th>
                  <th>OK</th>
                  <th>Fail</th>
                </tr>
              </thead>
              <tbody>
                {items.map((item) => (
                  <tr
                    key={item.id}
                    className={selectedID === item.id ? "row-selected" : ""}
                  >
                    <td>
                      <button className="btn link" onClick={() => setSelectedID(item.id)}>
                        {item.id.slice(0, 8)}...
                      </button>
                    </td>
                    <td>{item.vacancy_id}</td>
                    <td>
                      <StatusBadge status={item.status} />
                    </td>
                    <td>{formatDate(item.created_at)}</td>
                    <td>{item.total_found}</td>
                    <td>{item.succeeded}</td>
                    <td>{item.failed}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {/* Details Panel */}
          {selectedID && details?.manifest && (
            <div className="hh-details-panel">
              <div className="hh-details-summary">
                <span>
                  Vacancy: <strong>{details.manifest.vacancy_id}</strong>
                </span>
                <span>
                  Total: <strong>{details.manifest.total_found}</strong>
                </span>
                <span>
                  Success: <strong>{details.manifest.succeeded}</strong>
                </span>
                <span>
                  Failed: <strong>{details.manifest.failed}</strong>
                </span>
                {details.manifest.dry_run && (
                  <span className="status warn">Dry run</span>
                )}
              </div>

              {selectedFiles.length > 0 && (
                <details>
                  <summary>Files ({selectedFiles.length})</summary>
                  <ul className="plain-list">
                    {selectedFiles.map((file) => (
                      <li key={file}>
                        <a
                          href={hhExtractionFileURL(selectedID, file)}
                          target="_blank"
                          rel="noreferrer"
                        >
                          {file}
                        </a>
                      </li>
                    ))}
                  </ul>
                </details>
              )}

              <details>
                <summary>Candidates ({details.manifest.candidates.length})</summary>
                <div className="table-scroll">
                  <table>
                    <thead>
                      <tr>
                        <th>Resume ID</th>
                        <th>FIO</th>
                        <th>Status</th>
                        <th>Original</th>
                        <th>Error</th>
                      </tr>
                    </thead>
                    <tbody>
                      {details.manifest.candidates.map((c) => (
                        <tr key={`${c.candidate_id}-${c.resume_id}`}>
                          <td className="mono-cell">{c.resume_id}</td>
                          <td>{c.fio}</td>
                          <td>{c.status}</td>
                          <td>{c.downloaded_original ? "yes" : "no"}</td>
                          <td>{c.error_message || "-"}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </details>
            </div>
          )}

          {selectedID && details?.status === "running" && (
            <p className="muted">Extraction is running...</p>
          )}
        </div>
      )}
    </div>
  );
}
