"use client";

import { useCallback, useEffect, useId, useRef, useState } from "react";
import { ApiError } from "@/lib/api/envelope";
import { isTerminal, statusOf, type OperationView } from "@/lib/api/operations";
import { useOperations } from "@/lib/useOperations";
import { relativeTime } from "@/lib/relativeTime";
import ConfirmModal from "./ConfirmModal";
import EmptyState from "./EmptyState";

// progressPercent derives a 0–100 progress value from paired `*_total`/`*_done`
// metadata fields the daemon reports (e.g. sources_total/sources_done). Returns
// null when the operation reports no such progress.
function progressPercent(metadata: Record<string, unknown>): number | null {
  for (const key of Object.keys(metadata)) {
    if (!key.endsWith("_total")) continue;
    const doneKey = `${key.slice(0, -"_total".length)}_done`;
    const total = Number(metadata[key]);
    const done = Number(metadata[doneKey]);
    if (Number.isFinite(total) && total > 0 && Number.isFinite(done)) {
      return Math.min(100, Math.max(0, Math.round((done / total) * 100)));
    }
  }
  return null;
}

// OperationsIndicator is the header's global operations affordance: a compact
// toggle showing the running count and an anchored panel listing the session's
// operations with live status, progress, dismiss, and cancel.
export default function OperationsIndicator() {
  const { operations, running, seen, cancel, dismiss } = useOperations();
  const [open, setOpen] = useState(false);
  const [confirmId, setConfirmId] = useState<string | null>(null);
  const [cancelBusy, setCancelBusy] = useState(false);
  const [cancelErrors, setCancelErrors] = useState<Record<string, string>>({});

  const panelId = useId();
  const containerRef = useRef<HTMLDivElement>(null);
  const toggleRef = useRef<HTMLButtonElement>(null);

  const close = useCallback(() => {
    const wasInside = containerRef.current?.contains(document.activeElement ?? null);
    setOpen(false);
    if (wasInside) toggleRef.current?.focus();
  }, []);

  // Close on Escape and outside click while open.
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") close();
    };
    const onDown = (e: MouseEvent) => {
      if (!containerRef.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("keydown", onKey);
    document.addEventListener("mousedown", onDown);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("mousedown", onDown);
    };
  }, [open, close]);

  // The indicator stays hidden until the session has observed an operation.
  if (!seen) return null;

  const confirmOp = confirmId ? operations.find((o) => o.id === confirmId) ?? null : null;

  async function doCancel(op: OperationView) {
    setCancelBusy(true);
    try {
      await cancel(op.id);
      setCancelErrors((prev) => {
        const next = { ...prev };
        delete next[op.id];
        return next;
      });
      setConfirmId(null);
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : String(e);
      setCancelErrors((prev) => ({ ...prev, [op.id]: msg }));
      setConfirmId(null);
    } finally {
      setCancelBusy(false);
    }
  }

  return (
    <div className="app-ops" ref={containerRef}>
      <button
        type="button"
        ref={toggleRef}
        className="app-ops__toggle p-button--base u-no-margin--bottom"
        aria-expanded={open}
        aria-controls={panelId}
        aria-label={
          running > 0
            ? `${running} ${running === 1 ? "operation" : "operations"} running`
            : "Operations"
        }
        onClick={() => (open ? close() : setOpen(true))}
      >
        <i
          className={running > 0 ? "p-icon--spinner u-animation--spin" : undefined}
          aria-hidden="true"
        >
          {running === 0 && (
            <svg
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth={2}
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="M22 12h-4l-3 9L9 3l-3 9H2" />
            </svg>
          )}
        </i>
        {running > 0 && <span className="app-ops__count">{running}</span>}
      </button>

      {open && (
        <div className="app-ops-panel" id={panelId} role="group" aria-label="Operations">
          <ul className="app-ops-panel__list" aria-live="polite">
            {operations.length === 0 && (
              <li className="app-ops-panel__empty">
                <EmptyState
                  headline="No operations yet"
                  guidance="Background work — ingest, batch answer, export — appears here as it runs. The CLI does the same work:"
                  command="rag-cli.rag k ingest <name> <source-id> --file <path>"
                />
              </li>
            )}
            {operations.map((op) => {
              const status = statusOf(op);
              const dotClass = {
                running: "is-running",
                succeeded: "is-succeeded",
                failed: "is-failed",
                cancelled: "is-cancelled",
              }[status];
              const pct = progressPercent(op.metadata);
              const cancellable = status === "running" && op.may_cancel;
              const cancelErr = cancelErrors[op.id];
              return (
                <li key={op.id} className={`app-ops-row app-ops-row--${status}`}>
                  <div className="app-ops-row__main">
                    <span className={`app-status-dot ${dotClass}`} aria-hidden="true" />
                    <span className="app-ops-row__desc">{op.description}</span>
                    <span
                      className="app-ops-row__time u-text--muted p-text--small"
                      title={new Date(op.created_at).toLocaleString()}
                    >
                      {relativeTime(op.created_at)}
                    </span>
                    <span className="app-ops-row__actions">
                      {cancellable ? (
                        <button
                          type="button"
                          className="p-button--base u-no-margin--bottom"
                          onClick={() => setConfirmId(op.id)}
                        >
                          Cancel
                        </button>
                      ) : isTerminal(op) ? (
                        <button
                          type="button"
                          className="p-button--base u-no-margin--bottom app-ops-row__dismiss"
                          aria-label="Dismiss"
                          onClick={() => dismiss(op.id)}
                        >
                          ×
                        </button>
                      ) : null}
                    </span>
                  </div>

                  {status === "cancelled" && (
                    <p className="app-ops-row__note u-text--muted p-text--small u-no-margin--bottom">
                      Cancelled
                    </p>
                  )}

                  {status === "failed" && op.err && (
                    <p className="app-ops-row__error p-text--small u-no-margin--bottom">{op.err}</p>
                  )}

                  {cancelErr && (
                    <p className="app-ops-row__error p-text--small u-no-margin--bottom">
                      Could not cancel: {cancelErr}
                    </p>
                  )}

                  {status === "running" && pct !== null && (
                    <div className="app-ops-row__progress" aria-hidden="true">
                      <div className="app-ops-row__progress-bar" style={{ width: `${pct}%` }} />
                    </div>
                  )}
                </li>
              );
            })}
          </ul>
        </div>
      )}

      {confirmOp && (
        <ConfirmModal
          title="Cancel operation"
          confirmLabel="Cancel operation"
          destructive
          busy={cancelBusy}
          onConfirm={() => void doCancel(confirmOp)}
          onClose={() => setConfirmId(null)}
        >
          <p>
            Stop <strong>{confirmOp.description}</strong>? Work already done is kept; the operation
            will not complete.
          </p>
        </ConfirmModal>
      )}
    </div>
  );
}
