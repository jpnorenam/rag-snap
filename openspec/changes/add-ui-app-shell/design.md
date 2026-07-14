# Design: add-ui-app-shell

UX authority: [`docs/ux/00-foundation.md`](../../../docs/ux/00-foundation.md) (read first) and
[`docs/ux/01-app-shell.md`](../../../docs/ux/01-app-shell.md) (this change's doc). Where this
design and those docs overlap, the UX docs win; conventions are also codified in the
`ui-conventions` skill.

## Context

The UI (`ui/`) is a Next.js static export embedded into `ragd` and served under `/ui/`. Today the
entire app shell — `<Sidebar>`, `<Header>`, dark mode, `captureTokenFromUrl()` — is rendered
*inside* `ChatScreen.tsx` ([ChatScreen.tsx:186-211](../../../ui/components/ChatScreen.tsx#L186)),
and `ui/app/page.tsx` is the only route. Sidebar entries other than Chat are disabled
`<button>` placeholders. The daemon already ships the full operations surface
(`rest-api-operations`): `GET /1.0/operations`, `GET/DELETE /1.0/operations/{id}`, and the
`GET /1.0/events?type=operation` websocket (`internal/api/handlers_operations.go`,
`internal/api/events.go`). Nothing in the UI consumes it.

Constraints that shape everything below:

- Static export (`output: 'export'`, `basePath: '/ui'`, `trailingSlash: true`): no server
  components with state, no route handlers, no dynamic path segments. Pages are
  `ui/app/<route>/page.tsx`, client-side only.
- Browser `WebSocket` cannot set an `Authorization` header; the loopback cookie set by the
  `/ui/login` handoff travels with same-origin websocket upgrades (the chat socket already relies
  on this, [chat.ts:85-90](../../../ui/lib/api/chat.ts#L85)).
- No UI dependencies may be added (no component library, no icon library, no ws client lib).

## Goals / Non-Goals

**Goals:**

- Multi-page navigation: sidebar entries become real routes as changes land; shell state
  (operations, dark mode) survives route changes.
- Global operations indicator + panel: any screen can start an async operation and the user can
  watch/cancel it from anywhere in the app.
- Shared primitives (`EmptyState`, `Spinner`, `ConfirmModal`, `.app-status-dot`) that Changes 2–7
  import instead of re-implementing.

**Non-Goals:**

- No new screens beyond Chat (Knowledge/Search/Answer/Prompts/Status pages belong to Changes 2–6;
  their nav entries stay disabled "Soon" placeholders here).
- No REST API or daemon changes; no toast/notification system (completion feedback lives in the
  indicator, per UX doc).
- No persistence of the operations list beyond what the daemon returns (daemon restarts drop
  in-flight ops by spec).

## Decisions

### D1: Hoist the shell into a client `AppShell` rendered by the root layout

Move `.app-shell` / `<Sidebar>` / dark-mode / `captureTokenFromUrl()` out of `ChatScreen` into a
new `ui/components/AppShell.tsx` (`"use client"`), rendered by `ui/app/layout.tsx` around
`{children}`. `AppShell` also mounts `OperationsProvider` so operations state lives above all
routes. Screens shrink to `<Header title="…">…</Header> + <main className="app-main …">`.

*Why:* the App Router layout persists across client-side navigations, so the events websocket,
dark-mode state, and the sidebar never remount when switching sections. The alternative — each
screen rendering its own shell (status quo) — duplicates markup per screen and would tear down the
operations socket on every navigation. `layout.tsx` stays a server component for the `metadata`
export; the client boundary starts at `AppShell`.

### D2: Sidebar items are data-driven links; disabled items are `<span>`s

`NAV_ITEMS` gains `href` (`/`, `/knowledge/`, `/search/`, `/answer/`, `/prompts/`, `/status/`) and
loses the hardcoded `active` flag. Enabled items render `<Link href>` with
`aria-current="page"` + the 3px orange left-border active recipe, derived from `usePathname()`
(normalizing the trailing slash and `basePath`). Disabled items render non-focusable
`<span>`s keeping the "Soon" badge — replacing today's `<button disabled>`, per foundation §9
(non-navigable items are never links or buttons). **Status** is added to the rail bottom (above
the dark-mode toggle) with a new `"status"` member in the `IconName` union (pulse/heartbeat line
icon, same inline-SVG pattern). In this change only Chat (`/`) is enabled; later changes flip
entries to links as their pages land.

### D3: `Header` owns `document.title` and the operations indicator

`Header` gets a `useEffect` that sets `document.title = "<title> — RAG"` from its `title` prop,
and renders the operations indicator in `.app-topbar__meta` *alongside* screen-provided children
(chat's connection dot coexists). *Why:* every screen already renders `<Header title>`, so this
gives per-route titles and a globally present indicator with zero per-screen wiring; a
pathname→title map in the layout would duplicate what screens already declare.

### D4: Operations state is one React Context fed by events-ws with polling fallback

`ui/components/common/OperationsProvider.tsx` + `ui/lib/useOperations.ts` expose:

```ts
interface OperationsContextValue {
  operations: UiOperation[];        // newest first, session-scoped
  running: number;                  // derived count
  track(op: OperationView): void;   // called by screens after postAsync
  cancel(id: string): Promise<void>;    // DELETE /1.0/operations/{id}
  dismiss(id: string): void;            // local removal of a terminal row
}
```

- **Seeding:** on mount, `GET /1.0/operations` populates the list so a reload doesn't lose
  running ops.
- **Live updates:** one `WebSocket` to `apiUrl("/1.0/events?type=operation")` (URL built with the
  same origin-rewrite logic as `buildWsUrl` in `chat.ts`; cookie auth rides the upgrade). Each
  `operation` event's `metadata` is a full operation view — upsert by `id`. Reconnect with
  capped exponential backoff (~1s → ~30s); on every (re)connect, re-fetch the list to close the
  gap, honoring the spec's "subscribe before launching" advice.
- **Fallback:** while the socket is down and any tracked operation is running, poll
  `GET /1.0/operations/{id}` every few seconds. Degradation is silent — no error banner (UX doc
  §States).
- **Status mapping** uses `status_code` (running / succeeded / failed / cancelled distinguishable
  by code), never the status text, per `rest-api-operations`.

*Why a context and not a hook per screen:* the indicator lives in the header on every route, and
Changes 2/4/7 all feed it; one subscription shared app-wide avoids N sockets and is the pattern
the foundation doc explicitly reserves Context for. Why not poll `GET /1.0/operations` on an
interval only: the events socket is push-based, cheaper, and already spec'd for exactly this.

### D5: API client additions stay inside the envelope pattern

- `deleteSync<T>(path)` added to `ui/lib/api/envelope.ts` as a sibling of `getSync`/`postSync`
  (same `request()` core) — needed for cancel; Change 2 reuses it for KB/source deletion.
- New `ui/lib/api/operations.ts` module: `OperationView` interface mirroring the daemon view
  (`id`, `class`, `description`, `created_at`, `updated_at`, `status`, `status_code`,
  `resources`, `metadata`, `may_cancel`, `err`), plus `listOperations()`, `getOperation(id)`,
  `cancelOperation(id)`, and the events-socket connector. `null` arrays normalize to `[]`.

### D6: Indicator/panel anatomy per the UX doc; no new visual language

Compact `<button>` in the header meta slot: activity line-icon + running count
(`aria-label="N operations running"`, `aria-expanded`, `aria-controls`); hidden until the session
has seen at least one operation; spinner variant while anything runs. Clicking toggles
`.app-ops-panel`, a right-anchored dropdown card (surface `--vf-color-background-alt`, border
`--vf-color-border-default`) listing operations newest-first. Row = `.app-status-dot`
(caution=running, positive=succeeded, negative=failed; cancelled rendered distinctly via a muted
dot + "Cancelled" text) · description · relative timestamp (absolute in `title`) · right side
Cancel (`p-button--base`, only while running **and** `may_cancel`, routed through `ConfirmModal`)
or dismiss ×. Failed rows show `err` underneath in `p-text--small` + negative token. Progress
metadata renders a thin token-colored bar (inline `width` % is the sanctioned inline style). The
list is `aria-live="polite"`; the panel closes on Escape and outside click. All styles go in
`globals.scss` under `// --- ops ---` with `.app-ops-*` BEM names.

### D7: Shared primitives in `ui/components/common/`

- `EmptyState.tsx` — muted icon, one-line headline, one sentence of guidance including the CLI
  equivalent, optional primary action (foundation §7).
- `Spinner.tsx` — `p-icon--spinner u-animation--spin` + visible label.
- `ConfirmModal.tsx` — `p-modal` + `p-modal__dialog`, `role="dialog" aria-modal="true"
  aria-labelledby`; plain variant and type-to-confirm variant (negative button `[disabled]` until
  the typed name matches exactly); focus moved in on open, trapped (hand-rolled Tab-cycling over
  the dialog's focusable elements — no dependency), restored on close; Escape + overlay click
  close.
- `.app-status-dot` — generalized from `.chat__status-dot`; the chat dot switches to it (keep the
  old class as an alias only if needed mid-change, delete before done).

This change ships `ConfirmModal`'s only consumer (cancel-operation), and `EmptyState`'s only
consumer is the ops panel's "No operations yet" body; both exist here primarily so Changes 2–7
import rather than re-invent them.

## Risks / Trade-offs

- **[Shell refactor touches the only working screen]** Moving Sidebar/Header out of `ChatScreen`
  can regress chat (ws lifecycle, KB chips, connection dot). → Keep `ChatScreen`'s internals
  untouched except deleting the shell wrapper; verify chat end-to-end in the installed snap
  before merging.
- **[Events socket message loss]** Best-effort delivery (slow subscribers drop events) or a
  reconnect gap can miss a terminal transition. → Re-fetch `GET /1.0/operations` on every
  (re)connect and poll tracked running ops while disconnected; the daemon list is the source of
  truth, events are only a change signal.
- **[Cookie-only ws auth]** If the UI was opened without the `/ui/login` cookie handoff (fragment
  token only), the events upgrade is refused. → Same failure mode as chat today; the indicator
  degrades to polling (which carries the Authorization header) — no user-facing error.
- **[Static-export active-state edge]** `usePathname()` values include `basePath` and trailing
  slashes inconsistently across dev/export. → Normalize both sides before comparing; verify in
  the exported build, not just `next dev`.
- **[Session-scoped panel]** Dismissed rows reappear after reload if the daemon still lists the
  op. Accepted: the daemon list is truth; dismiss is a cosmetic de-clutter.

## Migration Plan

1. Land `AppShell` + Sidebar/Header changes with Chat as the only route (pure refactor, no
   behavior change) — then the operations context/indicator on top.
2. `make all`, `cd ui && npm run build`, `snapcraft -v`, `sudo snap install --dangerous`, and
   exercise: navigation, chat regression, then a real ingest via
   `rag-cli.rag k ingest …` (or the API) to watch the indicator live, including cancel.
3. Rollback is `git revert` — no API, config, schema, or snap-packaging changes; the embedded UI
   is rebuilt from source on the next snap build.

No snapcraft.yaml, snap interface, hook, config-key, or secret changes. Nothing touches
OpenSearch, the inference server, or Tika.

## Open Questions

- None blocking. (Whether later changes need `putSync` is deferred to the change that first needs
  it, per foundation §5.)
