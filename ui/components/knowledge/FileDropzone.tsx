"use client";

import { useId, useRef, useState } from "react";

interface Props {
  // Accept attribute for the native input (e.g. ".tar.gz", ".yaml").
  accept?: string;
  // Human hint shown when no file is chosen.
  hint: string;
  // Currently chosen file, lifted to the parent.
  file: File | null;
  onFile: (file: File | null) => void;
  label: string;
}

// formatSize renders a byte count as a compact human string.
function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const kb = bytes / 1024;
  if (kb < 1024) return `${kb.toFixed(1)} KB`;
  return `${(kb / 1024).toFixed(1)} MB`;
}

// FileDropzone wraps a native <input type="file"> in a dashed drop area. The
// native input is the real keyboard/AT path; the dropzone is drag-and-drop
// enhancement only (foundation §Ingest). Chosen file name + size are shown.
export default function FileDropzone({ accept, hint, file, onFile, label }: Props) {
  const inputId = useId();
  const inputRef = useRef<HTMLInputElement>(null);
  const [over, setOver] = useState(false);

  const classes = ["app-dropzone", over ? "is-over" : ""].filter(Boolean).join(" ");

  return (
    <div
      className={classes}
      onDragOver={(e) => {
        e.preventDefault();
        setOver(true);
      }}
      onDragLeave={() => setOver(false)}
      onDrop={(e) => {
        e.preventDefault();
        setOver(false);
        const dropped = e.dataTransfer.files?.[0];
        if (dropped) onFile(dropped);
      }}
    >
      <label htmlFor={inputId} className="app-dropzone__label">
        {label}
      </label>
      <input
        id={inputId}
        ref={inputRef}
        type="file"
        accept={accept}
        className="app-dropzone__input"
        onChange={(e) => onFile(e.target.files?.[0] ?? null)}
      />
      <p className="app-dropzone__hint u-text--muted p-text--small">
        {file ? `${file.name} — ${formatSize(file.size)}` : hint}
      </p>
    </div>
  );
}
