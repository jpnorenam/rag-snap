"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import Header from "@/components/Header";
import EmptyState from "@/components/common/EmptyState";
import { useOperations } from "@/lib/useOperations";
import { isTerminal, statusOf, type OperationView } from "@/lib/api/operations";
import { normalizeQAFile, type ResultsParseError } from "@/lib/results";
import type { ParsedQAFile, QAFile } from "@/lib/types";
import type { BatchManifest } from "@/lib/api/answer";
import ManifestRunner from "@/components/answer/ManifestRunner";
import BuildWizard from "@/components/answer/BuildWizard";
import RunningBatch from "@/components/answer/RunningBatch";
import ResultsReview from "@/components/answer/ResultsReview";
import ResultsOpener from "@/components/answer/ResultsOpener";
import ConfirmModal from "@/components/common/ConfirmModal";
import ErrorBoundary from "@/components/common/ErrorBoundary";

// Step is the client-side state machine for the Answer section. All flows share
// one page (docs/ux/04-answer-batch.md) and converge on the review surface.
type Step = "landing" | "run" | "wizard" | "running" | "review";

// ReviewContext carries what the review surface shows alongside the parsed
// results: the manifest name and the KBs used, plus the originating operation id
// (present for a batch run, absent for a results file opened from disk).
interface ReviewContext {
  parsed: ParsedQAFile;
  manifestName?: string;
  knowledgeBases?: string[];
  operationId?: string;
}

// kbsFromResources recovers the knowledge-base names from an operation's
// resources map (the daemon records them as "/1.0/knowledge/<name>"), so a run
// resumed from the operations context still shows its KB chips even when the
// originating manifest is no longer in local state.
function kbsFromResources(resources: Record<string, string[]>): string[] | undefined {
  const refs = resources?.knowledge;
  if (!refs || refs.length === 0) return undefined;
  return refs.map((r) => r.replace(/^\/1\.0\/knowledge\//, ""));
}

export default function AnswerScreen() {
  const ops = useOperations();
  const [step, setStep] = useState<Step>("landing");
  // The batch-run operation id we are tracking through the running → review
  // transition. Read back out of the operations context so a run survives
  // navigation away and back to /answer/.
  const [runOpId, setRunOpId] = useState<string | null>(null);
  const [review, setReview] = useState<ReviewContext | null>(null);
  const [runManifest, setRunManifest] = useState<BatchManifest | null>(null);
  // Display name for the run, supplied by the flow that started it: the uploaded
  // manifest's filename (Flow 1) or the name the user entered in the wizard's
  // configure step (Flow 2). Not derived from manifest.version, which is "1.0"
  // for nearly every manifest.
  const [runName, setRunName] = useState<string | undefined>(undefined);
  const [error, setError] = useState<string | null>(null);
  // Cancel-run confirmation (running view) and its in-flight flag.
  const [confirmCancel, setConfirmCancel] = useState(false);
  const [cancelBusy, setCancelBusy] = useState(false);

  // The tracked run operation, resolved from the shared operations list.
  const runOp = useMemo<OperationView | null>(
    () => (runOpId ? ops.operations.find((o) => o.id === runOpId) ?? null : null),
    [ops.operations, runOpId]
  );

  // Reactive resume: a batch run is modal until explicitly resolved (cancelled,
  // or its results consumed/exited), so whenever we are not already tracking one
  // and a live batch operation exists — running OR completed — adopt it. This
  // fires on mount and on every operations-list change, so arriving at /answer/
  // (fresh load, sidebar, or the indicator link) always surfaces the run; the
  // status-routing effect below decides whether that means running or review.
  // Consumed/exited operations are skipped: they have been resolved and must not
  // pull the user back.
  useEffect(() => {
    // Adopt only from the landing state: a run/wizard in progress or an opened
    // results file must not be interrupted. Every arrival at /answer/ that isn't
    // mid-task lands here first, so this still covers fresh load, sidebar, and
    // the indicator link.
    if (runOpId || step !== "landing") return;
    const batch = ops.operations.find(
      (o) => o.class === "task" && isBatchRun(o) && !ops.isConsumed(o.id) && !ops.isExited(o.id)
    );
    if (batch) setRunOpId(batch.id);
  }, [ops, runOpId, step]);

  // Route the step off the tracked operation's status — the single place that
  // decides running vs review vs error. Runs on adoption (resume) and on every
  // status change, so a completed-while-away run lands on the review surface and
  // a still-running one on the running view.
  useEffect(() => {
    if (!runOp) return;
    if (!isTerminal(runOp)) {
      setStep("running");
      return;
    }
    const status = statusOf(runOp);
    if (status === "succeeded") {
      try {
        const parsed = normalizeQAFile(metadataToQAFile(runOp.metadata));
        setReview({
          parsed,
          manifestName: runName,
          // Prefer the originating manifest's KBs; fall back to the operation's
          // resources so a resumed run still shows its KB chips.
          knowledgeBases: runManifest?.knowledge_bases ?? kbsFromResources(runOp.resources),
          operationId: runOp.id,
        });
        setStep("review");
      } catch (e) {
        setError((e as ResultsParseError).message ?? "could not read batch results");
        setStep("landing");
      }
    } else {
      // Cancelled or failed: the operations panel shows the detail; return the
      // user to the landing state so they can retry.
      setError(runOp.err || `batch ${status}`);
      setStep("landing");
    }
  }, [runOp, runManifest, runName]);

  // startRun is invoked by the manifest runner and the wizard's Run batch: it
  // registers the operation and switches to the running view.
  const startRun = useCallback(
    (tracked: OperationView, manifest: BatchManifest, name?: string) => {
      // Record the originating route so the global operations indicator can
      // link back here from any tab.
      ops.track(tracked, "/answer/");
      setRunManifest(manifest);
      setRunName(name);
      setRunOpId(tracked.id);
      setError(null);
      setStep("running");
    },
    [ops]
  );

  // openResults is invoked by Flow 3 (open a results file) to jump straight to
  // the review surface without a run. No operationId: there is no lifecycle to
  // consume/exit — the source is a file on disk.
  const openResults = useCallback((parsed: ParsedQAFile, name?: string) => {
    setReview({ parsed, manifestName: name });
    setError(null);
    setStep("landing"); // reset first so a stale runOpId can't linger
    setRunOpId(null);
    setStep("review");
  }, []);

  // Local reset back to the landing cards, clearing all run-scoped state. Used
  // after a cancelled run or an exited review; the operation's own lifecycle
  // (cancel / markExited) is handled by the callers before this.
  const resetToLanding = useCallback(() => {
    setStep("landing");
    setRunOpId(null);
    setRunManifest(null);
    setRunName(undefined);
    setReview(null);
    setError(null);
  }, []);

  // cancelRun cancels the in-flight batch operation (running view's only exit
  // besides navigating away) and returns to landing.
  const cancelRun = useCallback(async () => {
    if (!runOp) return;
    setCancelBusy(true);
    try {
      await ops.cancel(runOp.id);
      ops.dismiss(runOp.id);
    } catch {
      // Even if the daemon refuses (already finishing), drop the local view;
      // the indicator still reflects the daemon's truth.
    } finally {
      setCancelBusy(false);
      setConfirmCancel(false);
      resetToLanding();
    }
  }, [ops, runOp, resetToLanding]);

  // exitReview is invoked from the review surface's Exit confirm. It marks the
  // operation exited (removing it from the indicator, blocking auto-resume) and
  // returns to landing. A results-file review (no operationId) just closes.
  const exitReview = useCallback(() => {
    const id = review?.operationId;
    if (id) ops.markExited(id);
    resetToLanding();
  }, [ops, review, resetToLanding]);

  // markConsumed bridge for the review surface (export / successful collaborate).
  const consumeReview = useCallback(() => {
    const id = review?.operationId;
    if (id) ops.markConsumed(id);
  }, [ops, review]);

  return (
    <>
      <Header title="Answer RFPs" />
      <main className="app-main answer">
        {error && (
          <div className="p-notification--negative" role="alert">
            <div className="p-notification__content">
              <p className="p-notification__message">{error}</p>
            </div>
          </div>
        )}

        {step === "landing" && (
          <AnswerLanding
            onRun={() => setStep("run")}
            onBuild={() => setStep("wizard")}
            onReview={openResults}
            onReviewError={setError}
          />
        )}

        {step === "run" && (
          <ManifestRunner onRun={startRun} onCancel={resetToLanding} onError={setError} />
        )}

        {step === "wizard" && (
          // Backstop: if the wizard ever throws mid-render (an unexpected
          // metadata shape the normalizer didn't catch), surface it as a
          // notification and return to landing rather than crashing the page.
          <ErrorBoundary
            onError={(msg) => {
              setError(`The build wizard hit an unexpected error: ${msg}`);
              resetToLanding();
            }}
            fallback={() => null}
          >
            <BuildWizard onRun={startRun} onCancel={resetToLanding} onError={setError} />
          </ErrorBoundary>
        )}

        {step === "running" && (
          // The running view's only in-place exit is Cancel run (confirmed);
          // navigating to another section leaves the run alive in the indicator.
          <RunningBatch op={runOp} onCancelRun={() => setConfirmCancel(true)} />
        )}

        {step === "review" && review && (
          <ResultsReview
            parsed={review.parsed}
            manifestName={review.manifestName}
            knowledgeBases={review.knowledgeBases}
            resumable={review.operationId !== undefined}
            consumed={review.operationId ? ops.isConsumed(review.operationId) : false}
            onConsumed={consumeReview}
            onExit={exitReview}
          />
        )}
      </main>

      {confirmCancel && (
        <ConfirmModal
          title="Cancel run"
          confirmLabel="Cancel run"
          destructive
          busy={cancelBusy}
          onConfirm={() => void cancelRun()}
          onClose={() => setConfirmCancel(false)}
        >
          <p>Cancel this batch run? Progress will be lost.</p>
        </ConfirmModal>
      )}
    </>
  );
}

// AnswerLanding is the three-card landing state (docs/ux/04-answer-batch.md).
function AnswerLanding({
  onRun,
  onBuild,
  onReview,
  onReviewError,
}: {
  onRun: () => void;
  onBuild: () => void;
  onReview: (parsed: ParsedQAFile, name?: string) => void;
  onReviewError: (message: string) => void;
}) {
  return (
    <>
      <EmptyState
        headline="Answer a batch of questions"
        guidance="Run a prepared manifest, build one from an RFP document, or review a previous results file."
        command="rag-cli.rag answer batch <manifest.yaml>"
      />
      <div className="answer-entry-grid">
        <button type="button" className="answer-entry" onClick={onRun}>
          <span className="answer-entry__title">Run a manifest</span>
          <span className="answer-entry__desc u-text--muted p-text--small">
            Upload a YAML manifest of questions and run it against your knowledge bases.
          </span>
        </button>

        <button type="button" className="answer-entry" onClick={onBuild}>
          <span className="answer-entry__title">Build from a document</span>
          <span className="answer-entry__desc u-text--muted p-text--small">
            Extract candidate questions from a PDF, DOCX, XLSX, or CSV, then review and run them.
          </span>
        </button>

        <div className="answer-entry answer-entry--opener">
          <span className="answer-entry__title">Review results</span>
          <span className="answer-entry__desc u-text--muted p-text--small">
            Open a previously exported results file to review the answers.
          </span>
          <ResultsOpener onOpen={onReview} onError={onReviewError} />
        </div>
      </div>
    </>
  );
}

// isBatchRun heuristically identifies a batch-answer run among tracked
// operations by its description or its progress metadata keys. The daemon does
// not expose an operation subtype, so we match on the shape it publishes.
function isBatchRun(op: OperationView): boolean {
  if (/answer/i.test(op.description) && /batch|question/i.test(op.description)) return true;
  return "questions_total" in (op.metadata ?? {}) && "results" in (op.metadata ?? {});
}

// metadataToQAFile shapes the run operation's metadata into a QAFile for
// normalization. The daemon publishes generated_at/model/results on completion.
function metadataToQAFile(metadata: Record<string, unknown>): QAFile {
  return {
    generated_at: typeof metadata.generated_at === "string" ? metadata.generated_at : "",
    model: typeof metadata.model === "string" ? metadata.model : "",
    results: (metadata.results as QAFile["results"]) ?? [],
  };
}
