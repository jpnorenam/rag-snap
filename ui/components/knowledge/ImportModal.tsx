"use client";

import { useId, useState } from "react";
import FileDropzone from "@/components/knowledge/FileDropzone";
import GdriveImport from "@/components/knowledge/GdriveImport";
import { errorMessage } from "@/lib/api/envelope";
import type { OperationView } from "@/lib/api/operations";
import { importKnowledge } from "@/lib/api/knowledge";
import { useModalDialog } from "@/lib/useModalDialog";

interface Props {
  onStarted: (op: OperationView) => void;
  onClose: () => void;
}

type Source = "file" | "gdrive";

// ImportModal restores a knowledge base from an archive. The source is chosen
// between a local file upload and Google Drive; the file path uploads a
// previously exported archive with an optional target name and force-overwrite.
export default function ImportModal({ onStarted, onClose }: Props) {
  const titleId = useId();
  const nameId = useId();
  const forceId = useId();
  const { dialogRef, onKeyDown } = useModalDialog(onClose);
  const [source, setSource] = useState<Source>("file");
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

        <div className="p-form__group" role="radiogroup" aria-label="Import source">
          <div className="kb-ingest__tabs">
            <label className="p-radio kb-ingest__tab">
              <input
                type="radio"
                className="p-radio__input"
                name="import-source"
                checked={source === "file"}
                onChange={() => setSource("file")}
              />
              <span className="p-radio__label">From file</span>
            </label>
            <label className="p-radio kb-ingest__tab">
              <input
                type="radio"
                className="p-radio__input"
                name="import-source"
                checked={source === "gdrive"}
                onChange={() => setSource("gdrive")}
              />
              <span className="p-radio__label">From Google Drive</span>
            </label>
          </div>
        </div>

        {source === "file" ? (
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

            <footer className="p-modal__footer">
              <button type="button" className="p-button u-no-margin--bottom" onClick={onClose}>
                Cancel
              </button>
              <button type="submit" className="p-button--positive u-no-margin--bottom" disabled={busy}>
                {busy ? "Starting…" : "Import"}
              </button>
            </footer>
          </form>
        ) : (
          <GdriveImport onStarted={onStarted} onClose={onClose} />
        )}
      </div>
    </div>
  );
}
