# Tasks: add-ui-app-shell

## 1. API client groundwork

- [x] 1.1 Add `deleteSync` to `ui/lib/api/envelope.ts` following the existing `request()` pattern
- [x] 1.2 Create `ui/lib/api/operations.ts`: `OperationView` interface mirroring `internal/api/operations.go`'s `operationView` JSON, a status-code constant map (copy exact values from the Go source), and `listOperations` / `getOperation` / `cancelOperation` verbs (normalize `null` arrays/maps)

## 2. Shared primitives (`ui/components/common/`)

- [x] 2.1 Create `Spinner.tsx` (`p-icon--spinner u-animation--spin` aria-hidden + visible label)
- [x] 2.2 Create `EmptyState.tsx` per foundation §7 (muted icon, headline, guidance including CLI-equivalent command, primary action slot)
- [x] 2.3 Create `ConfirmModal.tsx`: focus-trapped `p-modal` (`role="dialog"`, `aria-modal`, `aria-labelledby`, Escape + overlay close, focus restore), plain and type-to-confirm variants (destructive button disabled until input matches the object name exactly)
- [x] 2.4 Generalize `.chat__status-dot` into `.app-status-dot` (caution/positive/negative variants) in `globals.scss` and switch the chat screen to it

## 3. Navigation shell

- [x] 3.1 Rework `Sidebar.tsx`: add `href` to `NAV_ITEMS`, reorder to Chat / Knowledge bases / Search / Answer RFPs / Prompts, render enabled items as `next/link` with active state from `usePathname()` (`is-active` + `aria-current="page"`), disabled items as non-focusable `<span>` + "Soon"
- [x] 3.2 Add the `"status"` pulse/heartbeat icon to the `NavIcon` union and pin the Status entry to the rail footer above the dark-mode toggle
- [x] 3.3 Add a `usePageTitle` helper (`ui/lib/`) setting `document.title = "<Section> — RAG"`; wire it plus `<Header title>` into the chat page (and any route enabled in this change)
- [ ] 3.4 Verify collapsed 620px rail: icons + active indicator only, labels via `title`, no horizontal page scroll

## 4. Operations tracking context

- [x] 4.1 Create `ui/components/common/OperationsProvider.tsx` + `ui/lib/useOperations.ts`: tracked-operations map, `track()` API with optional completion callback, newest-first ordering, seed from `GET /1.0/operations` on mount
- [x] 4.2 Implement the `/1.0/events?type=operation` websocket subscription with exponential-backoff reconnect (reuse the chat websocket's auth approach), updating tracked ops from event metadata
- [x] 4.3 Implement the polling fallback: while the socket is down, poll `GET /1.0/operations/{id}` every ~3s for tracked running ops; also sweep stale running ops to self-heal dropped events; degradation is silent (no banner)
- [x] 4.4 Mount the provider once in `ui/app/layout.tsx` inside the app shell

## 5. Indicator and panel

- [x] 5.1 Create the header operations indicator: hidden until first tracked op, spinner variant + running count while running, `aria-label="N operations running"`, `aria-expanded`/`aria-controls` toggle; render it in the `<Header>` status slot alongside chat's connection dot
- [x] 5.2 Create the anchored `.app-ops-panel` (surface `--vf-color-background-alt`, border `--vf-color-border-default`): rows with `.app-status-dot`, description, relative timestamp (absolute in `title`), failed-row error text (`p-text--small` + negative token), optional progress bar (inline width %), dismiss ×; cancelled state distinct from failed; list is `aria-live="polite"`; closes on Escape and outside click
- [x] 5.3 Wire Cancel: shown only while `may_cancel` && running, flows through `ConfirmModal` (plain variant), then `cancelOperation`; row transitions to cancelled
- [x] 5.4 Add all new styles to `globals.scss` under `// --- operations ---` (BEM, `--vf-*` tokens only) and document the popover pattern in `docs/ux/00-foundation.md` §6

## 6. Verification

- [ ] 6.1 Verify compliance with the `ui-conventions` skill: light **and** dark themes, keyboard-only walkthrough (panel toggle/Escape, modal focus trap/restore), all colors via `--vf-*` tokens (zero new hex), four view states where applicable, only sanctioned patterns
- [ ] 6.2 Shared definition-of-done checklist from `docs/ux/01-app-shell.md`: sidebar active state + non-focusable "Soon" spans; panel keyboard + `aria-live`; cancel through `ConfirmModal` with cancelled ≠ failed; 620px rail
- [ ] 6.3 `npm run build` in `ui/` (static export succeeds), then `make all`
- [ ] 6.4 Build the snap (clean the go-cli part first to avoid a stale binary), install with `--dangerous`, and exercise end-to-end: track a real ingest operation, watch events-driven updates, kill the socket to confirm polling fallback, cancel an op
