import { postSync } from "./envelope";

// SearchResult is the API view of a single retrieval hit from
// POST /1.0/search, matching the daemon's searchResult.
export interface SearchResult {
  score: number;
  base: string;
  source_id: string;
  created_at: string;
  label: string;
  content: string;
  // matched is the daemon's verdict on whether the hit matches the query
  // (relevance score above its threshold). Absent from daemons that predate
  // it; splitResults treats a missing flag as matching.
  matched?: boolean;
}

// search runs hybrid (neural + lexical) retrieval over the named bases with
// the verbatim query — no LLM involved, parity with `k search`. The count is
// always sent explicitly so the page's default (10) applies rather than the
// endpoint's (15).
export async function search(
  query: string,
  bases: string[],
  count: number
): Promise<SearchResult[]> {
  const hits = await postSync<SearchResult[] | null>("/1.0/search", {
    query,
    bases,
    count,
  });
  return hits ?? [];
}
