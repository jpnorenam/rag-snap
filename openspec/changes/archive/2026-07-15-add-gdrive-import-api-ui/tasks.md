## 1. Reusable OAuth loopback core (Go)

- [x] 1.1 Refactor `runLoopbackFlow` in `cmd/cli/basic/knowledge/gdrive_auth.go` into a reusable, non-interactive core: expose (a) build the consent URL + start the ephemeral loopback callback listener (returns `consent_url`, PKCE verifier, `state` nonce), and (b) await the callback with context/timeout and exchange the code. Keep the CLI's stdout-printing wrapper on top so `k import --url` behaviour is unchanged.
- [x] 1.2 Validate the `state` nonce in the callback handler; ensure the ephemeral listener is single-use and shut down on completion, timeout, or context cancel.
- [x] 1.3 Confirm token cache load/save/refresh + credential resolution (`resolveClientCredentials`, `loadCachedDriveToken`, `saveDriveToken`, `refreshDriveToken`) are reusable from the daemon process (its own `$SNAP_USER_DATA`/config); expose a helper to report configured/connected/account without triggering a browser flow.

## 2. Daemon Drive endpoints (`internal/api`)

- [x] 2.1 Add `internal/api/handlers_gdrive.go` with a mutex-guarded single pending-flow state (ephemeral listener, verifier, state, result/error).
- [x] 2.2 `GET /1.0/knowledge/gdrive/status` → `{ configured, connected, pending, account?, error? }`; `configured` computed from `gdrive.client.id`/`gdrive.client.secret`. Never serialize tokens.
- [x] 2.3 `POST /1.0/knowledge/gdrive/connect` → start the flow in a background goroutine, return `{ consent_url }` promptly; 4xx when unconfigured; supersede any existing pending flow.
- [x] 2.4 `POST /1.0/knowledge/gdrive/disconnect` → delete the stored token.
- [x] 2.5 `POST /1.0/knowledge/gdrive/resolve` `{ url }` → `{ kind, archives:[{id,name,size}] }` via `ParseDriveURL`/`ListDriveArchives`/`GetDriveFileName`; specific errors for not-found / no-access / not-a-Drive-URL / not-connected.
- [x] 2.6 `POST /1.0/knowledge/gdrive/import` `{ id, name, target?, force }` → imports **one** archive per call as a tracked operation (`DownloadDriveArchive` → `ImportKnowledgeBase`), deriving the KB name from the filename when `target` is empty, cleaning the temp archive in a `defer`, and returning a single operation via `respondAsync`. Decision (resolves design open question): the UI issues one call per selected archive, so each archive is an independently tracked operation and `postAsync` + the operations context are reused unchanged.
- [x] 2.7 Register all routes in `internal/api/server.go` `registerAPI` (both unix + loopback transports) with `requireAuth`, preserving grouping/order.
- [x] 2.8 Update the local-import handler doc comment in `handlers_engine.go` that claims the Drive flow is "intentionally not exposed."

## 3. UI API client (`ui/lib/api`)

- [x] 3.1 Add `ui/lib/api/gdrive.ts` with typed `status`/`connect`/`disconnect`/`resolve`/`import` verbs through `envelope.ts` (add `deleteSync`/etc. only if needed); normalize `null` arrays to `[]`.
- [x] 3.2 Type the archive, status, and resolve views to mirror the daemon responses.

## 4. UI import modal + Drive flow (`ui/components`, `ui/app/globals.scss`)

- [x] 4.1 Extend `ui/components/knowledge/ImportModal.tsx` (or add a `GdriveImportModal`) with a **From file | From Google Drive** source chooser matching the ingest modal's tab pattern; remove the Change-2 "use the CLI for Drive" hint line.
- [x] 4.2 Connect step: not-configured info state (links Status page + names config keys); connect card; waiting state that `window.open`s the consent URL in a new tab, polls `status`, has a focusable Cancel; connected state with account + confirm-guarded Disconnect.
- [x] 4.3 Locate step: single URL field with client-side shape validation; resolve on submit; specific resolution error messages.
- [x] 4.4 Pick step: fieldset+legend checkbox group (name · size · modified) with a tri-state Select-all (`indeterminate`), selected count, and a Force checkbox.
- [x] 4.5 Import step: `Import N archives` → `postAsync` per archive tracked via the operations context; close modal immediately; list refreshes as imports land; per-archive partial failure via the ops panel.
- [x] 4.6 Google line-icon rendered inline (`aria-hidden` SVG, `stroke="currentColor"`) in the connect card — the Sidebar `IconName` union is nav-specific, so an inline line-SVG (foundation §6) is used instead of extending it; Drive styles added under a `// --- gdrive import ---` header in `globals.scss` using only `--vf-*` tokens.
- [x] 4.7 Multi-step a11y: `aria-live="polite"` step-change announcements, a heading per step, keyboard-only reachable consent handoff and Cancel.

## 5. Docs & API spec

- [x] 5.1 Document the new endpoints in `rest-api.yaml`.
- [x] 5.2 Update `docs/usage.md` where UI-vs-CLA Drive behaviour or token storage is described; note the daemon token is independent of the CLI token.
- [x] 5.3 Sync the change's delta specs into `openspec/specs/{rest-api-gdrive-import,local-ui-app}` — deferred to `/opsx:archive`, which is the canonical merge step (syncing now would double-apply the deltas at archive time).

## 6. Verification

- [x] 6.1 Run `make all` (tidy fmt vet lint test build) and `npm run build`/lint for the UI.
- [x] 6.2 `ui-conventions` compliance verified by inspection + `npm run build`: colors only via `--vf-*` tokens, all four view states present, native controls with focus-trapped modal, tri-state select-all, `aria-live` step announcements, focusable Cancel. Live visual dark/light + 620px pass to be eyeballed when running the app (see 6.3).
- [ ] 6.3 (Not run in this environment — requires a real snap install.) Build and install the snap; validate end-to-end on a strict install: confirm the daemon can bind the ephemeral loopback callback and the browser redirect reaches it; export → upload to Drive → connect → resolve folder → select-all → import → KBs appear; verify denial and timeout produce distinct recoverable states; verify disconnect deletes the token.
