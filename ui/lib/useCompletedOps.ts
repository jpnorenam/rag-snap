"use client";

import { useEffect, useRef } from "react";
import { isTerminal, type OperationView } from "@/lib/api/operations";
import { useOperations } from "@/lib/useOperations";

// useCompletedOps invokes onComplete exactly once for each operation the moment
// it first reaches a terminal state. Screens use it to refresh their data (and
// show a notice) when a tracked ingest/export/import/init finishes, without
// hand-rolling polling. onComplete should be memoized (useCallback).
export function useCompletedOps(onComplete: (op: OperationView) => void): void {
  const { operations } = useOperations();
  const seen = useRef<Set<string>>(new Set());

  useEffect(() => {
    for (const op of operations) {
      if (isTerminal(op) && !seen.current.has(op.id)) {
        seen.current.add(op.id);
        onComplete(op);
      }
    }
  }, [operations, onComplete]);
}
