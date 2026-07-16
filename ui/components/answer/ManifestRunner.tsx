"use client";

import { useCallback, useState } from "react";
import { errorMessage } from "@/lib/api/envelope";
import { parseManifest, ManifestParseError } from "@/lib/manifest";
import { runBatch, type BatchManifest } from "@/lib/api/answer";
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

// ManifestRunner is Flow 1: upload a YAML manifest, preview it (client-side
// parse), then run it as a tracked operation. Invalid YAML never reaches the API.
export default function ManifestRunner({ onRun, onCancel, onError }: Props) {
  const [manifest, setManifest] = useState<BatchManifest | null>(null);
  const [fileName, setFileName] = useState<string>("");
  const [parseError, setParseError] = useState<string | null>(null);
  const [temperature, setTemperature] = useState<number>(DEFAULT_TEMPERATURE);
  const [running, setRunning] = useState(false);

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

          <ol className="answer-preview__questions">
            {manifest.questions.map((q, i) => (
              <li key={q.id ?? i}>{q.question}</li>
            ))}
          </ol>
        </div>
      )}
    </section>
  );
}
