import { useRef, useState } from "react";
import { uploadFile } from "../api";

const ACCEPT = ".csv,.pdf,.jpg,.jpeg,.png";
const MAX_BYTES = 10 * 1024 * 1024;

interface LocalUpload {
  key: string;
  name: string;
  pct: number;
  status: "uploading" | "accepted" | "error";
  error?: string;
}

export default function UploadDropzone({ onAccepted }: { onAccepted: () => void }) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [active, setActive] = useState(false);
  const [items, setItems] = useState<LocalUpload[]>([]);

  const update = (key: string, patch: Partial<LocalUpload>) =>
    setItems((prev) => prev.map((i) => (i.key === key ? { ...i, ...patch } : i)));

  const handleFiles = (files: FileList | null) => {
    if (!files) return;
    for (const file of Array.from(files)) {
      const key = `${file.name}-${Date.now()}-${Math.random()}`;
      const ext = file.name.toLowerCase().slice(file.name.lastIndexOf("."));
      if (!ACCEPT.split(",").includes(ext)) {
        setItems((p) => [...p, { key, name: file.name, pct: 0, status: "error", error: "unsupported type (CSV, PDF, JPEG, PNG)" }]);
        continue;
      }
      if (file.size > MAX_BYTES) {
        setItems((p) => [...p, { key, name: file.name, pct: 0, status: "error", error: "exceeds 10 MB limit" }]);
        continue;
      }
      setItems((p) => [...p, { key, name: file.name, pct: 0, status: "uploading" }]);
      uploadFile(file, (pct) => update(key, { pct }))
        .then(() => {
          update(key, { status: "accepted", pct: 100 });
          onAccepted();
        })
        .catch((err: Error) => update(key, { status: "error", error: err.message }));
    }
  };

  return (
    <div className="card">
      <h2>Upload statements</h2>
      <div
        className={`dropzone${active ? " active" : ""}`}
        onClick={() => inputRef.current?.click()}
        onDragOver={(e) => { e.preventDefault(); setActive(true); }}
        onDragLeave={() => setActive(false)}
        onDrop={(e) => { e.preventDefault(); setActive(false); handleFiles(e.dataTransfer.files); }}
      >
        <div>Drag &amp; drop bank or card statements here, or click to browse</div>
        <div className="hint">CSV, PDF, JPEG or PNG · up to 10 MB each</div>
        <input ref={inputRef} type="file" accept={ACCEPT} multiple hidden
          onChange={(e) => { handleFiles(e.target.files); e.target.value = ""; }} />
      </div>
      {items.map((i) => (
        <div className="upload-item" key={i.key}>
          <span className="name">{i.name}</span>
          {i.status === "uploading" && (
            <>
              <div className="progress-track">
                <div className="progress-fill" style={{ width: `${i.pct}%` }} />
              </div>
              <span className="status-chip">{i.pct}%</span>
            </>
          )}
          {i.status === "accepted" && <span className="status-chip done">✓ processing…</span>}
          {i.status === "error" && <span className="status-chip error">✕ {i.error}</span>}
        </div>
      ))}
    </div>
  );
}
