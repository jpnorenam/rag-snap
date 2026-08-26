"use client";

import { useCallback, useMemo, useState } from "react";
import { errorMessage } from "@/lib/api/envelope";
import { parseManifest, ManifestParseError } from "@/lib/manifest";
import { compileDomains, resolveDomain } from "@/lib/domains";
import { runBatch, type BatchDomain, type BatchManifest } from "@/lib/api/answer";
import type { OperationView } from "@/lib/api/operations";

interface Props {
  // onRun hands the started operation, the manifest, and a display name up to
  // AnswerScreen, which tracks it and switches to the running view. The name is
  // the uploaded file's name (extension stripped) so the review surface and the
  // handoff payload identify the batch by its manifest, not "Batch 1.0".
  onRun: (op: OperationView, manifest: BatchManifest, name?: string) => void;
  onCancel: () => void;
  onError: (message: string) => void;
}

// DEFAULT_TEMPERATURE matches the CLI `answer batch` default.
const DEFAULT_TEMPERATURE = 0.1;

// RoutingPreview is the pre-run view of a manifest's `domains` block: per entry,
// how many questions it would apply to, and the pattern each question resolves
// to. It is advisory — computed by the client copy of the resolver
// (lib/domains.ts), while the domain recorded on each result is what actually
// ran — so it is labelled as such on screen.
export interface RoutingPreview {
  entries: { domain: BatchDomain; count: number }[];
  // matches[i] is the pattern question i resolves to, or null when the table
  // reaches it for nothing.
  matches: (string | null)[];
  unrouted: number;
}

// routingPreview resolves every question against the manifest's routing table.
// It returns null when there is no table to preview (and, defensively, when the
// table does not compile — parseManifest already rejects that, so a manifest
// that reached this screen cannot carry one). Exported for its own test: the
// counts and the unrouted set are what the preview claims about a run.
export function routingPreview(manifest: BatchManifest): RoutingPreview | null {
  const domains = manifest.domains ?? [];
  if (domains.length === 0) return null;
  let compiled;
  try {
    compiled = compileDomains(domains);
  } catch {
    return null;
  }

  // resolveDomain returns the compiled entry's own object, so identity maps the
  // hit back to the row whose count it belongs to.
  const row = new Map<BatchDomain, number>();
  compiled.forEach((c, i) => row.set(c.domain, i));
  const entries = compiled.map((c) => ({ domain: c.domain, count: 0 }));

  // The run resolves on the question's own id with no positional fallback
  // (chat.RunBatch passes q.ID through verbatim), so an id-less question routes
  // only through a catch-all. Mirror that rather than substituting its position.
  const matches = manifest.questions.map((q) => {
    const hit = resolveDomain(compiled, q.id ?? "", q.source ?? "");
    if (!hit) return null;
    const i = row.get(hit);
    if (i !== undefined) entries[i].count++;
    return hit.match;
  });

  return { entries, matches, unrouted: matches.filter((m) => m === null).length };
}

// ManifestRunner is Flow 1: upload a YAML manifest, preview it (client-side
// parse), then run it as a tracked operation. Invalid YAML never reaches the API.
export default function ManifestRunner({ onRun, onCancel, onError }: Props) {
  const [manifest, setManifest] = useState<BatchManifest | null>(null);
  const [fileName, setFileName] = useState<string>("");
  const [parseError, setParseError] = useState<string | null>(null);
  const [temperature, setTemperature] = useState<number>(DEFAULT_TEMPERATURE);
  const [running, setRunning] = useState(false);

  // Resolved once per parsed manifest: the preview walks every question, so it
  // is not recomputed on a temperature change or a re-render.
  const routing = useMemo(() => (manifest ? routingPreview(manifest) : null), [manifest]);

  const onFile = useCallback(async (file: File | undefined) => {
    if (!file) return;
    setParseError(null);
    setManifest(null);
    setFileName(file.name);
    try {
      const text = await file.text();
      setManifest(parseManifest(text));
    } catch (e) {
      // Keep the file re-selectable: clear the parsed manifest, show the error.
      setManifest(null);
      setParseError(
        e instanceof ManifestParseError ? e.message : `could not read manifest: ${String(e)}`
      );
    }
  }, []);

  const doRun = useCallback(async () => {
    if (!manifest) return;
    setRunning(true);
    try {
      const body: BatchManifest = { ...manifest, temperature };
      const { view } = await runBatch(body);
      // Name the run by the uploaded file, with any .yaml/.yml extension
      // stripped (e.g. "vendor-rfp.yaml" → "vendor-rfp").
      const name = fileName.replace(/\.ya?ml$/i, "") || undefined;
      onRun(view, body, name);
    } catch (e) {
      onError(errorMessage(e));
      setRunning(false);
    }
  }, [manifest, temperature, fileName, onRun, onError]);

  return (
    <section className="answer-flow">
      <div className="answer-flow__head">
        <h2 className="p-heading--4 u-no-margin--bottom">Run a manifest</h2>
        <button type="button" className="p-button--base u-no-margin--bottom" onClick={onCancel}>
          Back
        </button>
      </div>

      {/* Run controls sit on one line so, once a file is selected, everything
          needed to run is visible without scrolling past the question preview.
          Temperature and Run batch appear only after a valid parse. */}
      <div className="answer-flow__controls">
        <div className={`p-form-validation ${parseError ? "is-error" : ""}`}>
          <label className="p-form__label" htmlFor="manifest-file">
            Manifest file (YAML)
          </label>
          <input
            id="manifest-file"
            type="file"
            accept=".yaml,.yml,text/yaml,application/x-yaml"
            className="p-form-validation__input"
            onChange={(e) => void onFile(e.target.files?.[0])}
          />
          {parseError && (
            <p className="p-form-validation__message" role="alert">
              {parseError}
            </p>
          )}
        </div>

        {manifest && (
          <>
            <div className="p-form__group answer-flow__temp">
              <label htmlFor="temperature">Temperature</label>
              <input
                id="temperature"
                type="number"
                min={0}
                max={2}
                step={0.1}
                value={temperature}
                onChange={(e) => setTemperature(Number(e.target.value))}
              />
            </div>

            <button
              type="button"
              className="p-button--positive u-no-margin--bottom answer-flow__run"
              onClick={() => void doRun()}
              disabled={running}
            >
              {running ? (
                <>
                  <i className="p-icon--spinner u-animation--spin" aria-hidden="true" /> Running…
                </>
              ) : (
                "Run batch"
              )}
            </button>
          </>
        )}
      </div>

      {manifest && (
        <div className="answer-preview">
          <p className="u-text--muted p-text--small u-no-margin--bottom">
            {fileName}
            {manifest.version ? ` · version ${manifest.version}` : ""}
          </p>

          {manifest.knowledge_bases && manifest.knowledge_bases.length > 0 && (
            <div className="answer-preview__kbs">
              <span className="kb-selector__label">Knowledge bases:</span>
              {manifest.knowledge_bases.map((kb) => (
                <span key={kb} className="p-chip">
                  <span className="p-chip__value">{kb}</span>
                </span>
              ))}
            </div>
          )}

          {/* Routing summary: what the `domains` block would do to this
              manifest, before it is sent. Absent entirely when the manifest
              carries no routing table, so an older manifest previews exactly as
              it did before. */}
          {routing && (
            <div className="answer-preview__routing">
              <p className="answer-preview__routing-head u-no-margin--bottom">
                Domain routing
                <span className="u-text--muted p-text--small">
                  {" · shown for review — the domain recorded on each answer is what ran"}
                </span>
              </p>
              <ul className="answer-preview__domains">
                {routing.entries.map(({ domain, count }, i) => (
                  <li key={i} className="answer-preview__domain">
                    <code>{domain.match}</code>
                    <span className="u-text--muted p-text--small">
                      {` · ${count} question${count === 1 ? "" : "s"}`}
                    </span>
                    <span className="answer-preview__domain-context">
                      {domain.context?.trim()
                        ? domain.context
                        : `Keywords only: ${(domain.keywords ?? []).join(", ")}`}
                    </span>
                  </li>
                ))}
              </ul>
              <p className="u-text--muted p-text--small u-no-margin--bottom">
                {routing.unrouted === 0
                  ? "Every question matches an entry."
                  : `${routing.unrouted} of ${manifest.questions.length} question${
                      manifest.questions.length === 1 ? "" : "s"
                    } match no entry and are answered with no domain, marked below.`}
              </p>
            </div>
          )}

          <ol className="answer-preview__questions">
            {manifest.questions.map((q, i) => (
              <li key={q.id ?? i}>
                {q.question}
                {/* Which entry reaches this question, and — the point of 7.2 —
                    which questions it reaches nothing for. */}
                {routing &&
                  (routing.matches[i] === null ? (
                    <span className="answer-preview__q-domain is-unrouted">no domain</span>
                  ) : (
                    <span className="answer-preview__q-domain">{routing.matches[i]}</span>
                  ))}
              </li>
            ))}
          </ol>
        </div>
      )}
    </section>
  );
}
