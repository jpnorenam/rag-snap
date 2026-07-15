"use client";

import { useId, useState } from "react";
import FileDropzone from "@/components/knowledge/FileDropzone";
import { errorMessage } from "@/lib/api/envelope";
import type { OperationView } from "@/lib/api/operations";
import { ingestBatch, type BatchItem } from "@/lib/api/knowledge";
import { parseBatchManifest, type PreviewEntry } from "@/lib/batchManifest";
import { useModalDialog } from "@/lib/useModalDialog";

interface Props {
  name: string;
  onStarted: (op: OperationView) => void;
  onClose: () => void;
}

// BatchIngestModal uploads the YAML manifest `k ingest --batch` accepts, parses
// it client-side, previews entries (with a supported/unsupported indicator), and
// starts the supported entries as a single tracked operation.
export default function BatchIngestModal({ name, onStarted, onClose }: Props) {
  const titleId = useId();
  const forceId = useId();
  const { dialogRef, onKeyDown } = useModalDialog(onClose);
  const [preview, setPreview] = useState<PreviewEntry[] | null>(null);
  const [items, setItems] = useState<BatchItem[]>([]);
  const [force, setForce] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const onFile = async (f: File | null) => {
    setError(null);
    setPreview(null);
    setItems([]);
    if (!f) return;
    try {
      const text = await f.text();
      const parsed = parseBatchManifest(text);
      if (parsed.error) {
        setError(parsed.error);
        return;
      }
      setPreview(parsed.preview);
      setItems(parsed.items);
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  const submit = async () => {
    if (items.length === 0) {
      setError("No supported entries to ingest.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      onStarted(await ingestBatch(name, items, force));
    } catch (e) {
      setBusy(false);
      setError(errorMessage(e));
    }
  };

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
            Batch ingest
          </h2>
        </header>

        <form
          className="p-form p-form--stacked"
          onSubmit={(e) => {
            e.preventDefault();
            void submit();
          }}
        >
          <div className={`p-form__group ${error ? "p-form-validation is-error" : ""}`}>
            <FileDropzone
              accept=".yaml,.yml"
              label="Manifest"
              hint="Drop a .yaml manifest here, or click to choose one."
              file={null}
              onFile={(f) => void onFile(f)}
            />
            {error && <p className="p-form-validation__message">{error}</p>}
            <p className="p-form-help-text u-text--muted">
              Same schema as <code>k ingest --batch</code> — see <code>docs/usage.md</code>.
            </p>
          </div>

          {preview && preview.length > 0 && (
            <div className="kb__table-wrap">
              <table aria-label="Manifest entries">
                <thead>
                  <tr>
                    <th>Entry</th>
                    <th>Type</th>
                    <th>Source</th>
                  </tr>
                </thead>
                <tbody>
                  {preview.map((entry, i) => (
                    <tr key={`${entry.id}-${i}`} className={entry.unsupported ? "kb-batch__row--skip" : ""}>
                      <td>{entry.id || "—"}</td>
                      <td>
                        <span className="p-chip">
                          <span className="p-chip__value">{entry.type}</span>
                        </span>
                      </td>
                      <td>
                        {entry.source}
                        {entry.unsupported && (
                          <span className="u-text--muted p-text--small"> — {entry.unsupported}</span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          <div className="p-form__group">
            <label className="p-checkbox">
              <input
                type="checkbox"
                className="p-checkbox__input"
                id={forceId}
                checked={force}
                onChange={(e) => setForce(e.target.checked)}
              />
              <span className="p-checkbox__label">Force re-ingest</span>
            </label>
          </div>

          <footer className="p-modal__footer">
            <button type="button" className="p-button u-no-margin--bottom" onClick={onClose}>
              Cancel
            </button>
            <button
              type="submit"
              className="p-button--positive u-no-margin--bottom"
              disabled={busy || items.length === 0}
            >
              {busy ? "Starting…" : `Start batch (${items.length})`}
            </button>
          </footer>
        </form>
      </div>
    </div>
  );
}
