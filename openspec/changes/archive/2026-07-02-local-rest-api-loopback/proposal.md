## Why

The `ragd` daemon exposes the `/1.0` REST API only over a unix domain socket, authenticated by `SO_PEERCRED`. A unix socket is unreachable by a browser and awkward for any non-CLI local client (scripts, a future UI, another process). We want `ragd` to also serve the same API over an opt-in **loopback TCP listener** authenticated by a per-installation bearer token — a local network surface that is testable on its own with `curl`.

This change deliberately carries **only the loopback (local REST API) surface**. It is split out from the combined `feat/local-rest-api` branch, whose browser UI (the `ui/` SPA and `internal/webui/` embedding) lands separately. The loopback listener is the backend seam the UI later plugs into, so shipping it first lets the API surface be reviewed and merged without the front-end.

## What Changes

- **Add an opt-in loopback HTTP listener to `ragd`.** In addition to the unix socket, the daemon can open a `127.0.0.1` TCP listener that serves the existing `/1.0/...` API on a shared mux. The listener **refuses any non-loopback bind** (defence-in-depth on both the configured host and the resolved address). The unix socket remains the only default surface.
- **Add localhost bearer-token authentication.** `SO_PEERCRED` is unavailable for TCP, so loopback connections authenticate by a daemon-generated high-entropy token, presented as `Authorization: Bearer <token>` (or the `rag_ui_token` cookie, for websocket upgrades that cannot set headers). Comparison is constant-time. The unix socket keeps peercred unchanged.
- **Make authentication transport-aware.** A single auth seam routes unix connections to the peercred check and loopback connections to the token check, tagged per-connection via `http.Server.ConnContext`.
- **Persist and discover the token.** The token is written owner-only (`0600`) under `$SNAP_COMMON` for restart survival. Its **value** is never read off disk by clients: the daemon returns it in the `GET /1.0` config summary, which is already peercred-gated to root + access-group members — exactly the principals the token grants. This avoids the strict-confinement chown restriction on group-readable files.
- **Wire config + packaging.** New opt-in config keys, install-hook seeding, and the `network-bind` plug for the loopback port.
- **NOT in scope (deferred to the UI change):** the `ui/` SPA, `internal/webui/` `go:embed`, serving `/ui/`, the `rag ui` browser-launch command, the `make ui` / Node snapcraft build step, and remote/HTTPS/OIDC exposure.

### External services touched

None. This is purely `ragd`'s own listener/auth surface; the OpenSearch, inference, and Tika clients and the `/1.0` handlers are reused unchanged across both transports.

### New config keys (snapctl, package-scoped with user override)

- `api.loopback.enabled` — whether `ragd` opens the loopback listener (default `false`, opt-in like the daemon itself).
- `api.loopback.address` — loopback bind address (default `127.0.0.1:0`, i.e. an OS-assigned port discovered at runtime; a non-loopback value is refused).

(The `feat/local-rest-api` branch named these `api.ui.*`; see design.md Decision 1 for why this branch uses `api.loopback.*`.)

### User-facing surface

- No new CLI command. The loopback URL and token are discoverable by a trusted client over the existing unix socket via `GET /1.0` (`config.loopback`).
- **Docs:** a new `docs/rest-api-loopback.md` (or a section in `docs/rest-api.md`) covering enabling the listener, the token model, and a `curl` example. The `rag ui` launch flow is documented by the separate UI change.

## Capabilities

### New Capabilities
- `rest-api-loopback`: the opt-in loopback TCP listener that serves the `/1.0` API same-mux with the unix socket and refuses non-loopback binds.
- `rest-api-localhost-auth`: the per-installation bearer-token authentication path for the loopback transport (generation, persistence, presentation, and trusted-client discovery).

### Modified Capabilities
- `rest-api-server`: the "no TCP/HTTPS listener is opened" requirement is relaxed to permit the opt-in loopback listener; the single-transport authentication requirement becomes transport-aware.

## Impact

- **Code (new):** `internal/api/loopback.go`, `internal/api/token.go`, `internal/api/loopback_test.go`.
- **Code (modified):** `internal/api/config.go` (`UIConfig`→loopback config + `ResolveUIConfig`), `internal/api/auth.go` (transport tagging + `authenticateToken`), `internal/api/server.go` (second listener, `registerAPI` split, `configSummary` loopback section — **without** the `internal/webui` import or `/ui/` routes), `cmd/ragd/main.go` (wire config into `Options`), `internal/apiclient/resources.go` (`ServerInfo`/loopback info struct for discovery).
- **Packaging:** `snap/hooks/install` (seed the two keys), `snap/snapcraft.yaml` (`network-bind` plug + rationale; **no** Node/`npm` build step), `.gitignore` (only non-UI entries).
- **Docs:** new loopback REST section.
- **Dependencies:** none new. Uses `crypto/rand`, `crypto/subtle`, `net`, `net/http` from stdlib and the existing `go-snapctl/env`.
- **Testability:** `internal/api/loopback_test.go` exercises the listener and token auth end-to-end over real TCP with no UI present, so the surface is verifiable via `go test ./internal/api/` and `curl`.