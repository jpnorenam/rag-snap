"use client";

import { useId, useState } from "react";
import FileDropzone from "@/components/knowledge/FileDropzone";
import { errorMessage } from "@/lib/api/envelope";
import type { OperationView } from "@/lib/api/operations";
import { importKnowledge } from "@/lib/api/knowledge";
import { useModalDialog } from "@/lib/useModalDialog";

interface Props {
  onStarted: (op: OperationView) => void;
  onClose: () => void;
}

// ImportModal uploads a previously exported archive (.tar.gz) to restore a
// knowledge base, with an optional target name and a force-overwrite option.
export default function ImportModal({ onStarted, onClose }: Props) {
  const titleId = useId();
  const nameId = useId();
  const forceId = useId();
  const { dialogRef, onKeyDown } = useModalDialog(onClose);
  const [archive, setArchive] = useState<File | null>(null);
  const [name, setName] = useState("");
  const [force, setForce] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    if (!archive) {
      setError("Choose an exported archive to import.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      onStarted(await importKnowledge(archive, name.trim(), force));
    } catch (e) {
      setError(errorMessage(e));
      setBusy(false);
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
            Import knowledge base
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
              accept=".tar.gz,.tgz"
              label="Exported archive"
              hint="Drop a .tar.gz archive here, or click to choose one."
              file={archive}
              onFile={(f) => setArchive(f)}
            />
            {error && <p className="p-form-validation__message">{error}</p>}
          </div>

          <div className="p-form__group">
            <label htmlFor={nameId}>Target name (optional)</label>
            <input
              id={nameId}
              type="text"
              value={name}
              autoComplete="off"
              onChange={(e) => setName(e.target.value)}
            />
            <p className="p-form-help-text">Defaults to the name recorded in the archive.</p>
          </div>

          <div className="p-form__group">
            <label className="p-checkbox">
              <input
                type="checkbox"
                className="p-checkbox__input"
                id={forceId}
                checked={force}
                onChange={(e) => setForce(e.target.checked)}
              />
              <span className="p-checkbox__label">Overwrite an existing knowledge base</span>
            </label>
            {force && (
              <p className="p-form-help-text u-text--muted">
                A base with the same name will be replaced. This cannot be undone.
              </p>
            )}
          </div>

          <p className="u-text--muted p-text--small">
            Importing from Google Drive? Use <code>rag-cli.rag k import --url &lt;drive-url&gt;</code>{" "}
            (UI support planned).
          </p>

          <footer className="p-modal__footer">
            <button type="button" className="p-button u-no-margin--bottom" onClick={onClose}>
              Cancel
            </button>
            <button type="submit" className="p-button--positive u-no-margin--bottom" disabled={busy}>
              {busy ? "Starting…" : "Import"}
            </button>
          </footer>
        </form>
      </div>
    </div>
  );
}
