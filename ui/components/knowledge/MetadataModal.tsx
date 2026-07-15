"use client";

import { useId } from "react";
import type { SourceMetadata } from "@/lib/api/knowledge";
import { useModalDialog } from "@/lib/useModalDialog";

interface Props {
  source: SourceMetadata;
  onClose: () => void;
}

// FIELD_ORDER lists the metadata fields to surface in the definition list, in a
// sensible reading order. Unknown extra fields still appear in the raw JSON.
const FIELD_ORDER: { key: keyof SourceMetadata; label: string }[] = [
  { key: "source_id", label: "Source ID" },
  { key: "title", label: "Title" },
  { key: "file_name", label: "File name" },
  { key: "file_path", label: "Path / URL" },
  { key: "content_type", label: "Content type" },
  { key: "author", label: "Author" },
  { key: "language", label: "Language" },
  { key: "status", label: "Status" },
  { key: "chunk_count", label: "Chunks" },
  { key: "content_length", label: "Content length" },
  { key: "ingested_at", label: "Ingested at" },
  { key: "updated_at", label: "Updated at" },
];

// MetadataModal renders a source's stored metadata as a definition list plus the
// raw JSON in a copyable block (parity with `k metadata`).
export default function MetadataModal({ source, onClose }: Props) {
  const titleId = useId();
  const { dialogRef, onKeyDown } = useModalDialog(onClose);

  const rows = FIELD_ORDER.filter(({ key }) => {
    const v = source[key];
    return v !== undefined && v !== null && v !== "";
  });

  return (
    <div className="p-modal app-modal" onClick={onClose} onKeyDown={onKeyDown}>
      <div
        className="p-modal__dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        ref={dialogRef}
        onClick={(e) => e.stopPropagation()}
      >
        <header className="p-modal__header">
          <h2 className="p-modal__title" id={titleId}>
            Source metadata
          </h2>
        </header>

        <dl className="kb-meta__list">
          {rows.map(({ key, label }) => (
            <div className="kb-meta__row" key={String(key)}>
              <dt className="u-text--muted">{label}</dt>
              <dd>{String(source[key])}</dd>
            </div>
          ))}
        </dl>

        <p className="u-text--muted p-text--small u-no-margin--bottom">Raw JSON</p>
        <div className="p-code-snippet">
          <pre className="p-code-snippet__block">
            <code>{JSON.stringify(source, null, 2)}</code>
          </pre>
        </div>

        <footer className="p-modal__footer">
          <button type="button" className="p-button u-no-margin--bottom" onClick={onClose}>
            Close
          </button>
        </footer>
      </div>
    </div>
  );
}
