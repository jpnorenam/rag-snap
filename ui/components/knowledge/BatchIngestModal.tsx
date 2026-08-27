"use client";

import { useId, useState } from "react";
import FileDropzone from "@/components/knowledge/FileDropzone";
import { normalizeExtensions } from "@/components/knowledge/IngestModal";
import { errorMessage } from "@/lib/api/envelope";
import type { OperationView } from "@/lib/api/operations";
import { ingestBatch, previewRepo, type BatchItem, type RepoPreview } from "@/lib/api/knowledge";
import {
  FILE_JOB_UNSUPPORTED,
  builderJobToBatchItem,
  parseBatchManifest,
  serializeBatchManifest,
  type BuilderJob,
  type PreviewEntry,
} from "@/lib/batchManifest";
import { useModalDialog } from "@/lib/useModalDialog";

interface Props {
  name: string;
  onStarted: (op: OperationView) => void;
  onClose: () => void;
}

type Mode = "upload" | "build";

// LABEL_PATTERN mirrors the daemon's knowledge-label format.
const LABEL_PATTERN = /^[a-z0-9][a-z0-9-]{0,31}$/;

// JobErrors carries per-field validation messages for one builder job.
interface JobErrors {
  source?: string;
  extensions?: string;
  label?: string;
  targetKB?: string;
}

// JobPreviewState tracks one job's repo-preview call.
interface JobPreviewState {
  busy?: boolean;
  result?: RepoPreview;
  error?: string;
}

// Row is one editable job plus the state that belongs to the row rather than to
// the manifest.
//
// `id` is a stable identity, not a position: rows are keyed by it so React keeps
// each row's DOM with its own job when one is removed from the middle, and
// jobPreviews is keyed by it so a preview cannot end up attached to a different
// job. Keying by array index instead left the surviving rows showing the removed
// row's state.
//
// `extText` is the extensions field exactly as typed. `job.extensions` holds the
// normalized list; keeping the raw text beside it lets the input stay controlled
// without normalization eating a comma mid-keystroke.
interface Row {
  id: number;
  job: BuilderJob;
  extText: string;
}

// nextRowId hands out row identities. A module-level counter is enough: ids need
// only be unique within a mounted modal, and never leave it.
let nextRowId = 0;

// emptyJob returns a fresh builder job of the given type.
function emptyJob(type: BuilderJob["type"] = "url"): BuilderJob {
  return { name: "", type, source: "", targetKB: "", branch: "", path: "", extensions: [], label: "" };
}

// newRow wraps a job as a row, seeding the extensions text from the job so a job
// imported from a manifest shows its extensions.
function newRow(job: BuilderJob = emptyJob()): Row {
  return { id: nextRowId++, job, extText: job.extensions.join(", ") };
}

// isRepoJob reports whether a job type expands into repository files.
function isRepoJob(type: BuilderJob["type"]): boolean {
  return type === "github-repo" || type === "gitea-repo";
}

// validateJob returns field-level errors for a runnable job; `file` jobs are
// excluded from runs and never block, so they validate clean.
//
// base is the knowledge base this modal ingests into. A job carrying a target_kb
// for a different base is an error rather than something to quietly redirect: a
// batch ingest here loads one base, so honouring the manifest as written is not
// possible and running it anyway would fill the wrong base.
function validateJob(job: BuilderJob, base: string): JobErrors {
  const errs: JobErrors = {};
  const targetKB = job.targetKB.trim();
  // Index names are lower-cased, so a target differing only in case is the same
  // base (knowledge.FullIndexName).
  if (targetKB !== "" && targetKB.toLowerCase() !== base.trim().toLowerCase()) {
    errs.targetKB = `This job targets “${targetKB}”, but this batch ingests into “${base}”. Clear it to ingest here, or run the manifest with \`rag-cli.rag k ingest --batch\`.`;
  }
  if (job.type === "file") return errs;
  const source = job.source.trim();
  if (job.type === "url" && !/^https?:\/\//.test(source)) {
    errs.source = "Enter a valid http(s) URL.";
  } else if (job.type === "github-repo" && !/^(https:\/\/github\.com\/)?[^/\s]+\/[^/\s]+/.test(source)) {
    errs.source = "Enter owner/repo or https://github.com/owner/repo.";
  } else if (job.type === "gitea-repo" && !/^https?:\/\/[^/\s]+\/[^/\s]+\/[^/\s]+/.test(source)) {
    errs.source = "Enter a full URL, e.g. https://gitea.example.com/owner/repo.";
  }
  if (isRepoJob(job.type) && job.extensions.length === 0) {
    errs.extensions = "At least one extension is required — a repo job only fetches matching files.";
  }
  if (job.label && !LABEL_PATTERN.test(job.label)) {
    errs.label = "Lowercase letters, digits, and hyphens (max 32 characters).";
  }
  return errs;
}

// BatchIngestModal batch-ingests via two modes: uploading the YAML manifest
// `k ingest --batch` accepts (parsed client-side with a supported/unsupported
// preview), or building that manifest visually — add/edit jobs, validate
// inline, preview repo matches, download the YAML for CLI use, or run the
// supported jobs as a single tracked operation.
export default function BatchIngestModal({ name, onStarted, onClose }: Props) {
  const titleId = useId();
  const forceId = useId();
  const { dialogRef, onKeyDown } = useModalDialog(onClose);
  const [mode, setMode] = useState<Mode>("upload");
  const [preview, setPreview] = useState<PreviewEntry[] | null>(null);
  const [items, setItems] = useState<BatchItem[]>([]);
  const [uploadedJobs, setUploadedJobs] = useState<BuilderJob[]>([]);
  const [rows, setRows] = useState<Row[]>([newRow()]);
  const [jobsTouched, setJobsTouched] = useState(false);
  // Keyed by row id, not row position, so removing a row cannot reattach a
  // preview to a different job.
  const [jobPreviews, setJobPreviews] = useState<Record<number, JobPreviewState>>({});
  const [force, setForce] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const onFile = async (f: File | null) => {
    setError(null);
    setPreview(null);
    setItems([]);
    setUploadedJobs([]);
    if (!f) return;
    try {
      const text = await f.text();
      // The destination is passed so a job routed elsewhere by target_kb is held
      // back with the reason, instead of being redirected into this base.
      const parsed = parseBatchManifest(text, name);
      if (parsed.error) {
        setError(parsed.error);
        return;
      }
      setPreview(parsed.preview);
      setItems(parsed.items);
      setUploadedJobs(parsed.jobs);
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  const editInBuilder = () => {
    setRows(uploadedJobs.length > 0 ? uploadedJobs.map((job) => newRow(job)) : [newRow()]);
    setJobsTouched(true);
    setJobPreviews({});
    setError(null);
    setMode("build");
  };

  // updateRow patches one row by id, optionally also patching its job. Editing a
  // job invalidates the preview shown for that row.
  const updateRow = (id: number, patch: Partial<BuilderJob>, extText?: string) => {
    setJobsTouched(true);
    setRows((prev) =>
      prev.map((row) =>
        row.id === id
          ? { ...row, job: { ...row.job, ...patch }, extText: extText ?? row.extText }
          : row
      )
    );
    setJobPreviews((prev) => {
      if (!prev[id]) return prev;
      const next = { ...prev };
      delete next[id];
      return next;
    });
  };

  // setExtensions keeps the raw text and the normalized list in step, so what the
  // field shows and what validation and the manifest see never disagree.
  const setExtensions = (id: number, text: string) => {
    updateRow(id, { extensions: normalizeExtensions(text) }, text);
  };

  const addJob = () => {
    setRows((prev) => [...prev, newRow()]);
  };

  const removeJob = (id: number) => {
    setRows((prev) => prev.filter((row) => row.id !== id));
    // Ids are stable, so the surviving rows' previews need no re-keying.
    setJobPreviews((prev) => {
      if (!prev[id]) return prev;
      const next = { ...prev };
      delete next[id];
      return next;
    });
  };

  const previewJob = async (row: Row) => {
    const { id, job } = row;
    const errs = validateJob(job, name);
    if (errs.source || errs.extensions) {
      setJobPreviews((prev) => ({ ...prev, [id]: { error: errs.source ?? errs.extensions } }));
      return;
    }
    setJobPreviews((prev) => ({ ...prev, [id]: { busy: true } }));
    try {
      const result = await previewRepo({
        type: job.type === "github-repo" ? "github" : "gitea",
        source: job.source.trim(),
        branch: job.branch.trim() || undefined,
        path: job.path.trim() || undefined,
        extensions: job.extensions,
      });
      setJobPreviews((prev) => ({ ...prev, [id]: { result } }));
    } catch (e) {
      setJobPreviews((prev) => ({ ...prev, [id]: { error: errorMessage(e) } }));
    }
  };

  const jobs = rows.map((row) => row.job);
  const jobErrors = jobs.map((job) => validateJob(job, name));
  const runnableJobs = jobs.filter(
    (job, i) => builderJobToBatchItem(job) !== null && Object.keys(jobErrors[i]).length === 0
  );
  const builderValid =
    jobs.every((_, i) => Object.keys(jobErrors[i]).length === 0) && runnableJobs.length > 0;
  const startCount = mode === "upload" ? items.length : runnableJobs.length;

  const downloadYaml = () => {
    const url = URL.createObjectURL(
      new Blob([serializeBatchManifest(jobs)], { type: "application/yaml" })
    );
    const a = document.createElement("a");
    a.href = url;
    a.download = `${name}-batch.yaml`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const submit = async () => {
    // Only the jobs that validated clean are sent — the same set the button
    // counts, so a held-back job cannot slip into the run.
    const batch =
      mode === "upload"
        ? items
        : (runnableJobs.map(builderJobToBatchItem).filter(Boolean) as BatchItem[]);
    if (batch.length === 0 || (mode === "build" && !builderValid)) {
      setError("No supported entries to ingest.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      onStarted(await ingestBatch(name, batch, force));
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
          <div className="p-form__group" role="radiogroup" aria-label="Batch mode">
            <div className="kb-ingest__tabs">
              <label className="p-radio kb-ingest__tab">
                <input
                  type="radio"
                  className="p-radio__input"
                  name="batch-mode"
                  checked={mode === "upload"}
                  onChange={() => {
                    setMode("upload");
                    setError(null);
                  }}
                />
                <span className="p-radio__label">Upload manifest</span>
              </label>
              <label className="p-radio kb-ingest__tab">
                <input
                  type="radio"
                  className="p-radio__input"
                  name="batch-mode"
                  checked={mode === "build"}
                  onChange={() => {
                    setMode("build");
                    setError(null);
                  }}
                />
                <span className="p-radio__label">Build manifest</span>
              </label>
            </div>
          </div>

          {mode === "upload" && (
            <>
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
                <>
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
                              {entry.warning && (
                                <span className="u-text--muted p-text--small"> — {entry.warning}</span>
                              )}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                  <div className="p-form__group">
                    <button type="button" className="p-button u-no-margin--bottom" onClick={editInBuilder}>
                      Edit in builder
                    </button>
                  </div>
                </>
              )}
            </>
          )}

          {mode === "build" && (
            <>
              {rows.map((row, i) => {
                const { id, job } = row;
                const errs = jobsTouched ? jobErrors[i] : {};
                const jp = jobPreviews[id];
                return (
                  <fieldset key={id} className="kb-batch__job">
                    <legend className="kb-batch__job-legend">
                      Job {i + 1}
                      <button
                        type="button"
                        className="p-button--base u-no-margin--bottom kb-batch__job-remove"
                        aria-label={`Remove job ${i + 1}`}
                        disabled={rows.length === 1}
                        onClick={() => removeJob(id)}
                      >
                        Remove
                      </button>
                    </legend>

                    {job.targetKB && (
                      <div
                        className={`p-form__group ${errs.targetKB ? "p-form-validation is-error" : ""}`}
                      >
                        <p className="p-form-help-text">
                          Manifest <code>target_kb</code>: <strong>{job.targetKB}</strong>
                        </p>
                        {errs.targetKB ? (
                          <>
                            <p className="p-form-validation__message">{errs.targetKB}</p>
                            <button
                              type="button"
                              className="p-button u-no-margin--bottom"
                              onClick={() => updateRow(id, { targetKB: "" })}
                            >
                              Ingest into {name} instead
                            </button>
                          </>
                        ) : (
                          <p className="p-form-help-text u-text--muted">
                            This is the base being ingested, so it changes nothing.
                          </p>
                        )}
                      </div>
                    )}

                    <div className="p-form__group">
                      <label htmlFor={`${titleId}-type-${id}`}>Type</label>
                      <select
                        id={`${titleId}-type-${id}`}
                        value={job.type}
                        onChange={(e) => updateRow(id, { type: e.target.value as BuilderJob["type"] })}
                      >
                        <option value="url">url</option>
                        <option value="github-repo">github-repo</option>
                        <option value="gitea-repo">gitea-repo</option>
                        {job.type === "file" && <option value="file">file</option>}
                      </select>
                      {job.type === "file" && (
                        <p className="p-form-help-text u-text--muted">
                          {FILE_JOB_UNSUPPORTED} Kept for the downloaded manifest; excluded from a
                          run.
                        </p>
                      )}
                    </div>

                    <div className={`p-form__group ${errs.source ? "p-form-validation is-error" : ""}`}>
                      <label htmlFor={`${titleId}-source-${id}`}>Source</label>
                      <input
                        id={`${titleId}-source-${id}`}
                        type="text"
                        value={job.source}
                        autoComplete="off"
                        placeholder={
                          job.type === "github-repo"
                            ? "owner/repo"
                            : job.type === "gitea-repo"
                              ? "https://gitea.example.com/owner/repo"
                              : "https://example.com/page"
                        }
                        onChange={(e) => updateRow(id, { source: e.target.value })}
                      />
                      {errs.source && <p className="p-form-validation__message">{errs.source}</p>}
                    </div>

                    <div className="p-form__group">
                      <label htmlFor={`${titleId}-name-${id}`}>Source ID (optional)</label>
                      <input
                        id={`${titleId}-name-${id}`}
                        type="text"
                        value={job.name}
                        autoComplete="off"
                        onChange={(e) => updateRow(id, { name: e.target.value })}
                      />
                    </div>

                    {isRepoJob(job.type) && (
                      <>
                        <div className="p-form__group">
                          <label htmlFor={`${titleId}-branch-${id}`}>Branch (optional)</label>
                          <input
                            id={`${titleId}-branch-${id}`}
                            type="text"
                            value={job.branch}
                            autoComplete="off"
                            onChange={(e) => updateRow(id, { branch: e.target.value })}
                          />
                        </div>
                        <div className="p-form__group">
                          <label htmlFor={`${titleId}-path-${id}`}>Path prefix (optional)</label>
                          <input
                            id={`${titleId}-path-${id}`}
                            type="text"
                            value={job.path}
                            autoComplete="off"
                            placeholder="docs/"
                            onChange={(e) => updateRow(id, { path: e.target.value })}
                          />
                        </div>
                        <div className={`p-form__group ${errs.extensions ? "p-form-validation is-error" : ""}`}>
                          <label htmlFor={`${titleId}-ext-${id}`}>File extensions</label>
                          <input
                            id={`${titleId}-ext-${id}`}
                            type="text"
                            value={row.extText}
                            autoComplete="off"
                            placeholder=".md, .rst, .txt"
                            onChange={(e) => setExtensions(id, e.target.value)}
                            // Blur tidies what is shown into the normalized form
                            // that was already parsed on every keystroke, so the
                            // field, the validation, and the manifest agree.
                            onBlur={() => updateRow(id, {}, job.extensions.join(", "))}
                          />
                          {errs.extensions && (
                            <p className="p-form-validation__message">{errs.extensions}</p>
                          )}
                        </div>
                        <div className="p-form__group">
                          <button
                            type="button"
                            className="p-button u-no-margin--bottom"
                            disabled={jp?.busy}
                            onClick={() => void previewJob(row)}
                          >
                            {jp?.busy ? (
                              <>
                                <i className="p-icon--spinner u-animation--spin" aria-hidden="true" />{" "}
                                Previewing…
                              </>
                            ) : (
                              "Preview files"
                            )}
                          </button>
                          {jp?.result && (
                            <span className="kb-batch__job-preview" aria-live="polite">
                              <strong>{jp.result.total}</strong> file
                              {jp.result.total === 1 ? "" : "s"} match{jp.result.total === 1 ? "es" : ""}
                              {jp.result.truncated && (
                                <span className="u-text--muted"> (listing truncated)</span>
                              )}
                            </span>
                          )}
                          {jp?.error && (
                            <span className="kb-batch__job-preview p-form-validation__message" role="alert">
                              {jp.error}
                            </span>
                          )}
                        </div>
                      </>
                    )}

                    <div className={`p-form__group ${errs.label ? "p-form-validation is-error" : ""}`}>
                      <label htmlFor={`${titleId}-label-${id}`}>Label (optional)</label>
                      <input
                        id={`${titleId}-label-${id}`}
                        type="text"
                        value={job.label}
                        autoComplete="off"
                        onChange={(e) => updateRow(id, { label: e.target.value })}
                      />
                      {errs.label && <p className="p-form-validation__message">{errs.label}</p>}
                    </div>
                  </fieldset>
                );
              })}

              <div className="p-form__group">
                <button type="button" className="p-button u-no-margin--bottom" onClick={addJob}>
                  Add job
                </button>
              </div>

              {error && (
                <p className="p-form-validation__message" role="alert">
                  {error}
                </p>
              )}
            </>
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
            {mode === "build" && (
              <button
                type="button"
                className="p-button u-no-margin--bottom"
                disabled={jobs.every((job) => !job.source.trim())}
                onClick={downloadYaml}
              >
                Download YAML
              </button>
            )}
            <button
              type="submit"
              className="p-button--positive u-no-margin--bottom"
              disabled={busy || startCount === 0 || (mode === "build" && !builderValid)}
            >
              {busy ? "Starting…" : `Start batch (${startCount})`}
            </button>
          </footer>
        </form>
      </div>
    </div>
  );
}
