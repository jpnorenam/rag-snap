"use client";

import { useCallback, useState } from "react";
import type { Notice } from "@/components/KnowledgeScreen";
import { errorMessage } from "@/lib/api/envelope";
import { statusOf, type OperationView } from "@/lib/api/operations";
import { initEngine, type EngineInitResult } from "@/lib/api/knowledge";
import { useOperations } from "@/lib/useOperations";
import { useCompletedOps } from "@/lib/useCompletedOps";

interface Props {
  notify: (n: Notice) => void;
  onInitialized: () => void;
}

// EngineGate is the caution banner shown when the knowledge engine is
// uninitialized. It runs POST /1.0/knowledge-engine as a tracked operation and,
// on success, surfaces the resolved model IDs in a copyable notice (parity with
// `k init`). It never blocks the rest of the page.
export default function EngineGate({ notify, onInitialized }: Props) {
  const { track } = useOperations();
  const [busy, setBusy] = useState(false);
  const [opId, setOpId] = useState<string | null>(null);

  const onComplete = useCallback(
    (op: OperationView) => {
      if (op.id !== opId) return;
      setBusy(false);
      if (statusOf(op) === "succeeded") {
        const meta = op.metadata as EngineInitResult;
        const embedding = meta.embedding_model_id ?? "";
        const rerank = meta.rerank_model_id ?? "";
        const snippet =
          embedding || rerank
            ? `knowledge.model.embedding = ${embedding}\nknowledge.model.rerank   = ${rerank}`
            : undefined;
        notify({
          kind: "positive",
          message: "Knowledge engine initialized. Embedding and rerank models are ready.",
          snippet,
        });
        onInitialized();
      } else {
        notify({ kind: "negative", message: op.err || "Engine initialization failed." });
      }
    },
    [opId, notify, onInitialized]
  );

  useCompletedOps(onComplete);

  const onInit = async () => {
    setBusy(true);
    try {
      const op = await initEngine();
      setOpId(op.id);
      track(op);
    } catch (e) {
      setBusy(false);
      notify({ kind: "negative", message: errorMessage(e) });
    }
  };

  return (
    <div className="p-notification--caution" role="status">
      <div className="p-notification__content">
        <p className="p-notification__message">
          The knowledge engine is not initialized. Chat and ingestion need embedding models and
          pipelines.
        </p>
        <button
          type="button"
          className="p-button--positive u-no-margin--bottom"
          onClick={() => void onInit()}
          disabled={busy}
        >
          {busy ? (
            <>
              <i className="p-icon--spinner u-animation--spin" aria-hidden="true" /> Initializing…
            </>
          ) : (
            "Initialize engine"
          )}
        </button>
      </div>
    </div>
  );
}
