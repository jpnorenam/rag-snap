// URL state for the Search page: a shared or reloaded /search/?… URL reproduces
// the same search. q is the query, b the knowledge bases, k the result count,
// and g the kapa.ai source-group ids (each list as repeated params).

// TOP_K_OPTIONS are the selectable result budgets. 10 matches `k search --top`;
// 15 (the chat REPL's retrieval default) stays available as an option.
export const TOP_K_OPTIONS = [5, 10, 15, 25];
export const DEFAULT_K = 10;

export interface SearchScope {
  q: string;
  bases: string[];
  k: number;
  kapaGroups: string[];
}

// parseK resolves a `k` URL param to a sanctioned option, falling back to the
// default on anything unknown or invalid.
export function parseK(raw: string | null): number {
  const k = Number(raw);
  return TOP_K_OPTIONS.includes(k) ? k : DEFAULT_K;
}

// searchQuery renders a scope as the page's query string (without "?").
export function searchQuery(scope: SearchScope): string {
  const url = new URLSearchParams();
  url.set("q", scope.q);
  for (const b of scope.bases) url.append("b", b);
  url.set("k", String(scope.k));
  for (const g of scope.kapaGroups) url.append("g", g);
  return url.toString();
}

// parseSearchQuery reads a scope back from the page's URL params.
export function parseSearchQuery(params: Pick<URLSearchParams, "get" | "getAll">): SearchScope {
  return {
    q: params.get("q")?.trim() ?? "",
    bases: params.getAll("b"),
    k: parseK(params.get("k")),
    kapaGroups: params.getAll("g").filter((g) => g !== ""),
  };
}
