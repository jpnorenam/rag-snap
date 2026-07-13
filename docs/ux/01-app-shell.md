# 01 — UX guidelines: `add-ui-app-shell`

Foundation change: multi-page navigation + the global operations UX every later change consumes. Read `00-foundation.md` first.

## Information architecture

Sidebar order (top → bottom), matching the existing `NAV_ITEMS` in `ui/components/Sidebar.tsx`:

1. **Chat** — `/` (existing)
2. **Knowledge bases** — `/knowledge/`
3. **Search** — `/search/`
4. **Answer RFPs** — `/answer/`
5. **Prompts** — `/prompts/`
6. **Status** — `/status/` (pin to the bottom of the rail, above the dark-mode toggle, like a utility item)

Rules:
- Each route is a static-export page: `ui/app/knowledge/page.tsx`, etc. (`basePath /ui`, `trailingSlash: true` — links are `/knowledge/`).
- Convert `NAV_ITEMS` to real `next/link` entries as each change lands. Until then, keep the disabled `<span>` + "Soon" badge exactly as today — do **not** pre-enable routes that render nothing.
- Active state: `aria-current="page"` + the existing 3px orange left-border recipe. Derive active from `usePathname()`.
- The `<Header>` title reflects the section; also set `document.title` to `"<Section> — RAG"`.
- Keep all icons in the `NavIcon` union; add `"status"` (pulse/heartbeat line icon) in this change.

## Global operations indicator

The one new visual primitive of this change. It makes async CLI-parity work (ingest, batch answer, export) visible anywhere in the app.

### Placement & anatomy
- A compact button in the `<Header>` right-hand status slot (shared with chat's connection dot — they coexist): line-SVG activity icon + count of running operations. Hidden when there have been no operations this session.
- While anything is running: spinner variant of the icon + count, `aria-label="N operations running"`.
- Clicking toggles an anchored **operations panel** (right-aligned dropdown card, `.app-ops-panel`, surface `--vf-color-background-alt`, border `--vf-color-border-default`): a list of the session's operations, newest first.

### Panel row anatomy
Each row: status dot (generalized `.app-status-dot`: caution=running, positive=succeeded, negative=failed) · operation description ("Ingesting `handbook.pdf` into `default`") · relative timestamp · right side: **Cancel** (`p-button--base`, only while running, confirm per foundation §8) or a dismiss ×. Failed rows show the error message underneath in `p-text--small` + negative color token. If an operation reports progress metadata, render a thin token-colored progress bar under the row (inline width % is the allowed inline style).

### Behavior
- Implement as a React Context (`ui/components/common/OperationsProvider.tsx` + `ui/lib/useOperations.ts`, structured per `web-code-standards` `cs:react.component.structure.context`): screens call `track(operation)` after `postAsync`; the provider subscribes to `/1.0/events` (websocket, auto-reconnect with backoff) and falls back to polling `GET /1.0/operations/{id}` every few seconds if the socket drops.
- Completion feedback is **in the indicator** (dot turns positive/negative, count decrements) — no toast system. Screens that initiated an operation may additionally reflect completion locally (e.g. refresh their list).
- Panel closes on Escape and outside click; the toggle button is `aria-expanded` + `aria-controls`; the list is `aria-live="polite"` so completions are announced.

## Shared primitives to create here (in `ui/components/common/`)

- `EmptyState.tsx` — foundation §7 pattern.
- `Spinner.tsx` — icon + visible label wrapper.
- `ConfirmModal.tsx` — foundation §8 (plain + type-to-confirm variants); focus-trapped `p-modal`.
- `.app-status-dot` styles generalized from `.chat__status-dot`.

Later changes must import these, not re-implement them.

## States & edge cases
- Events socket unreachable but API fine: indicator still works via polling; no error banner (degradation is silent).
- Daemon unreachable (`ApiError.code === 0`): the *screen* shows the standard connection error; the indicator simply shows nothing new.
- Operation list from `GET /1.0/operations` on mount seeds the panel so a page reload doesn't lose running ops.

## Definition of done (UX)
Foundation checklist, plus:
- [ ] Sidebar: enabled items are links with correct active state; pending items remain non-focusable spans with "Soon"
- [ ] Operations panel: keyboard toggle/close, `aria-expanded`, `aria-live` announcements
- [ ] Cancel flows through `ConfirmModal`; a cancelled op renders distinctly from a failed one
- [ ] Collapsed 620px rail shows icons + active indicator only, tooltips via `title`
