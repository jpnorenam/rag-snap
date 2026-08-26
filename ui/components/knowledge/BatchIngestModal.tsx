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
}

// JobPreviewState tracks one job's repo-preview call.
interface JobPreviewState {
  busy?: boolean;
  result?: RepoPreview;
  error?: string;
}

// emptyJob returns a fresh builder job of the given type.
function emptyJob(type: BuilderJob["type"] = "url"): BuilderJob {
  return { name: "", type, source: "", branch: "", path: "", extensions: [], label: "" };
}

// isRepoJob reports whether a job type expands into repository files.
function isRepoJob(type: BuilderJob["type"]): boolean {
  return type === "github-repo" || type === "gitea-repo";
}

// validateJob returns field-level errors for a runnable job; `file` jobs are
// excluded from runs and never block, so they validate clean.
function validateJob(job: BuilderJob): JobErrors {
  const errs: JobErrors = {};
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
  const [jobs, setJobs] = useState<BuilderJob[]>([emptyJob()]);
  const [jobsTouched, setJobsTouched] = useState(false);
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
      const parsed = parseBatchManifest(text);
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
    setJobs(uploadedJobs.length > 0 ? uploadedJobs : [emptyJob()]);
    setJobsTouched(true);
    setJobPreviews({});
    setError(null);
    setMode("build");
  };

  const updateJob = (index: number, patch: Partial<BuilderJob>) => {
    setJobsTouched(true);
    setJobs((prev) => prev.map((job, i) => (i === index ? { ...job, ...patch } : job)));
    // Editing a job invalidates its shown preview.
    setJobPreviews((prev) => {
      if (!prev[index]) return prev;
      const next = { ...prev };
      delete next[index];
      return next;
    });
  };

  const addJob = () => {
    setJobs((prev) => [...prev, emptyJob()]);
  };

  const removeJob = (index: number) => {
    setJobs((prev) => prev.filter((_, i) => i !== index));
    setJobPreviews((prev) => {
      const next: Record<number, JobPreviewState> = {};
      for (const [k, v] of Object.entries(prev)) {
        const i = Number(k);
        if (i < index) next[i] = v;
        else if (i > index) next[i - 1] = v;
      }
      return next;
    });
  };

  const previewJob = async (index: number) => {
    const job = jobs[index];
    const errs = validateJob(job);
    if (errs.source || errs.extensions) {
      setJobPreviews((prev) => ({ ...prev, [index]: { error: errs.source ?? errs.extensions } }));
      return;
    }
    setJobPreviews((prev) => ({ ...prev, [index]: { busy: true } }));
    try {
      const result = await previewRepo({
        type: job.type === "github-repo" ? "github" : "gitea",
        source: job.source.trim(),
        branch: job.branch.trim() || undefined,
        path: job.path.trim() || undefined,
        extensions: job.extensions,
      });
      setJobPreviews((prev) => ({ ...prev, [index]: { result } }));
    } catch (e) {
      setJobPreviews((prev) => ({ ...prev, [index]: { error: errorMessage(e) } }));
    }
  };

  const jobErrors = jobs.map(validateJob);
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
    const batch =
      mode === "upload"
        ? items
        : (jobs.map(builderJobToBatchItem).filter(Boolean) as BatchItem[]);
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
              {jobs.map((job, i) => {
                const errs = jobsTouched ? jobErrors[i] : {};
                const jp = jobPreviews[i];
                return (
                  <fieldset key={i} className="kb-batch__job">
                    <legend className="kb-batch__job-legend">
                      Job {i + 1}
                      <button
                        type="button"
                        className="p-button--base u-no-margin--bottom kb-batch__job-remove"
                        aria-label={`Remove job ${i + 1}`}
                        disabled={jobs.length === 1}
                        onClick={() => removeJob(i)}
                      >
                        Remove
                      </button>
                    </legend>

                    <div className="p-form__group">
                      <label htmlFor={`${titleId}-type-${i}`}>Type</label>
                      <select
                        id={`${titleId}-type-${i}`}
                        value={job.type}
                        onChange={(e) => updateJob(i, { type: e.target.value as BuilderJob["type"] })}
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
                      <label htmlFor={`${titleId}-source-${i}`}>Source</label>
                      <input
                        id={`${titleId}-source-${i}`}
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
                        onChange={(e) => updateJob(i, { source: e.target.value })}
                      />
                      {errs.source && <p className="p-form-validation__message">{errs.source}</p>}
                    </div>

                    <div className="p-form__group">
                      <label htmlFor={`${titleId}-name-${i}`}>Source ID (optional)</label>
                      <input
                        id={`${titleId}-name-${i}`}
                        type="text"
                        value={job.name}
                        autoComplete="off"
                        onChange={(e) => updateJob(i, { name: e.target.value })}
                      />
                    </div>

                    {isRepoJob(job.type) && (
                      <>
                        <div className="p-form__group">
                          <label htmlFor={`${titleId}-branch-${i}`}>Branch (optional)</label>
                          <input
                            id={`${titleId}-branch-${i}`}
                            type="text"
                            value={job.branch}
                            autoComplete="off"
                            onChange={(e) => updateJob(i, { branch: e.target.value })}
                          />
                        </div>
                        <div className="p-form__group">
                          <label htmlFor={`${titleId}-path-${i}`}>Path prefix (optional)</label>
                          <input
                            id={`${titleId}-path-${i}`}
                            type="text"
                            value={job.path}
                            autoComplete="off"
                            placeholder="docs/"
                            onChange={(e) => updateJob(i, { path: e.target.value })}
                          />
                        </div>
                        <div className={`p-form__group ${errs.extensions ? "p-form-validation is-error" : ""}`}>
                          <label htmlFor={`${titleId}-ext-${i}`}>File extensions</label>
                          <input
                            id={`${titleId}-ext-${i}`}
                            type="text"
                            defaultValue={job.extensions.join(", ")}
                            autoComplete="off"
                            placeholder=".md, .rst, .txt"
                            onBlur={(e) => updateJob(i, { extensions: normalizeExtensions(e.target.value) })}
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
                            onClick={() => void previewJob(i)}
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
                      <label htmlFor={`${titleId}-label-${i}`}>Label (optional)</label>
                      <input
                        id={`${titleId}-label-${i}`}
                        type="text"
                        value={job.label}
                        autoComplete="off"
                        onChange={(e) => updateJob(i, { label: e.target.value })}
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
