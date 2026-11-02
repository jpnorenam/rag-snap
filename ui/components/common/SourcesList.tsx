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

// A citation group the model may emit: "[1]" and the list forms "[1, 2]",
// "[1;2]", with a tolerated "S" prefix ("[S1]") — mirroring the daemon's
// markerGroup. Leading spaces are included so a group removed entirely leaves
// no dangling space, and a group glued to a preceding word ("arr[1]") is not a
// citation.
const MARKER_GROUP = /([ \t]*)(?<!\w)\[\s*[Ss]?(\d{1,3})((?:\s*[,;]\s*[Ss]?\d{1,3})*)\s*\]/g;

// Fenced code blocks and inline code: their brackets are code, never markers.
const CODE_SPAN = /```[^]*?(?:```|$)|`[^`\n]*`/g;

// normalizeMarkers rewrites citation groups outside code to the "[1][2]" form
// the Markdown renderer turns into chips. When keep is given, markers outside
// it (ones the daemon did not accept as citations) are dropped.
export function normalizeMarkers(text: string, keep?: Set<number>): string {
  const prose = (seg: string) =>
    seg.replace(MARKER_GROUP, (_m, lead: string, first: string, rest: string) => {
      const nums = [first, ...(rest.match(/\d+/g) ?? [])]
        .map(Number)
        .filter((n) => !keep || keep.has(n));
      return nums.length > 0 ? lead + nums.map((n) => `[${n}]`).join("") : "";
    });
  let out = "";
  let last = 0;
  for (const m of text.matchAll(CODE_SPAN)) {
    out += prose(text.slice(last, m.index)) + m[0];
    last = m.index + m[0].length;
  }
  return out + prose(text.slice(last));
}
