"use client";

import { useEffect } from "react";
import Sidebar from "@/components/Sidebar";
import OperationsProvider from "@/components/common/OperationsProvider";
import { useDarkMode } from "@/lib/useDarkMode";
import { captureTokenFromUrl } from "@/lib/api/token";

// AppShell is the persistent application chrome rendered by the root layout so
// it survives client-side route changes: the dark navigation rail, the
// app-wide operations tracker, and dark-mode state. Screens render only their
// <Header> + <main>; the shell wraps them. (Design D1.)
export default function AppShell({ children }: { children: React.ReactNode }) {
  const [darkMode, toggleDark] = useDarkMode();

  // Capture a fragment token (if the launch flow used one) before any API call.
  useEffect(() => {
    captureTokenFromUrl();
  }, []);

  return (
    <OperationsProvider>
      <div className="app-shell">
        <Sidebar darkMode={darkMode} onToggleDark={toggleDark} />
        <div className="app-content">{children}</div>
      </div>
    </OperationsProvider>
  );
}
