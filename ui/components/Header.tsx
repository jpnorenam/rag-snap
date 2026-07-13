"use client";

import { useEffect } from "react";
import OperationsIndicator from "@/components/common/OperationsIndicator";

interface Props {
  title: string;
  children?: React.ReactNode;
}

// Header is the slim top bar for the content area. Branding and the dark-mode
// toggle live in the sidebar; this shows the active view's title, any
// screen-specific controls (e.g. the chat connection status) passed as
// children, and the global operations indicator. It also drives document.title
// from the section title so route changes update the browser tab (foundation §9).
export default function Header({ title, children }: Props) {
  useEffect(() => {
    document.title = `${title} — RAG`;
  }, [title]);

  return (
    <header className="app-topbar">
      <h1 className="app-topbar__title">{title}</h1>
      <div className="app-topbar__meta">
        {children}
        <OperationsIndicator />
      </div>
    </header>
  );
}
