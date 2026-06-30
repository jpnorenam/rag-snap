# Local UI — Tasks

Ordered so the **chat vertical slice** is reachable as early as possible. Groups 1–6 land
the end-to-end chat experience; 7–8 are packaging and docs. Knowledge-management and
answer-review screens are deferred (noted in group 9).

## 1. UI scaffold (framework & style)

- [x] 1.1 Create `ui/` with a Next.js App Router project, TypeScript, and `next.config.js` set to `output: 'export'` (static SPA; build emits `ui/out`).
- [x] 1.2 Add Canonical Vanilla Framework via Sass: `vanilla-framework` dep, `app/globals.scss` with `@use "vanilla-framework"` + `@include vanilla-framework.vanilla;`.
- [x] 1.3 Port the app shell from `rag-snap-ui` style: `app/layout.tsx`, header, and a dark-mode toggle persisted in `localStorage` (`is-dark` class on `<html>`).
- [x] 1.4 Add `lib/types.ts` carrying over the `QAItem` / `QAFile` / `ParsedQAFile` contract (tolerate both `result` and `results`); leave unwired.
- [x] 1.5 Verify `npm run build` produces a static `ui/out/index.html` with assets.

## 2. API client layer (same-origin)

- [x] 2.1 Add `lib/api/rootPath.ts` exporting `ROOT_PATH` (default empty → same-origin), mirroring lxd-ui.
- [x] 2.2 Add `lib/api/envelope.ts`: a `fetch` helper that unwraps `sync` (`metadata`), reads the operation ref from `async`, and throws a typed error on `error` responses (the analogue of lxd-ui `handleResponse`).
- [x] 2.3 Add `lib/api/server.ts` (`GET /` and `GET /1.0` server info) and confirm the client builds `${ROOT_PATH}/1.0/...` URLs.
- [x] 2.4 Add a token-injection point in the fetch helper (Authorization header / credentials) for loopback auth, sourced at runtime (not build time).

## 3. Daemon: embed and serve the UI

- [x] 3.1 Add `internal/webui/` with `//go:embed` of the built UI (`ui/out`) into an `embed.FS`; include a committed placeholder so `go build` never fails on a missing dir.
- [x] 3.2 Implement the static handler over the embedded FS with an `index.html` SPA fallback (LXD `uiHTTPDir` pattern).
- [x] 3.3 Wire routes in `internal/api/server.go`: serve `/ui/` (assets, unauthenticated) and redirect `GET /` → `/ui/`.

## 4. Daemon: loopback listener

- [x] 4.1 Add `api.ui.enabled` (default off) and `api.ui.address` (default `127.0.0.1:0`) config keys with package defaults + user override.
- [x] 4.2 In `internal/api` add a loopback HTTP listener that shares the existing mux; refuse to bind a non-loopback address.
- [x] 4.3 In `cmd/ragd/main.go` open the loopback listener when `api.ui.enabled`, alongside the unchanged unix socket; log the resolved URL.

## 5. Daemon: localhost token auth

- [x] 5.1 Generate a high-entropy localhost token on enable; persist it under `$SNAP_COMMON` group-readable, reuse on restart.
- [x] 5.2 Make `auth.go` transport-aware: unix conn → existing peercred check (unchanged); loopback conn → validate the token; reject `/1.0/...` without a valid token.
- [x] 5.3 Keep `/ui/` static assets servable without the token; gate only `/1.0/...`.
- [x] 5.4 Add a unit test covering: valid token admitted, missing/invalid rejected, unix peercred path untouched.

## 6. Chat screen (vertical slice)

- [x] 6.1 Add `lib/api/chat.ts`: `POST /1.0/chat`, read `metadata.websocket.url` + `secret`, open the websocket at `.../websocket?secret=...`.
- [x] 6.2 Build the chat view: prompt input, message list, and incremental rendering of streamed `token` frames; render `think` frames distinctly; handle the `done` frame.
- [x] 6.3 Add `lib/api/knowledge.ts` `GET /1.0/knowledge`; render an active-KB selector and send `{type:"set-active-kbs",bases:[...]}` over the open socket mid-session.
- [x] 6.4 Handle session lifecycle: multi-turn on one connection, reconnect by starting a fresh session on close/error, surface API errors via the envelope helper.

## 7. CLI launch command

- [x] 7.1 Add `rag ui`: ensure the listener is available, resolve the loopback URL, read the token, and open the browser with the token applied (decide handoff per design Open Questions).
- [x] 7.2 When `api.ui.enabled` is false, print how to enable it instead of failing silently.

## 8. Packaging, build & docs

- [x] 8.1 Add a Makefile target to build the UI (`npm ci && npm run build`) and ensure it runs before the Go build / embed.
- [x] 8.2 Update `snap/snapcraft.yaml`: a build step/part that produces `ui/out` for the embed (Node toolchain), and loopback `network-bind` for the `ragd` app.
- [x] 8.3 Add a docs UI section (enable the listener, token/trust model, `rag ui` launch) under `docs/`.

## 9. Deferred (later phases — not in this change)

- [ ] 9.1 Knowledge-base management screens (create/delete/sources/ingest/export/import).
- [ ] 9.2 Migrate the `answer batch` review experience onto the preserved `QAItem`/`ParsedQAFile` contract.
- [ ] 9.3 Remote/web exposure: non-loopback bind, HTTPS/TLS, client certs / OIDC (attaches at the `auth.go` transport seam).
