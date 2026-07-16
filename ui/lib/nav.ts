// Shared navigation model: the single source of truth for the app's sections,
// their routes, and their labels. Both the Sidebar (which renders the rail) and
// the operations indicator (which labels a "go to this section" link) read from
// here so section names never drift between the two.

export type IconName = "chat" | "prompt" | "knowledge" | "search" | "rfp" | "status" | "docs";

export interface NavItem {
  id: string;
  label: string;
  icon: IconName;
  href: string;
  // enabled sections are links; the rest stay "Soon" placeholders until their
  // change lands.
  enabled?: boolean;
}

// Primary navigation, top → bottom (docs/ux/01-app-shell.md). Flip `enabled` on
// as each section's change ships.
export const NAV_ITEMS: NavItem[] = [
  { id: "chat", label: "Chat", icon: "chat", href: "/", enabled: true },
  { id: "knowledge", label: "Knowledge bases", icon: "knowledge", href: "/knowledge/", enabled: true },
  { id: "search", label: "Search", icon: "search", href: "/search/", enabled: true },
  { id: "answer", label: "Answer RFPs", icon: "rfp", href: "/answer/", enabled: true },
  { id: "prompts", label: "Prompts", icon: "prompt", href: "/prompts/", enabled: true },
];

// Status is a utility entry pinned to the bottom of the rail (above the toggle).
export const STATUS_ITEM: NavItem = {
  id: "status",
  label: "Status",
  icon: "status",
  href: "/status/",
  enabled: true,
};

// normalizePath strips a trailing slash (but keeps root "/") so paths compare
// equal regardless of the export's trailing-slash style. basePath ("/ui") is
// already excluded from usePathname() values.
export function normalizePath(path: string): string {
  if (path.length > 1 && path.endsWith("/")) return path.slice(0, -1);
  return path;
}

// sectionLabel resolves a route (as passed to track()) to its human section
// label, matching on the normalized path. Returns undefined for an unknown
// route so callers can fall back gracefully.
export function sectionLabel(href: string): string | undefined {
  const target = normalizePath(href);
  const all = [...NAV_ITEMS, STATUS_ITEM];
  return all.find((item) => normalizePath(item.href) === target)?.label;
}
