"use client";

import { useCallback, useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import Header from "@/components/Header";
import KbList from "@/components/knowledge/KbList";
import KbDetail from "@/components/knowledge/KbDetail";
import EngineGate from "@/components/knowledge/EngineGate";
import { errorMessage } from "@/lib/api/envelope";
import { knowledgeInitialized } from "@/lib/api/server";

// Notice is a transient banner shown after a mutation or operation completes.
export interface Notice {
  kind: "positive" | "negative" | "caution";
  message: string;
  // Optional copyable snippet (used to surface engine-init model IDs).
  snippet?: string;
}

// NoticeBanner renders a Notice with the sanctioned notification markup.
export function NoticeBanner({ notice, onClose }: { notice: Notice; onClose: () => void }) {
  return (
    <div
      className={`p-notification--${notice.kind}`}
      role={notice.kind === "negative" ? "alert" : "status"}
    >
      <div className="p-notification__content">
        <p className="p-notification__message">{notice.message}</p>
        {notice.snippet && (
          <div className="p-code-snippet u-no-margin--bottom">
            <pre className="p-code-snippet__block">
              <code>{notice.snippet}</code>
            </pre>
          </div>
        )}
      </div>
      <button
        type="button"
        className="p-notification__close"
        aria-label="Close notification"
        onClick={onClose}
      >
        Close
      </button>
    </div>
  );
}

// KnowledgeScreen is the /knowledge/ section: it renders the KB list, or a KB's
// detail when ?kb=<name> is present (query-param routing for the static export).
// It also owns the engine-init gate and the shared notice banner.
export default function KnowledgeScreen() {
  const params = useSearchParams();
  const kb = params.get("kb");

  const [initialized, setInitialized] = useState<boolean | null>(null);
  const [notice, setNotice] = useState<Notice | null>(null);

  const notify = useCallback((n: Notice) => setNotice(n), []);

  const refreshEngine = useCallback(async () => {
    try {
      setInitialized(await knowledgeInitialized());
    } catch {
      // Treat an unknown engine state as initialized so the gate never blocks a
      // reachable-but-quirky daemon; real API errors surface on the list itself.
      setInitialized(true);
    }
  }, []);

  useEffect(() => {
    void refreshEngine();
  }, [refreshEngine]);

  return (
    <>
      <Header title="Knowledge bases" />
      <main className="app-main kb">
        {notice && <NoticeBanner notice={notice} onClose={() => setNotice(null)} />}

        {initialized === false && (
          <EngineGate
            notify={notify}
            onInitialized={() => void refreshEngine()}
          />
        )}

        {kb ? (
          <KbDetail name={kb} notify={notify} />
        ) : (
          <KbList notify={notify} onError={(e) => notify({ kind: "negative", message: errorMessage(e) })} />
        )}
      </main>
    </>
  );
}
