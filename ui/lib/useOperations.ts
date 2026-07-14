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
  // postAsync). Idempotent: an already-known id is updated in place.
  track: (op: OperationView) => void;
  // cancel issues DELETE /1.0/operations/{id}; rejects (throwing the ApiError)
  // when the daemon refuses, so callers can surface the message.
  cancel: (id: string) => Promise<void>;
  // dismiss removes a terminal row locally (cosmetic; the daemon list is truth).
  dismiss: (id: string) => void;
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
