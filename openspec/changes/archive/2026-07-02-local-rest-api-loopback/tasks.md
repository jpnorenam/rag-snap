## 1. Config: loopback keys and resolver

- [x] 1.1 In `internal/api/config.go`, add `confAPILoopbackEnabled = "api.loopback.enabled"` and `confAPILoopbackAddress = "api.loopback.address"` constants and a `defaultLoopbackAddress = "127.0.0.1:0"` default.
- [x] 1.2 Add a `LoopbackConfig{ Enabled bool; Address string }` type and a `ResolveLoopbackConfig(ctx *common.Context) LoopbackConfig` that reads the two keys, applying defaults when unset (reuse `getBool`).
- [x] 1.3 Confirm the resolver never returns an error for a missing key (opt-in default off), consistent with `ResolveSocketConfig`.

## 2. Loopback listener

- [x] 2.1 Create `internal/api/loopback.go` with `loopbackConn`/`loopbackListener` wrappers that tag every accepted connection, and `listenLoopback(cfg LoopbackConfig)` that validates the host and binds a TCP listener.
- [x] 2.2 Implement `requireLoopbackHost(host string)`: reject empty host (all-interfaces), accept `localhost`, accept loopback IPs, reject non-loopback IPs — each with a clear error.
- [x] 2.3 Add the post-bind defence-in-depth check: if the resolved `*net.TCPAddr` is not loopback, close the listener and error.

## 3. Token generation and persistence

- [x] 3.1 Create `internal/api/token.go` with `localhostToken()` returning `(path, value, error)`: reuse a non-empty existing file, otherwise generate a 256-bit hex token via `crypto/rand` and write it `0600`.
- [x] 3.2 Implement `tokenPath()` resolving `$SNAP_COMMON/ragd/ui.token` with an `os.TempDir()` fallback off-snap; `MkdirAll` the parent `0755`.
- [x] 3.3 Ensure NO chown / group-permission logic exists (strict confinement forbids it; a fatal chown crash-loops the daemon — see design Decision 4).

## 4. Transport-aware authentication

- [x] 4.1 In `internal/api/auth.go`, add `transportContextKey`, `transportKind` (`transportUnix`/`transportLoopback`), and `transportFromRequest` defaulting to `transportUnix` when unmarked.
- [x] 4.2 Update `connContext` to switch on the connection type: `*credConn` → stamp `transportUnix` + peercred; `*loopbackConn` → stamp `transportLoopback`.
- [x] 4.3 In `authenticate`, branch to `authenticateToken` when the transport is loopback; leave the peercred path unchanged for unix.
- [x] 4.4 Add `authenticateToken`: read the token from `Authorization: Bearer` or the `rag_ui_token` cookie, reject empty/invalid, and compare with `crypto/subtle.ConstantTimeCompare`. Add the `uiTokenCookie`/`bearerToken` helpers.

## 5. Server wiring (loopback listener, no UI serving)

- [x] 5.1 In `internal/api/server.go`, add `loopback LoopbackConfig`, `token string`, `loopbackSrv *http.Server`, `loopbackListenAddr string` fields and the `UI`→`Loopback` field on `Options`; set it in `New`.
- [x] 5.2 In `Serve`, when `loopback.Enabled`, call `startLoopback()` to open the listener (fatal on error, closing the unix listener), serve it in a goroutine, and shut it down on context cancel alongside `httpSrv`.
- [x] 5.3 Implement `startLoopback()`: ensure the token via `localhostToken()`, build the loopback router, call `listenLoopback`, create the `http.Server` with `ConnContext: connContext`, record the resolved address, and log it.
- [x] 5.4 Extract `registerAPI(mux)` from `routes()` (all `/1.0/...` handlers) and call it from both `routes()` and the new `loopbackRoutes()`.
- [x] 5.5 Implement `loopbackRoutes()` to register ONLY the discovery root (`GET /{$}` → `handleRoot`) plus `registerAPI` — do NOT import `internal/webui`, do NOT serve `/ui/`, do NOT add `/ui/login` or a root→`/ui/` redirect.
- [x] 5.6 Extend `configSummary` with a `loopback` section (`enabled`, and when enabled `address`/`url`/`token`/`token_path`); keep it out of the summary when disabled.

## 6. Daemon and client wiring

- [x] 6.1 In `cmd/ragd/main.go`, call `api.ResolveLoopbackConfig(appCtx)`, pass it as `Options.Loopback`, and log the listener address when enabled.
- [x] 6.2 In `internal/apiclient/resources.go`, add a `LoopbackInfo` struct (`enabled`/`address`/`url`/`token`/`token_path`) and a `ServerInfo(ctx)` method fetching `GET /1.0` so a trusted unix-socket client can discover the port and token. Do NOT port `cmd/cli/basic/ui.go` or register a `ui` command in `cmd/cli/main.go`.

## 7. Packaging and config seeding

- [x] 7.1 In `snap/hooks/install`, seed `config.package.api.loopback.enabled="false"` and `config.package.api.loopback.address="127.0.0.1:0"` with an explanatory comment.
- [x] 7.2 In `snap/snapcraft.yaml`, add the `network-bind` plug to the `ragd` app with a comment that the bind is loopback-only (enforced in `loopback.go`). Do NOT add the `node` build-snap, the `npm` build step, or the `internal/webui/dist` copy.
- [x] 7.3 Bring over only the non-UI `.gitignore` entries; do not add `ui/` or `internal/webui/dist/` rules. (No non-UI entries needed: existing `.gitignore` already covers `bin/`/`*.snap`; token file lives under `$SNAP_COMMON` at runtime.)

## 8. Tests

- [x] 8.1 Create `internal/api/loopback_test.go` with a `startTestServerWithLoopback` helper that enables the loopback listener (`SNAP_COMMON`→temp dir, `Address: 127.0.0.1:0`) and waits for readiness.
- [x] 8.2 Add tests: valid bearer token admitted, valid cookie admitted, missing token rejected, invalid token rejected — all over real TCP. Do NOT port the `/ui/` asset or root-redirect tests.
- [x] 8.3 Add a `requireLoopbackHost` / `listenLoopback` unit test asserting non-loopback and all-interfaces addresses are refused.

## 9. Docs and verification

- [x] 9.1 Add a loopback section to `docs/rest-api.md` (or a new `docs/rest-api-loopback.md`): enabling `api.loopback.enabled`, the token model, discovering the port/token via `GET /1.0` over the socket, and a `curl` example with `Authorization: Bearer`. Cross-link the chat-API-key drop-in from `docs/rest-api.md`.
- [x] 9.2 Run `make all` (tidy fmt vet lint test build) and fix any findings. (tidy/fmt/vet/test/build pass; golangci-lint v2 clean on all changed files — remaining findings are pre-existing in untouched packages.)
- [x] 9.3 Build and install the snap, seed the keys (`rag set --package` on an existing install if needed), set `api.loopback.enabled=true`, restart `ragd`, and verify a `curl` to `http://127.0.0.1:PORT/1.0` with the bearer token succeeds and without it is rejected; confirm the unix socket + peercred path is unchanged. (Verified: resolved URL discovered over the socket, bearer→200, no-token→403, socket peercred unchanged.)
