# Fixes

## UI token unreadable under strict confinement (2026-06-30)

### Symptom

`rag ui` failed for a normal (non-root) user with:

```
Error: reading the UI token at /var/snap/rag-cli/common/ragd/ui.token: open ...: permission denied
You must be a member of the API access group to read it.
```

On a freshly generated token the daemon also crash-looped on startup
(`systemd … status=1/FAILURE`), recovering only because the subsequent restart
reused the already-written token file.

### Root cause

The loopback UI listener authenticates browser requests with a daemon-generated
localhost bearer token persisted at `$SNAP_COMMON/ragd/ui.token`. To let a
trusted CLI caller read it, the daemon tried to `chown` the file to the
configured access group (`api.socket.group`) and set mode `0640`.

Under **strict snap confinement**, snapd's seccomp profile denies `chown` to an
arbitrary group — even for a root-running daemon. The chown failed
(`operation not permitted`), so the file stayed `root:root 0640` and no
non-root user could read it. This is the same restriction the unix socket
already documents and sidesteps via peercred + mode `0666`; the token file had
no equivalent fallback. On a fresh token the chown error was fatal, crashing
the daemon.

### Fix

Stop relying on a group-readable token file. Instead, return the token **value**
over the `GET /1.0` endpoint, which is already gated by `requireAuth`
(SO_PEERCRED): only root and members of the access group can reach it — exactly
the principals the token is scoped to grant. This preserves the intended trust
boundary (see `openspec/changes/local-ui/design.md` Decision 2) without
depending on a chown that confinement forbids, and it removes the startup crash.

Changes:

- `internal/api/token.go`
  - `localhostToken()` no longer takes a `group` argument and no longer chowns.
    The token file is persisted owner-only (`0600`) purely so the token survives
    daemon restarts; clients never read it.
  - Removed the now-dead `applyTokenPermissions` helper and the `os/user` /
    `strconv` imports.
- `internal/api/server.go`
  - `startUI` calls `localhostToken()` and treats any token error as fatal
    (no token means no auth); removed the soft-error "permissions warning" path.
  - `configSummary` returns the token value as `config.ui.token` (alongside the
    retained `token_path` for diagnostics). Updated the doc comments to reflect
    that the summary now intentionally returns this one secret over the
    peercred-gated endpoint.
- `internal/apiclient/resources.go`
  - `UIInfo` gains a `Token` field (`json:"token"`).
- `cmd/cli/basic/ui.go`
  - `rag ui` uses `ui.Token` from the daemon response instead of reading the
    token file; removed the `readToken` helper and the `os` import.

### Verification

- `go build ./...`, `go vet ./...`, and `go test ./internal/api/...` pass.
- After rebuilding/reinstalling the snap, `rag-cli.rag ui` succeeds as a normal
  user and prints/opens the `…/ui/login?token=…` handoff URL.

### Operational notes discovered

- Invoke the snap app as **`rag-cli.rag`**; the bare `rag` symlink can dispatch
  to an older installed revision missing newer subcommands.
- The `install` hook only seeds package config keys on a fresh install, not on a
  `--dangerous` refresh. Use `sudo rag-cli.rag set --package <key>=<val>` to
  register a key the user `set` path rejects as "unknown key".
- Default `api.socket.group` is `rag`, which may not exist on a dev host; set it
  to a group you belong to (e.g. `sudo`) and restart `rag-cli.ragd`.
