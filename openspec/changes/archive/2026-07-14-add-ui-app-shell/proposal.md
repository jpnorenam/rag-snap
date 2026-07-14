# Proposal: add-ui-app-shell

## Why

The browser UI ships a single Chat screen; the sidebar renders four disabled "Soon" placeholders and there is no way to see or cancel the daemon's background operations (ingest, batch answer, export) from the browser. Every other change in the UI↔CLI parity plan (`docs/ux/PLAN.md` Changes 2–7) needs two foundations this change provides: real multi-page navigation in the Next.js static export, and a shared async-operations UX backed by the already-shipped `rest-api-operations` capability.

## What Changes

- Convert the sidebar from a single-page button list into real navigation: enabled entries become `next/link` routes with active state derived from `usePathname()` (`aria-current="page"`); pending entries become non-focusable placeholder `<span>`s keeping the "Soon" badge until their change lands. Add a **Status** entry (pinned to the rail bottom, above the dark-mode toggle) and a `status` icon to the `NavIcon` union.
- Route plumbing for the multi-page static export: each section is a `ui/app/<route>/page.tsx` page (`basePath /ui`, `trailingSlash: true`); route changes update the `<Header>` title and `document.title` (`"<Section> — RAG"`). Only Chat renders a real screen in this change — other routes stay disabled placeholders.
- A **global operations indicator** in the `<Header>` status slot: shows the count of running operations, toggles an anchored operations panel listing the session's operations (newest first) with status dot, description, relative timestamp, progress bar when the operation reports progress metadata, error message on failure, and a **Cancel** action (behind a confirm modal) for running operations with `may_cancel`.
- Operations state as a React Context (`OperationsProvider` + `useOperations` hook): seeds from `GET /1.0/operations` on mount, subscribes to the `/1.0/events` websocket (auto-reconnect with backoff), silently falls back to polling `GET /1.0/operations/{id}` if the socket drops; screens register work via `track(operation)` after `postAsync`. Cancellation calls `DELETE /1.0/operations/{id}` via a new `deleteSync` sibling in `ui/lib/api/envelope.ts` and a new `ui/lib/api/operations.ts` module.
- Shared UI primitives in `ui/components/common/` that later changes must import, not re-implement: `EmptyState`, `Spinner`, `ConfirmModal` (plain and type-to-confirm variants, focus-trapped), plus the `.app-status-dot` styles generalized from `.chat__status-dot`.

No breaking changes. This is UI-only: **no REST API work** (`rest-api-operations` already covers list/inspect/cancel/events) and **no Go/CLI changes**.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `local-ui-app`: navigation becomes multi-page routing over the static export (sidebar links, active state, per-route titles); a new requirement set covers the global operations indicator/panel (list, live progress via events websocket with polling fallback, cancel) and the shared UI primitives (empty state, spinner, confirm modals, status dot) that subsequent UI changes consume.

## Impact

- **Code**: `ui/` only — `ui/components/Sidebar.tsx`, `ui/components/Header.tsx`, new `ui/components/common/*` (OperationsProvider, EmptyState, Spinner, ConfirmModal), new `ui/lib/useOperations.ts`, new `ui/lib/api/operations.ts`, `deleteSync` in `ui/lib/api/envelope.ts`, route pages under `ui/app/`, styles in `ui/app/globals.scss`. The Go embed of `ui/out` picks the new assets up on the next snap build; no daemon code changes.
- **APIs consumed**: existing `GET /1.0/operations`, `GET /1.0/operations/{id}`, `DELETE /1.0/operations/{id}`, `GET /1.0/events` (websocket). No API surface added or modified.
- **External services**: none — OpenSearch, the inference server, and Tika are not touched; everything talks to the local `ragd` daemon only.
- **Config**: no new config keys (neither `package` nor `user` scope).
- **User-facing surface**: browser UI affordances only (sidebar navigation, operations indicator/panel). No CLI commands, flags, or slash commands change, so `--help` and `apps/completion.bash` are unaffected. Documentation to update: `docs/local-ui.md` (navigation + operations UX). `docs/rest-api.md` / `rest-api.yaml` need no changes.
- **UX guidelines**: governed by `docs/ux/00-foundation.md` and `docs/ux/01-app-shell.md`; the design links them and tasks carry their Definition of done checklist.
- **Dependencies**: none added — no component library; icons stay inline SVG.
