"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import EmptyState from "@/components/common/EmptyState";
import KapaSourcePicker from "@/components/common/KapaSourcePicker";
import Spinner from "@/components/common/Spinner";
import { errorMessage } from "@/lib/api/envelope";
import { listKnowledge, type KnowledgeBase } from "@/lib/api/knowledge";
import { search, type SearchResult } from "@/lib/api/search";
import { DEFAULT_K, parseSearchQuery, searchQuery, TOP_K_OPTIONS } from "@/lib/searchUrl";

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
  // kapaGroups is the kapa.ai source-group selection (ids); none means kapa.ai
  // is not searched.
  const [kapaGroups, setKapaGroups] = useState<string[]>([]);
  // Problems the last search reported without failing, e.g. kapa.ai unavailable.
  const [warnings, setWarnings] = useState<string[]>([]);
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

  const runSearch = useCallback(async (q: string, scope: string[], k: number, groups: string[]) => {
    if (inFlight.current) return;
    inFlight.current = true;
    setSearching(true);
    setSearchError(null);
    setWarnings([]);
    setResults(null);
    setSearchedBases(groups.length > 0 ? [...scope, "kapa.ai"] : scope);
    try {
      const res = await search(q, scope, k, groups);
      setResults(res.results);
      setWarnings(res.warnings);
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
    async (restore?: { q: string; b: string[]; k: number; g: string[] }) => {
      setBasesError(null);
      try {
        const list = await listKnowledge();
        setBases(list);
        const fromUrl = restore
          ? restore.b.filter((name) => list.some((kb) => kb.name === name))
          : [];
        const scope = fromUrl.length > 0 ? fromUrl : defaultSelection(list);
        setSelected(scope);
        if (restore && restore.q && (scope.length > 0 || restore.g.length > 0)) {
          void runSearch(restore.q, scope, restore.k, restore.g);
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
    const fromUrl = parseSearchQuery(params);
    setQuery(fromUrl.q);
    setTopK(fromUrl.k);
    setKapaGroups(fromUrl.kapaGroups);
    void loadBases({ q: fromUrl.q, b: fromUrl.bases, k: fromUrl.k, g: fromUrl.kapaGroups });
  }, [params, loadBases]);

  const toggleBase = useCallback((name: string) => {
    setSelected((prev) =>
      prev.includes(name) ? prev.filter((b) => b !== name) : [...prev, name]
    );
  }, []);

  // A search needs a query and at least one source: a knowledge base or a
  // kapa.ai source group.
  const hasSource = selected.length > 0 || kapaGroups.length > 0;
  const canSubmit =
    query.trim() !== "" && hasSource && !searching && ((bases?.length ?? 0) > 0 || kapaGroups.length > 0);

  function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!canSubmit) return;
    const q = query.trim();
    // One history entry per executed search: the URL is the shareable record.
    router.push(`/search/?${searchQuery({ q, bases: selected, k: topK, kapaGroups })}`);
    // Focus stays in the query input after submit (foundation/AT contract),
    // including when the submit button was clicked.
    inputRef.current?.focus();
    void runSearch(q, selected, topK, kapaGroups);
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
          disabled={searching || !query.trim() || !hasSource}
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
        {bases !== null && bases.length > 0 && !hasSource && (
          <span className="p-text--small search__scope-hint">
            Select at least one knowledge base or kapa.ai source group to search.
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

      {/* kapa.ai sits in its own row with its own label, so a chip is never
          ambiguous about which retrieval source it scopes. */}
      <div className="search__kapa">
        <KapaSourcePicker selected={kapaGroups} onChange={setKapaGroups} disabled={searching} />
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

      {warnings.length > 0 && (
        <div className="p-notification--caution">
          <div className="p-notification__content">
            {warnings.map((w) => (
              <p key={w} className="p-notification__message">
                {w}
              </p>
            ))}
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
                    lands (Change 2 flips it to a link). A kapa.ai hit's source
                    is a kapa.ai URL and never links to a local knowledge base;
                    its chip above reads "kapa.ai" (the daemon's base for it). */}
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
