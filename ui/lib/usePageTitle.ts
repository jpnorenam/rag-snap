"use client";

import { useEffect } from "react";

// usePageTitle keeps document.title in step with the section the user is on.
// Static export has no server metadata for client-side navigations, so each
// page sets its own title alongside its <Header title>.
export function usePageTitle(section: string): void {
  useEffect(() => {
    document.title = `${section} — RAG`;
  }, [section]);
}
