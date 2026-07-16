"use client";

import Spinner from "@/components/common/Spinner";
import { statusOf, type OperationView } from "@/lib/api/operations";

interface Props {
  // The tracked run operation, resolved from the operations context by
  // AnswerScreen. Null briefly until the tracked list includes it.
  op: OperationView | null;
  // Opens the "Cancel run?" confirmation. A batch run is modal: the only in-place
  // way to leave the running view is to cancel it (progress lost). Navigating to
  // another section instead leaves the run alive in the global indicator.
  onCancelRun: () => void;
}

// RunningBatch shows live progress for a batch run. Progress comes from the
// operation's questions_done/questions_total metadata; a single spinner is the
// fallback when the operation exposes no per-question counts.
export default function RunningBatch({ op, onCancelRun }: Props) {
  const done = numberMeta(op, "questions_done");
  const total = numberMeta(op, "questions_total");
  const status = op ? statusOf(op) : "running";

  return (
    <section className="answer-flow">
      <div className="answer-flow__head">
        <h2 className="p-heading--4 u-no-margin--bottom">Answering questions</h2>
        <button
          type="button"
          className="p-button--base u-no-margin--bottom"
          onClick={onCancelRun}
        >
          Cancel run
        </button>
      </div>

      <div className="answer-running" aria-live="polite">
        {total !== null && total > 0 ? (
          <>
            <p className="u-no-margin--bottom">
              Answered {done ?? 0} of {total} question{total === 1 ? "" : "s"}…
            </p>
            <div
              className="answer-running__bar"
              role="progressbar"
              aria-valuenow={done ?? 0}
              aria-valuemin={0}
              aria-valuemax={total}
            >
              <span
                className="answer-running__bar-fill"
                style={{ width: `${Math.round(((done ?? 0) / total) * 100)}%` }}
              />
            </div>
          </>
        ) : (
          <Spinner label={`Answering questions… (${status})`} />
        )}
        <p className="u-text--muted p-text--small">
          This runs as a background operation — you can track or cancel it from the operations
          indicator in the top bar.
        </p>
      </div>
    </section>
  );
}

function numberMeta(op: OperationView | null, key: string): number | null {
  const v = op?.metadata?.[key];
  return typeof v === "number" ? v : null;
}
