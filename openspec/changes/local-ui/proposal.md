# Local UI

## Why

The new `ragd` daemon gives `rag-cli` a programmatic REST API, but the only client
today is the CLI. Driving knowledge bases and chat still means a terminal. We want a
local browser UI — starting with interactive chat — that talks to that API, so users
get the RAG experience without the REPL. The existing `rag-snap-ui` is a Firebase-backed
*reviewer* for `answer batch` JSON output, not an API client; we replicate its framework
and visual style here but build a real client of the local `ragd` API. This first phase
keeps everything local (loopback only); a later phase can expose the same surface as a
web application.

## What Changes

- **Add a browser UI** (Next.js App Router, React + TypeScript, Canonical Vanilla
  Framework) built as a static SPA export and **embedded into the `ragd` binary** via
  `go:embed`. The daemon serves it same-origin with the API, so there is no CORS layer
  and the chat websocket is reachable directly.
- **Add a loopback HTTP listener to `ragd`** (`127.0.0.1` only, opt-in via config). A
  browser cannot dial the existing unix socket, so the daemon gains a local TCP listener
  that serves the embedded UI under `/ui/` and the existing `/1.0/...` API on the same
  mux. Remote/HTTPS/OIDC exposure remains **deferred** — the listener binds loopback only.
- **Add a localhost bearer-token auth path** for the TCP listener. `SO_PEERCRED` cannot
  authenticate TCP peers, so connections over the loopback listener are authenticated by
  a daemon-generated token (stored group-readable under `$SNAP_COMMON`). The unix socket
  keeps its peercred model unchanged. This token is the local trust boundary now and is
  the seam where TLS client certs / OIDC slot in when the surface goes remote.
- **Add a `rag ui` CLI command** that ensures the listener is running, reads the token,
  and opens the browser at the UI URL with the token applied — a one-command launch.
- **Chat-first feature scope.** The initial UI delivers the interactive chat vertical
  slice: start a session (`POST /1.0/chat`), connect the returned websocket operation,
  stream tokens and `<think>` blocks, and switch active knowledge bases mid-session.
  Knowledge-base management and migrating the `answer batch` review experience are
  explicitly later phases (the UI is structured to grow into them; the `QAItem` /
  `ParsedQAFile` type contract from `rag-snap-ui` is preserved for that migration).

### External services touched

- No new external services. The UI is a client of the existing `ragd` API, which already
  owns the OpenSearch / inference / Tika clients. Chat streaming reuses the existing
  inference path unchanged.

### New config keys (snapctl, package-scoped with user override)

- `api.ui.enabled` — whether `ragd` opens the loopback listener and serves the UI
  (default off; opt-in like the daemon itself).
- `api.ui.address` — loopback bind address for the listener (default `127.0.0.1:0`,
  i.e. an OS-assigned port discovered at runtime; never a non-loopback address in this
  change).

### User-facing surface

- New `rag ui` command (launch/inspect the local UI).
- New snap-served UI at the loopback listener under `/ui/`.
- Docs: a UI section in `docs/` covering enabling the listener, the token model, and the
  `rag ui` launch flow.

## Capabilities

### New Capabilities

- `local-ui-app`: the embedded browser UI itself — its framework/build (Next.js static
  export + Vanilla Framework), the API client layer that targets `${ROOT_PATH}/1.0/...`
  same-origin, and the chat screen (session start, websocket streaming, active-KB
  switching).
- `rest-api-ui-serving`: the `ragd` loopback HTTP listener, same-origin serving of the
  embedded UI under `/ui/` alongside the `/1.0` API, the SPA fallback to `index.html`,
  and the `rag ui` launch command.
- `rest-api-localhost-auth`: localhost bearer-token authentication for the loopback
  listener (token generation, storage, and the auth check), distinct from and additive
  to the unix-socket peercred model.

### Modified Capabilities

<!-- None at spec level. The existing rest-api-server, rest-api-chat, and
     rest-api-operations capabilities are reused unchanged; this change adds the UI,
     a loopback transport, and an additive auth path, none of which alter existing
     endpoint behaviour or the unix-socket contract. -->

## Impact

- **Affected code:**
  - New `ui/` (Next.js + TypeScript + Vanilla Framework SPA, static export to `ui/out`).
  - New `internal/webui/` — `go:embed` of the built UI and the static/SPA-fallback handler.
  - `internal/api/` — loopback listener setup (`socket.go`/`server.go`), token auth in
    `auth.go`, route wiring for `/ui/` and `GET /` redirect.
  - `cmd/cli/` — new `rag ui` command; config keys in the storage/defaults layer.
  - `cmd/ragd/main.go` — start the loopback listener when `api.ui.enabled`.
- **Snap/build:**
  - `snap/snapcraft.yaml` — a build step (or part) that runs the Node/Next.js static
    export so `ragd` can embed it; the `ragd` app may gain the loopback listener.
    Loopback-only `network-bind` is required for the TCP listener.
  - `Makefile` — targets to build the UI and embed it before the Go build.
- **Docs:** new UI section; note the localhost-token trust boundary.
- **Out of scope (later phases):** remote/HTTPS exposure, TLS client certs / OIDC,
  knowledge-base management screens, and the `answer batch` review migration.
