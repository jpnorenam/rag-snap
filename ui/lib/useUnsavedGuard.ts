"use client";

import { useEffect, useRef, useState } from "react";
import { usePathname, useRouter } from "next/navigation";

// useUnsavedGuard protects in-progress edits from being lost.
//
// Two exits have to be covered: leaving the page entirely (browser reload/close,
// guarded with beforeunload) and navigating within the SPA (a sidebar link, which
// never unloads the document, so beforeunload never fires). The latter is caught
// by intercepting clicks on internal links during the capture phase: the
// navigation is held, the target href is stashed, and the caller renders a
// confirm dialog. Confirming replays the navigation with the guard suppressed.
export function useUnsavedGuard(dirty: boolean) {
  const router = useRouter();
  const pathname = usePathname();
  // Pending internal navigation held back by the guard, or null when none. This
  // is the href as rendered (basePath included).
  const [pendingHref, setPendingHref] = useState<string | null>(null);

  // Read inside the (non-reactive) capture-phase listener.
  const dirtyRef = useRef(dirty);
  dirtyRef.current = dirty;

  // Set while replaying a confirmed navigation, so the listener lets it through.
  const bypassRef = useRef(false);

  useEffect(() => {
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      if (!dirtyRef.current) return;
      // The browser shows its own generic wording; preventDefault is what asks.
      e.preventDefault();
    };

    const onClick = (e: MouseEvent) => {
      if (!dirtyRef.current || bypassRef.current) return;
      // Let the browser handle modified clicks (new tab/window) as usual.
      if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) {
        return;
      }

      const anchor = (e.target as HTMLElement | null)?.closest("a");
      const href = anchor?.getAttribute("href");
      if (!anchor || !href || anchor.target === "_blank") return;
      // Only same-origin, in-app navigations are ours to guard.
      if (!href.startsWith("/") || href.startsWith("//")) return;

      e.preventDefault();
      setPendingHref(href);
    };

    window.addEventListener("beforeunload", onBeforeUnload);
    // Capture phase: run before next/link's own click handler navigates.
    document.addEventListener("click", onClick, true);
    return () => {
      window.removeEventListener("beforeunload", onBeforeUnload);
      document.removeEventListener("click", onClick, true);
    };
  }, []);

  // Discard the edits and go where the user was heading. The href carries the
  // basePath (that is what next/link rendered) but router.push prepends it
  // itself, so strip it back off first — otherwise /ui/status/ would be pushed
  // as /ui/ui/status/. The basePath is derived at runtime rather than hardcoded:
  // usePathname() excludes it, window.location.pathname includes it.
  const confirmNavigation = () => {
    const href = pendingHref;
    setPendingHref(null);
    if (!href) return;

    const full = window.location.pathname;
    const basePath = full.endsWith(pathname) ? full.slice(0, full.length - pathname.length) : "";
    const route = basePath && href.startsWith(basePath) ? href.slice(basePath.length) : href;

    bypassRef.current = true;
    router.push(route || "/");
  };

  const cancelNavigation = () => setPendingHref(null);

  return { pendingHref, confirmNavigation, cancelNavigation };
}
