import { postSyncWithWarnings } from "./envelope";

// SearchResult is the API view of a single retrieval hit from
// POST /1.0/search, matching the daemon's searchResult.
export interface SearchResult {
  score: number;
  base: string;
  source_id: string;
  created_at: string;
  label: string;
  content: string;
}

// SearchResponse is a search's hits plus any warnings, such as kapa.ai being
// requested but unavailable (the local hits are still returned).
export interface SearchResponse {
  results: SearchResult[];
  warnings: string[];
}

// toSearchResponse normalizes the daemon's response: null hits or warnings
// become empty lists.
export function toSearchResponse(
  hits: SearchResult[] | null | undefined,
  warnings: string[] | null | undefined
): SearchResponse {
  return { results: hits ?? [], warnings: warnings ?? [] };
}

// search runs hybrid (neural + lexical) retrieval over the named bases with
// the verbatim query — no LLM involved, parity with `k search`. The count is
// always sent explicitly so the page's default (10) applies rather than the
// endpoint's (15). kapaGroups adds kapa.ai, scoped to those group ids; it is
// sent only when non-empty (an empty list selects nothing). kapa.ai hits come
// after the local ones, with base "kapa.ai".
export async function search(
  query: string,
  bases: string[],
  count: number,
  kapaGroups: string[] = []
): Promise<SearchResponse> {
  const { metadata, warnings } = await postSyncWithWarnings<SearchResult[] | null>("/1.0/search", {
    query,
    bases,
    count,
    ...(kapaGroups.length > 0 ? { kapa_groups: kapaGroups } : {}),
  });
  return toSearchResponse(metadata, warnings);
}
