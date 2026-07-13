"use client";

interface Props {
  title: string;
  children?: React.ReactNode;
}

// Header is the slim top bar for the content area. Branding and the dark-mode
// toggle now live in the sidebar; this shows the active view's title and any
// screen-specific controls (e.g. the connection status) passed as children.
export default function Header({ title, children }: Props) {
  return (
    <header className="app-topbar">
      <h1 className="app-topbar__title">{title}</h1>
      <div className="app-topbar__meta">{children}</div>
    </header>
  );
}
