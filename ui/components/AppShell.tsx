"use client";

import { useEffect, useState } from "react";
import Sidebar from "@/components/Sidebar";
import OperationsProvider from "@/components/common/OperationsProvider";
import { useDarkMode } from "@/lib/useDarkMode";
import { captureTokenFromUrl } from "@/lib/api/token";
import { getServerInfo } from "@/lib/api/server";
import { formatBuild } from "@/lib/buildInfo";

// AppShell is the persistent application chrome rendered by the root layout so
// it survives client-side route changes: the dark navigation rail, the
// app-wide operations tracker, and dark-mode state. Screens render only their
// <Header> + <main>; the shell wraps them. (Design D1.)
export default function AppShell({ children }: { children: React.ReactNode }) {
  const [darkMode, toggleDark] = useDarkMode();
  // The running build ("v0.0.9 · cd3ac8925556") for the sidebar footer; empty
  // until GET /1.0 answers, and left empty if it fails (the footer just omits it).
  const [build, setBuild] = useState("");

  // Capture a fragment token (if the launch flow used one) before any API call,
  // then read the build once: it can't change without restarting ragd.
  useEffect(() => {
    captureTokenFromUrl();
    getServerInfo()
      .then((info) => setBuild(formatBuild(info.version, info.commit)))
      .catch(() => setBuild(""));
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
