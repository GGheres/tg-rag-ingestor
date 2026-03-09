import { useEffect, useState } from "react";
import { getJobs } from "../api/client";
import StatusBadge from "../components/StatusBadge";
import type { Job } from "../types";

export default function JobsPage() {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  async function loadJobs() {
    try {
      setLoading(true);
      const data = await getJobs();
      setJobs(data);
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void loadJobs();
  }, []);

  return (
    <section className="card">
      <div className="card-header">
        <h2>Jobs</h2>
        <button className="btn" onClick={() => void loadJobs()} disabled={loading}>
          Refresh
        </button>
      </div>
      {error && <p className="error">{error}</p>}
      {loading ? (
        <p>Loading jobs...</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>ID</th>
              <th>Type</th>
              <th>Status</th>
              <th>Progress</th>
              <th>Created</th>
            </tr>
          </thead>
          <tbody>
            {jobs.map((job) => (
              <tr key={job.id}>
                <td>{job.id.slice(0, 8)}...</td>
                <td>{job.job_type}</td>
                <td>
                  <StatusBadge status={job.status} />
                </td>
                <td>
                  {job.progress_done}/{job.progress_total}
                </td>
                <td>{new Date(job.created_at).toLocaleString()}</td>
              </tr>
            ))}
            {jobs.length === 0 && (
              <tr>
                <td colSpan={5}>No jobs yet.</td>
              </tr>
            )}
          </tbody>
        </table>
      )}
    </section>
  );
}
