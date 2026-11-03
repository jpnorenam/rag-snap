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
// fallback when the operation exposes no per-question counts. Once every question
// is answered, a judged run reports phase "judging" with judged_done/judged_total,
// and the view follows it.
export default function RunningBatch({ op, onCancelRun }: Props) {
  const judging = op?.metadata?.phase === "judging";
  const done = numberMeta(op, judging ? "judged_done" : "questions_done");
  const total = numberMeta(op, judging ? "judged_total" : "questions_total");
  const status = op ? statusOf(op) : "running";
  const noun = judging ? "answer" : "question";

  return (
    <section className="answer-flow">
      <div className="answer-flow__head">
        <h2 className="p-heading--4 u-no-margin--bottom">
          {judging ? "Judging answers" : "Answering questions"}
        </h2>
        <button
          type="button"
          className="p-button--base u-no-margin--bottom"
          onClick={onCancelRun}
        >
          Cancel run
        </button>
      </div>

      {typeof op?.metadata?.judge_unavailable === "string" && op.metadata.judge_unavailable !== "" && (
        <div className="p-notification--information">
          <div className="p-notification__content">
            <h3 className="p-notification__title">Answers will not be judged</h3>
            <p className="p-notification__message">
              {op.metadata.judge_unavailable} To stop this notice, run{" "}
              <code>sudo rag-cli.rag set judge.enabled=false</code>.
            </p>
          </div>
        </div>
      )}

      <div className="answer-running" aria-live="polite">
        {total !== null && total > 0 ? (
          <>
            <p className="u-no-margin--bottom">
              {judging ? "Judged" : "Answered"} {done ?? 0} of {total} {noun}
              {total === 1 ? "" : "s"}…
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
          <Spinner label={`${judging ? "Judging answers" : "Answering questions"}… (${status})`} />
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
