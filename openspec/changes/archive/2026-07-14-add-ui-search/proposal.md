# add-ui-search

## Why

The UI parity plan (docs/ux/PLAN.md) gives the browser UI feature parity with the CLI. Retrieval-only inspection — what `k search` and the in-chat `/search` slash command do — has no UI equivalent yet: the sidebar's Search entry is a disabled placeholder, even though the daemon already exposes `POST /1.0/search`. Users cannot inspect what retrieval returns (chunks, scores, provenance) without dropping to the terminal, which is the main way to debug a knowledge base.

## What Changes

- New `/search/` page in the browser UI (`ui/app/search/page.tsx` + a `SearchScreen` component), per `docs/ux/03-search.md`:
  - Query bar (Vanilla `p-search-box`); query and scope persist in the URL (`/search/?q=…`) so results are shareable and reloadable.
  - Scope row: knowledge-base multi-select chips (reusing the `p-chip`/`p-chip--positive` pattern from `ChatScreen.tsx`) and a top-k `<select>` (5/10/15/25, default 10 — parity with `k search --top`).
  - Ranked result cards showing full, untruncated chunk text plus score (3 decimals), KB name, source ID, and provenance — matching what `k search` prints.
  - All four standard view states, plus distinct no-hits and no-KBs-exist states; results region announced via `aria-live`.
- New `ui/lib/api/search.ts` feature module wrapping the existing `POST /1.0/search` endpoint (typed request/response mirroring the daemon's `searchRequest`/`searchResult`).
- Enable the existing sidebar "Search" entry (`ui/components/Sidebar.tsx`) as a live route.
- Footer "View source metadata" link to `/knowledge/?kb=<name>&source=<id>` only if the knowledge route exists (Change 2 `add-ui-knowledge` has not landed); otherwise render the source ID as plain text.

**No daemon/REST API changes**: `POST /1.0/search` (internal/api/handlers_search.go) already provides hybrid retrieval with score/base/source/provenance/content per hit. This change is a pure API client.

External services: OpenSearch is exercised indirectly through the existing daemon endpoint; the inference server and Tika are untouched.

Config: no new config keys (neither `package` nor `user` scoped).

User-facing surface: a new UI page and a newly-enabled sidebar entry. Its UX documentation already exists as `docs/ux/03-search.md` (this change implements it); `docs/ux/PLAN.md` tracks it as Change 3. No CLI command, flag, or REPL behavior changes.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `local-ui-app`: add requirements for the `/search/` retrieval-inspection page — URL-persisted query/scope, KB chip + top-k scoping, full-chunk result cards with score and provenance, distinct empty/no-hits/no-KBs/error states, and the enabled sidebar Search route. (Same delta pattern as the prior `add-ui-status-settings` / `add-prompts-api-and-ui` changes.)

## Impact

- `ui/app/search/page.tsx` — new route (static export, client-side only).
- `ui/components/SearchScreen.tsx` — new screen component.
- `ui/lib/api/search.ts` — new API feature module over `postSync` (search is synchronous, not an operation).
- `ui/components/Sidebar.tsx` — flip the Search item to `enabled: true`.
- `ui/app/globals.scss` — `// --- search ---` section for `.search-result` card styles (tokens only).
- No Go code, no snapcraft, no config, no secrets changes. No new npm dependencies.
- Interaction with future Change 2 (`add-ui-knowledge`): the metadata link target; feature-detected so this change works standalone.
