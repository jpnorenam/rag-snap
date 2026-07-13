# Design: add-ui-app-shell

## Context

The browser UI (`ui/`, Next.js static export embedded in `ragd`, served under `/ui/`) has one working screen: Chat at `/`. `Sidebar.tsx` renders a hardcoded `NAV_ITEMS` array of disabled `<button>` placeholders ("Soon"); there is no routing beyond the root page and no UI surface for the daemon's background operations, even though the API side is complete: `GET /1.0/operations`, `GET/DELETE /1.0/operations/{id}`, `GET /1.0/operations/{id}/wait`, and the `GET /1.0/events` websocket are all implemented in `internal/api/` (LXD-style operation objects with `id`, `description`, `status_code`, `may_cancel`, `err`, `metadata`; events are `{type: "operation"|"logging", timestamp, metadata}` with type filtering).

Five more screens are planned (knowledge, search, answer, prompts, status), each per its own `docs/ux/0N-*.md`. This change implements `docs/ux/01-app-shell.md` on top of `docs/ux/00-foundation.md`: real navigation plus the global operations UX and the shared primitives all later changes consume. It is UI-only — no Go, no snapcraft.yaml, no new snap interfaces, no config keys (snapctl or otherwise), and no new secrets.

All UI work follows the `ui-conventions` skill (Vanilla 4 classes hand-written in JSX, `--vf-*` tokens only, feature-prefixed BEM in `globals.scss`, static-export constraints: `basePath /ui`, `trailingSlash: true`, links like `/knowledge/`).

## Goals / Non-Goals

**Goals:**
- Sidebar entries become real `next/link` navigation with `aria-current="page"` active state; a new bottom-pinned **Status** utility entry (new `"status"` icon in the `NavIcon` union); still-unbuilt routes stay disabled `<span>` + "Soon".
- Per-section `<Header>` title and `document.title` (`"<Section> — RAG"`).
- Global operations indicator in the header status slot + anchored operations panel (list, status dots, relative timestamps, error detail, progress bar, cancel with confirm, dismiss).
- `OperationsProvider` context + `useOperations` hook: `track()` API, `/1.0/events` websocket subscription with reconnect/backoff, per-operation polling fallback, seed from `GET /1.0/operations` on mount.
- Shared primitives in `ui/components/common/`: `EmptyState`, `Spinner`, `ConfirmModal` (plain + type-to-confirm), generalized `.app-status-dot`.
- `deleteSync` verb in `ui/lib/api/envelope.ts` and a typed `ui/lib/api/operations.ts` module.

**Non-Goals:**
- No actual feature screens (knowledge/search/answer/prompts/status content land in later changes). This change only enables a route when its page exists; if no screen ships here, its nav item stays "Soon".
- No toast/notification system — completion feedback lives in the indicator.
- No daemon changes (e.g. operation persistence across restarts stays out of scope, per `rest-api-operations`).
- No changes to the chat screen beyond adopting shared context/primitives where trivially compatible.

## Decisions

### D1. Navigation via `usePathname()` in `Sidebar.tsx`, keeping the `NAV_ITEMS` array
Keep the existing data-driven `NAV_ITEMS` structure, adding `href` per item. Enabled items render `<Link href>`, disabled ones render the current non-focusable `<span>` + "Soon" badge (foundation §9: non-navigable items are never links or buttons — note this *changes* the current disabled `<button>` markup to `<span>`, aligning with the UX doc). Active = `usePathname()` prefix-match against `href` (exact for `/`), setting `is-active` + `aria-current="page"`. Alternative — per-page hardcoded active flags — rejected: duplicates state and breaks as routes are added.

Sidebar order per the UX doc: Chat, Knowledge bases, Search, Answer RFPs, Prompts (note: this reorders the current array, which has Prompts second), then Status pinned to the footer above the dark-mode toggle.

### D2. Titles owned by each page, not centralized routing config
Each `ui/app/<route>/page.tsx` renders `<Header title="…">` and sets `document.title = "<Section> — RAG"` in a `useEffect`. With static export there is no server metadata for client navigations; a tiny shared helper (e.g. `usePageTitle(section)` in `ui/lib/`) keeps it one line per page. Alternative — a central route→title map consumed by the layout — rejected: pages already own their `<Header>`, and a map is a second source of truth.

### D3. Operations state in a single React Context, screens push via `track()`
`OperationsProvider` (in `ui/components/common/`, mounted once in `ui/app/layout.tsx` inside the shell) holds a `Map<id, TrackedOperation>` in state. Screens call `track({ operationPath, description })` right after `postAsync`. This is the one sanctioned Context (foundation §4). The provider is the *only* consumer of the events socket and polling — screens never hand-roll polling. Screens that need completion side effects (refresh a list) register a callback with `track()` or read the op status from the hook.

Session-scoped, in-memory only (plus a mount-time seed from `GET /1.0/operations` so reloads recover *running* ops); finished ops dismissed by the user are dropped. No `localStorage` persistence — the daemon itself doesn't persist operations across restarts, so the UI shouldn't pretend to.

### D4. Events websocket primary, polling fallback, silent degradation
On mount the provider opens `wss?/ws` to `apiUrl("/1.0/events?type=operation")` (same-origin; token/cookie auth rides along as it does for the chat websocket). Operation events carry the full operation view in `metadata` — update the map by `id`. On socket error/close: schedule reconnect with exponential backoff (e.g. 1s → 30s cap), and while disconnected poll `GET /1.0/operations/{id}` every ~3s for each tracked *running* op only. No error banner for socket loss (UX doc: degradation is silent); daemon-unreachable errors surface on screens, not in the indicator.

Alternative — `GET /1.0/operations/{id}/wait` long-poll per op — rejected: one hanging request per operation, no progress updates mid-flight, and the events socket already exists.

### D5. Indicator + panel as header children, popover hand-rolled
`OperationsIndicator` renders in the `<Header>` right-hand `children` slot (coexists with chat's connection dot). Hidden until the first `track()` of the session; spinner icon variant + count while anything runs; `aria-label="N operations running"`, `aria-expanded`/`aria-controls` on the toggle. The panel is a right-aligned absolutely-positioned card (`.app-ops-panel`, surface `--vf-color-background-alt`, border `--vf-color-border-default`), list `aria-live="polite"`, closes on Escape and outside click (document listener + ref containment). Vanilla has no popover pattern in the sanctioned set, so this is a custom `.app-*` component — its styles go in `globals.scss` under `// --- operations ---` and the pattern gets added to `docs/ux/00-foundation.md` §6.

Row anatomy per the UX doc: `.app-status-dot` (caution=running, positive=succeeded, negative=failed; cancelled rendered distinctly from failed — muted/neutral dot + "Cancelled" text, distinguished via `status_code`), description, relative timestamp (absolute in `title`), Cancel (`p-button--base`, only while `may_cancel && running`) or dismiss ×; failed rows show `err` underneath in `p-text--small` + negative color; progress metadata renders a thin token-colored bar (inline `width: N%` is the allowed inline style).

### D6. Cancel flows through the shared `ConfirmModal`
Cancel = plain confirm variant (object named in body) → `deleteSync("/1.0/operations/{id}")`. `ConfirmModal` is a focus-trapped `p-modal` (`role="dialog"`, `aria-modal`, `aria-labelledby`, Escape + overlay close, focus moved on open and restored on close) with two variants: plain, and type-to-confirm (input must match the object name exactly before the `p-button--negative` enables) for later destructive flows like KB deletion. Never `window.confirm`.

### D7. API client additions follow the existing envelope pattern
`deleteSync` is a sibling of `getSync`/`postSync` reusing `request()`. `ui/lib/api/operations.ts` exports the `OperationView` interface mirroring `internal/api/operations.go`'s `operationView` JSON (id, class, description, created_at, updated_at, status, status_code, resources, metadata, may_cancel, err) plus `listOperations`, `getOperation`, `cancelOperation`; `null` arrays/maps normalized.

### D8. Status-code mapping constant
Running/succeeded/failed/cancelled are distinguished by the numeric `status_code` (per `rest-api-operations`), mirrored once as a constant in `ui/lib/api/operations.ts` — copy the exact values from `internal/api/operations.go` at implementation time rather than trusting text status.

## Risks / Trade-offs

- [Events socket auth differs from fetch: browsers can't set an Authorization header on websockets] → Same situation as the existing chat websocket — the loopback cookie travels on same-origin WS upgrades; reuse whatever token-in-URL mechanism `ChatScreen.tsx` uses if the cookie is absent. Verify against a real snap install, not just `next dev`.
- [Slow-consumer event drops: the hub drops events for full buffers, so a terminal event could be missed] → Polling fallback also runs a low-frequency sweep (`GET /1.0/operations/{id}`) for any op still marked running with a stale `updated_at`, so a dropped completion event self-heals.
- [Reordering nav + swapping disabled `<button>`→`<span>` touches existing tested-by-eye UI] → Small, isolated diff in `Sidebar.tsx`; verify in light/dark and at 620px collapsed rail per the shared checklist.
- [Panel/indicator introduces a new visual pattern outside Vanilla's stock set] → Scoped `.app-ops-panel` styles with sanctioned tokens only; documented in foundation §6 so later changes don't invent competing popovers.
- [No automated UI tests exist; regressions are manual] → Keep the definition-of-done checklist in tasks.md (both themes, 620px, keyboard-only walkthrough) as the acceptance gate; `make all` still gates the Go side (unchanged).

## Migration Plan

Pure addition; ships with the next snap build (UI is embedded via `go:embed`). Rollback = revert the commit. No data, config, or API migrations. Later screen changes depend on this one landing first (they import the primitives and the operations context).

## Open Questions

- ~~Which routes ship enabled in *this* change?~~ **Resolved during implementation: none.** No screen for knowledge/search/answer/prompts/status exists yet, and the UX doc forbids pre-enabling a route that renders nothing, so Chat (`/`) remains the only link and the other six entries stay non-focusable "Soon" placeholders. Each later change flips its own `enabled` flag in `NAV_ITEMS` when its page lands.
- Consequence to keep in mind: `track()` therefore has no caller yet — the indicator only appears when the provider *seeds* a running operation from `GET /1.0/operations` (e.g. one started from the CLI). The first screen to call `postAsync` (knowledge ingest) is what exercises the tracking path from inside the UI.
