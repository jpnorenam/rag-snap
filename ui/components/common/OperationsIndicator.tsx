"use client";

import { useCallback, useEffect, useId, useRef, useState } from "react";
import ConfirmModal from "@/components/common/ConfirmModal";
import type { TrackedOperation } from "@/components/common/OperationsProvider";
import { useOperations } from "@/lib/useOperations";
import {
  isCancelled,
  isFailed,
  isRunning,
  isSucceeded,
  progressPercent,
} from "@/lib/api/operations";
import { absoluteTime, relativeTime } from "@/lib/time";

// OperationsIndicator is the app-wide view of background work: a compact button
// in the header showing how many operations are running, which opens a panel
// listing this session's operations. It is the only completion feedback in the
// app — there is no toast system — so it lives in the header of every screen.
export default function OperationsIndicator() {
  const { operations, runningCount, cancel, dismiss } = useOperations();
  const [open, setOpen] = useState(false);
  const [confirming, setConfirming] = useState<TrackedOperation | null>(null);
  const panelId = useId();
  const rootRef = useRef<HTMLDivElement>(null);

  // Close on Escape and on a click outside the panel. The confirm modal owns
  // Escape while it is open, so leave it alone.
  useEffect(() => {
    if (!open) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !confirming) setOpen(false);
    };
    const onPointerDown = (e: MouseEvent) => {
      if (confirming) return;
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("keydown", onKeyDown);
    document.addEventListener("mousedown", onPointerDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      document.removeEventListener("mousedown", onPointerDown);
    };
  }, [open, confirming]);

  const confirmCancel = useCallback(async () => {
    const op = confirming;
    setConfirming(null);
    if (op) await cancel(op.id);
  }, [confirming, cancel]);

  // Nothing has happened this session: show nothing.
  if (operations.length === 0) return null;

  return (
    <div className="app-ops" ref={rootRef}>
      <button
        type="button"
        className="app-ops__toggle p-button--base u-no-margin--bottom"
        aria-expanded={open}
        aria-controls={panelId}
        aria-label={
          runningCount > 0
            ? `${runningCount} ${runningCount === 1 ? "operation" : "operations"} running`
            : "Operations"
        }
        onClick={() => setOpen((o) => !o)}
      >
        {runningCount > 0 ? (
          <i className="p-icon--spinner u-animation--spin" aria-hidden="true" />
        ) : (
          <ActivityIcon />
        )}
        <span aria-hidden="true">{runningCount > 0 ? runningCount : operations.length}</span>
      </button>

      {open && (
        <div className="app-ops-panel" id={panelId}>
          <h2 className="app-ops-panel__title">Operations</h2>
          <ul className="app-ops-panel__list" aria-live="polite">
            {operations.map((op) => (
              <OperationRow
                key={op.id}
                op={op}
                onCancel={() => setConfirming(op)}
                onDismiss={() => dismiss(op.id)}
              />
            ))}
          </ul>
        </div>
      )}

      {confirming && (
        <ConfirmModal
          title="Cancel operation?"
          confirmLabel="Cancel operation"
          onConfirm={() => void confirmCancel()}
          onClose={() => setConfirming(null)}
        >
          <p>
            Stops “{confirming.description}” before it finishes. Work already done is not
            rolled back.
          </p>
        </ConfirmModal>
      )}
    </div>
  );
}

// OperationRow renders one operation: state dot, what it is doing, when it
// started, and either a cancel action (while it can still be cancelled) or a
// dismiss control once it is over.
function OperationRow({
  op,
  onCancel,
  onDismiss,
}: {
  op: TrackedOperation;
  onCancel: () => void;
  onDismiss: () => void;
}) {
  const running = isRunning(op);
  const percent = running ? progressPercent(op) : null;
  const classes = ["app-ops-row", running ? "" : "app-ops-row--finished"]
    .filter(Boolean)
    .join(" ");

  return (
    <li className={classes}>
      <div className="app-ops-row__main">
        <span className={["app-status-dot", statusModifier(op)].join(" ")} />
        <div className="app-ops-row__body">
          <span className="app-ops-row__description">{op.description}</span>
          <span className="app-ops-row__meta u-text--muted p-text--small">
            <span title={absoluteTime(op.created_at)}>{relativeTime(op.created_at)}</span>
            {" · "}
            {statusLabel(op)}
          </span>
        </div>
        {running && op.may_cancel ? (
          <button
            type="button"
            className="p-button--base u-no-margin--bottom"
            onClick={onCancel}
          >
            Cancel
          </button>
        ) : (
          !running && (
            <button
              type="button"
              className="p-button--base u-no-margin--bottom"
              aria-label={`Dismiss ${op.description}`}
              onClick={onDismiss}
            >
              ×
            </button>
          )
        )}
      </div>

      {op.err && isFailed(op) && <p className="app-ops-row__error p-text--small">{op.err}</p>}
      {op.cancelError && <p className="app-ops-row__error p-text--small">{op.cancelError}</p>}

      {percent !== null && (
        <div className="app-ops-row__progress">
          <div className="app-ops-row__progress-bar" style={{ width: `${percent}%` }} />
        </div>
      )}
    </li>
  );
}

// statusModifier maps the operation's state to the shared status-dot variant.
function statusModifier(op: TrackedOperation): string {
  if (isSucceeded(op)) return "is-succeeded";
  if (isFailed(op)) return "is-failed";
  if (isCancelled(op)) return "is-cancelled";
  return "is-running";
}

// statusLabel names the state in words — a cancelled operation must not read as
// a failed one.
function statusLabel(op: TrackedOperation): string {
  if (isSucceeded(op)) return "Succeeded";
  if (isFailed(op)) return "Failed";
  if (isCancelled(op)) return "Cancelled";
  return op.status;
}

// ActivityIcon is the resting (nothing running) indicator glyph.
function ActivityIcon() {
  return (
    <svg
      width={16}
      height={16}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M3 12h4l3 8 4-16 3 8h4" />
    </svg>
  );
}
