# Proposal: add-ui-app-shell

## Why

The browser UI currently ships a single working screen (Chat); every other sidebar entry is a disabled placeholder, and there is no way to see or manage the daemon's background operations (ingest, batch answer, export) from the browser. Upcoming CLI-parity screens (knowledge, search, answer batches, prompts, status) all need real navigation and a shared way to surface long-running operations — this change builds that foundation so the later screens can land independently on top of it.

## What Changes

- Convert the sidebar from disabled placeholder buttons into real client-side navigation: `next/link` entries for enabled routes, `aria-current="page"` active state derived from `usePathname()`, and per-section `<Header>` title + `document.title`. Routes that have no screen yet stay as non-focusable "Soon" placeholders.
- Add a **Status** nav entry (new `status` icon in the `NavIcon` union), pinned to the bottom of the rail as a utility item.
- Add a **global operations indicator**: a compact button in the `<Header>` status slot showing a count of running operations, toggling an anchored operations panel listing the session's operations (status dot, description, relative timestamp, cancel/dismiss, error detail, optional progress bar).
- Add an operations tracking context (`OperationsProvider` + `useOperations`): screens call `track(operation)` after `postAsync`; the provider subscribes to the daemon's `/1.0/events` websocket (auto-reconnect with backoff), falls back to polling `GET /1.0/operations/{id}`, and seeds from `GET /1.0/operations` on mount.
- Add the shared UI primitives later changes must import instead of re-implementing: `EmptyState`, `Spinner`, `ConfirmModal` (plain + type-to-confirm variants), and a generalized `.app-status-dot` style.
- Extend the API client with a `deleteSync` sibling in `ui/lib/api/envelope.ts` (needed for operation cancel) plus a typed operations module in `ui/lib/api/`.

No breaking changes; the chat screen keeps working unchanged at `/`.

## Capabilities

### New Capabilities

- `ui-app-shell`: multi-page navigation in the browser UI — sidebar links with active state, static-export routes, section titles, disabled-route placeholders, and the shared presentation primitives (`EmptyState`, `Spinner`, `ConfirmModal`, status dot) every later screen consumes.
- `ui-operations-tracking`: session-wide visibility of background operations in the UI — the header indicator, the operations panel (status, errors, progress, cancel via confirm flow), and the tracking context that consumes the existing `/1.0/operations` + `/1.0/events` API with polling fallback.

### Modified Capabilities

_None._ `local-ui-app` already requires client-side SPA routing; this change adds new capabilities on top of it without altering existing requirements. The daemon-side `rest-api-operations` spec is consumed as-is (all needed endpoints — `GET /1.0/operations`, `GET/DELETE /1.0/operations/{id}`, `GET /1.0/events` — are already implemented in `internal/api/server.go`).

## Impact

- **Code**: `ui/` only — `ui/components/Sidebar.tsx` (links + status icon), `ui/components/Header.tsx` consumers, new `ui/app/<route>/page.tsx` placeholders for enabled routes, new `ui/components/common/` (`OperationsProvider.tsx`, `EmptyState.tsx`, `Spinner.tsx`, `ConfirmModal.tsx`), new `ui/lib/useOperations.ts`, new `ui/lib/api/operations.ts`, `deleteSync` in `ui/lib/api/envelope.ts`, styles in `ui/app/globals.scss`. No Go changes.
- **External services**: none of the three backends (OpenSearch, inference server, Tika) are touched; the UI talks only to the local `ragd` daemon, whose API is unchanged.
- **Config**: no new config keys (neither `package` nor `user` scoped). No new secrets.
- **User-facing surface**: browser UI only — new navigation and the operations indicator/panel. No CLI commands, flags, or REPL behavior change. Documentation: the change implements `docs/ux/01-app-shell.md`; any new sanctioned UI pattern introduced along the way must be added to `docs/ux/00-foundation.md` §6 per its own rule.
- **Dependencies**: none added — no component library, icons stay inline SVG per the foundation conventions.
