# Local UI — Design

## Context

The `feat/local-rest-api` branch added `ragd`: a daemon that serves a versioned REST API
(`GET /`, `/1.0/...`) over a **unix socket only**, authenticated by `SO_PEERCRED` group
membership. The HTTPS/network listener and remote auth (TLS certs / tokens / OIDC) were
**explicitly deferred** to a follow-up. Chat is already a websocket-class async operation:
`POST /1.0/chat` returns an operation whose metadata carries a websocket URL plus a one-time
`secret`; the client dials `GET /1.0/operations/{id}/websocket?secret=...` and exchanges
`prompt` / `set-active-kbs` control frames for streamed `token` / `think` / `done` frames.

We want a local browser UI for this API, chat-first. Two pieces of prior art frame the design:

- **`rag-snap-ui`** (the existing UI) — Next.js 16 App Router, React 18, TypeScript, static
  export (`output: 'export'`), **Canonical Vanilla Framework** (Sass), flat `components/` +
  `lib/` layout, dark-mode via an `is-dark` class. Crucially it is *not* an API client: it
  reviews `answer batch` JSON and persists through Firebase. We take its **framework and
  style**, not its data layer.
- **LXD + lxd-ui** — the canonical pattern for a daemon-served UI. lxd-ui (React/Vite/
  react-query/Vanilla) calls `${ROOT_PATH}/1.0/...` **same-origin**; the LXD daemon serves
  the static UI under `/ui/` on the *same mux* as the API, with an `index.html` SPA fallback.
  The decisive finding: **the UI is reachable only over LXD's HTTPS network listener, never
  the unix socket** — a browser cannot dial a unix socket. LXD requires the operator to
  enable `core.https_address` first; the UI then authenticates same-origin via the TLS client
  cert established for that listener.

The constraint that drives this design: **our API is unix-socket-only, and a browser cannot
reach a unix socket.** Something must bridge browser → API.

## Goals / Non-Goals

**Goals:**

- Serve a browser UI for the local `ragd` API, reachable from a normal browser.
- Deliver the interactive **chat** vertical slice end-to-end (start session → websocket →
  streamed tokens → mid-session KB switch).
- Replicate `rag-snap-ui`'s framework (Next.js static export) and Vanilla Framework style.
- Keep the change local-only and preserve the existing unix-socket + peercred contract
  unchanged.
- Choose an architecture whose seams grow cleanly into the later web-exposed phase.

**Non-Goals:**

- Remote/non-loopback exposure, HTTPS/TLS, client certs, OIDC (still deferred).
- Knowledge-base management screens and the `answer batch` review migration (later phases;
  only the data-type contract is preserved).
- Changing any existing `/1.0/...` endpoint behaviour or the unix-socket auth model.
- Server-side rendering or a separate Node.js runtime in production.

## Decisions

### Decision 1 — Bridge the browser with a loopback TCP listener on `ragd` (same-origin), not a CLI proxy

`ragd` gains an opt-in HTTP listener bound to **loopback only** (`api.ui.enabled`,
`api.ui.address`, default `127.0.0.1:0` → OS-assigned port). It serves the embedded UI under
`/ui/` and the existing `/1.0/...` API on the **same mux/origin**, exactly like LXD. `GET /`
redirects to `/ui/`.

- **Why over a `rag ui` user-space proxy** (a CLI process that reverse-proxies browser→unix
  socket, where peercred still works because the proxy runs as the user): the proxy keeps the
  daemon untouched, but it is a throwaway. The stated trajectory is "local now, web later."
  The loopback listener *is* the component that later becomes the web listener — go remote by
  changing the bind address and adding TLS/OIDC, with the UI, routing, and auth seam already
  in place. The proxy would be deleted at that step. Same-origin serving also gives us
  CORS-free fetch and a native same-origin websocket for chat for free.
- **Trade-off:** this reopens a network listener that the REST API change deferred. We contain
  it: opt-in (off by default), loopback-only with a hard refusal to bind non-loopback in this
  change, and the unix socket remains the only default surface. Remote exposure remains a
  conscious, separate future decision.

### Decision 2 — Authenticate loopback with a daemon-generated localhost bearer token

`SO_PEERCRED` is a property of unix-socket connections; it is unavailable for TCP. So loopback
`/1.0/...` requests authenticate with a high-entropy token the daemon generates and stores in
`$SNAP_COMMON` readable by the configured access group (the same trust boundary as the
socket). `auth.go` gains a transport-aware check: unix connections → peercred (unchanged);
loopback connections → token.

- **Why a token, not "trust all loopback":** any local user (and, via DNS-rebinding-class
  tricks, potentially web content) can reach a loopback port. Loopback is not an
  authentication boundary. A token scoped to the access group keeps parity with the socket's
  trust model.
- **Why a static token now, not TLS certs/OIDC:** those belong to the deferred remote phase.
  The token is the minimal local secret and is the explicit seam where cert/OIDC verification
  attaches later — `auth.go` already branches per transport.
- **Static UI assets under `/ui/` are unauthenticated** (like LXD's `AllowUntrusted` UI
  routes) so the shell can load; only `/1.0/...` requires the token.
- **Token delivery:** `rag ui` reads the token (group-readable) and hands it to the browser at
  launch (e.g. a one-shot localhost handoff or a URL the UI consumes into `sessionStorage` and
  scrubs from the address bar). The token is **never** baked into the embedded assets — it is
  per-installation. Exact handoff mechanism is an open question (below).

### Decision 3 — Embed the static UI in the `ragd` binary via `go:embed`

The UI lives in this repo under `ui/`, builds to a static export, and is embedded into `ragd`
through a new `internal/webui/` package (`//go:embed`), served by an `http.FileServer` over
the embedded FS with an `index.html` SPA fallback (LXD's `uiHTTPDir` pattern).

- **Why over LXD's `LXD_UI`-dir-on-disk approach:** a single self-contained binary fits a
  strictly-confined snap and matches how this project already bundles Tika/elasticdump — no
  runtime path wiring, no extra stage layout to keep in sync. `go:embed` is the more
  self-contained of the two equally-valid options the research surfaced.
- **Why keep the UI in this repo, not `rag-snap-ui`:** the user wants `rag-snap-ui` left
  untouched and all UI artifacts created here. One repo, one snap, one CI path.
- **Build wiring:** a snapcraft build step / Makefile target runs the Node/Next.js static
  export into `ui/out` *before* the Go build so the embed has content. CI builds Node first.

### Decision 4 — Replicate `rag-snap-ui`'s stack; swap Firebase for a same-origin REST client

Next.js App Router + `output: 'export'` + React + TypeScript + Vanilla Framework (Sass),
flat `components/` + `lib/`, dark-mode toggle. The data layer is rebuilt: a `lib/api/` with
resource modules (`server.ts`, `chat.ts`, `knowledge.ts`) using `fetch` against
`${ROOT_PATH}/1.0/...`, plus a helper that unwraps the `sync`/`async`/`error` envelope (the
analogue of lxd-ui's `handleResponse`). `ROOT_PATH` defaults to empty (same-origin). The
`QAItem`/`ParsedQAFile` types are carried over verbatim for the later review migration but
left unwired.

- **Data fetching:** plain `fetch` + small hooks for the slice; react-query is optional and
  not required for chat (which is websocket-driven). We keep dependencies close to
  `rag-snap-ui` to honor "replicate."
- **Chat mechanics in the browser:** `POST /1.0/chat` → read `metadata.websocket.url` +
  `secret` → open a `WebSocket` to that URL with `?secret=` → send `{type:"prompt",...}` /
  `{type:"set-active-kbs",bases:[...]}`, render incoming `token`/`think`/`done` frames. This
  maps 1:1 onto the existing handler; no API change needed.

### Decision 5 — Chat-first scoping inside a structure that grows

The UI ships with a chat screen and a knowledge-base list (needed for active-KB selection).
Knowledge-management CRUD screens and the answer-review experience are deferred but the
`lib/api/` modules and component layout anticipate them, so later phases add screens without
reshaping the app.

## Risks / Trade-offs

- **Reopening a network listener after deferring it** → Mitigate: opt-in (default off),
  loopback-only with explicit non-loopback refusal, unchanged unix-socket default, and a token
  gate on `/1.0/...`. Remote exposure stays a separate future decision.
- **Loopback port is reachable by any local user / browser content (DNS rebinding)** →
  Mitigate: token-gate all `/1.0/...`; consider checking `Origin`/`Host` and a fixed loopback
  host. Static assets are harmless without a token.
- **Token leakage via URL/referrer/history if passed in the address bar** → Mitigate: prefer a
  non-URL handoff; if a URL is used, the UI consumes it into `sessionStorage` and immediately
  strips it from the location. Resolve in Open Questions.
- **Build complexity: Node toolchain now required to build the snap** → Mitigate: gate the UI
  build behind a make target / snapcraft part; the project already vendors a Node tarball for
  elasticdump, so Node-in-build is precedented. Embedded assets mean no runtime Node.
- **Websocket secret is one-time and per-operation** → the browser must connect promptly after
  `POST /1.0/chat`; handle reconnect by starting a fresh session. Acceptable for chat.
- **`go:embed` requires assets present at compile time** → Mitigate: a committed placeholder
  `ui/out` (or build-ordering enforcement) so `go build` never fails on a missing embed dir.

## Migration Plan

1. Scaffold `ui/` (Next.js static export + Vanilla Framework), `lib/api/` client + envelope
   helper, `QAItem`/`ParsedQAFile` types, dark-mode shell.
2. Add `internal/webui/` (`go:embed` + SPA-fallback handler).
3. Add the loopback listener + `/ui/` and `/` routes to `internal/api/`; gate on
   `api.ui.enabled` / `api.ui.address` in `cmd/ragd`.
4. Add localhost token generation/storage + transport-aware auth in `auth.go`.
5. Build the chat screen against `POST /1.0/chat` + the websocket; add the KB list +
   active-KB switch.
6. Add `rag ui` (ensure listener, read token, open browser) and config defaults/keys.
7. Wire snapcraft/Makefile to build the UI before the Go build; add loopback `network-bind`.
8. Docs: UI section (enable, token model, `rag ui`).

**Rollback:** the listener is opt-in; with `api.ui.enabled=false` the daemon behaves exactly
as before this change. Reverting the snap parts drops the embedded UI with no effect on the
unix-socket API.

## Open Questions

- **Token handoff mechanism:** localhost loopback redirect from `rag ui` (like the gdrive
  OAuth loopback) vs. a URL fragment the UI scrubs vs. a short-lived cookie set by the daemon.
  Leaning toward a daemon-set cookie scoped to the loopback origin to keep the token out of JS
  and URLs.
- **Fixed port vs. OS-assigned (`:0`):** a fixed default port is friendlier for bookmarks but
  risks collisions; `:0` is safe but requires `rag ui` to discover the port. Default `:0`,
  allow override.
- **react-query or plain fetch** for the non-chat data calls — decide when KB screens land;
  chat needs neither.
- **Snap confinement for `network-bind` on loopback** — confirm the interface/plug wiring
  needed for a strictly-confined daemon to bind a loopback TCP port.
