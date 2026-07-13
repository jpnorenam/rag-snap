"use client";

import { useEffect } from "react";
import OperationsProvider from "@/components/common/OperationsProvider";
import Sidebar from "@/components/Sidebar";
import { captureTokenFromUrl } from "@/lib/api/token";
import { useDarkMode } from "@/lib/useDarkMode";

// AppShell is the frame every screen plugs into: the dark navigation rail plus
// the content column, with the operations tracker mounted once around them so a
// background operation stays visible across navigations. Pages render their own
// <Header> and <main> as children.
export default function AppShell({ children }: { children: React.ReactNode }) {
  const [darkMode, toggleDark] = useDarkMode();

  // Capture a fragment token (if the launch flow used one) before any API call,
  // including the operations tracker's first fetch.
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
