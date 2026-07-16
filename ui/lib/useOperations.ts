"use client";

import { createContext, useContext } from "react";
import type { OperationView } from "@/lib/api/operations";

// OperationsContextValue is the shared operations tracker consumed app-wide via
// useOperations(). It lives above every route (mounted in AppShell) so the
// events websocket and the operations list survive client-side navigation.
export interface OperationsContextValue {
  // The session's operations, newest first.
  operations: OperationView[];
  // Count of operations that have not reached a terminal state.
  running: number;
  // True once at least one operation has been observed this session (gates the
  // header indicator's visibility).
  seen: boolean;
  // track registers a newly started operation (called by screens after a
  // postAsync). Idempotent: an already-known id is updated in place. An optional
  // `route` records which section started the operation (e.g. "/answer/") so the
  // indicator can offer a link back to it; it is kept in a client-side side-map,
  // separate from the daemon-owned OperationView, so live updates never clobber
  // it.
  track: (op: OperationView, route?: string) => void;
  // routeOf returns the originating route recorded for an operation via track(),
  // or undefined when none is known (e.g. a CLI-started operation, or one
  // adopted from the daemon after a page reload). A row with no route stays
  // non-clickable (foundation §9: non-navigable items are never links).
  routeOf: (id: string) => string | undefined;
  // cancel issues DELETE /1.0/operations/{id}; rejects (throwing the ApiError)
  // when the daemon refuses, so callers can surface the message.
  cancel: (id: string) => Promise<void>;
  // dismiss removes a terminal row locally (cosmetic; the daemon list is truth).
  dismiss: (id: string) => void;
  // markConsumed records that a completed operation's results have been handled
  // elsewhere (exported to disk, or collaborated to the Web UI with an ACK) and
  // removes its row from the indicator: it no longer needs attention. Client-side
  // knowledge, kept in a side-map the daemon cannot overwrite.
  markConsumed: (id: string) => void;
  // markExited records that the user explicitly dismissed a completed batch's
  // review surface. Like markConsumed it removes the row and blocks auto-resume.
  markExited: (id: string) => void;
  // isConsumed / isExited report the side-map state so a screen can decide
  // whether to auto-resume an operation and how stern its exit warning should be.
  isConsumed: (id: string) => boolean;
  isExited: (id: string) => boolean;
}

export const OperationsContext = createContext<OperationsContextValue | null>(null);

// useOperations returns the shared operations tracker. Must be called within an
// OperationsProvider (mounted by AppShell).
export function useOperations(): OperationsContextValue {
  const ctx = useContext(OperationsContext);
  if (!ctx) {
    throw new Error("useOperations must be used within an OperationsProvider");
  }
  return ctx;
}
