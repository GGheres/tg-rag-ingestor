type Props = {
  status: string;
};

export default function StatusBadge({ status }: Props) {
  const normalized = status.toLowerCase();
  const cls =
    normalized === "succeeded" || normalized === "active"
      ? "status success"
      : normalized === "failed" || normalized === "error"
      ? "status danger"
      : normalized === "running"
      ? "status warn"
      : "status";

  return <span className={cls}>{status}</span>;
}
