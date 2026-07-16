"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import EmptyState from "@/components/common/EmptyState";
import Spinner from "@/components/common/Spinner";
import { errorMessage } from "@/lib/api/envelope";
import { listKnowledge, type KnowledgeBase } from "@/lib/api/knowledge";
import { search, type SearchResult } from "@/lib/api/search";

// TOP_K_OPTIONS are the selectable result budgets. 10 matches `k search --top`;
// 15 (the chat REPL's retrieval default) stays available as an option.
const TOP_K_OPTIONS = [5, 10, 15, 25];
const DEFAULT_K = 10;

// parseK resolves a `k` URL param to a sanctioned option, falling back to the
// default on anything unknown or invalid.
function parseK(raw: string | null): number {
  const k = Number(raw);
  return TOP_K_OPTIONS.includes(k) ? k : DEFAULT_K;
}

// defaultSelection picks the initial base scope (design Decision 3): exactly
// one base → it; a base named `default` exists → only it (mirrors
// `k search -b default`); otherwise all — the only choice that never produces
// an unsubmittable initial state.
function defaultSelection(bases: KnowledgeBase[]): string[] {
  if (bases.length === 1) return [bases[0].name];
  if (bases.some((b) => b.name === "default")) return ["default"];
  return bases.map((b) => b.name);
}

export default function SearchScreen() {
  const router = useRouter();
  const params = useSearchParams();

  const [query, setQuery] = useState("");
  const [bases, setBases] = useState<KnowledgeBase[] | null>(null);
  const [basesError, setBasesError] = useState<string | null>(null);
  const [selected, setSelected] = useState<string[]>([]);
  const [topK, setTopK] = useState(DEFAULT_K);
  const [searching, setSearching] = useState(false);
  // null = no search has completed (initial); [] = a search returned no hits.
  const [results, setResults] = useState<SearchResult[] | null>(null);
  const [searchError, setSearchError] = useState<string | null>(null);
  // The scope a completed search actually ran against, for the no-hits copy —
  // the live chip selection may have changed since.
  const [searchedBases, setSearchedBases] = useState<string[]>([]);

  const inputRef = useRef<HTMLInputElement>(null);
  // Guards the URL restore so strict-mode's double mount (and later param
  // updates from our own router.push) don't re-fire the auto-run.
  const restored = useRef(false);
  // Guards against double-submit while a request is in flight.
  const inFlight = useRef(false);

  const runSearch = useCallback(async (q: string, scope: string[], k: number) => {
    if (inFlight.current) return;
    inFlight.current = true;
    setSearching(true);
    setSearchError(null);
    setResults(null);
    setSearchedBases(scope);
    try {
      setResults(await search(q, scope, k));
    } catch (e) {
      setSearchError(errorMessage(e));
    } finally {
      inFlight.current = false;
      setSearching(false);
    }
  }, []);

  // loadBases fetches the chip list and resolves the initial selection. When
  // restoring from a URL, bases that no longer exist are dropped; if none
  // survive, the default scope applies instead.
  const loadBases = useCallback(
    async (restore?: { q: string; b: string[]; k: number }) => {
      setBasesError(null);
      try {
        const list = await listKnowledge();
        setBases(list);
        const fromUrl = restore
          ? restore.b.filter((name) => list.some((kb) => kb.name === name))
          : [];
        const scope = fromUrl.length > 0 ? fromUrl : defaultSelection(list);
        setSelected(scope);
        if (restore && restore.q && scope.length > 0) {
          void runSearch(restore.q, scope, restore.k);
        }
      } catch (e) {
        setBases(null);
        setBasesError(errorMessage(e));
      }
    },
    [runSearch]
  );

  // Restore query/scope from the URL once and auto-run the search, so a
  // shared or reloaded /search/?q=… URL reproduces its results.
  useEffect(() => {
    if (restored.current) return;
    restored.current = true;
    const q = params.get("q")?.trim() ?? "";
    const k = parseK(params.get("k"));
    setQuery(q);
    setTopK(k);
    void loadBases({ q, b: params.getAll("b"), k });
  }, [params, loadBases]);

  const toggleBase = useCallback((name: string) => {
    setSelected((prev) =>
      prev.includes(name) ? prev.filter((b) => b !== name) : [...prev, name]
    );
  }, []);

  const canSubmit =
    query.trim() !== "" && selected.length > 0 && !searching && (bases?.length ?? 0) > 0;

  function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!canSubmit) return;
    const q = query.trim();
    const url = new URLSearchParams();
    url.set("q", q);
    for (const b of selected) url.append("b", b);
    url.set("k", String(topK));
    // One history entry per executed search: the URL is the shareable record.
    router.push(`/search/?${url.toString()}`);
    // Focus stays in the query input after submit (foundation/AT contract),
    // including when the submit button was clicked.
    inputRef.current?.focus();
    void runSearch(q, selected, topK);
  }

  const noBases = bases !== null && bases.length === 0;
  const showInitial =
    !searching && !searchError && results === null && !basesError && !noBases;

  // Connection state for the "Knowledge bases:" label, mirroring the chat
  // screen's dot: a successful listKnowledge() means OpenSearch is reachable;
  // a failure (the daemon down, or the knowledge store unreachable) is shown as
  // "Unavailable" rather than a bare label.
  const kbState: "loading" | "connected" | "unavailable" = basesError
    ? "unavailable"
    : bases === null
      ? "loading"
      : "connected";

  return (
    <main className="app-main search">
      <form className="p-search-box search__bar" role="search" onSubmit={onSubmit}>
        <input
          ref={inputRef}
          type="search"
          className="p-search-box__input"
          aria-label="Search knowledge bases"
          placeholder="Search your knowledge bases"
          autoComplete="off"
          required
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <button
          type="reset"
          className="p-search-box__reset"
          onClick={() => {
            setQuery("");
            inputRef.current?.focus();
          }}
        >
          <i className="p-icon--close">Clear</i>
        </button>
        <button
          type="submit"
          className="p-search-box__button"
          disabled={searching || !query.trim() || selected.length === 0}
        >
          {searching ? (
            <i className="p-icon--spinner u-animation--spin">Searching</i>
          ) : (
            <i className="p-icon--search">Search</i>
          )}
        </button>
      </form>

      <div className="search__scope">
        <span className="search__scope-label" id="search-bases-label">
          <span
            className={`app-status-dot ${
              kbState === "connected" ? "is-connected" : kbState === "unavailable" ? "is-error" : ""
            }`}
          />
          {kbState === "connected"
            ? "Connected · Knowledge bases:"
            : kbState === "unavailable"
              ? "Unavailable · Knowledge bases:"
              : "Knowledge bases:"}
        </span>
        {bases === null && !basesError && <Spinner label="Loading knowledge bases…" />}
        {bases?.map((b) => (
          <button
            key={b.name}
            type="button"
            onClick={() => toggleBase(b.name)}
            className={`p-chip u-no-margin--bottom ${
              selected.includes(b.name) ? "p-chip--positive" : ""
            }`}
          >
            <span className="p-chip__value">{b.name}</span>
          </button>
        ))}
        {bases !== null && bases.length > 0 && selected.length === 0 && (
          <span className="p-text--small search__scope-hint">
            Select at least one knowledge base to search.
          </span>
        )}
        <label className="search__topk" htmlFor="search-topk">
          <span className="search__scope-label">Results</span>
          <select
            id="search-topk"
            value={topK}
            onChange={(e) => setTopK(Number(e.target.value))}
          >
            {TOP_K_OPTIONS.map((k) => (
              <option key={k} value={k}>
                {k}
              </option>
            ))}
          </select>
        </label>
      </div>

      {basesError && (
        <div className="p-notification--negative" role="alert">
          <div className="p-notification__content">
            <p className="p-notification__message">{basesError}</p>
            <button
              type="button"
              className="p-button u-no-margin--bottom"
              onClick={() => void loadBases()}
            >
              Retry
            </button>
          </div>
        </div>
      )}

      {noBases && (
        <div className="p-notification--caution">
          <div className="p-notification__content">
            <p className="p-notification__message">
              Create and ingest a knowledge base first — there is nothing to search yet. From
              the CLI: <code>rag-cli.rag k create &lt;name&gt;</code>
            </p>
          </div>
        </div>
      )}

      {searchError && (
        <div className="p-notification--negative" role="alert">
          <div className="p-notification__content">
            <p className="p-notification__message">{searchError}</p>
          </div>
        </div>
      )}

      <section className="search__results">
        <h2 className="u-off-screen">Results</h2>
        <p className="p-text--small u-text--muted search__count" aria-live="polite">
          {results !== null
            ? `${results.length} result${results.length === 1 ? "" : "s"}`
            : ""}
        </p>

        {searching && <Spinner label="Searching…" />}

        {showInitial && (
          <EmptyState
            headline="Search your knowledge bases."
            guidance="Hybrid semantic + lexical retrieval with reranking — this returns the matching chunks directly, no LLM involved."
            command={'rag-cli.rag k search "<query>"'}
          />
        )}

        {results !== null && results.length === 0 && (
          <div className="search__no-hits">
            <p className="u-no-margin--bottom">
              No matching chunks in <strong>{searchedBases.join(", ")}</strong>.
            </p>
            <p className="p-text--small u-text--muted">
              Try widening the base selection or raising the Results count.
            </p>
          </div>
        )}

        {results !== null && results.length > 0 && (
          <ol className="search__list">
            {results.map((r, i) => (
              <li key={`${r.base}-${r.source_id}-${i}`} className="search-result">
                <div className="search-result__header">
                  <span className="search-result__rank u-text--muted">{i + 1}</span>
                  <strong className="search-result__source">{r.source_id}</strong>
                  <span className="p-chip u-no-margin--bottom">
                    <span className="p-chip__value">{r.base}</span>
                  </span>
                  <span className="search-result__score p-text--small u-text--muted">
                    {r.score.toFixed(3)}
                  </span>
                </div>
                <p className="search-result__body">{r.content}</p>
                {/* Source ID stays plain text until the knowledge-detail route
                    lands (Change 2 flips it to a link). */}
                <p
                  className="search-result__footer p-text--small u-text--muted"
                  title={r.created_at}
                >
                  Source: {r.source_id} · {r.label}
                </p>
              </li>
            ))}
          </ol>
        )}
      </section>
    </main>
  );
}
