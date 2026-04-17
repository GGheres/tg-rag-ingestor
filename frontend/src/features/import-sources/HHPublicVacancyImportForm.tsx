import { useMemo, useState } from "react";
import { importHHPublicVacancies } from "../../api/client";
import type { ImportResult } from "./types";

type Props = {
  onResult: (result: ImportResult) => void;
};

function todayISODate() {
  return new Date().toISOString().slice(0, 10);
}

export default function HHPublicVacancyImportForm({ onResult }: Props) {
  const [form, setForm] = useState({
    text: "",
    area: "",
    professionalRole: "",
    dateFrom: todayISODate(),
    dateTo: todayISODate(),
    maxItems: "500",
    title: "",
    sourceName: "",
  });
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const canSubmit = useMemo(() => {
    if (loading) return false;
    if (!form.dateFrom.trim() || !form.dateTo.trim()) return false;
    const parsedMax = Number(form.maxItems);
    return Number.isFinite(parsedMax) && parsedMax > 0;
  }, [form.dateFrom, form.dateTo, form.maxItems, loading]);

  async function handleSubmit() {
    const now = new Date().toISOString();
    setLoading(true);
    setError(null);

    const parsedMax = Number(form.maxItems);
    const title =
      form.title.trim() ||
      (form.text.trim() ? `HH public vacancies: ${form.text.trim()}` : "HH public vacancies");

    try {
      const result = await importHHPublicVacancies({
        text: form.text.trim() || undefined,
        area: form.area.trim() || undefined,
        professional_role: form.professionalRole.trim() || undefined,
        date_from: form.dateFrom.trim(),
        date_to: form.dateTo.trim(),
        max_items: parsedMax,
        title: form.title.trim() || undefined,
        source_name: form.sourceName.trim() || undefined,
      });

      onResult({
        id: crypto.randomUUID(),
        method: "hh-public-vacancies",
        title,
        status:
          result.trash_count > 0 || result.fetch_stats.truncated_windows > 0 ? "partial" : "success",
        itemLabel: "vacancies",
        itemCount: result.imported_count,
        startedAt: now,
        finishedAt: new Date().toISOString(),
        summary:
          `${result.imported_count} imported, ${result.processed_count} processed, ` +
          `${result.chunk_count} chunks, ${result.fetch_stats.requests_made} HH requests`,
      });

      setForm((prev) => ({
        ...prev,
        text: "",
        area: "",
        professionalRole: "",
        title: "",
        sourceName: "",
      }));
    } catch (err) {
      const message = err instanceof Error ? err.message : "Unknown error";
      setError(message);
      onResult({
        id: crypto.randomUUID(),
        method: "hh-public-vacancies",
        title,
        status: "error",
        itemLabel: "vacancies",
        itemCount: 0,
        startedAt: now,
        finishedAt: new Date().toISOString(),
        error: message,
      });
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="import-form">
      <div className="import-form-fields">
        <label>
          Search text
          <input
            type="text"
            value={form.text}
            onChange={(e) => setForm((prev) => ({ ...prev, text: e.target.value }))}
            placeholder="golang, data scientist, product manager"
            disabled={loading}
          />
        </label>

        <label>
          Area ID (optional)
          <input
            type="text"
            value={form.area}
            onChange={(e) => setForm((prev) => ({ ...prev, area: e.target.value }))}
            placeholder="1"
            disabled={loading}
          />
        </label>

        <label>
          Professional role ID (optional)
          <input
            type="text"
            value={form.professionalRole}
            onChange={(e) => setForm((prev) => ({ ...prev, professionalRole: e.target.value }))}
            placeholder="96"
            disabled={loading}
          />
        </label>

        <label>
          Date from
          <input
            type="date"
            value={form.dateFrom}
            onChange={(e) => setForm((prev) => ({ ...prev, dateFrom: e.target.value }))}
            disabled={loading}
          />
        </label>

        <label>
          Date to
          <input
            type="date"
            value={form.dateTo}
            onChange={(e) => setForm((prev) => ({ ...prev, dateTo: e.target.value }))}
            disabled={loading}
          />
        </label>

        <label>
          Max vacancies
          <input
            type="number"
            min={1}
            max={5000}
            step={1}
            value={form.maxItems}
            onChange={(e) => setForm((prev) => ({ ...prev, maxItems: e.target.value }))}
            disabled={loading}
          />
          <span className="field-hint">Hard capped at 5000 per import</span>
        </label>

        <label>
          Source title (optional)
          <input
            type="text"
            value={form.title}
            onChange={(e) => setForm((prev) => ({ ...prev, title: e.target.value }))}
            placeholder="HH public vacancies for Golang"
            disabled={loading}
          />
        </label>

        <label>
          Source name (optional)
          <input
            type="text"
            value={form.sourceName}
            onChange={(e) => setForm((prev) => ({ ...prev, sourceName: e.target.value }))}
            placeholder="hh_public_vacancies_golang"
            disabled={loading}
          />
        </label>
      </div>

      <div className="field-hint">
        Imports public HH search results through the existing JSON ingestion flow. For wide ranges,
        the backend splits the search window by publication time to avoid the 2000-result HH limit.
      </div>

      {error && <div className="import-inline-error">{error}</div>}

      <div className="import-form-actions">
        <button className="btn primary" type="button" onClick={handleSubmit} disabled={!canSubmit}>
          {loading ? "Importing..." : "Import Public HH Vacancies"}
        </button>
      </div>
    </div>
  );
}
