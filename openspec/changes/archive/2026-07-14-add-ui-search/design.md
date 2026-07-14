# add-ui-search — Design

## Context

The daemon already exposes `POST /1.0/search` (`internal/api/handlers_search.go`): body
`{query, bases[], count}`, sync response of hits `{score, base, source_id, created_at,
provenance, content}`. The CLI (`k search`) and REPL (`/search`) already consume the same
retrieval path. The UI shell, envelope client (`getSync`/`postSync`), `listKnowledge()`,
`EmptyState`, `Spinner`, and the ChatScreen chip pattern all exist. The sidebar Search item
exists but is disabled. This change is a pure API client: no Go, no snapcraft, no config,
no secrets, no new snap interfaces.

UX contract: `docs/ux/03-search.md` on top of `docs/ux/00-foundation.md`. UI conventions per
the `ui-conventions` skill (Vanilla tokens only, flat `globals.scss`, sanctioned patterns).

## Goals / Non-Goals

**Goals:**
- `/search/` page with URL-persisted query/scope, chip + top-k scoping, full-chunk result
  cards with score and provenance, and all the distinct view states.
- Thin typed API module `ui/lib/api/search.ts` over the existing endpoint.
- Enable the sidebar Search entry.

**Non-Goals:**
- No daemon/REST API changes (the endpoint's default `count` of 15 is irrelevant — the UI
  always sends an explicit count).
- No pagination, no sidebar filters, no query rewriting — top-k is the result budget, same
  as the CLI.
- No source-metadata view (that is Change 2, `add-ui-knowledge`).

## Decisions

1. **URL state via `useSearchParams` + `router.replace`, page wrapped in `<Suspense>`.**
   `useSearchParams` in a statically exported App Router page requires a Suspense boundary
   (build fails without one); no page in the app uses it yet, so this sets the precedent:
   `page.tsx` renders `<Suspense><SearchScreen /></Suspense>`. On submit the screen writes
   `?q=…&b=<base>&b=…&k=…` with `router.replace` (no history spam per keystroke; one entry
   per executed search via `router.push` on submit). On mount, if `q` is present, restore
   scope and auto-run the search. Alternative considered: local state only — rejected, the
   UX doc requires shareable/reloadable searches.

2. **Encoding: repeated `b` params for bases, `k` for top-k.** Repeated params survive
   URLSearchParams round-trips cleanly and avoid inventing a delimiter that could collide
   with base names. Unknown/invalid `k` values fall back to 10; bases in the URL that no
   longer exist are dropped after `listKnowledge()` resolves.

3. **Default scope resolution order:** exactly one base → select it; a base named `default`
   exists → select only it (mirrors `k search -b default`); otherwise → select all. The UX
   doc leaves the "multiple bases, none named default" case open; selecting all is the only
   option that never produces an unsubmittable initial state. Zero-base submission is
   blocked client-side (disabled submit + hint) instead of letting the daemon 400.

4. **Search is `postSync`, not an operation.** The endpoint returns synchronously; no
   `useOperations`, no polling. In-flight state is local: submit button disabled with
   spinner icon, results area replaced by `Spinner`. A ref guards against double-submit
   (foundation §5/§7).

5. **Source metadata link: plain text now.** The UX doc says feature-detect the Change-2
   route at build time. There is no clean client-side route-existence check in a static
   export, and `/knowledge/` does not exist yet — so this change renders the source ID as
   plain text, and Change 2 flips it to a `<Link href="/knowledge/?kb=…&source=…">` when the
   route lands (one-line follow-up recorded in that change's scope). Alternative considered:
   a hardcoded feature flag — same effect, more machinery.

6. **Component/CSS layout.** `ui/components/SearchScreen.tsx` (`"use client"`, typed Props,
   className arrays per conventions); result card is a `.search-result` block
   (`__header`, `__score`, `__body`, `__footer` elements) in a new `// --- search ---`
   section of `globals.scss`, surface `--vf-color-background-alt`, standard border token,
   `white-space: pre-wrap` on the body. `p-search-box` verified present in the installed
   vanilla-framework 4.51. KB chips reuse the ChatScreen toggle markup verbatim; the KB name
   inside a result header is a non-interactive `<span class="p-chip">`.

7. **API module shape.** `ui/lib/api/search.ts` exports `SearchResult` (mirroring the
   daemon's `searchResult` JSON) and
   `search(query: string, bases: string[], count: number): Promise<SearchResult[]>`
   normalizing a `null` metadata array to `[]`, per the envelope conventions.

## Risks / Trade-offs

- [Auto-running a search on mount from URL params fires a POST on page load] → It is a
  read-only retrieval; this is exactly the shareable-URL behavior the UX doc asks for.
  Guard with a single-run ref so React strict-mode double-mount doesn't double-fire.
- [Base names with reserved URL characters] → always `URLSearchParams`-encode; never build
  query strings by concatenation.
- [Chat REPL default is 15 while the page defaults to 10] → intentional parity split per
  the UX doc: 10 matches `k search --top`; 15 stays available in the select.
- [No automated UI tests in the repo] → verification is manual: build the snap or run
  `ragd` locally, walk the four states, light + dark, 620px width, keyboard-only.

## Open Questions

None — the UX doc plus the existing endpoint pin down the behavior; the one ambiguity
(default selection with multiple bases and no `default`) is resolved by Decision 3.
