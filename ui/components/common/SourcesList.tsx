"use client";

import type { CitedSource } from "@/lib/types";
import type { CitationMap } from "@/components/common/Markdown";

interface Props {
  sources: CitedSource[];
  /** Show each source's [n] marker (chat), or only its label (batch answers). */
  showMarkers?: boolean;
  /** Show the "Sources" heading; off where a surrounding control names it. */
  showLabel?: boolean;
  className?: string;
}

// SourcesList renders the documents an answer cited, in first-cited order: the
// label, linked when the source has a link, and the base it came from.
export default function SourcesList({ sources, showMarkers = true, showLabel = true, className }: Props) {
  if (sources.length === 0) return null;
  const classes = ["sources-list", className].filter(Boolean).join(" ");
  return (
    <div className={classes}>
      {showLabel && <span className="sources-list__label">Sources</span>}
      <ul className="sources-list__items">
        {sources.map((s) => (
          <li key={`${s.marker}-${s.key}`} className="sources-list__item">
            {showMarkers && <span className="sources-list__marker">[{s.marker}]</span>}
            {isHttpUrl(s.href) ? (
              <a href={s.href} target="_blank" rel="noopener noreferrer">
                {s.label}
              </a>
            ) : (
              <span>{s.label}</span>
            )}
            {s.base && <span className="u-text--muted p-text--small"> · {s.base}</span>}
          </li>
        ))}
      </ul>
    </div>
  );
}

// citationMap indexes sources by marker number for the Markdown renderer.
export function citationMap(sources: CitedSource[] | null | undefined): CitationMap {
  return new Map((sources ?? []).map((s) => [s.marker, s]));
}

function isHttpUrl(href: string | undefined): href is string {
  return !!href && /^https?:\/\//i.test(href);
}
