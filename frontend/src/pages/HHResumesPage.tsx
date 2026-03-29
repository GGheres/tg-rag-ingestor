import { FormEvent, useEffect, useMemo, useState } from "react";
import {
  exchangeHHOAuthCode,
  getHHConfig,
  getHHExtraction,
  hhExtractionFileURL,
  listHHExtractions,
  listHHVacancies,
  startHHExtraction,
} from "../api/client";
import type {
  HHConfigStatus,
  HHExtractionDetails,
  HHExtractionListItem,
  HHOAuthExchangeResponse,
  HHVacancyCatalog,
  HHVacancyListItem,
} from "../types";
import StatusBadge from "../components/StatusBadge";

function formatDate(value?: string): string {
  if (!value) {
    return "-";
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }
  return parsed.toLocaleString();
}

function findOAuthCode(value: string): string | null {
  const trimmed = value.trim();
  if (!trimmed) {
    return null;
  }

  if (/^https?:\/\//i.test(trimmed)) {
    try {
      const url = new URL(trimmed);
      const code = url.searchParams.get("code");
      if (code?.trim()) {
        return code.trim();
      }
    } catch {
      // Ignore invalid URLs and fall through to query parsing.
    }
  }

  const queryStart = trimmed.indexOf("?");
  const queryLikeValue =
    queryStart >= 0 ? trimmed.slice(queryStart + 1) : trimmed.startsWith("?") || trimmed.includes("=") ? trimmed : "";
  if (!queryLikeValue) {
    return null;
  }

  const params = new URLSearchParams(queryLikeValue.replace(/^[?#]/, ""));
  const code = params.get("code");
  if (code?.trim()) {
    return code.trim();
  }

  return null;
}

function clearOAuthCodeFromURL() {
  const url = new URL(window.location.href);
  if (!url.searchParams.has("code")) {
    return;
  }
  url.searchParams.delete("code");
  const nextURL = `${url.pathname}${url.search}${url.hash}`;
  window.history.replaceState({}, document.title, nextURL);
}

export default function HHResumesPage() {
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
  });
  const [oauthCode, setOauthCode] = useState("");

  async function loadConfigAndExtractions(options?: { silent?: boolean }) {
    const silent = Boolean(options?.silent);
    try {
      if (!silent) {
        setLoading(true);
      }
      const [cfgResp, listResp] = await Promise.all([getHHConfig(), listHHExtractions()]);
      setConfig(cfgResp);
      setItems(listResp);
      if (!selectedID && listResp.length > 0) {
        setSelectedID(listResp[0].id);
      }
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      if (!silent) {
        setLoading(false);
      }
    }
  }

  useEffect(() => {
    void loadConfigAndExtractions();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    const codeFromURL = findOAuthCode(window.location.href);
    if (codeFromURL) {
      setOauthCode((current) => current || codeFromURL);
    }
  }, []);

  useEffect(() => {
    const hasRunning = items.some((item) => item.status === "running");
    if (!hasRunning) {
      return;
    }
    const timer = window.setInterval(() => {
      void loadConfigAndExtractions({ silent: true });
      if (selectedID) {
        void loadDetails(selectedID);
      }
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

    try {
      setSubmitting(true);
      await startHHExtraction({
        vacancy_id: form.vacancyID.trim(),
        manager_account_id: form.managerAccountID.trim() || undefined,
        dry_run: form.dryRun,
        export_pdf: form.exportPDF,
        save_originals: form.saveOriginals,
      });
      setForm((prev) => ({ ...prev, vacancyID: "" }));
      await loadConfigAndExtractions();
      setError(null);
    } catch (err) {
      setError((err as Error).message);
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
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setAuthSubmitting(false);
    }
  }

  const selectedFiles = useMemo(() => {
    if (!selectedID) {
      return [];
    }
    if (details?.files && details.files.length > 0) {
      return details.files;
    }
    const row = items.find((item) => item.id === selectedID);
    return row?.files ?? [];
  }, [details?.files, items, selectedID]);

  const vacancyOptions = useMemo(() => vacancyCatalog?.vacancies ?? [], [vacancyCatalog?.vacancies]);

  if (loading) {
    return <p>Loading HeadHunter integration status...</p>;
  }

  return (
    <section className="details-grid">
      {error && <p className="error">{error}</p>}

      <div className="card">
        <h2>HeadHunter OAuth2</h2>
        <p>
          Configured: <strong>{config?.configured ? "yes" : "no"}</strong> | Access token:{" "}
          <strong>{config?.has_access_token ? "present" : "missing"}</strong> | Refresh token:{" "}
          <strong>{config?.has_refresh_token ? "present" : "missing"}</strong>
        </p>
        <p>
          Redirect URI: <code>{config?.redirect_uri || "-"}</code>
        </p>
        {config?.oauth_authorize_url ? (
          <p>
            Authorize URL:{" "}
            <a href={config.oauth_authorize_url} target="_blank" rel="noreferrer">
              Open HH OAuth page
            </a>
          </p>
        ) : null}
        <form className="grid-form" onSubmit={onExchangeCode}>
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
        {oauthResult ? (
          <pre>
            {`HH_ACCESS_TOKEN=${oauthResult.access_token}
HH_REFRESH_TOKEN=${oauthResult.refresh_token}
expires_in=${oauthResult.expires_in}`}
          </pre>
        ) : null}
      </div>

      <div className="card">
        <h2>Run Extraction</h2>
        <div className="card-header">
          <p>
            {config?.has_access_token || config?.has_refresh_token
              ? "Можно подтянуть вакансии автоматически из HH и выбрать нужную."
              : "Сначала сохраните HH_ACCESS_TOKEN/HH_REFRESH_TOKEN в .env и перезапустите backend, затем загрузите вакансии."}
          </p>
          <button
            className="btn"
            type="button"
            onClick={() => void onLoadVacancies()}
            disabled={vacancyLoading || (!config?.has_access_token && !config?.has_refresh_token)}
          >
            {vacancyLoading ? "Loading vacancies..." : "Load my vacancies"}
          </button>
        </div>
        {vacancyCatalog ? (
          <>
            <p>
              Loaded vacancies: <strong>{vacancyCatalog.vacancies.length}</strong> | Accounts:{" "}
              <strong>{vacancyCatalog.accounts.length}</strong>
            </p>
            {vacancyCatalog.accounts.length > 0 ? (
              <p>
                Accounts:{" "}
                {vacancyCatalog.accounts
                  .map((account) => `${account.employer_name} (${account.id}${account.is_current ? ", current" : ""})`)
                  .join(" | ")}
              </p>
            ) : null}
          </>
        ) : null}
        <form className="grid-form" onSubmit={onStartExtraction}>
          {vacancyOptions.length > 0 ? (
            <label>
              Choose vacancy from HH
              <select
                value={
                  form.managerAccountID && form.vacancyID ? `${form.managerAccountID}::${form.vacancyID}` : ""
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
                  <option key={`${item.manager_account_id}:${item.status}:${item.id}`} value={`${item.manager_account_id}::${item.id}`}>
                    [{item.status}] {item.name} (#{item.id}) - {item.employer_name}
                  </option>
                ))}
              </select>
            </label>
          ) : null}
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
                  managerAccountID: vacancyOptions.some((item) => item.id === e.target.value) ? prev.managerAccountID : "",
                }))
              }
              required
            />
          </label>
          {form.managerAccountID ? (
            <p>
              Selected manager account: <code>{form.managerAccountID}</code>
            </p>
          ) : null}
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
              onChange={(e) => setForm((prev) => ({ ...prev, saveOriginals: e.target.checked }))}
            />
            Save original PDFs/RTF
          </label>
          <button className="btn primary" type="submit" disabled={submitting}>
            {submitting ? "Starting..." : "Start extraction"}
          </button>
        </form>
      </div>

      <div className="card">
        <div className="card-header">
          <h2>Extractions</h2>
          <button className="btn" onClick={() => void loadConfigAndExtractions()}>
            Refresh
          </button>
        </div>
        <div className="table-scroll">
          <table>
            <thead>
              <tr>
                <th>Extraction ID</th>
                <th>Vacancy</th>
                <th>Status</th>
                <th>Created</th>
                <th>Found</th>
                <th>Success</th>
                <th>Failed</th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr key={item.id} className={selectedID === item.id ? "row-selected" : ""}>
                  <td>
                    <button className="btn link" onClick={() => setSelectedID(item.id)}>
                      {item.id}
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
              {items.length === 0 ? (
                <tr>
                  <td colSpan={7}>No extraction runs yet.</td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>

      <div className="card">
        <h2>Extraction Details</h2>
        {!selectedID ? (
          <p>Select extraction from table.</p>
        ) : details?.status === "running" ? (
          <p>Extraction is running...</p>
        ) : details?.manifest ? (
          <>
            <p>
              Vacancy: <strong>{details.manifest.vacancy_id}</strong> | Provider:{" "}
              <strong>{details.manifest.provider}</strong> | Dry run:{" "}
              <strong>{details.manifest.dry_run ? "yes" : "no"}</strong>
            </p>
            <p>
              Total: <strong>{details.manifest.total_found}</strong> | Success:{" "}
              <strong>{details.manifest.succeeded}</strong> | Failed: <strong>{details.manifest.failed}</strong>
            </p>
            {selectedFiles.length > 0 ? (
              <>
                <h3>Files</h3>
                <ul className="plain-list">
                  {selectedFiles.map((file) => (
                    <li key={file}>
                      <a href={hhExtractionFileURL(selectedID, file)} target="_blank" rel="noreferrer">
                        {file}
                      </a>
                    </li>
                  ))}
                </ul>
              </>
            ) : null}
            <h3>Manifest candidates</h3>
            <div className="table-scroll">
              <table>
                <thead>
                  <tr>
                    <th>Candidate ID</th>
                    <th>Resume ID</th>
                    <th>FIO</th>
                    <th>Status</th>
                    <th>Original</th>
                    <th>Included</th>
                    <th>Error</th>
                  </tr>
                </thead>
                <tbody>
                  {details.manifest.candidates.map((candidate) => (
                    <tr key={`${candidate.candidate_id}-${candidate.resume_id}`}>
                      <td className="mono-cell">{candidate.candidate_id || "-"}</td>
                      <td className="mono-cell">{candidate.resume_id}</td>
                      <td>{candidate.fio}</td>
                      <td>{candidate.status}</td>
                      <td>{candidate.downloaded_original ? "yes" : "no"}</td>
                      <td>{candidate.included_in_combined ? "yes" : "no"}</td>
                      <td>{candidate.error_message || "-"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </>
        ) : (
          <p>Manifest is not available yet.</p>
        )}
      </div>
    </section>
  );
}
