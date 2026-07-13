## Why

The `ragd` daemon already exposes the full REST API over a unix socket and, on this
branch, over an opt-in loopback TCP listener authenticated by a daemon-generated
localhost bearer token (`api.loopback.*`, `internal/api/loopback.go` / `token.go` /
`auth.go`). But there is still no browser client: the loopback listener serves only
`/1.0/...` — its `loopbackRoutes()` deliberately omits `/ui/` assets and the root
redirect, with a comment that those "belong to the deferred UI change." This change is
that deferred UI: a local browser UI, chat-first, embedded in and served by `ragd`, so
users get the RAG experience without the terminal REPL.

A complete UI implementation already exists on the `feat/local-rest-api` reference
branch. The goal here is to **port it faithfully** — same framework, styling, and UX —
onto this branch, where the loopback transport and token auth it depends on are already
in place. Keeping it a separate change gives a clean commit history and a small,
reviewable UI-only diff.

## What Changes

- **Add a browser UI** under `ui/` — a Next.js (App Router) + React + TypeScript
  single-page app styled with Canonical's Vanilla Framework (Sass), statically
  exported (`output: 'export'`) to `ui/out`. It is a real client of the local API
  (not the Firebase-backed `rag-snap-ui` reviewer), calling `${ROOT_PATH}/1.0/...`
  same-origin. First feature: interactive **chat** (start session via `POST /1.0/chat`,
  connect the returned websocket, stream `token`/`think` frames, switch active
  knowledge bases mid-session). The `QAItem`/`ParsedQAFile` type contract is carried
  over unwired for a later `answer batch` review migration.
- **Embed the UI into `ragd`** via a new `internal/webui/` package (`//go:embed` of
  the built assets under `dist/`, with a committed placeholder so `go build` never
  fails on a missing embed dir) and a static file handler with an `index.html` SPA
  fallback.
- **Serve the UI same-origin on the existing loopback listener.** Extend
  `loopbackRoutes()` in `internal/api/server.go` to mount `/ui/` (static assets,
  unauthenticated) and redirect `GET /` → `/ui/`, alongside the unchanged `/1.0/...`
  API. Add a `/ui/login?token=...` handoff endpoint that sets a loopback-scoped cookie
  and redirects into the SPA, keeping the token out of the SPA's JS and URL bar.
- **Add a `rag ui` CLI command** (`cmd/cli/basic/ui.go`) that queries the daemon over
  the trusted unix socket for the loopback listener's URL and token (already exposed as
  `apiclient.LoopbackInfo` via `GET /1.0`), builds the `/ui/login` handoff URL, and
  opens the browser (`--no-browser` prints it instead). When the loopback listener is
  disabled it explains how to enable it rather than failing silently.
- **Build wiring:** a `make ui` target and a snapcraft build step that run the Next.js
  static export into `internal/webui/dist/` **before** the Go build; the `ragd` app
  keeps its existing loopback `network-bind`.
- **Docs:** a UI section under `docs/` covering enabling the listener, the token/trust
  model, and the `rag ui` launch flow.

**Not changing:** the loopback listener itself, the localhost bearer-token generation
and transport-aware auth, and the unix-socket + peercred contract — all already
implemented and specced on this branch (`rest-api-loopback`, `rest-api-localhost-auth`).
No `/1.0/...` endpoint behaviour changes.

### External services touched

None. The UI is a client of the existing `ragd` API, which already owns the OpenSearch /
inference / Tika clients. Chat streaming reuses the existing inference path unchanged.

## Capabilities

### New Capabilities

- `local-ui-app`: the embedded browser UI itself — its framework/build (Next.js static
  export + Vanilla Framework), the same-origin API client layer targeting
  `${ROOT_PATH}/1.0/...` with envelope unwrapping, the chat screen (session start,
  websocket streaming, active-KB switching), and the preserved answer-review data
  contract.
- `rest-api-ui-serving`: serving the embedded UI same-origin on the existing loopback
  listener — mounting `/ui/` (unauthenticated static assets), the `GET /` → `/ui/`
  redirect, the SPA fallback to `index.html`, the `/ui/login` token-handoff endpoint,
  and the `rag ui` launch command.

### Modified Capabilities

<!-- None at spec level. rest-api-loopback (the loopback TCP listener) and
     rest-api-localhost-auth (token generation + transport-aware auth) already exist on
     this branch and are reused unchanged; this change only adds UI assets, same-origin
     serving on top of that listener, and the launch command. The `/ui/login` handoff
     consumes the existing token without altering how it is generated or validated. -->

## Impact

- **Affected code:**
  - New `ui/` — Next.js + TypeScript + Vanilla Framework SPA (static export to `ui/out`);
    `lib/api/` resource modules + envelope helper + token-injection point; chat screen;
    dark-mode shell; `QAItem`/`ParsedQAFile` types (unwired).
  - New `internal/webui/` — `go:embed` of the built UI and the static/SPA-fallback handler
    (plus a committed placeholder `dist/index.html`).
  - `internal/api/server.go` — extend `loopbackRoutes()` with `/ui/` serving, `GET /` →
    `/ui/` redirect, and the `/ui/login` handoff handler (loopback routes only; the unix
    socket routes are untouched).
  - `cmd/cli/basic/ui.go` + `cmd/cli/main.go` — new `rag ui` command, registered in the
    fixed command order.
- **Config keys:** none new. Reuses the existing `api.loopback.enabled` (default false)
  and `api.loopback.address` (default `127.0.0.1:0`) package-scoped keys with user
  override. (Note the reference branch used `api.ui.*` and `Config.UI`; the port must
  adapt to this branch's `api.loopback.*` / `apiclient.LoopbackInfo`.)
- **Snap/build:** `snap/snapcraft.yaml` gains a Node static-export build step producing
  `internal/webui/dist/` before the Go build (Node toolchain; the project already vendors
  Node for elasticdump); the `ragd` app's existing loopback `network-bind` covers the
  listener. `Makefile` gains a `ui` target run before the Go build.
- **New dependencies:** Node/npm at build time only (Next.js, React, TypeScript,
  `vanilla-framework`, Sass). No new runtime dependency — assets are embedded, so no
  Node at runtime.
- **Docs:** new UI section under `docs/` (enable the listener, token/trust model,
  `rag ui`).
- **Out of scope (later phases):** remote/HTTPS/non-loopback exposure, TLS client certs /
  OIDC, knowledge-base management screens, and the `answer batch` review migration.