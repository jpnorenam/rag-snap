## Context

`ragd` today serves the `/1.0` REST API over a single unix-domain-socket listener, authenticated by `SO_PEERCRED` (root or `api.socket.group` members). This is the state on the current branch (`c4b4b2e`, the archived `add-local-rest-api` change).

The combined `feat/local-rest-api` branch adds two intertwined things on top of that base, in one large commit (`8505116`) plus follow-up fixes:

1. **Loopback surface** — a `127.0.0.1` TCP listener, transport-aware auth, and a bearer token, so the same API is reachable off a network socket.
2. **Browser UI** — a Next.js SPA (`ui/`) embedded via `go:embed` (`internal/webui/`), served under `/ui/`, plus a `rag ui` launch command and a Node build step.

This change replicates **only (1)** onto `feat/local-rest-api-loopback` so the API surface can be reviewed and merged independently. The UI (2) lands as a separate change that builds on this one.

Key constraints (see project `CLAUDE.md`):
- **Config is snapctl-only**, two-layer precedence (`package` seeded by the install hook, then `user` overrides that must reference an already-existing package key). New keys must be seeded in `snap/hooks/install` or a `user set` of them is rejected. Config-touching paths only run inside the snap.
- **Strict confinement** denies the daemon `chown`-ing files to an arbitrary group (already learned for the socket: it stays world-connectable and relies on peercred). The token file inherits the same restriction.
- **Secrets go in environment variables, never config.** The localhost token is a daemon-generated secret held in memory + an owner-only file, not a config key or an env var — consistent with that rule (config carries no secrets).

The loopback backend is entangled with UI code at exactly two seams: `server.go` imports `internal/webui` and its loopback router serves `/ui/` + `/ui/login`. The design below cuts precisely at those seams.

## Goals / Non-Goals

**Goals:**
- Serve the existing `/1.0` API over an opt-in loopback TCP listener, sharing one handler set with the unix socket.
- Authenticate loopback requests with a per-installation bearer token (header or cookie), constant-time compared; leave peercred on the unix socket untouched.
- Make the token discoverable by a trusted unix-socket client (`GET /1.0`) without relying on a group-readable file.
- Keep everything testable with no UI present: `go test ./internal/api/` and `curl` against the loopback port.
- Refuse any non-loopback bind, twice (config host check + post-bind address check).

**Non-Goals:**
- The `ui/` SPA, `internal/webui/` embedding, serving `/ui/`, the `/ui/login` cookie-handoff endpoint, and the `rag ui` command — all deferred to the UI change.
- The `make ui` target and the Node/`npm` snapcraft build step.
- Remote exposure: HTTPS, non-loopback binds, TLS client certs, OIDC. The token is explicitly the seam where those attach later.

## Decisions

### Decision 1 — Name the config keys `api.loopback.*`, not `api.ui.*`

The source branch calls the keys `api.ui.enabled` / `api.ui.address` because the listener existed to serve the UI. On a UI-free branch that naming is misleading: the capability is "a loopback API listener," and the UI is one future consumer. This change uses **`api.loopback.enabled`** and **`api.loopback.address`**, seeded as package keys in `snap/hooks/install` (default `false` / `127.0.0.1:0`).

- *Alternative — keep `api.ui.*`:* minimizes rename churn when the UI change merges, but bakes UI vocabulary into a general listener and reads oddly with no UI. Rejected.
- *Merge consideration:* when the UI change lands it will reference these keys; it should be rebased to `api.loopback.*` (or add a `ui` sub-section that reuses the loopback address). Called out in the UI change's migration, not here.
- Correspondingly, the Go `UIConfig` type / `ResolveUIConfig` become `LoopbackConfig` / `ResolveLoopbackConfig`, and the `Server.ui`/`uiToken`/`uiSrv` fields become `loopback`/`token`/`loopbackSrv`. The `configSummary` section is keyed `loopback` (not `ui`).

### Decision 2 — Two listeners, one shared handler set (`registerAPI`)

Keep the unix socket as the primary blocking serve loop; run the loopback listener in a goroutine. Both `http.Server`s use `ConnContext: connContext` so each connection is tagged with its transport. Extract the `/1.0/...` route registration into `registerAPI(mux)` and call it from both routers, so the two transports can never expose divergent endpoints. This mirrors the source implementation minus the UI router.

- The loopback router (`loopbackRoutes`) registers **only** `registerAPI` plus a discovery root (`GET /{$}` → `handleRoot`, same as the socket). It does **not** serve `/ui/`, does **not** import `internal/webui`, and does **not** register `/ui/login`.
- *Alternative — a single listener that toggles between transports:* impossible; unix vs TCP are distinct `net.Listener`s. *Alternative — duplicate route tables:* rejected, drifts over time.

### Decision 3 — Transport-aware auth at the single `authenticate` seam

Tag each accepted connection: `credConn` (unix, already exists) → `transportUnix` + captured peercred; a new `loopbackConn` wrapper → `transportLoopback`. `connContext` stamps the transport (and, for unix, the creds) onto the request context. `authenticate` branches: loopback → `authenticateToken`, otherwise → the existing peercred path. Default when unmarked is `transportUnix` (preserves historical single-listener behavior and keeps existing tests valid).

`authenticateToken` accepts the token via `Authorization: Bearer <token>` **or** the `rag_ui_token` cookie, compared with `crypto/subtle.ConstantTimeCompare`. The cookie path is retained (not UI-specific): a browser websocket upgrade cannot set an `Authorization` header, so any future websocket client on loopback needs the cookie. It costs nothing to keep and avoids a second migration when the UI lands.

### Decision 4 — Token: owner-only file for persistence, socket for delivery

Generate a 256-bit hex token (`crypto/rand`). Persist it `0600` under `$SNAP_COMMON/ragd/ui.token` (temp-dir fallback off-snap) purely so it survives restarts; reuse a non-empty existing file rather than regenerating. **Never** attempt to chown it group-readable — strict confinement forbids it and a fatal chown would crash-loop the daemon (this bug was already hit and fixed upstream in `a28bae1`).

Clients get the token **value** from `GET /1.0`'s `configSummary`, which is peercred-gated to root + access-group members — precisely the principals the token grants. So no client ever reads the file; the file is an implementation detail of restart survival.

- The token filename can stay `ui.token` for continuity, or be renamed `loopback.token`. Keeping `ui.token` avoids orphaning a token file across the future UI merge; either is fine. Recommend keeping `ui.token`.
- *Alternative — regenerate on every start:* breaks any client holding a token across a daemon restart/reload. Rejected.

### Decision 5 — Discovery via `apiclient.ServerInfo`, no `rag ui` command

Port the `apiclient` addition (`ServerInfo` fetching `GET /1.0`, with a `Loopback`/`UIInfo`-style struct exposing `enabled`, `address`, `url`, `token`) so a trusted CLI client can discover the OS-assigned port and token over the unix socket. Do **not** port `cmd/cli/basic/ui.go` or register a `ui` command in `cmd/cli/main.go` — that is browser-launch glue belonging to the UI change. A short `curl` recipe in the docs demonstrates the surface instead.

### Decision 6 — Packaging: `network-bind` yes, Node build no

Add the `network-bind` plug to the `ragd` app in `snapcraft.yaml` (with a comment that the bind is loopback-only, enforced in `loopback.go`) so the opt-in listener can bind its port. Do **not** add the `node/20/stable` build-snap, the `npm ci && npm run build` step, or the `internal/webui/dist` copy — there is no UI to build. Skip the `make ui` / `build-ragd`-with-UI Makefile additions; a plain `go build ./cmd/ragd` suffices with no embedded assets. `.gitignore`: bring over only the non-UI entries (drop `ui/`, `internal/webui/dist/*` rules).

## Risks / Trade-offs

- **[Divergent auth across transports]** A bug could let a loopback caller in without a token, or break peercred. → The single `authenticate` seam plus `loopback_test.go` covering valid-header, valid-cookie, missing, and invalid token over real TCP, and the existing peercred tests, guard both paths. Default-unix-when-unmarked keeps current tests meaningful.
- **[Accidental non-loopback exposure]** A misconfigured `api.loopback.address` could expose the API on a LAN. → Refuse the bind in two places (config host validation + post-bind `IP.IsLoopback()` check), make it a fatal startup error, and gate the whole listener behind opt-in `enabled=false`. The `network-bind` plug is required regardless but the code refuses non-loopback addresses.
- **[Token file under strict confinement]** Chowning would crash-loop the daemon. → Never chown; `0600` owner-only; deliver the value over the peercred socket. This is the already-proven fix, not a new bet.
- **[Config keys renamed vs the UI branch]** `api.loopback.*` here vs `api.ui.*` there means the UI branch needs a small rebase. → Documented as a merge note; the alternative (UI vocabulary on a general listener) is worse long-term.
- **[Token in the `GET /1.0` summary]** Returning a secret in a diagnostic endpoint. → Acceptable and intentional: that endpoint is peercred-gated to exactly the token's grantees; it is strictly less exposure than a group-readable file, and no lesser-privileged path returns it.

## Migration Plan

This is additive and opt-in; there is no data migration and the default behavior (unix socket only) is unchanged.

1. Land the code + spec deltas on `feat/local-rest-api-loopback`.
2. `snap/hooks/install` seeds `api.loopback.enabled=false` / `api.loopback.address=127.0.0.1:0` as package keys. On an **existing** install these keys won't exist until the install/refresh hook runs; a maintainer setting them before then must use `snapctl set --package` (per existing ragd operational notes), because a `user set` of an unseeded key is rejected.
3. Build/install the snap, `sudo rag set api.loopback.enabled=true`, restart `ragd`, and verify with `curl -H "Authorization: Bearer $(…GET /1.0 token…)" http://127.0.0.1:PORT/1.0`.
4. **Rollback:** set `api.loopback.enabled=false` and restart `ragd`; the daemon returns to unix-socket-only. No persistent state beyond the owner-only token file, which is inert while disabled.

## Open Questions

- Keep the token filename `ui.token`, or rename to `loopback.token`? (Recommend keep, for continuity across the UI merge.)
- Keep the `rag_ui_token` cookie name, or rename to `rag_loopback_token`? (Recommend keep; renaming later would break an in-flight UI branch for no functional gain.)
- Should a minimal `rag` CLI affordance (e.g. `rag status` printing the loopback URL) ship here, or wait for the UI change's `rag ui`? (Leaning: docs-only `curl` here; no new command.)