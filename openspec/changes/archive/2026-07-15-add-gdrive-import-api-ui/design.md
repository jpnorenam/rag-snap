## Context

The CLI already imports knowledge-base archives from Google Drive (`k import --url <drive-url>`), with an interactive `huh` multi-select picker and an `--all` flag. That flow lives entirely in `cmd/cli/basic/knowledge`:

- `gdrive.go` — `ParseDriveURL`, `ListDriveArchives`, `GetDriveFileName`, `DownloadDriveArchive` (pure network helpers against the Drive v3 API, given an access token).
- `gdrive_auth.go` — the OAuth2 Authorization-Code-with-PKCE **loopback** flow (`runLoopbackFlow` binds `127.0.0.1:0`, opens a browser, waits for Google's redirect, exchanges the code), plus token cache load/save/refresh (`LoadOrAuthenticateDrive`) and credential resolution from env or `gdrive.client.id`/`gdrive.client.secret` config.
- `import.go` — `ImportKnowledgeBase`, which restores an extracted export directory or a `.tar.gz` into OpenSearch.

The daemon (`internal/api`) already exposes local import (`POST /1.0/knowledge/import`) but its doc comment says "the interactive Google Drive auth flow is intentionally not exposed." Local-file import is client-side in the CLI because the strictly-confined daemon has no `home` plug and cannot read the user's filesystem. **Drive import has no such constraint**: it is network (Google) + OpenSearch only, both of which the daemon already reaches. The only genuinely interactive piece is the one-time OAuth consent, which we can mediate over REST.

Transport model (relevant to the callback design): the daemon serves an identical `/1.0` API over two listeners — a unix socket (peercred auth) and a loopback TCP listener (token auth, used by the browser UI). Both already bind loopback/local endpoints, so binding one more ephemeral loopback port for the OAuth callback is consistent with existing confinement.

The UI side is specified by `docs/ux/07-gdrive-import.md`: not a new page — the Change-2 `/knowledge/` import modal gains a **From file | From Google Drive** source chooser and a multi-step Drive flow (connect → locate → pick → import).

## Goals / Non-Goals

**Goals:**
- Daemon REST surface for: starting Drive OAuth, polling its completion, reading connection status, disconnecting, resolving a Drive URL into archives, and importing selected archives as tracked operations.
- UI parity with `k import --url`: folder picking with select-all (`--all`), single-file URLs skip the picker, force-overwrite, per-archive success/failure in the operations panel.
- Reuse the existing Drive helpers and `ImportKnowledgeBase` unchanged; refactor only the OAuth loopback flow so both CLI and daemon can share it.
- Never render tokens or OAuth URLs containing secrets in the UI or operation descriptions.

**Non-Goals:**
- No new config keys, no new secrets, no changes to how credentials are resolved.
- Not touching the inference server or Tika.
- Not exposing Drive *export* (upload to Drive) — import only, matching the CLI.
- Not sharing the OAuth token file between the CLI (`$SNAP_USER_DATA`, per-user) and the daemon (its own service data dir). The daemon maintains its own token; connecting in the UI does not connect the CLI and vice-versa.
- No general OAuth framework — this is Drive-specific.

## Decisions

### 1. Drive import runs in the daemon, as one tracked operation per archive
The daemon resolves the URL, then starts imports through the existing `s.ops.runTask` machinery calling `knowledge.ImportKnowledgeBase`. Each selected archive becomes **its own** operation (download → import) rather than one batch operation, because the UX requires per-archive partial-failure visibility in the operations panel ("partial failure is per-archive, not all-or-nothing"). Download uses `DownloadDriveArchive` (already streams to a temp `.tar.gz`, never buffering in RAM); the operation cleans the temp file in a `defer`.

*Alternative considered:* one batch operation importing N archives sequentially (like `runIngest` does for sources). Rejected: a single failed archive would fail or muddy the whole operation, and the ops panel could not show per-archive status.

### 2. OAuth is mediated, not blocking: a background flow + a status endpoint
`POST /1.0/knowledge/gdrive/connect` starts the loopback OAuth flow **in a background goroutine** and returns immediately with `{ consent_url }`. The daemon binds the ephemeral `127.0.0.1:<port>` callback server (as the CLI does today), and the UI opens `consent_url` in a new browser tab. The browser's redirect after consent hits the daemon's callback server directly on loopback; the daemon exchanges the code and persists the token. The UI learns the outcome by polling `GET /1.0/knowledge/gdrive/status`, which reports `configured` / `connected` / `pending` / `error` plus the connected account email when available.

This keeps every request short — no handler blocks for up to 5 minutes waiting on a human. The daemon holds at most **one** pending flow at a time (a mutex-guarded flow state with the ephemeral listener, PKCE verifier, and a `state` nonce); a second `connect` while one is pending cancels/replaces the first. The callback validates the `state` nonce before accepting a code.

*Alternative considered:* modelling the OAuth wait as an async Operation the UI tracks like any other. Rejected: operations are the vocabulary for *work the user asked the daemon to do*; an auth handshake with distinct connected/denied/timeout states maps poorly onto operation success/failure, and the UX doc explicitly wants a bespoke waiting state, not the ops panel. A dedicated status endpoint is simpler and matches `docs/ux/07`'s "polls/subscribes for auth completion."

*Alternative considered:* using the daemon's own loopback API port as the redirect URI (fixed path). Rejected: that port is token-authenticated and the redirect from Google carries no token; an unauthenticated callback route on the main API is a larger attack surface than a short-lived, single-use ephemeral listener scoped to one pending flow.

### 3. Refactor `runLoopbackFlow` into a reusable, non-interactive core
Today `runLoopbackFlow` prints to stdout and animates a spinner — CLI-only behaviour. Extract the mechanics (bind listener, build consent URL with PKCE, serve callback, exchange code) into a small reusable type in `cmd/cli/basic/knowledge` that exposes: build the consent URL + start the callback server (returning the URL), and await completion (channel/context). The CLI's interactive wrapper keeps its stdout prints; the daemon uses the core directly from its background goroutine. Token cache load/save/refresh (`LoadOrAuthenticateDrive` internals) are reused as-is; the daemon just calls them from its own process where `$SNAP_USER_DATA`/config point at the service's data dir.

### 4. Endpoint surface (new capability `rest-api-gdrive-import`)
All under `/1.0/knowledge/gdrive`, all `requireAuth`, registered in both `registerAPI` transports:
- `GET  …/status` → `{ configured, connected, pending, account?, error? }`. `configured=false` when `gdrive.client.id`/`secret` are unset (drives the UI's "not configured" state).
- `POST …/connect` → starts the flow, returns `{ consent_url }` (202-style). 409 if credentials unconfigured.
- `POST …/disconnect` → deletes the stored token (confirm is a UI concern). 
- `POST …/resolve` `{ url }` → `{ kind: "file"|"folder", archives: [{ id, name, size }] }`; specific 4xx for not-found / no-access / not-a-Drive-URL. Single-file URLs return one archive (name via `GetDriveFileName`).
- `POST …/import` `{ archive_ids: [...], name?, force }` → starts one operation per archive; returns the created operations (or, following existing `respondAsync` shape, the first/primary — see Open Questions). Reuses resolution context so the server maps ids→names.

`rest-api.yaml` documents each. Token/consent URLs never appear in any response except `consent_url` (which is the pre-consent Google URL with no secret — PKCE keeps the client secret server-side).

### 5. UI: extend the import modal into a source chooser; Drive is a stepped sub-flow
Per `ui-conventions` and `docs/ux/00-foundation.md`:
- The existing `ImportModal` gains a top **From file | From Google Drive** radio/tab (same pattern as `IngestModal`). "From file" keeps today's behaviour; the CLI-hint line is removed.
- Drive steps live in the same `p-modal`, announced via `aria-live="polite"`, each with a heading: **Connect** (status-driven: not-configured info state / connect card / waiting state / connected+disconnect), **Locate** (one URL field, client-side shape validation, resolve on submit), **Pick** (fieldset+legend checkbox group of archives with a tri-state select-all = `--all`, force checkbox), **Import** (`Import N archives` → `postAsync` per archive via the operations context, close modal).
- New `ui/lib/api/gdrive.ts` client (typed `status`/`connect`/`disconnect`/`resolve`/`import` verbs through `envelope.ts`); a Google `NavIcon` added to the `IconName` union; Drive styles appended to `globals.scss` under a `// --- gdrive import ---` header. All colors via `--vf-*` tokens; works in light and dark.
- Connect opens the consent URL with `window.open(url, "_blank")` (never navigates the SPA away) and polls `status` on an interval until `connected`/`error`, then advances. Cancel is focusable immediately and stops polling.

### 6. Config & secrets unchanged
`gdrive.client.id` / `gdrive.client.secret` remain `package`-scoped config, read via the existing snapctl-backed `storage.Config` the daemon already holds. No new keys, no env-var secrets introduced. `configured` in `status` is computed from those two keys being non-empty.

## Risks / Trade-offs

- **[Confinement: daemon binding an ephemeral loopback callback port]** → The daemon already binds a loopback TCP listener for its API, so `network-bind` is present; the ephemeral OAuth listener uses the same interface. Validate on a real strict install (per CLAUDE.md, build+install the snap) that the browser's redirect to `127.0.0.1:<port>` reaches the daemon.
- **[Daemon token vs CLI token divergence]** → Stored separately by design (§Non-Goals). Document in `docs/usage.md`/UI copy that connecting in the UI is independent of the CLI. Trust copy stays accurate: "stored by the daemon on this machine, used only to read the archives you select."
- **[Redirect reaches the wrong local process]** → A single-use ephemeral port plus a `state` nonce validated in the callback mitigates cross-flow/CSRF confusion; only the matching pending flow accepts a code.
- **[Long/human-paced consent blocking resources]** → The flow runs in a background goroutine with a 5-minute timeout; no request handler blocks. Only one pending flow at a time bounds resource use.
- **[Partial import failure]** → Per-archive operations make partial success first-class; the KB list refreshes as each import lands. No all-or-nothing messaging.
- **[Secret leakage]** → PKCE keeps `client_secret` server-side; only the pre-consent `consent_url` is returned; tokens are never serialized into responses or operation descriptions.

## Migration Plan

Additive only — no schema, config, or data migration. New endpoints and a new UI source; existing local import is untouched. Rollback = revert the change; no persisted state beyond the daemon's own Drive token file, which `disconnect` (or manual deletion) removes. Validate by building/installing the snap and round-tripping an exported archive: export → upload to Drive → connect → resolve folder → select-all → import → confirm KBs appear.

## Open Questions

- **Import response shape for N archives.** `respondAsync` returns a single operation + its URL. For multiple archives we either (a) return an array of operations (new response helper), or (b) start them and return the list of operation ids in a small envelope. Leaning (b) for minimal change; confirm during spec.
- **Force + collision reporting.** UX asks the force helper text to list which selected names collide "if the API can report it." `resolve`/`import` could cross-check archive stems against existing KB names to populate this; treat as a nice-to-have — ship without collision listing if it complicates the resolve contract.
- **Account email source.** `status.account` requires a `userinfo`/`about` call (extra scope or the Drive `about.get` endpoint). If the current `drive.readonly` scope doesn't expose it cleanly, omit the email and show a generic "Connected to Google Drive" — the UX doc already allows "email if the API exposes it."
