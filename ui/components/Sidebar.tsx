"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { NAV_ITEMS, STATUS_ITEM, normalizePath, type IconName, type NavItem } from "@/lib/nav";

// Sidebar is the Canonical-style dark navigation rail. Sections that have
// shipped render as real next/link routes with an active state; sections still
// to land render as non-focusable placeholders with a "Soon" badge (foundation
// §9: non-navigable items are never links or buttons). The dark-mode toggle
// lives in the footer, and Status is pinned just above it. The nav model
// (NAV_ITEMS/STATUS_ITEM) lives in @/lib/nav so the operations indicator can
// resolve a section's label from the same source.

// External documentation for the snap, opened in a new tab from the footer
// (mirroring lxd-ui's bottom-of-rail Documentation link).
const DOCS_URL = "https://github.com/jpnorenam/rag-snap/tree/main/docs";

interface Props {
  darkMode: boolean;
  onToggleDark: () => void;
}

export default function Sidebar({ darkMode, onToggleDark }: Props) {
  const pathname = usePathname();
  const current = normalizePath(pathname ?? "/");

  return (
    <nav className="app-sidebar" aria-label="Main">
      <div className="app-sidebar__brand">
        <span className="app-sidebar__logo">
          <svg width="24" height="24" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"
            />
          </svg>
        </span>
        <span className="app-sidebar__title">RAG</span>
      </div>

      <ul className="app-sidebar__nav">
        {NAV_ITEMS.map((item) => (
          <li key={item.id}>
            <NavEntry item={item} current={current} />
          </li>
        ))}
      </ul>

      <div className="app-sidebar__footer">
        <a
          href={DOCS_URL}
          className="app-sidebar__item"
          target="_blank"
          rel="noreferrer noopener"
          aria-label="Documentation (opens in a new tab)"
          title="Documentation"
        >
          <NavIcon name="docs" />
          <span className="app-sidebar__label">Documentation</span>
          <ExternalIcon />
        </a>
        <NavEntry item={STATUS_ITEM} current={current} />
        <button
          type="button"
          onClick={onToggleDark}
          className="app-sidebar__item app-sidebar__toggle"
          aria-label="Toggle dark mode"
        >
          {darkMode ? (
            <svg width="20" height="20" fill="currentColor" viewBox="0 0 20 20" aria-hidden="true">
              <path
                fillRule="evenodd"
                d="M10 2a1 1 0 011 1v1a1 1 0 11-2 0V3a1 1 0 011-1zm4 8a4 4 0 11-8 0 4 4 0 018 0zm-.464 4.95l.707.707a1 1 0 001.414-1.414l-.707-.707a1 1 0 00-1.414 1.414zm2.12-10.607a1 1 0 010 1.414l-.706.707a1 1 0 11-1.414-1.414l.707-.707a1 1 0 011.414 0zM17 11a1 1 0 100-2h-1a1 1 0 100 2h1zm-7 4a1 1 0 011 1v1a1 1 0 11-2 0v-1a1 1 0 011-1zM5.05 6.464A1 1 0 106.465 5.05l-.708-.707a1 1 0 00-1.414 1.414l.707.707zm1.414 8.486l-.707.707a1 1 0 01-1.414-1.414l.707-.707a1 1 0 011.414 1.414zM4 11a1 1 0 100-2H3a1 1 0 000 2h1z"
                clipRule="evenodd"
              />
            </svg>
          ) : (
            <svg width="20" height="20" fill="currentColor" viewBox="0 0 20 20" aria-hidden="true">
              <path d="M17.293 13.293A8 8 0 016.707 2.707a8.001 8.001 0 1010.586 10.586z" />
            </svg>
          )}
          <span className="app-sidebar__label">{darkMode ? "Light mode" : "Dark mode"}</span>
        </button>
      </div>
    </nav>
  );
}

// NavEntry renders a nav item as a link (shipped section) or a non-focusable
// placeholder span (coming-soon section).
function NavEntry({ item, current }: { item: NavItem; current: string }) {
  if (!item.enabled) {
    return (
      <span
        className={["app-sidebar__item", "app-sidebar__item--soon"].join(" ")}
        aria-disabled="true"
        // Names the section: in the collapsed 620px rail the label is hidden and
        // the tooltip is all the user has to go on.
        title={`${item.label} — coming soon`}
      >
        <NavIcon name={item.icon} />
        <span className="app-sidebar__label">{item.label}</span>
        <span className="app-sidebar__soon">Soon</span>
      </span>
    );
  }

  const active = normalizePath(item.href) === current;
  const classes = ["app-sidebar__item", active ? "is-active" : ""].filter(Boolean).join(" ");
  return (
    <Link
      href={item.href}
      className={classes}
      aria-current={active ? "page" : undefined}
      title={item.label}
    >
      <NavIcon name={item.icon} />
      <span className="app-sidebar__label">{item.label}</span>
    </Link>
  );
}

// NavIcon renders a small line icon for each navigation entry.
function NavIcon({ name }: { name: IconName }) {
  const common = {
    width: 20,
    height: 20,
    viewBox: "0 0 24 24",
    fill: "none",
    stroke: "currentColor",
    strokeWidth: 2,
    strokeLinecap: "round" as const,
    strokeLinejoin: "round" as const,
    "aria-hidden": true,
    className: "app-sidebar__icon",
  };
  switch (name) {
    case "chat":
      return (
        <svg {...common}>
          <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
        </svg>
      );
    case "prompt":
      return (
        <svg {...common}>
          <path d="M4 7V4h16v3M9 20h6M12 4v16" />
        </svg>
      );
    case "knowledge":
      return (
        <svg {...common}>
          <path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20M4 19.5A2.5 2.5 0 0 0 6.5 22H20V2H6.5A2.5 2.5 0 0 0 4 4.5z" />
        </svg>
      );
    case "search":
      return (
        <svg {...common}>
          <circle cx="11" cy="11" r="7" />
          <path d="m21 21-4.3-4.3" />
        </svg>
      );
    case "rfp":
      return (
        <svg {...common}>
          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
          <path d="M14 2v6h6M9 15l2 2 4-4" />
        </svg>
      );
    case "status":
      return (
        <svg {...common}>
          <path d="M22 12h-4l-3 9L9 3l-3 9H2" />
        </svg>
      );
    case "docs":
      return (
        <svg {...common}>
          <path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20" />
          <path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z" />
        </svg>
      );
  }
}

// ExternalIcon is the small arrow-out-of-box glyph marking a link that opens off
// the app (in a new tab), pinned to the end of the row like the "Soon" badge.
function ExternalIcon() {
  return (
    <svg
      className="app-sidebar__external"
      width={14}
      height={14}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M15 3h6v6M10 14 21 3M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" />
    </svg>
  );
}
