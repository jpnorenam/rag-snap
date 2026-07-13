# Local UI — Tasks

Ordered so the **chat vertical slice** is reachable as early as possible. The loopback
listener and localhost token auth already exist on this branch (`rest-api-loopback`,
`rest-api-localhost-auth`); these tasks only add the UI, its serving, the token handoff,
and the launch command. Most `ui/` and `internal/webui/` files can be ported near-verbatim
from the `feat/local-rest-api` reference branch — the diffs are the `api.loopback.*` /
`Config.Loopback` naming reconciliation (group 5) and mounting `/ui/` on `loopbackRoutes()`
(group 3).

## 1. UI scaffold (framework & style)

- [x] 1.1 Port `ui/` from `feat/local-rest-api`: Next.js App Router project, TypeScript, `next.config.js` with `output: 'export'` (static SPA → `ui/out`), `package.json` / `package-lock.json` / `tsconfig.json`.
- [x] 1.2 Port Vanilla Framework via Sass: `vanilla-framework` dep, `app/globals.scss` (`@use "vanilla-framework"` + `@include vanilla-framework.vanilla;`).
- [x] 1.3 Port the app shell: `app/layout.tsx`, `app/page.tsx`, `components/Header.tsx`, and `lib/useDarkMode.ts` (dark-mode toggle persisted in `localStorage`, `is-dark` class on `<html>`).
- [x] 1.4 Port `lib/types.ts` carrying over the `QAItem` / `ParsedQAFile` contract (tolerate both `result` and `results`); leave unwired.
- [x] 1.5 Run `cd ui && npm ci && npm run build` and verify it produces a static `ui/out/index.html` with assets.

## 2. API client layer (same-origin)

- [x] 2.1 Port `lib/api/rootPath.ts` exporting `ROOT_PATH` (default empty → same-origin).
- [x] 2.2 Port `lib/api/envelope.ts`: the `fetch` helper that unwraps `sync` (`metadata`), reads the operation ref from `async`, and throws a typed error on `error` responses.
- [x] 2.3 Port `lib/api/server.ts` (`GET /` and `GET /1.0` server info); confirm the client builds `${ROOT_PATH}/1.0/...` URLs.
- [x] 2.4 Ensure the fetch helper attaches the localhost token at runtime (Authorization header / loopback cookie credential), sourced at runtime — never baked into the static assets.

## 3. Daemon: embed and serve the UI on the loopback listener

- [x] 3.1 Port `internal/webui/` from `feat/local-rest-api`: `//go:embed all:dist`, `Assets()`, `Handler()` with the `index.html` SPA fallback, and `assetExists`; include the committed placeholder `internal/webui/dist/index.html` so `go build` never fails on a missing dir. Port `internal/webui/webui_test.go`.
- [x] 3.2 Extend `loopbackRoutes()` in `internal/api/server.go` to mount `/ui/` (static assets from `webui.Handler()`, unauthenticated, via `http.StripPrefix`) and add `GET /{$}` → redirect to `/ui/`. Leave the unix-socket `routes()` untouched (no `/ui/`).
- [x] 3.3 Update the `loopbackRoutes()` doc comment that currently says `/ui/` and the root redirect are deferred — they now exist.

## 4. Chat screen (vertical slice)

- [x] 4.1 Port `lib/api/chat.ts`: `POST /1.0/chat`, read `metadata.websocket.url` + `secret`, open the websocket at `.../websocket?secret=...`.
- [x] 4.2 Port `components/ChatScreen.tsx`: prompt input, message list, incremental rendering of streamed `token` frames, distinct `think` rendering, `done` handling; multi-turn on one connection; reconnect by starting a fresh session; surface API errors via the envelope helper.
- [x] 4.3 Port `lib/api/knowledge.ts` (`GET /1.0/knowledge`); render an active-KB selector and send `{type:"set-active-kbs",bases:[...]}` over the open socket mid-session.

## 5. Daemon: token handoff + CLI launch command

- [x] 5.1 Add a `/ui/login` handler to `loopbackRoutes()`: accept the localhost token (query param), establish it as a loopback-scoped credential (cookie), and redirect into `/ui/`. Reachable without prior auth.
- [x] 5.2 Port `cmd/cli/basic/ui.go` (`rag ui`) from the reference branch, **reconciling naming to this branch**: read `ui := info.Config.Loopback` (`apiclient.LoopbackInfo`, not `Config.UI`); use its `Enabled` / `URL` / `Token`; build the `/ui/login` handoff URL; open the browser; support `--no-browser`.
- [x] 5.3 In the disabled path, print how to enable the listener using **`api.loopback.enabled`** (not `api.ui.enabled`) and how to restart `ragd`.
- [x] 5.4 Register `basic.UICommand(ctx)` in `cmd/cli/main.go`, preserving the fixed command order (`cobra.EnableCommandSorting = false`).

## 6. Packaging, build & docs

- [x] 6.1 Add a `make ui` target (`cd ui && npm ci && npm run build`, then copy `ui/out` → `internal/webui/dist/`) and ensure it runs before the Go build/embed; port the reference Makefile UI section.
- [x] 6.2 Update `snap/snapcraft.yaml`: add the Node static-export build step producing `internal/webui/dist/` before the Go build (via the `node/20/stable` build-snap); confirm the `ragd` app already plugs loopback `network-bind`.
- [x] 6.3 Add a docs UI section under `docs/` (enable `api.loopback.enabled`, token/trust model, `rag ui` launch); update `docs/usage.md`, the `rag ui` `--help`/usage text, and `apps/completion.bash` for the new command.

## 7. Validation

- [x] 7.1 Run `make all` (tidy fmt vet lint test build) and `cd ui && npm run build`; fix any failures.
- [x] 7.2 Build and install the snap; set `api.loopback.enabled=true`, restart `ragd`, run `rag ui`, and verify the browser opens the UI, chat streams tokens, and switching active KBs mid-session works over the loopback listener.
- [x] 7.3 Verify `rag ui` with the listener disabled prints the `api.loopback.enabled` guidance instead of failing, and that the unix socket still serves `/1.0/...` with no `/ui/` route.

## 8. Deferred (later phases — not in this change)

- [ ] 8.1 Knowledge-base management screens (create/delete/sources/ingest/export/import).
- [ ] 8.2 Migrate the `answer batch` review experience onto the preserved `QAItem`/`ParsedQAFile` contract.
- [ ] 8.3 Remote/web exposure: non-loopback bind, HTTPS/TLS, client certs / OIDC (attaches at the `auth.go` transport seam).