"use client";

import OperationsIndicator from "@/components/common/OperationsIndicator";

interface Props {
  title: string;
  children?: React.ReactNode;
}

// Header is the slim top bar for the content area. Branding and the dark-mode
// toggle live in the sidebar; this shows the active section's title and a status
// slot holding any screen-specific controls (e.g. the chat connection state)
// alongside the app-wide operations indicator.
export default function Header({ title, children }: Props) {
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
