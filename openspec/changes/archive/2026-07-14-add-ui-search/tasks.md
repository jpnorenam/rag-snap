# add-ui-search — Tasks

## 1. API client

- [x] 1.1 Create `ui/lib/api/search.ts`: `SearchResult` interface mirroring the daemon's `searchResult` (score, base, source_id, created_at, provenance, content) and `search(query, bases, count)` via `postSync("/1.0/search", …)`, normalizing `null` to `[]`

## 2. Search screen

- [x] 2.1 Create `ui/app/search/page.tsx`: static-export client page wrapping `<SearchScreen />` in `<Suspense>` (required for `useSearchParams`), with `<Header title="Search">`
- [x] 2.2 Create `ui/components/SearchScreen.tsx` skeleton: `p-search-box` query bar (`aria-label="Search knowledge bases"`, Enter submits), scope row, results region with off-screen `<h2>Results</h2>` and `aria-live="polite"`
- [x] 2.3 Scope row: KB toggle chips reusing the ChatScreen `p-chip`/`p-chip--positive` markup fed by `listKnowledge()`, plus "Results" `<select>` (5/10/15/25, default 10); default selection per design Decision 3 (one base → it; `default` exists → it; else all); block submit with zero bases selected
- [x] 2.4 URL round-trip: on submit `router.push` `?q=…&b=…&b=…&k=…`; on mount restore state from `useSearchParams` and auto-run the search once (strict-mode-safe ref guard); drop URL bases that no longer exist and fall back to 10 on invalid `k`
- [x] 2.5 Result cards: rank number (muted), source ID (strong), KB name as non-interactive `<span class="p-chip">`, score right-aligned to 3 decimals (`p-text--small u-text--muted`); body renders full chunk with `white-space: pre-wrap`, no truncation; footer shows provenance details in `p-text--small` with the source ID as plain text (Change-2 link deferred)
- [x] 2.6 States: initial `EmptyState` (retrieval-only explanation + CLI hint `rag-cli.rag k search "<query>"`); loading spinner replacing results with submit disabled against double-submit; no-hits message naming the searched bases and suggesting wider scope or higher top-k; no-KBs caution notification; error notification per foundation §7 including the daemon-unreachable message
- [x] 2.7 Focus/AT behavior: focus stays in the query input after submit; live region announces "N results"; chips and select in tab order between input and results

## 3. Shell integration & styles

- [x] 3.1 Enable the Search item in `ui/components/Sidebar.tsx` (`enabled: true`); verify `aria-current="page"` on `/search/`
- [x] 3.2 Add `// --- search ---` section to `ui/app/globals.scss`: `.search-result` block (`__header`, `__score`, `__body`, `__footer`), surface `--vf-color-background-alt`, border `--vf-color-border-default`, tokens only

## 4. Verification

- [x] 4.1 `ui-conventions` compliance pass: light + dark themes, keyboard-only walkthrough, 620px collapsed rail with no horizontal scroll, only sanctioned patterns, zero hardcoded hex
- [x] 4.2 UX definition of done from `docs/ux/03-search.md`: URL round-trip reproduces the search on reload; chips match ChatScreen in both themes; full chunk text with scores/provenance matches `k search` output for the same query; no-hits vs error vs no-KBs are three distinct states
- [x] 4.3 Build the UI (`next build` static export succeeds with the Suspense boundary) and run `make all`; exercise the page against a running `ragd` with at least two KBs (one named `default`) — verify default scoping, top-k options, and an unreachable-daemon error
