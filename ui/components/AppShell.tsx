"use client";

import { useEffect, useState } from "react";
import Sidebar from "@/components/Sidebar";
import OperationsProvider from "@/components/common/OperationsProvider";
import { useDarkMode } from "@/lib/useDarkMode";
import { captureTokenFromUrl } from "@/lib/api/token";
import { getServerInfo } from "@/lib/api/server";
import { formatBuild } from "@/lib/buildInfo";

// BUILD_RETRY_MS is the backoff for a failed build read; after the last one the
// footer stays empty until the tab is next shown.
const BUILD_RETRY_MS = [2000, 5000, 15000, 30000];

// BUILD_REREAD_MS is how stale the build may be before a returning tab re-reads it.
const BUILD_REREAD_MS = 5 * 60 * 1000;

// AppShell is the persistent application chrome rendered by the root layout so
// it survives client-side route changes: the dark navigation rail, the
// app-wide operations tracker, and dark-mode state. Screens render only their
// <Header> + <main>; the shell wraps them. (Design D1.)
export default function AppShell({ children }: { children: React.ReactNode }) {
  const [darkMode, toggleDark] = useDarkMode();
  // The running build ("v0.0.9 · cd3ac8925556") for the sidebar footer; empty
  // until GET /1.0 answers (the footer just omits it meanwhile).
  const [build, setBuild] = useState("");

  // Capture a fragment token (if the launch flow used one) before any API call,
  // then read the build. A failed read (ragd still starting, auth not yet
  // settled) is retried with backoff, and a tab that comes back after a while
  // re-reads it, so a snap refresh, which restarts ragd, shows up without a
  // reload. GET /1.0 is not free, so it is never polled.
  useEffect(() => {
    captureTokenFromUrl();
    let cancelled = false;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let attempt = 0;
    let lastRead = 0;
    const read = () => {
      clearTimeout(retry);
      getServerInfo()
        .then((info) => {
          if (cancelled) return;
          attempt = 0;
          lastRead = Date.now();
          setBuild(formatBuild(info.version, info.commit));
        })
        .catch(() => {
          if (cancelled || attempt >= BUILD_RETRY_MS.length) return;
          retry = setTimeout(read, BUILD_RETRY_MS[attempt++]);
        });
    };
    const onVisible = () => {
      if (document.visibilityState === "visible" && Date.now() - lastRead > BUILD_REREAD_MS) read();
    };
    read();
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      cancelled = true;
      clearTimeout(retry);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, []);

  return (
    <OperationsProvider>
      <div className="app-shell">
        <Sidebar darkMode={darkMode} onToggleDark={toggleDark} build={build} />
        <div className="app-content">{children}</div>
      </div>
    </OperationsProvider>
  );
}
