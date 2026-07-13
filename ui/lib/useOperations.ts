"use client";

import { useContext } from "react";
import {
  OperationsContext,
  type OperationsContextValue,
} from "@/components/common/OperationsProvider";

// useOperations is how screens reach the session's operations tracker: call
// track(op) with the operation returned by postAsync and the header indicator
// takes it from there.
export function useOperations(): OperationsContextValue {
  const ctx = useContext(OperationsContext);
  if (!ctx) {
    throw new Error("useOperations must be used within an OperationsProvider");
  }
  return ctx;
}
