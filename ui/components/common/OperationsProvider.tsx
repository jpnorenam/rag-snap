"use client";

import { createContext, useCallback, useEffect, useRef, useState } from "react";
import { wsUrl } from "@/lib/api/envelope";
import {
  cancelOperation,
  getOperation,
  isRunning,
  isTerminal,
  listOperations,
  type OperationView,
} from "@/lib/api/operations";

// TrackedOperation is an operation view plus the local-only state the panel
// needs: a cancel request that the daemon refused (shown on the row).
export interface TrackedOperation extends OperationView {
  cancelError?: string;
}

export interface OperationsContextValue {
  // The session's operations, newest first.
  operations: TrackedOperation[];
  // How many of them are still running.
  runningCount: number;
  // Register an operation returned by postAsync. onDone, if given, fires once
  // when the operation reaches a terminal state (e.g. to refresh a list).
  track: (op: OperationView, onDone?: (op: OperationView) => void) => void;
  // Request cancellation, then let events/polling move the row to cancelled.
  cancel: (id: string) => Promise<void>;
  // Drop a finished operation from the panel.
  dismiss: (id: string) => void;
}

export const OperationsContext = createContext<OperationsContextValue | null>(null);

// How often the fallback poller runs, and how stale a running operation may get
// (no event, no update) before we re-fetch it even while the socket looks fine.
const POLL_INTERVAL_MS = 3000;
const STALE_AFTER_MS = 15000;

// Reconnect backoff for the events socket.
const BACKOFF_MIN_MS = 1000;
const BACKOFF_MAX_MS = 30000;

// OperationsProvider owns every long-running operation the session knows about.
// It is the only place that talks to /1.0/events and the operations endpoints:
// screens hand it an operation via track() and read state through useOperations,
// so no screen ever hand-rolls a polling loop.
//
// Updates arrive over the events websocket; if that socket drops we reconnect
// with backoff and poll the tracked running operations meanwhile. The
// degradation is silent by design — a missing events socket is not an error the
// user can act on, and screens surface real API failures themselves.
export default function OperationsProvider({ children }: { children: React.ReactNode }) {
  const [operations, setOperations] = useState<TrackedOperation[]>([]);

  // Mirrors of state read inside socket/timer callbacks.
  const operationsRef = useRef<TrackedOperation[]>([]);
  operationsRef.current = operations;

  // Latest view seen per operation id, including views for operations that are
  // not tracked yet: an operation can complete between postAsync resolving and
  // the screen calling track(), and this buffer lets track() catch up.
  const latestRef = useRef<Map<string, OperationView>>(new Map());
  // Which operations are in the panel. Kept outside state so that socket and
  // timer callbacks can test membership without a stale closure, and so state
  // updaters stay pure.
  const trackedIdsRef = useRef<Set<string>>(new Set());
  // Completion callbacks, cleared once fired.
  const doneHandlersRef = useRef<Map<string, (op: OperationView) => void>>(new Map());
  const connectedRef = useRef(false);

  // apply merges a fresh view into the tracked list, firing the completion
  // handler once, on the transition into a terminal state.
  const apply = useCallback((view: OperationView) => {
    const previous = latestRef.current.get(view.id);
    latestRef.current.set(view.id, view);
    if (!trackedIdsRef.current.has(view.id)) return; // Buffered for a later track().

    setOperations((prev) =>
      prev.map((o) => (o.id === view.id ? { ...view, cancelError: o.cancelError } : o))
    );

    if (isTerminal(view) && (!previous || !isTerminal(previous))) {
      const onDone = doneHandlersRef.current.get(view.id);
      if (onDone) {
        doneHandlersRef.current.delete(view.id);
        onDone(view);
      }
    }
  }, []);

  const track = useCallback((op: OperationView, onDone?: (op: OperationView) => void) => {
    // A newer view may already have arrived over the socket: an operation can
    // finish before the screen that launched it gets a chance to track it.
    const latest = latestRef.current.get(op.id) ?? op;
    latestRef.current.set(op.id, latest);

    if (!trackedIdsRef.current.has(op.id)) {
      trackedIdsRef.current.add(op.id);
      setOperations((prev) => [latest, ...prev]);
    }

    if (!onDone) return;
    if (isTerminal(latest)) onDone(latest);
    else doneHandlersRef.current.set(op.id, onDone);
  }, []);

  const dismiss = useCallback((id: string) => {
    doneHandlersRef.current.delete(id);
    trackedIdsRef.current.delete(id);
    setOperations((prev) => prev.filter((o) => o.id !== id));
  }, []);

  const cancel = useCallback(async (id: string) => {
    try {
      await cancelOperation(id);
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      setOperations((prev) =>
        prev.map((o) => (o.id === id ? { ...o, cancelError: message } : o))
      );
    }
  }, []);

  // Seed from the daemon so a page reload does not lose operations that are
  // still running. Finished operations are left behind: they belong to whatever
  // page view started them, not to this one.
  useEffect(() => {
    listOperations()
      .then((ops) => {
        const seeded = ops.filter(
          (op) => isRunning(op) && !trackedIdsRef.current.has(op.id)
        );
        if (seeded.length === 0) return;
        seeded.forEach((op) => {
          latestRef.current.set(op.id, latestRef.current.get(op.id) ?? op);
          trackedIdsRef.current.add(op.id);
        });
        setOperations((prev) =>
          [...prev, ...seeded].sort((a, b) => b.created_at.localeCompare(a.created_at))
        );
      })
      .catch(() => {
        // The daemon being unreachable is the screen's story to tell, not ours.
      });
  }, []);

  // Events websocket: the primary source of progress and completion updates.
  useEffect(() => {
    let closed = false;
    let socket: WebSocket | null = null;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let backoff = BACKOFF_MIN_MS;

    const connect = () => {
      if (closed) return;
      let ws: WebSocket;
      try {
        ws = new WebSocket(wsUrl("/1.0/events?type=operation"));
      } catch {
        schedule();
        return;
      }
      socket = ws;

      ws.onopen = () => {
        connectedRef.current = true;
        backoff = BACKOFF_MIN_MS;
      };
      ws.onmessage = (ev) => {
        try {
          const event = JSON.parse(ev.data) as { type: string; metadata: OperationView };
          if (event.type === "operation" && event.metadata?.id) apply(event.metadata);
        } catch {
          // A malformed frame is not worth surfacing; polling covers us.
        }
      };
      ws.onclose = () => {
        connectedRef.current = false;
        if (socket === ws) socket = null;
        schedule();
      };
      ws.onerror = () => {
        connectedRef.current = false;
      };
    };

    const schedule = () => {
      if (closed || retry) return;
      retry = setTimeout(() => {
        retry = undefined;
        connect();
      }, backoff);
      backoff = Math.min(backoff * 2, BACKOFF_MAX_MS);
    };

    connect();
    return () => {
      closed = true;
      if (retry) clearTimeout(retry);
      socket?.close();
    };
  }, [apply]);

  // Fallback poller. While the socket is down it refreshes every running
  // operation; while it is up it only re-fetches operations that have gone
  // suspiciously quiet, which self-heals the events hub's best-effort delivery
  // (it drops messages for a slow consumer rather than blocking).
  useEffect(() => {
    const timer = setInterval(() => {
      const now = Date.now();
      const stale = operationsRef.current.filter((op) => {
        if (isTerminal(op)) return false;
        if (!connectedRef.current) return true;
        return now - new Date(op.updated_at).getTime() > STALE_AFTER_MS;
      });
      stale.forEach((op) => {
        getOperation(op.id)
          .then(apply)
          .catch(() => {
            // Unreachable daemon or a dropped operation: keep the row as-is.
          });
      });
    }, POLL_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [apply]);

  const runningCount = operations.filter(isRunning).length;

  return (
    <OperationsContext.Provider
      value={{ operations, runningCount, track, cancel, dismiss }}
    >
      {children}
    </OperationsContext.Provider>
  );
}
