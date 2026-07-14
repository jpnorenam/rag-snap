# Tasks: add-ui-app-shell

Read `docs/ux/00-foundation.md` first, then `docs/ux/01-app-shell.md`; they govern every visual
and interaction decision below.

## 1. App shell refactor (no behavior change)

- [x] 1.1 Create `ui/components/AppShell.tsx` (`"use client"`): `.app-shell` wrapper with `<Sidebar>`, dark-mode state (`useDarkMode`), one-time `captureTokenFromUrl()`, and children; render it from `ui/app/layout.tsx` around `{children}`
- [x] 1.2 Slim `ChatScreen.tsx` to `<Header title="Chat">…</Header>` + `<main className="app-main chat">` — remove its shell/Sidebar markup without touching chat internals (ws lifecycle, KB chips, connection dot)
- [x] 1.3 Sidebar: add `href` to `NAV_ITEMS`, render enabled entries as `next/link` with active state from `usePathname()` (normalize `basePath`/trailing slash) + `aria-current="page"`; disabled entries become non-focusable `<span>`s keeping the "Soon" badge
- [x] 1.4 Sidebar: add the Status entry pinned above the dark-mode toggle and a `"status"` icon (pulse/heartbeat line) to the `IconName` union
- [x] 1.5 Header: set `document.title = "<title> — RAG"` from the `title` prop via `useEffect`; keep the children status slot
- [x] 1.6 Verify the exported build (`npm run build`) still renders chat at `/ui/` with working dark mode and active nav state

## 2. API client for operations

- [x] 2.1 Add `deleteSync<T>` to `ui/lib/api/envelope.ts` following the existing `request()` pattern
- [x] 2.2 Create `ui/lib/api/operations.ts`: `OperationView` interface mirroring the daemon view (`id`, `class`, `description`, timestamps, `status`, `status_code`, `resources`, `metadata`, `may_cancel`, `err`), `listOperations()`, `getOperation(id)`, `cancelOperation(id)`; normalize `null` arrays to `[]`
- [x] 2.3 Add the events-socket connector in `operations.ts`: build the ws URL for `/1.0/events?type=operation` with the same origin-rewrite logic as `buildWsUrl` in `chat.ts` (cookie auth rides the upgrade)

## 3. Shared primitives (`ui/components/common/`)

- [x] 3.1 `Spinner.tsx`: `p-icon--spinner u-animation--spin` icon + visible label
- [x] 3.2 `EmptyState.tsx`: muted icon, one-line headline, one sentence of guidance including the CLI-equivalent command, optional primary action (foundation §7)
- [x] 3.3 `ConfirmModal.tsx`: `p-modal` + `p-modal__dialog`, `role="dialog" aria-modal="true" aria-labelledby`; plain and type-to-confirm variants (destructive button `[disabled]` until exact name match); focus moved in on open, hand-rolled focus trap, restored on close; Escape + overlay click close
- [x] 3.4 Generalize `.chat__status-dot` to `.app-status-dot` in `globals.scss` (caution=running, positive=succeeded, negative=failed, muted=cancelled) and switch the chat connection dot to it

## 4. Operations context

- [x] 4.1 Create `ui/components/common/OperationsProvider.tsx` + `ui/lib/useOperations.ts` exposing `{ operations, running, track, cancel, dismiss }`; mount the provider inside `AppShell`
- [x] 4.2 Seed the list from `GET /1.0/operations` on mount; upsert by `id`, ordered newest first; derive status from `status_code`, never the text
- [x] 4.3 Subscribe to the events websocket with capped exponential backoff (~1s → ~30s); re-fetch the operations list on every (re)connect
- [x] 4.4 Poll `GET /1.0/operations/{id}` every few seconds for running operations while the socket is down; degradation is silent (no error surface)

## 5. Operations indicator & panel

- [x] 5.1 Indicator button in the Header meta slot (coexists with screen children): activity line-icon + running count, hidden until the session has seen an operation, spinner variant + `aria-label="N operations running"` while running, `aria-expanded`/`aria-controls`
- [x] 5.2 `.app-ops-panel` dropdown card (surface `--vf-color-background-alt`, border `--vf-color-border-default`): rows with `.app-status-dot`, description, relative timestamp (absolute in `title`), dismiss × on terminal rows; failed rows show `err` in `p-text--small` + negative token; progress metadata renders a thin token-colored bar (inline width % only); empty body uses `EmptyState`
- [x] 5.3 Cancel action (`p-button--base`, only while running and `may_cancel`) routed through `ConfirmModal`; failed cancellation surfaces the API error without removing the row; cancelled rows render distinctly from failed
- [x] 5.4 Panel behavior: closes on Escape and outside click, list is `aria-live="polite"`; styles in `globals.scss` under `// --- ops ---` with `.app-ops-*` BEM names

## 6. Validation

- [x] 6.1 Run `make all` (tidy fmt vet lint test build) and `cd ui && npm run build` clean — Go tidy/fmt/vet/test/build pass and the UI static export builds clean (TypeScript check green). `golangci-lint` is not installed on this machine, but this change makes no Go changes, so vet is the effective Go gate.
- [x] 6.2 Build and install the snap (`snapcraft -v`, `sudo snap install --dangerous ./rag-cli_*.snap`); verify chat end-to-end (session, streaming, KB chips) — the shell refactor must not regress it (confirmed: chat works through the installed-snap UI)
- [x] 6.3 In the installed snap, run a real ingest (`rag-cli.rag k ingest …` or via API) and verify: indicator appears and counts, panel rows update live, reload re-seeds running ops, cancel flow works, ws-down fallback polls silently
- [x] 6.4 Verify compliance with the `ui-conventions` skill: both themes (`is-dark`), keyboard-only pass, all colors via `--vf-*` tokens, four view states where applicable, 620px collapsed rail
- [x] 6.5 Update `docs/local-ui.md`: navigation/sections and the operations indicator/panel (including cancel). No CLI, `rest-api.yaml`, or `apps/completion.bash` changes needed — confirmed via `git status` that only `docs/local-ui.md` and `ui/` files changed

## 7. Definition of done (UX) — from `docs/ux/00-foundation.md` + `docs/ux/01-app-shell.md`

- [x] 7.1 All four view states implemented per foundation §7; mutations follow §7's in-flight/success/failure rules (panel: empty=`EmptyState`, loaded=rows, ws/API degradation silent per spec; cancel mutation disables + shows "Working…", failure surfaces inline without losing the row)
- [x] 7.2 Looks correct in light **and** dark themes (`is-dark`) — verified in the installed-snap UI
- [x] 7.3 Usable at 620px (collapsed rail) — no horizontal page scroll; collapsed rail shows icons + active indicator only, tooltips via `title` — verified in the installed-snap UI
- [x] 7.4 Keyboard-only walkthrough passes (foundation §9); focus management on modals/routes verified — verified in the installed-snap UI
- [x] 7.5 Only sanctioned patterns (foundation §6) used; any new pattern added to `docs/ux/00-foundation.md` (ops indicator/panel already sanctioned in `01-app-shell.md`; primitives are the ones the foundation reserves)
- [x] 7.6 All colors via `--vf-*` tokens; zero hardcoded hex outside the sidebar SCSS vars
- [x] 7.7 Empty states include the CLI-equivalent command
- [x] 7.8 Sidebar: enabled items are links with correct active state; pending items remain non-focusable spans with "Soon"
- [x] 7.9 Operations panel: keyboard toggle/close, `aria-expanded`, `aria-live` announcements
- [x] 7.10 Cancel flows through `ConfirmModal`; a cancelled op renders distinctly from a failed one
