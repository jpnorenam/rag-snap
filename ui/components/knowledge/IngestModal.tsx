"use client";

import { useId, useState } from "react";
import FileDropzone from "@/components/knowledge/FileDropzone";
import { ApiError, errorMessage } from "@/lib/api/envelope";
import type { OperationView } from "@/lib/api/operations";
import {
  ingestBatch,
  ingestFile,
  ingestUrl,
  previewRepo,
  type RepoPreview,
} from "@/lib/api/knowledge";
import { useModalDialog } from "@/lib/useModalDialog";

interface Props {
  name: string;
  defaultLabel?: string;
  onStarted: (op: OperationView) => void;
  onClose: () => void;
}

type Mode = "upload" | "url" | "github" | "gitea";

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

// normalizeExtensions parses a comma/space-separated extension list into the
// dotted, lowercased, deduplicated form the daemon filters on.
export function normalizeExtensions(text: string): string[] {
  const seen = new Set<string>();
  for (const raw of text.split(/[\s,]+/)) {
    const t = raw.trim().toLowerCase().replace(/^\.+/, "");
    if (t) seen.add("." + t);
  }
  return [...seen];
}

// IngestModal ingests a single source by file upload, URL, or a GitHub/Gitea
// repository, with an optional force re-ingest. Repo modes expand server-side
// into one source per matched file and offer an advisory pre-ingest preview of
// the matched paths. It closes on submit; rows appear when the tracked
// operation completes. A duplicate-id error without force keeps the modal open.
export default function IngestModal({ name, defaultLabel, onStarted, onClose }: Props) {
  const titleId = useId();
  const urlId = useId();
  const sourceId = useId();
  const repoSourceId = useId();
  const branchId = useId();
  const pathId = useId();
  const extensionsId = useId();
  const labelId = useId();
  const forceId = useId();
  const { dialogRef, onKeyDown } = useModalDialog(onClose);

  const [mode, setMode] = useState<Mode>("upload");
  const [file, setFile] = useState<File | null>(null);
  const [url, setUrl] = useState("");
  const [sid, setSid] = useState("");
  const [sidTouched, setSidTouched] = useState(false);
  const [repoSource, setRepoSource] = useState("");
  const [branch, setBranch] = useState("");
  const [repoPath, setRepoPath] = useState("");
  const [extensionsText, setExtensionsText] = useState("");
  const [extensions, setExtensions] = useState<string[]>([]);
  const [label, setLabel] = useState("");
  const [force, setForce] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [sidError, setSidError] = useState<string | null>(null);
  const [labelError, setLabelError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [preview, setPreview] = useState<RepoPreview | null>(null);
  const [previewBusy, setPreviewBusy] = useState(false);

  const isRepo = mode === "github" || mode === "gitea";

  const chooseFile = (f: File | null) => {
    setFile(f);
    if (f && !sidTouched) setSid(slugify(f.name));
  };

  // Any change to what a repo job matches invalidates a shown preview.
  const invalidatePreview = () => setPreview(null);

  const normalizeExts = () => {
    const exts = normalizeExtensions(extensionsText);
    setExtensions(exts);
    setExtensionsText(exts.join(", "));
    return exts;
  };

  // validateRepo returns the repo fields to submit, or null after setting a
  // form-level error.
  const validateRepo = (): { source: string; exts: string[] } | null => {
    const source = repoSource.trim();
    if (mode === "github" && !/^(https:\/\/github\.com\/)?[^/\s]+\/[^/\s]+/.test(source)) {
      setError("Enter a GitHub repository as owner/repo or https://github.com/owner/repo.");
      return null;
    }
    if (mode === "gitea" && !/^https?:\/\/[^/\s]+\/[^/\s]+\/[^/\s]+/.test(source)) {
      setError("Enter a full Gitea repository URL, e.g. https://gitea.example.com/owner/repo.");
      return null;
    }
    const exts = normalizeExts();
    if (exts.length === 0) {
      setError("Enter at least one file extension — a repo ingest only fetches matching files.");
      return null;
    }
    return { source, exts };
  };

  const runPreview = async () => {
    setError(null);
    setPreview(null);
    const repo = validateRepo();
    if (!repo) return;
    setPreviewBusy(true);
    try {
      setPreview(
        await previewRepo({
          type: mode as "github" | "gitea",
          source: repo.source,
          branch: branch.trim() || undefined,
          path: repoPath.trim() || undefined,
          extensions: repo.exts,
        })
      );
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setPreviewBusy(false);
    }
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
    let repo: { source: string; exts: string[] } | null = null;
    if (isRepo) {
      repo = validateRepo();
      if (!repo) return;
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
      let op: OperationView;
      if (mode === "upload") {
        op = await ingestFile(name, file as File, sid.trim(), force, trimmedLabel || undefined);
      } else if (mode === "url") {
        op = await ingestUrl(name, url.trim(), sid.trim(), force, trimmedLabel || undefined);
      } else {
        op = await ingestBatch(
          name,
          [
            {
              type: mode,
              source: (repo as { source: string }).source,
              branch: branch.trim() || undefined,
              path: repoPath.trim() || undefined,
              extensions: (repo as { exts: string[] }).exts,
              label: trimmedLabel || undefined,
            },
          ],
          force
        );
      }
      onStarted(op);
    } catch (e) {
      setBusy(false);
      // Duplicate source id without force: keep the modal open, field-level.
      if (!isRepo && e instanceof ApiError && e.code === 409) {
        setSidError(
          `Source “${sid.trim()}” already exists. Enable force re-ingest to replace it.`
        );
        return;
      }
      setError(errorMessage(e));
    }
  };

  const modeTabs: { value: Mode; text: string }[] = [
    { value: "upload", text: "Upload file" },
    { value: "url", text: "From URL" },
    { value: "github", text: "GitHub repo" },
    { value: "gitea", text: "Gitea repo" },
  ];

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
              {modeTabs.map((tab) => (
                <label key={tab.value} className="p-radio kb-ingest__tab">
                  <input
                    type="radio"
                    className="p-radio__input"
                    name="ingest-mode"
                    checked={mode === tab.value}
                    onChange={() => {
                      setMode(tab.value);
                      setError(null);
                      setPreview(null);
                    }}
                  />
                  <span className="p-radio__label">{tab.text}</span>
                </label>
              ))}
            </div>
          </div>

          {mode === "upload" && (
            <div className={`p-form__group ${error ? "p-form-validation is-error" : ""}`}>
              <FileDropzone
                label="Document"
                hint="Drop a file here, or click to choose one."
                file={file}
                onFile={chooseFile}
              />
              {error && <p className="p-form-validation__message">{error}</p>}
            </div>
          )}

          {mode === "url" && (
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

          {isRepo && (
            <>
              <div className={`p-form__group ${error ? "p-form-validation is-error" : ""}`}>
                <label htmlFor={repoSourceId}>Repository</label>
                <input
                  id={repoSourceId}
                  type="text"
                  value={repoSource}
                  autoComplete="off"
                  placeholder={
                    mode === "github" ? "owner/repo" : "https://gitea.example.com/owner/repo"
                  }
                  onChange={(e) => {
                    setRepoSource(e.target.value);
                    invalidatePreview();
                  }}
                />
                {error ? (
                  <p className="p-form-validation__message">{error}</p>
                ) : (
                  <p className="p-form-help-text">
                    Files are fetched with the daemon’s{" "}
                    <code>{mode === "github" ? "GITHUB_TOKEN" : "GITEA_TOKEN"}</code> environment
                    variable.
                  </p>
                )}
              </div>

              <div className="p-form__group">
                <label htmlFor={branchId}>Branch (optional)</label>
                <input
                  id={branchId}
                  type="text"
                  value={branch}
                  autoComplete="off"
                  onChange={(e) => {
                    setBranch(e.target.value);
                    invalidatePreview();
                  }}
                />
                <p className="p-form-help-text">Defaults to the repository’s default branch.</p>
              </div>

              <div className="p-form__group">
                <label htmlFor={pathId}>Path prefix (optional)</label>
                <input
                  id={pathId}
                  type="text"
                  value={repoPath}
                  autoComplete="off"
                  placeholder="docs/"
                  onChange={(e) => {
                    setRepoPath(e.target.value);
                    invalidatePreview();
                  }}
                />
                <p className="p-form-help-text">Only files under this path are ingested.</p>
              </div>

              <div className="p-form__group">
                <label htmlFor={extensionsId}>File extensions</label>
                <input
                  id={extensionsId}
                  type="text"
                  value={extensionsText}
                  autoComplete="off"
                  placeholder=".md, .rst, .txt"
                  onChange={(e) => {
                    setExtensionsText(e.target.value);
                    invalidatePreview();
                  }}
                  onBlur={normalizeExts}
                />
                {extensions.length > 0 && (
                  <p className="kb-ingest__ext-chips" aria-label="Extensions to ingest">
                    {extensions.map((ext) => (
                      <span key={ext} className="p-chip">
                        <span className="p-chip__value">{ext}</span>
                      </span>
                    ))}
                  </p>
                )}
                <p className="p-form-help-text">
                  Comma-separated; only matching files are ingested.
                </p>
              </div>

              <div className="p-form__group">
                <button
                  type="button"
                  className="p-button u-no-margin--bottom"
                  disabled={previewBusy}
                  onClick={() => void runPreview()}
                >
                  {previewBusy ? (
                    <>
                      <i className="p-icon--spinner u-animation--spin" aria-hidden="true" />{" "}
                      Previewing…
                    </>
                  ) : (
                    "Preview files"
                  )}
                </button>
                {preview && (
                  <div className="kb-ingest__preview" aria-live="polite">
                    <p className="u-no-margin--bottom">
                      <strong>{preview.total}</strong> file{preview.total === 1 ? "" : "s"} match
                      {preview.total === 1 ? "es" : ""}.
                      {preview.truncated && (
                        <span className="u-text--muted">
                          {" "}
                          The repository listing was truncated; more files may exist.
                        </span>
                      )}
                    </p>
                    {preview.files.length > 0 && (
                      <ul className="p-list kb-ingest__preview-list">
                        {preview.files.map((f) => (
                          <li key={f} className="p-list__item">
                            <code>{f}</code>
                          </li>
                        ))}
                        {preview.total > preview.files.length && (
                          <li className="p-list__item u-text--muted">
                            …and {preview.total - preview.files.length} more
                          </li>
                        )}
                      </ul>
                    )}
                  </div>
                )}
              </div>
            </>
          )}

          {!isRepo && (
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
          )}

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
            <p className="p-form-help-text u-text--muted">
              {isRepo
                ? "Replace already-ingested files instead of skipping them."
                : "Replace an existing source with the same ID."}
            </p>
          </div>

          <footer className="p-modal__footer">
            <button type="button" className="p-button u-no-margin--bottom" onClick={onClose}>
              Cancel
            </button>
            <button type="submit" className="p-button--positive u-no-margin--bottom" disabled={busy}>
              {busy ? "Starting…" : isRepo ? "Ingest repository" : "Ingest"}
            </button>
          </footer>
        </form>
      </div>
    </div>
  );
}
