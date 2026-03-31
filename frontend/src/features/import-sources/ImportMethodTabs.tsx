import { IMPORT_METHODS, type ImportMethod } from "./types";

type Props = {
  active: ImportMethod;
  onChange: (method: ImportMethod) => void;
};

export default function ImportMethodTabs({ active, onChange }: Props) {
  return (
    <div className="import-tabs">
      {IMPORT_METHODS.map((m) => (
        <button
          key={m.id}
          className={`import-tab ${active === m.id ? "active" : ""}`}
          onClick={() => onChange(m.id)}
          type="button"
        >
          <span className="import-tab-icon">{m.icon}</span>
          <span className="import-tab-content">
            <span className="import-tab-label">{m.label}</span>
            <span className="import-tab-desc">{m.description}</span>
          </span>
        </button>
      ))}
    </div>
  );
}
