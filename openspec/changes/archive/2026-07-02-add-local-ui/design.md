# Local UI — Design

## Context

This branch already added `ragd`'s loopback surface: an opt-in TCP listener bound to
`127.0.0.1` (`api.loopback.enabled` / `api.loopback.address`, default `127.0.0.1:0`),
authenticated by a daemon-generated localhost bearer token stored owner-only under
`$SNAP_COMMON` (`internal/api/loopback.go`, `token.go`, `auth.go`). `auth.go` is already
transport-aware: unix connections use `SO_PEERCRED`, loopback connections use the token.
The daemon already exposes the loopback URL + token to trusted unix-socket clients via the
`GET /1.0` config summary (`apiclient.LoopbackInfo{Enabled, Address, URL, Token, TokenPath}`).

What is missing is the browser client and the serving of it. `loopbackRoutes()` in
`internal/api/server.go` deliberately serves only `/1.0/...` plus the discovery root, with a
comment that `/ui/` assets and the `GET /` redirect "belong to the deferred UI change." Chat
is already a websocket-class async operation: `POST /1.0/chat` returns an operation whose
metadata carries a websocket URL plus a one-time `secret`; the client dials
`GET /1.0/operations/{id}/websocket?secret=...` and exchanges `prompt` / `set-active-kbs`
control frames for streamed `token` / `think` / `done` frames.

A complete implementation of this UI already exists on the `feat/local-rest-api` reference
branch, where it was bundled into one `local-ui` change **together with** the loopback
listener and token auth. Because those two pieces already exist and are specced on this
branch (`rest-api-loopback`, `rest-api-localhost-auth`), this change is a narrower port:
only the UI app, its embedding, same-origin serving, the token handoff, and the launch
command. Two prior-art references frame the original design and still apply:

- **`rag-snap-ui`** — Next.js App Router, React, TypeScript, static export
  (`output: 'export'`), Canonical Vanilla Framework (Sass), flat `components/` + `lib/`,
  dark-mode via an `is-dark` class. It is *not* an API client (it reviews `answer batch`
  JSON via Firebase); we take its **framework and style**, not its data layer.
- **LXD + lxd-ui** — the daemon-served-UI pattern: lxd-ui calls `${ROOT_PATH}/1.0/...`
  same-origin, the daemon serves the static UI under `/ui/` on the same mux with an
  `index.html` SPA fallback, and a browser reaches it only over a network listener (never
  the unix socket). This is exactly the shape the loopback listener already provides.

## Goals / Non-Goals

**Goals:**

- Port the reference UI onto this branch **faithfully** — no functional or visual change
  from `feat/local-rest-api`.
- Serve the embedded UI same-origin on the existing loopback listener; deliver the chat
  vertical slice end-to-end (start session → websocket → streamed tokens → mid-session KB
  switch).
- Reuse the existing loopback listener and localhost token auth unchanged; add only the UI
  serving, token handoff, and launch command on top.
- Keep the change local-only and preserve the unix-socket + peercred contract unchanged.

**Non-Goals:**

- Re-implementing or altering the loopback listener or token generation/validation (already
  done on this branch).
- Remote/non-loopback exposure, HTTPS/TLS, client certs, OIDC (still deferred).
- Knowledge-base management screens and the `answer batch` review migration (later phases;
  only the data-type contract is preserved).
- Changing any existing `/1.0/...` endpoint behaviour, or adding `/ui/` to the unix socket.

## Decisions

### Decision 1 — Reuse the existing loopback listener; add serving only to `loopbackRoutes()`

The reference change introduced the loopback listener as part of the UI work. Here it
already exists, so the port collapses to extending `loopbackRoutes()` in
`internal/api/server.go` with: a `/ui/` static handler (from `internal/webui`), a
`GET /` → `/ui/` redirect, and a `/ui/login` handoff. The unix-socket `routes()` is left
untouched — `/ui/` lives only on the loopback mux, same-origin with `/1.0/...`, so fetch is
CORS-free and the chat websocket is a native same-origin connection.

- **Why not re-derive from the reference `rest-api-ui-serving` spec verbatim:** that spec's
  "Loopback HTTP listener" requirement is already satisfied on this branch by
  `rest-api-loopback`. Re-adding it would duplicate/conflict with existing specs. This
  change's `rest-api-ui-serving` delta therefore omits the listener requirement and covers
  only serving, fallback, handoff, and the command.

### Decision 2 — Embed the static UI in the `ragd` binary via `go:embed`

The UI lives under `ui/`, builds to a static export, and is embedded into `ragd` through a
new `internal/webui/` package (`//go:embed all:dist`), served by an `http.FileServer` over
the embedded FS with an `index.html` SPA fallback (LXD's `uiHTTPDir` pattern). A committed
placeholder `internal/webui/dist/index.html` ensures `go build` never fails on a missing
embed dir before the UI is built. This matches how the project already bundles
Tika/elasticdump: one self-contained binary, no runtime path wiring — a good fit for strict
confinement. Porting `internal/webui/webui.go` from the reference branch is nearly verbatim.

- **Alternative considered — `LXD_UI` dir-on-disk:** rejected; an on-disk asset path adds
  stage layout to keep in sync and does not suit a strictly-confined single-binary snap.

### Decision 3 — Replicate `rag-snap-ui`'s stack; a same-origin REST client, not Firebase

Next.js App Router + `output: 'export'` + React + TypeScript + Vanilla Framework (Sass),
flat `components/` + `lib/`, dark-mode toggle. The data layer is a `lib/api/` with resource
modules (`server.ts`, `chat.ts`, `knowledge.ts`) using `fetch` against `${ROOT_PATH}/1.0/...`
(default empty → same-origin) plus an `envelope.ts` helper that unwraps the
`sync`/`async`/`error` envelope (the analogue of lxd-ui's `handleResponse`). The
`QAItem`/`ParsedQAFile` types carry over verbatim, unwired, for the later review migration.
Chat mechanics: `POST /1.0/chat` → read `metadata.websocket.url` + `secret` → open a
`WebSocket` to `.../websocket?secret=...` → send `{type:"prompt",...}` /
`{type:"set-active-kbs",bases:[...]}`, render incoming `token`/`think`/`done` frames. This
maps 1:1 onto the existing handler; no API change. Plain `fetch` (no react-query) suffices
for the chat slice.

### Decision 4 — Token handoff via `/ui/login`, adapted to this branch's `api.loopback.*`

The reference `rag ui` reads the token over the trusted unix socket and opens
`/ui/login?token=...`, which sets a loopback-scoped cookie and redirects into the SPA,
keeping the token out of the SPA's JS and the persistent URL. That mechanism is preserved.
The port must reconcile two naming differences from the reference branch:

- Config keys are `api.loopback.enabled` / `api.loopback.address` here, **not** `api.ui.*`.
  User-facing "enable it" guidance and the disabled-path message must use `api.loopback.*`.
- The daemon exposes the loopback info as `info.Config.Loopback` (`apiclient.LoopbackInfo`),
  **not** `info.Config.UI`. The ported `cmd/cli/basic/ui.go` reads `ui := info.Config.Loopback`
  and its `Enabled` / `URL` / `Token` fields (already present on this branch).

The `/ui/login` handler itself is new on this branch and is added alongside the `/ui/`
serving in `loopbackRoutes()`. Static `/ui/` assets stay unauthenticated (like LXD's
`AllowUntrusted` UI routes); only `/1.0/...` requires the token, enforced by the existing
transport-aware `auth.go`.

### Decision 5 — Chat-first scoping inside a structure that grows

Ship a chat screen plus a knowledge-base list (needed for active-KB selection). KB
management CRUD and the answer-review experience are deferred, but the `lib/api/` modules and
component layout anticipate them so later phases add screens without reshaping the app.

## Risks / Trade-offs

- **Config/field naming drift from the reference branch** (`api.ui.*` / `Config.UI` there vs.
  `api.loopback.*` / `Config.Loopback` here) → Mitigate: the port explicitly rewrites the
  `rag ui` command and any user-facing strings to this branch's keys; a build + `rag ui`
  smoke test on the installed snap catches a missed reference.
- **`/ui/login` is a new endpoint not present in the loopback code yet** → Mitigate: it is
  additive and unauthenticated by design (it *presents* the token); it only sets a
  loopback-scoped cookie and redirects. `/1.0/...` enforcement is unchanged.
- **Token leakage via URL/referrer/history** → Mitigate: the token appears only on the
  one-shot `/ui/login` request; the handler moves it into a cookie and redirects to a
  token-free `/ui/` URL. Prefer not to persist it in `location`.
- **Build complexity: Node toolchain now required to build the snap** → Mitigate: gate the
  UI build behind a `make ui` target / snapcraft build step; the project already vendors a
  Node tarball for elasticdump, so Node-in-build is precedented. Embedded assets mean no Node
  at runtime.
- **`go:embed` requires assets present at compile time** → Mitigate: committed placeholder
  `internal/webui/dist/index.html`; `make ui` and the snapcraft step run the export into
  `dist/` **before** the Go build.
- **Websocket secret is one-time and per-operation** → the browser must connect promptly
  after `POST /1.0/chat`; reconnect by starting a fresh session. Acceptable for chat.
- **Reopening exposure concern** → not applicable in this change: the listener already
  exists, is opt-in (default off), loopback-only with a hard non-loopback refusal, and the
  unix socket remains the only default surface. This change adds no new bind.

## Migration Plan

1. Scaffold `ui/` (Next.js static export + Vanilla Framework), `lib/api/` client + envelope
   helper + token-injection point, `QAItem`/`ParsedQAFile` types, dark-mode shell. (Port
   from the reference branch's `ui/` verbatim.)
2. Add `internal/webui/` (`go:embed` + SPA-fallback handler + committed placeholder). (Port
   `webui.go` verbatim.)
3. Extend `loopbackRoutes()` in `internal/api/server.go`: mount `/ui/` (unauthenticated),
   add `GET /` → `/ui/` redirect and the `/ui/login` token-handoff handler.
4. Build the chat screen against `POST /1.0/chat` + the websocket; add the KB list +
   active-KB switch.
5. Add `rag ui` (`cmd/cli/basic/ui.go`), registered in `cmd/cli/main.go` in the fixed order;
   adapt it to `info.Config.Loopback` and `api.loopback.*`.
6. Wire `make ui` + snapcraft build step to produce `internal/webui/dist/` before the Go
   build; confirm the `ragd` app's existing loopback `network-bind` is present.
7. Docs: UI section under `docs/` (enable `api.loopback.enabled`, token model, `rag ui`).

**Rollback:** the listener is opt-in; with `api.loopback.enabled=false` the daemon behaves
exactly as before this change. Reverting the snap build step drops the embedded UI with no
effect on the unix-socket API.

## Open Questions

- **`/ui/login` credential form:** loopback-scoped cookie (keeps the token out of JS —
  preferred, matches the reference) vs. a URL fragment the SPA consumes into `sessionStorage`
  and scrubs. Decide during the port; the spec allows either.
- **Fixed port vs. OS-assigned (`:0`):** default stays `127.0.0.1:0` (safe, no collisions);
  `rag ui` discovers the resolved port from `LoopbackInfo.URL`. A fixed default is friendlier
  for bookmarks but out of scope here.
- **Snap confinement for the loopback bind:** confirm the `ragd` app already plugs
  `network-bind` on this branch (it should, from the loopback change) and that the snapcraft
  Node build-snap is available in CI.