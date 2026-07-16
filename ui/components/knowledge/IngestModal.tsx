"use client";

import { useId, useState } from "react";
import FileDropzone from "@/components/knowledge/FileDropzone";
import { ApiError, errorMessage } from "@/lib/api/envelope";
import type { OperationView } from "@/lib/api/operations";
import { ingestFile, ingestUrl } from "@/lib/api/knowledge";
import { useModalDialog } from "@/lib/useModalDialog";

interface Props {
  name: string;
  defaultLabel?: string;
  onStarted: (op: OperationView) => void;
  onClose: () => void;
}

type Mode = "upload" | "url";

// LABEL_PATTERN mirrors the daemon's knowledge-label format.
const LABEL_PATTERN = /^[a-z0-9][a-z0-9-]{0,31}$/;

// slugify turns a filename into a stable, human source-id default.
function slugify(filename: string): string {
  const base = filename.replace(/\.[^.]+$/, "");
  return base
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

// IngestModal ingests a single document by file upload or URL, with an optional
// force re-ingest. It closes on submit; the row appears when the tracked
// operation completes. A duplicate-id error without force keeps the modal open.
export default function IngestModal({ name, defaultLabel, onStarted, onClose }: Props) {
  const titleId = useId();
  const urlId = useId();
  const sourceId = useId();
  const labelId = useId();
  const forceId = useId();
  const { dialogRef, onKeyDown } = useModalDialog(onClose);

  const [mode, setMode] = useState<Mode>("upload");
  const [file, setFile] = useState<File | null>(null);
  const [url, setUrl] = useState("");
  const [sid, setSid] = useState("");
  const [sidTouched, setSidTouched] = useState(false);
  const [label, setLabel] = useState("");
  const [force, setForce] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [sidError, setSidError] = useState<string | null>(null);
  const [labelError, setLabelError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const chooseFile = (f: File | null) => {
    setFile(f);
    if (f && !sidTouched) setSid(slugify(f.name));
  };

  const submit = async () => {
    setError(null);
    setSidError(null);
    setLabelError(null);
    if (mode === "upload" && !file) {
      setError("Choose a file to ingest.");
      return;
    }
    if (mode === "url" && !/^https?:\/\//.test(url.trim())) {
      setError("Enter a valid http(s) URL.");
      return;
    }
    const trimmedLabel = label.trim();
    if (trimmedLabel && !LABEL_PATTERN.test(trimmedLabel)) {
      setLabelError(
        "Use lowercase letters, digits, and hyphens; start with a letter or digit (max 32 characters)."
      );
      return;
    }
    setBusy(true);
    try {
      const op =
        mode === "upload"
          ? await ingestFile(name, file as File, sid.trim(), force, trimmedLabel || undefined)
          : await ingestUrl(name, url.trim(), sid.trim(), force, trimmedLabel || undefined);
      onStarted(op);
    } catch (e) {
      setBusy(false);
      // Duplicate source id without force: keep the modal open, field-level.
      if (e instanceof ApiError && e.code === 409) {
        setSidError(
          `Source “${sid.trim()}” already exists. Enable force re-ingest to replace it.`
        );
        return;
      }
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
            Ingest document
          </h2>
        </header>

        <form
          className="p-form p-form--stacked"
          onSubmit={(e) => {
            e.preventDefault();
            void submit();
          }}
        >
          <div className="p-form__group" role="radiogroup" aria-label="Source">
            <div className="kb-ingest__tabs">
              <label className="p-radio kb-ingest__tab">
                <input
                  type="radio"
                  className="p-radio__input"
                  name="ingest-mode"
                  checked={mode === "upload"}
                  onChange={() => setMode("upload")}
                />
                <span className="p-radio__label">Upload file</span>
              </label>
              <label className="p-radio kb-ingest__tab">
                <input
                  type="radio"
                  className="p-radio__input"
                  name="ingest-mode"
                  checked={mode === "url"}
                  onChange={() => setMode("url")}
                />
                <span className="p-radio__label">From URL</span>
              </label>
            </div>
          </div>

          {mode === "upload" ? (
            <div className={`p-form__group ${error ? "p-form-validation is-error" : ""}`}>
              <FileDropzone
                label="Document"
                hint="Drop a file here, or click to choose one."
                file={file}
                onFile={chooseFile}
              />
              {error && <p className="p-form-validation__message">{error}</p>}
            </div>
          ) : (
            <div className={`p-form__group ${error ? "p-form-validation is-error" : ""}`}>
              <label htmlFor={urlId}>URL</label>
              <input
                id={urlId}
                type="url"
                value={url}
                autoComplete="off"
                onChange={(e) => setUrl(e.target.value)}
              />
              {error && <p className="p-form-validation__message">{error}</p>}
            </div>
          )}

          <div className={`p-form__group ${sidError ? "p-form-validation is-error" : ""}`}>
            <label htmlFor={sourceId}>Source ID</label>
            <input
              id={sourceId}
              type="text"
              value={sid}
              autoComplete="off"
              onChange={(e) => {
                setSid(e.target.value);
                setSidTouched(true);
              }}
            />
            {sidError ? (
              <p className="p-form-validation__message">{sidError}</p>
            ) : (
              <p className="p-form-help-text">
                The stable identifier used by forget and metadata.
              </p>
            )}
          </div>

          <div className={`p-form__group ${labelError ? "p-form-validation is-error" : ""}`}>
            <label htmlFor={labelId}>Label (optional)</label>
            <input
              id={labelId}
              type="text"
              className={labelError ? "p-form-validation__input" : ""}
              value={label}
              autoComplete="off"
              placeholder={defaultLabel || undefined}
              onChange={(e) => setLabel(e.target.value)}
            />
            {labelError ? (
              <p className="p-form-validation__message">{labelError}</p>
            ) : (
              <p className="p-form-help-text">
                Knowledge label for this source
                {defaultLabel ? (
                  <>
                    {" "}
                    (default: <code>{defaultLabel}</code>)
                  </>
                ) : null}
                . Reference it in your prompts to prioritize content.
              </p>
            )}
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
              <span className="p-checkbox__label">Force re-ingest</span>
            </label>
            <p className="p-form-help-text u-text--muted">Replace an existing source with the same ID.</p>
          </div>

          <footer className="p-modal__footer">
            <button type="button" className="p-button u-no-margin--bottom" onClick={onClose}>
              Cancel
            </button>
            <button type="submit" className="p-button--positive u-no-margin--bottom" disabled={busy}>
              {busy ? "Starting…" : "Ingest"}
            </button>
          </footer>
        </form>
      </div>
    </div>
  );
}
