"use client";

// Sidebar is the Canonical-style dark navigation rail. Only "Chat" is wired up
// today; the remaining entries are placeholders for features still to land and
// render as disabled items. The dark-mode toggle lives in the footer.

type IconName = "chat" | "prompt" | "knowledge" | "search" | "rfp";

interface NavItem {
  id: string;
  label: string;
  icon: IconName;
  active?: boolean;
  disabled?: boolean;
}

const NAV_ITEMS: NavItem[] = [
  { id: "chat", label: "Chat", icon: "chat", active: true },
  { id: "prompts", label: "Prompts", icon: "prompt", disabled: true },
  { id: "knowledge", label: "Knowledge Bases", icon: "knowledge", disabled: true },
  { id: "search", label: "Search", icon: "search", disabled: true },
  { id: "rfp", label: "Answer RFPs", icon: "rfp", disabled: true },
];

interface Props {
  darkMode: boolean;
  onToggleDark: () => void;
}

export default function Sidebar({ darkMode, onToggleDark }: Props) {
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
            <button
              type="button"
              className={`app-sidebar__item${item.active ? " is-active" : ""}`}
              aria-current={item.active ? "page" : undefined}
              disabled={item.disabled}
              title={item.disabled ? "Coming soon" : undefined}
            >
              <NavIcon name={item.icon} />
              <span className="app-sidebar__label">{item.label}</span>
              {item.disabled && <span className="app-sidebar__soon">Soon</span>}
            </button>
          </li>
        ))}
      </ul>

      <div className="app-sidebar__footer">
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
  }
}
