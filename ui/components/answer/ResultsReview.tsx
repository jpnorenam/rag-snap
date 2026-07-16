"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import QACard from "@/components/answer/QACard";
import ConfirmModal from "@/components/common/ConfirmModal";
import type { ParsedQAFile } from "@/lib/types";
import { buildPayload, collaborateInWebUI, type FallbackReason } from "@/lib/handoff";

interface Props {
  parsed: ParsedQAFile;
  manifestName?: string;
  knowledgeBases?: string[];
  // resumable is true when these results came from a batch run (an operation
  // that lives in the indicator and can be lost). false for a results file
  // opened from disk — nothing to lose, so Exit just closes without a warning.
  resumable: boolean;
  // consumed is true once the results have been exported or collaborated; it
  // softens the Exit confirmation (the user has them elsewhere).
  consumed: boolean;
  // onConsumed marks the operation consumed (export succeeded, or collaborate
  // ACK/fallback-download). onExit dismisses the review permanently.
  onConsumed: () => void;
  onExit: () => void;
}

// JUMP_LIST_THRESHOLD: show the anchor index only for longer result sets
// (docs/ux/04-answer-batch.md).
const JUMP_LIST_THRESHOLD = 10;

// TOAST_DISMISS_MS: how long a success toast stays before auto-dismissing.
const TOAST_DISMISS_MS = 4000;

// A transient banner shown above the review head: a positive toast on a
// successful handoff, or a caution note when the handoff fell back to a
// download so the user always knows what happened (never a silent failure).
interface Notice {
  tone: "positive" | "caution";
  message: string;
}

// ResultsReview is the shared review surface for a completed run and for an
// opened results file. It renders the CLI's results verbatim and exports them
// unchanged (BatchOutput shape).
export default function ResultsReview({
  parsed,
  manifestName,
  knowledgeBases,
  resumable,
  consumed,
  onConsumed,
  onExit,
}: Props) {
  const [notice, setNotice] = useState<Notice | null>(null);
  const [handingOff, setHandingOff] = useState(false);
  const [confirmExit, setConfirmExit] = useState(false);
  // Canceller for an in-flight handoff, torn down on unmount.
  const cancelHandoffRef = useRef<(() => void) | null>(null);
  // Auto-dismiss timer for a positive toast.
  const toastTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const download = useCallback(() => {
    // Re-emit the CLI's BatchOutput shape verbatim (generated_at, model,
    // results) so the file round-trips with `answer batch` output.
    const payload = {
      generated_at: parsed.generated_at,
      model: parsed.model,
      results: parsed.items,
    };
    const blob = new Blob([JSON.stringify(payload, null, 2)], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "batch-results.json";
    a.click();
    URL.revokeObjectURL(url);
  }, [parsed]);

  const exportJSON = useCallback(() => {
    download();
    // The results now live on disk: mark the operation consumed so it drops out
    // of the indicator and Exit no longer needs the scary warning.
    onConsumed();
  }, [download, onConsumed]);

  // Collaborate: hand the results to the deployed Web UI over postMessage. On
  // either fallback (popup blocked or no READY within the timeout) we download
  // the file instead and say so via a caution notice — never a silent failure.
  const collaborate = useCallback(() => {
    if (handingOff) return;
    setNotice(null);
    setHandingOff(true);

    const payload = buildPayload(parsed, manifestName, knowledgeBases);
    cancelHandoffRef.current = collaborateInWebUI(payload, {
      onAck: () => {
        cancelHandoffRef.current = null;
        setHandingOff(false);
        // Successfully delivered to the Web UI: results are safe elsewhere.
        onConsumed();
        setNotice({ tone: "positive", message: "Opened in the Web UI." });
      },
      onFallback: (reason: FallbackReason) => {
        cancelHandoffRef.current = null;
        setHandingOff(false);
        download();
        // The fallback downloaded the file, so the results are saved: consumed.
        onConsumed();
        setNotice({
          tone: "caution",
          message:
            reason === "popup-blocked"
              ? "Couldn't open the Web UI (the popup was blocked). Downloaded the results file instead — you can import it in the Web UI."
              : "Couldn't reach the Web UI in time. Downloaded the results file instead — you can import it in the Web UI.",
        });
      },
    });
  }, [handingOff, parsed, manifestName, knowledgeBases, download, onConsumed]);

  // Auto-dismiss the positive toast; the caution notice stays until the next
  // action so the user has time to read the fallback explanation.
  useEffect(() => {
    if (toastTimerRef.current !== null) {
      clearTimeout(toastTimerRef.current);
      toastTimerRef.current = null;
    }
    if (notice?.tone === "positive") {
      toastTimerRef.current = setTimeout(() => setNotice(null), TOAST_DISMISS_MS);
    }
    return () => {
      if (toastTimerRef.current !== null) clearTimeout(toastTimerRef.current);
    };
  }, [notice]);

  // Tear down an in-flight handoff (listener + timer) if the surface unmounts.
  useEffect(() => () => cancelHandoffRef.current?.(), []);

  const generatedAt = parsed.generated_at ? new Date(parsed.generated_at) : null;
  const showJumpList = parsed.items.length > JUMP_LIST_THRESHOLD;

  return (
    <>
    <section className="answer-review">
      {notice && (
        <div
          className={`p-notification--${notice.tone}`}
          role={notice.tone === "caution" ? "alert" : "status"}
        >
          <div className="p-notification__content">
            <p className="p-notification__message">{notice.message}</p>
          </div>
        </div>
      )}

      <div className="answer-review__head">
        <div>
          <h2 className="p-heading--4 u-no-margin--bottom">{manifestName ?? "Batch results"}</h2>
          <p className="u-text--muted p-text--small u-no-margin--bottom">
            {parsed.items.length} question{parsed.items.length === 1 ? "" : "s"}
            {parsed.model ? ` · ${parsed.model}` : ""}
            {generatedAt && !Number.isNaN(generatedAt.getTime()) ? (
              <>
                {" · "}
                <span title={generatedAt.toLocaleString()}>{generatedAt.toLocaleDateString()}</span>
              </>
            ) : null}
          </p>
          {knowledgeBases && knowledgeBases.length > 0 && (
            <div className="answer-review__kbs">
              {knowledgeBases.map((kb) => (
                <span key={kb} className="p-chip">
                  <span className="p-chip__value">{kb}</span>
                </span>
              ))}
            </div>
          )}
        </div>
        <div className="answer-review__actions">
          <button
            type="button"
            className="p-button--base u-no-margin--bottom"
            // Resumable runs confirm before exiting (blunt if unconsumed, soft
            // if already saved). A results file opened from disk has nothing to
            // lose, so Exit closes it immediately.
            onClick={() => (resumable ? setConfirmExit(true) : onExit())}
          >
            Exit
          </button>
          <button
            type="button"
            className="p-button u-no-margin--bottom"
            onClick={collaborate}
            disabled={handingOff}
          >
            {handingOff ? (
              <>
                <i className="p-icon--spinner u-animation--spin" aria-hidden="true" /> Opening…
              </>
            ) : (
              "Collaborate in Web UI"
            )}
          </button>
          <button type="button" className="p-button u-no-margin--bottom" onClick={exportJSON}>
            Export JSON
          </button>
        </div>
      </div>

      {showJumpList && (
        <nav className="answer-review__jump" aria-label="Jump to question">
          {parsed.items.map((_, i) => (
            <a key={i} href={`#qa-${i + 1}`} className="answer-review__jump-link">
              {i + 1}
            </a>
          ))}
        </nav>
      )}

      <div className="answer-review__cards">
        {parsed.items.map((item, i) => (
          <QACard key={item.id ?? i} item={item} index={i} />
        ))}
      </div>
    </section>

    {confirmExit && (
      <ConfirmModal
        title="Exit results"
        confirmLabel="Exit"
        destructive={!consumed}
        onConfirm={() => {
          setConfirmExit(false);
          onExit();
        }}
        onClose={() => setConfirmExit(false)}
      >
        {consumed ? (
          <p>Exit these results? You&rsquo;ve already saved them, so you can leave safely.</p>
        ) : (
          <p>
            Exit these results? If you haven&rsquo;t exported or sent them to the Web UI, they will
            be permanently lost — there is no way to return to them.
          </p>
        )}
      </ConfirmModal>
    )}
    </>
  );
}
