import type { SearchResult } from "./api/search";

// The search page shows matching hits and hides the rest as similar results.
// The daemon decides what matches (each hit's `matched` flag); this module only
// partitions and words it. The CLI twin is SplitByMatch / NoMatchesMessage /
// SimilarNotice in cmd/cli/basic/knowledge/search.go, and both are pinned to
// testdata/search_split_cases.json so they cannot diverge.

// isMatched reports a hit's match flag. A hit from a daemon that predates the
// flag counts as matching, which shows every hit, as before.
export function isMatched(hit: Pick<SearchResult, "matched">): boolean {
  return hit.matched !== false;
}

// splitResults partitions hits into matched and similar results, preserving
// their order (score descending, as the daemon returns them).
export function splitResults<T extends Pick<SearchResult, "matched">>(
  hits: T[]
): { matched: T[]; similar: T[] } {
  const matched: T[] = [];
  const similar: T[] = [];
  for (const h of hits) (isMatched(h) ? matched : similar).push(h);
  return { matched, similar };
}

// noMatchesMessage is shown when no hit matches. Mirrors the Go %q quoting for
// the plain queries users type.
export function noMatchesMessage(query: string): string {
  return `No results match ${JSON.stringify(query)}.`;
}

// similarNotice states how many similar results are hidden.
export function similarNotice(n: number): string {
  return n === 1
    ? "1 similar result doesn't match the query."
    : `${n} similar results don't match the query.`;
}
