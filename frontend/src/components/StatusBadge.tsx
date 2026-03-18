type Props = {
  status: string;
};

export default function StatusBadge({ status }: Props) {
  const normalized = status.toLowerCase();
  const cls =
    normalized === "succeeded" ||
    normalized === "active" ||
    normalized === "done" ||
    normalized === "audio_downloaded" ||
    normalized === "transcribed"
      ? "status success"
      : normalized === "failed" || normalized === "error" || normalized === "download_failed" || normalized === "transcription_failed"
      ? "status danger"
      : normalized === "running" || normalized === "queued" || normalized === "pending_manual_start" || normalized === "transcribing"
      ? "status warn"
      : "status";

  return <span className={cls}>{status}</span>;
}
