"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  cancelOperation,
  connectOperationEvents,
  getOperation,
  isTerminal,
  listOperations,
  type OperationView,
} from "@/lib/api/operations";
import { OperationsContext, type OperationsContextValue } from "@/lib/useOperations";

// Reconnect backoff bounds and the polling cadence used while the socket is down.
const BACKOFF_MIN_MS = 1000;
const BACKOFF_MAX_MS = 30000;
const POLL_INTERVAL_MS = 4000;

// How often to sweep for running operations that have gone quiet, and how long
// silence must last before one is considered stale. Both are generous: the
// sweep is a safety net behind the events socket, not a second polling loop.
const SWEEP_INTERVAL_MS = 5000;
const STALE_AFTER_MS = 20000;

// byCreatedDesc orders operations newest first (created_at is an RFC3339 string,
// so lexical compare matches chronological order).
function byCreatedDesc(a: OperationView, b: OperationView): number {
  return b.created_at.localeCompare(a.created_at);
}

// OperationsProvider owns the single app-wide operations tracker: it seeds from
// GET /1.0/operations, subscribes to the operation events websocket (reconnect
// with capped backoff, re-seeding on every connect), and silently falls back to
// polling running operations while the socket is unavailable.
export default function OperationsProvider({ children }: { children: React.ReactNode }) {
  const [operations, setOperations] = useState<OperationView[]>([]);
  const [seen, setSeen] = useState(false);

  // Refs read inside async callbacks/timers (see foundation §4).
  const opsRef = useRef<OperationView[]>([]);
  const dismissedRef = useRef<Set<string>>(new Set());
  const wsRef = useRef<WebSocket | null>(null);
  const backoffRef = useRef<number>(BACKOFF_MIN_MS);
  const reconnectRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const mountedRef = useRef(true);

  useEffect(() => {
    opsRef.current = operations;
  }, [operations]);

  // upsert inserts or updates a single operation, keeping the list newest-first
  // and honoring locally dismissed rows.
  const upsert = useCallback((op: OperationView) => {
    if (dismissedRef.current.has(op.id)) return;
    setSeen(true);
    setOperations((prev) => {
      const idx = prev.findIndex((o) => o.id === op.id);
      const next = idx === -1 ? [op, ...prev] : prev.map((o) => (o.id === op.id ? op : o));
      return next.sort(byCreatedDesc);
    });
  }, []);

  // seed (re)loads the authoritative list so a reload — or a reconnect gap —
  // does not lose work that is still running. The daemon never reaps its
  // operations registry, so the list also carries every operation completed
  // since ragd started: adopting those would light the indicator and fill the
  // panel with history the user never started. Only running operations are
  // adopted; already-known ids are still refreshed, so a row that reached a
  // terminal state while we were disconnected gets its final view.
  // Silent on failure — the screen owns connection errors.
  const seed = useCallback(async () => {
    try {
      const ops = await listOperations();
      if (!mountedRef.current) return;
      const adoptable = (op: OperationView) =>
        !dismissedRef.current.has(op.id) && !isTerminal(op);
      if (ops.some(adoptable)) setSeen(true);
      setOperations((prev) => {
        const known = new Map(prev.map((o) => [o.id, o]));
        for (const op of ops) {
          if (dismissedRef.current.has(op.id)) continue;
          // Adopt running work; refresh rows we already track (they may have
          // finished while we were away). Never import unrelated history.
          if (known.has(op.id) || !isTerminal(op)) known.set(op.id, op);
        }
        return Array.from(known.values()).sort(byCreatedDesc);
      });
    } catch {
      // Silent: websocket/API degradation must not surface an error banner.
    }
  }, []);

  const stopPolling = useCallback(() => {
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
  }, []);

  // startPolling refreshes running operations every few seconds while the
  // socket is down. Degradation is silent.
  const startPolling = useCallback(() => {
    if (pollRef.current) return;
    pollRef.current = setInterval(() => {
      const running = opsRef.current.filter((o) => !isTerminal(o));
      running.forEach((op) => {
        getOperation(op.id)
          .then((fresh) => mountedRef.current && upsert(fresh))
          .catch(() => {
            // Silent: the operation may have been reaped; leave the last view.
          });
      });
    }, POLL_INTERVAL_MS);
  }, [upsert]);

  // connect opens the events socket; on close it schedules a backoff reconnect
  // and starts polling; on (re)open it re-seeds and stops polling.
  const connect = useCallback(() => {
    const ws = connectOperationEvents({
      onOperation: upsert,
      onOpen: () => {
        backoffRef.current = BACKOFF_MIN_MS;
        stopPolling();
        void seed();
      },
      onClose: () => {
        if (wsRef.current === ws) wsRef.current = null;
        if (!mountedRef.current) return;
        startPolling();
        const delay = backoffRef.current;
        backoffRef.current = Math.min(delay * 2, BACKOFF_MAX_MS);
        reconnectRef.current = setTimeout(connect, delay);
      },
      onError: () => {
        // onClose fires next and drives the reconnect/poll fallback.
      },
    });
    wsRef.current = ws;
  }, [upsert, seed, startPolling, stopPolling]);

  // Lifecycle: seed immediately, then open the socket; tear everything down on
  // unmount.
  useEffect(() => {
    mountedRef.current = true;
    void seed();
    connect();
    return () => {
      mountedRef.current = false;
      if (reconnectRef.current) clearTimeout(reconnectRef.current);
      stopPolling();
      wsRef.current?.close();
      wsRef.current = null;
    };
  }, [seed, connect, stopPolling]);

  // Safety net for the healthy-socket case. The daemon's events hub is
  // best-effort: it drops an event for any subscriber whose buffer is full
  // rather than blocking the publisher, so a terminal event can be lost while
  // the socket is perfectly fine — leaving a row stuck on "running" forever.
  // Re-fetch any running operation that has gone quiet; the polling fallback
  // already covers the socket-down case.
  useEffect(() => {
    const timer = setInterval(() => {
      if (wsRef.current?.readyState !== WebSocket.OPEN) return;
      const now = Date.now();
      opsRef.current
        .filter((op) => !isTerminal(op) && now - new Date(op.updated_at).getTime() > STALE_AFTER_MS)
        .forEach((op) => {
          getOperation(op.id)
            .then((fresh) => mountedRef.current && upsert(fresh))
            .catch(() => {
              // Silent, as everywhere else in the tracker.
            });
        });
    }, SWEEP_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [upsert]);

  const track = useCallback((op: OperationView) => upsert(op), [upsert]);

  const cancel = useCallback(
    async (id: string) => {
      const op = await cancelOperation(id);
      upsert(op);
    },
    [upsert]
  );

  const dismiss = useCallback((id: string) => {
    dismissedRef.current.add(id);
    setOperations((prev) => prev.filter((o) => o.id !== id));
  }, []);

  const running = useMemo(() => operations.filter((o) => !isTerminal(o)).length, [operations]);

  const value = useMemo<OperationsContextValue>(
    () => ({ operations, running, seen, track, cancel, dismiss }),
    [operations, running, seen, track, cancel, dismiss]
  );

  return <OperationsContext.Provider value={value}>{children}</OperationsContext.Provider>;
}
