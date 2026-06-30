# Local web UI

`ragd` can serve a local **browser UI** for chatting with your knowledge bases. The UI is a
static single-page application embedded into the `ragd` binary and served from a
**loopback-only** HTTP listener, same-origin with the REST API. A browser cannot connect to
the daemon's unix socket, so this opt-in TCP listener is what bridges the browser to the API.

Remote/HTTPS exposure is intentionally **not** part of this surface: the listener binds
`127.0.0.1` only and refuses any non-loopback address.

- [Enabling the listener](#enabling-the-listener)
- [Launching with `rag ui`](#launching-with-rag-ui)
- [Trust model](#trust-model)

---

## Enabling the listener

The UI listener is opt-in and **off by default** (the unix socket remains the only default
surface). Enable it and restart the daemon:

```bash
# Turn on the loopback UI listener
sudo rag set api.ui.enabled=true

# Restart ragd so it opens the listener
sudo snap restart rag-cli.ragd
```

Two config keys control the listener:

| Key              | Default        | Meaning                                                                 |
| ---------------- | -------------- | ----------------------------------------------------------------------- |
| `api.ui.enabled` | `false`        | Whether `ragd` opens the loopback listener and serves the UI.           |
| `api.ui.address` | `127.0.0.1:0`  | Loopback bind address. `:0` picks an OS-assigned port. **Must be loopback** — a non-loopback address is refused at startup. |

The resolved URL (with the OS-assigned port) is written to the daemon log and reported by
`GET /1.0` under `config.ui`:

```bash
sudo snap logs rag-cli.ragd | grep 'serving UI'
# serving UI on http://127.0.0.1:43210/ui/ (token at /var/snap/rag-cli/common/ragd/ui.token)
```

---

## Launching with `rag ui`

The simplest way in is the `rag ui` command. It contacts the daemon over the trusted unix
socket, discovers the loopback URL and token, and opens your browser with the token applied:

```bash
rag ui

# Print the URL instead of opening a browser (e.g. on a headless host)
rag ui --no-browser
```

When the listener is disabled, `rag ui` explains how to enable it rather than failing
silently. You must be a member of the API access group (default `rag`) to read the token and
launch the UI.

---

## Trust model

The unix socket authenticates peers by their kernel credentials (`SO_PEERCRED`). Those
credentials do not exist for TCP connections, so the loopback listener authenticates with a
**localhost bearer token** instead:

- On first enable, the daemon generates a high-entropy token and stores it under
  `$SNAP_COMMON` (`ragd/ui.token`), **readable by the API access group** — the same trust
  boundary as the unix socket. Group members can read it; other users cannot. The token is
  reused across restarts.
- Requests to `/1.0/...` over the loopback listener must present the token (as a
  `Bearer` header or the `rag_ui_token` cookie). Requests without a valid token are rejected.
- **Static UI assets under `/ui/` load without the token** so the page shell can render;
  only the `/1.0/...` API is gated.
- `rag ui` performs the handoff by opening `/ui/login?token=…`, which sets an `HttpOnly`
  cookie scoped to the loopback origin and redirects into the app. The token therefore never
  enters the page's JavaScript or the address-bar history, and same-origin API calls and the
  chat websocket carry it automatically.
- The token is **per-installation** and is **never** baked into the embedded UI assets.

> **⚠️ A loopback port is reachable by any local user.** As with the unix socket, treat
> membership in the API access group — and possession of the token — as equivalent to full
> access over the RAG stack. The token is the local trust boundary, and the seam where TLS
> client certs / OIDC attach if the surface is ever exposed remotely (a separate, deferred
> decision).
