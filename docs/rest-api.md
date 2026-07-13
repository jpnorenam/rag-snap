# REST API (`ragd`)

`ragd` is an optional long-running daemon that exposes the same knowledge-management,
search, chat, and batch-answering capabilities over a **versioned REST API on a local unix
socket**. It owns the long-lived OpenSearch, inference, and Tika clients and holds the
backend secrets, so any local program — including the `rag` CLI itself — can drive the RAG
stack without rebuilding clients or handling credentials.

The surface is deliberately small: a unix socket (access gated by socket group membership),
an **opt-in loopback TCP listener** for local clients that cannot dial a unix socket (gated by
a per-installation bearer token), async operations with a progress events websocket, and an
auto-generated OpenAPI specification. Remote HTTPS access is intentionally **not** part of this
surface — the loopback listener binds `127.0.0.1` only and refuses any non-loopback address.

- [Security model](#security-model)
- [Service management](#service-management)
- [Socket configuration](#socket-configuration)
- [Loopback (local REST API over TCP)](#loopback-local-rest-api-over-tcp)
- [CLI integration](#cli-integration)
- [Quick start over the socket](#quick-start-over-the-socket)
- [Response envelope](#response-envelope)
- [Async operations and events](#async-operations-and-events)
- [Endpoint reference](#endpoint-reference)

---

## Security model

> **⚠️ Group membership is root-equivalent for the RAG stack.**
> A connection over the socket is granted **full, unscoped access** to every endpoint —
> there is no per-endpoint authorization. Any user in the configured access group (default
> `rag`) can create and delete knowledge bases, ingest and export data, run chat and batch
> answering, and read the daemon's effective configuration. Treat membership in this group
> as equivalent to root access over the RAG stack and grant it accordingly.

Access is decided per connection from the peer's operating-system credentials
(`SO_PEERCRED`). A peer is admitted only if its effective user is `root` or a member of the
configured access group. These credentials are stamped by the kernel and cannot be spoofed.

---

## Service management

The daemon is opt-in. Like `tika-server`, it is installed but **disabled by default** and
must be started explicitly:

```bash
# Start the daemon (and enable it across reboots)
sudo snap start --enable rag-cli.ragd

# Check status
snap services rag-cli

# Stop / restart
sudo snap stop rag-cli.ragd
sudo snap restart rag-cli.ragd

# Follow logs
sudo snap logs -f rag-cli.ragd
```

Backend secrets (`OPENSEARCH_USERNAME`, `OPENSEARCH_PASSWORD`, `CHAT_API_KEY`) are set on
the **daemon's** service environment, not on each CLI invocation and never in `snapctl`
config. The daemon reads the rest of its configuration (`chat.*`, `knowledge.*`, `tika.*`,
`api.socket.*`) from `snapctl` at startup.

To give the daemon an inference API key (e.g. for OpenRouter or AWS Bedrock), inject it via a
root-only systemd drop-in, then restart the daemon:

```bash
sudo mkdir -p /etc/systemd/system/snap.rag-cli.ragd.service.d
printf '[Service]\nEnvironment=CHAT_API_KEY=%s\n' "$YOUR_KEY" | \
  sudo tee /etc/systemd/system/snap.rag-cli.ragd.service.d/10-chat-key.conf >/dev/null
sudo chmod 600 /etc/systemd/system/snap.rag-cli.ragd.service.d/10-chat-key.conf
sudo systemctl daemon-reload
sudo snap restart rag-cli.ragd
```

The drop-in file is `root:root 0600`, so the secret is never world-readable and never passes
through the `snapctl` config store or the `GET /1.0` config summary.

> **Applying config changes:** the daemon snapshots config at startup. After changing config
> with `rag set ...`, reload the daemon to pick up the new values — either `sudo snap restart
> rag-cli.ragd`, or send it `SIGHUP` for an in-place reload (re-reads config and rebuilds the
> backend clients without dropping the socket). In-flight operations and chat sessions do not
> survive a full restart.

---

## Socket configuration

Two package-scoped config keys (user-overridable) control the socket:

| Key | Default | Purpose |
|---|---|---|
| `api.socket.group` | `rag` | Host group whose members (plus `root`) are granted access. Enforced by the daemon's `SO_PEERCRED` check, **not** by the socket's file ownership. |
| `api.socket.mode` | `0666` | Octal file mode for the socket. Defaults to world-connectable; the peercred check is the access gate. |

The daemon creates the socket at `$SNAP_COMMON/ragd/unix.socket`
(`/var/snap/rag-cli/common/ragd/unix.socket`) and `chmod`s it to `api.socket.mode`. It does
**not** change the socket's group owner: under strict confinement the snap's seccomp profile
forbids `chown`ing the socket to an arbitrary group, so the socket stays `root`-owned and
world-connectable, and **access is gated entirely by the peer-credential check** — a
connecting process is admitted only if its effective user is `root` or a member of
`api.socket.group` (resolved from the host's passwd/group databases at connect time).

> **Note:** because there is no file-permission (DAC) backstop, any local process can `connect()`
> to the socket — but an unauthenticated peer can do nothing beyond receive an immediate `403`.
> The peer credentials are stamped by the kernel and cannot be spoofed.

To grant a user access, add them to the group and start a fresh login session (or use `newgrp`)
so the new membership is visible to the peercred check:

```bash
# Override the access group if desired (must already exist as a package key)
sudo rag set api.socket.group=rag

# Add a user to the group, then start a new session so the membership takes effect.
# No daemon restart is needed — the peercred check reads group membership live.
sudo usermod -aG rag "$USER"
newgrp rag   # or log out and back in
```

A connection from a user outside the group and not `root` is rejected with HTTP `403` and a
message naming the group to join.

---

## Loopback (local REST API over TCP)

The unix socket is unreachable by a browser and awkward for non-CLI local clients (scripts,
another process). `ragd` can **optionally** serve the exact same `/1.0` API over a loopback TCP
listener bound to `127.0.0.1`. This is off by default and controlled by two package-scoped,
user-overridable config keys:

| Key | Default | Purpose |
|---|---|---|
| `api.loopback.enabled` | `false` | Whether the daemon opens the loopback listener at all. |
| `api.loopback.address` | `127.0.0.1:0` | Loopback bind address. `:0` means an OS-assigned port, discovered at runtime. A non-loopback host (e.g. `0.0.0.0:8080` or an empty host `:8080`) is **refused** — the daemon fails to start rather than exposing the API on a LAN. |

```bash
# Enable the loopback listener and restart the daemon to pick it up.
sudo rag set api.loopback.enabled=true
sudo snap restart rag-cli.ragd
```

### Token model

Because `SO_PEERCRED` is unavailable for TCP peers, loopback requests authenticate with a
**per-installation bearer token** rather than by group membership. On first enable the daemon
generates a high-entropy (256-bit) token, persists it owner-only (`0600`) under
`$SNAP_COMMON/ragd/ui.token` so it survives restarts, and reuses it thereafter. The comparison
is constant-time.

The token value is **never read off disk by clients**. Instead the daemon returns it in the
`GET /1.0` config summary, which is already peercred-gated to `root` and access-group members
— exactly the principals the token is scoped to grant. (This mirrors why the token file is not
made group-readable: under strict confinement the daemon cannot `chown` it to a group without
crashing, so delivery over the trusted socket is used instead.)

### Discovering the port and token over the socket

A trusted unix-socket client reads both the resolved port and the token from `GET /1.0` under
`config.loopback`:

```bash
SOCK=/var/snap/rag-cli/common/ragd/unix.socket

# The config summary's loopback section carries enabled/address/url/token/token_path.
curl --unix-socket "$SOCK" http://ragd/1.0 | jq '.metadata.config.loopback'
# {
#   "enabled": true,
#   "address": "127.0.0.1:38283",
#   "url": "http://127.0.0.1:38283",
#   "token": "…64 hex chars…",
#   "token_path": "/var/snap/rag-cli/common/ragd/ui.token"
# }
```

### Calling the API over loopback

Present the token as an `Authorization: Bearer` header (a browser websocket upgrade that
cannot set headers may instead carry it as the `rag_ui_token` cookie):

```bash
SOCK=/var/snap/rag-cli/common/ragd/unix.socket
URL=$(curl -s --unix-socket "$SOCK" http://ragd/1.0 | jq -r '.metadata.config.loopback.url')
TOKEN=$(curl -s --unix-socket "$SOCK" http://ragd/1.0 | jq -r '.metadata.config.loopback.token')

# Authenticated request over the loopback port.
curl -H "Authorization: Bearer $TOKEN" "$URL/1.0"

# The same request without the token is rejected with 403.
curl -i "$URL/1.0"
```

The unix socket and its `SO_PEERCRED` gate are unchanged when the loopback listener is enabled;
the two transports share one handler set and never diverge. To turn the listener back off, `sudo
rag set api.loopback.enabled=false` and restart the daemon; the owner-only token file is inert
while disabled.

> **Inference API key:** the loopback listener serves the same RAG stack, so giving the daemon
> a chat/inference key works exactly as for the socket — via the root-only systemd drop-in in
> [Service management](#service-management), never through config or the token.

---

## CLI integration

The `rag` CLI **detects a running `ragd` automatically**: if the socket exists and the caller
is trusted, knowledge, search, chat, and `answer batch` commands route through the daemon;
otherwise they fall back to constructing backend clients directly (the original behaviour).
No flags or extra steps are required. Detection is skipped under `--debug`, which forces the
direct/offline path.

---

## Quick start over the socket

Any HTTP client that can speak to a unix socket works. With `curl`:

```bash
SOCK=/var/snap/rag-cli/common/ragd/unix.socket

# Discover the API: version, auth state, and feature extensions
curl --unix-socket "$SOCK" http://ragd/

# Server info: effective (secret-free) config summary and backend readiness
curl --unix-socket "$SOCK" http://ragd/1.0

# List knowledge bases (sync)
curl --unix-socket "$SOCK" http://ragd/1.0/knowledge

# Hybrid search (sync)
curl --unix-socket "$SOCK" -X POST http://ragd/1.0/search \
  -H 'Content-Type: application/json' \
  -d '{"knowledge":["project-docs"],"query":"how do I rotate credentials?"}'
```

A trusted root response reports `"auth":"trusted"`; an untrusted caller sees `"untrusted"`.

---

## Response envelope

Every JSON response is one of three shapes. Clients switch on the numeric
`status_code` / `error_code`, not the text status.

```jsonc
// sync — HTTP 200
{ "type": "sync", "status": "Success", "status_code": 200, "metadata": { ... } }

// async — HTTP 202, with Location: /1.0/operations/<uuid>
{ "type": "async", "status": "Operation created", "status_code": 100,
  "operation": "/1.0/operations/<uuid>", "metadata": { /* operation object */ } }

// error — HTTP 4xx/5xx
{ "type": "error", "error_code": 404, "error": "knowledge base not found" }
```

---

## Async operations and events

Long-running work — knowledge-engine init, ingest, export/import, and batch answering —
returns `202` with an operation URL. Poll, long-poll, or cancel it:

```bash
SOCK=/var/snap/rag-cli/common/ragd/unix.socket

# Poll an operation
curl --unix-socket "$SOCK" http://ragd/1.0/operations/<uuid>

# Long-poll until it reaches a terminal state (or N seconds elapse)
curl --unix-socket "$SOCK" "http://ragd/1.0/operations/<uuid>/wait?timeout=30"

# Cancel a cancellable operation
curl --unix-socket "$SOCK" -X DELETE http://ragd/1.0/operations/<uuid>
```

For live progress, subscribe to the events websocket (`GET /1.0/events?type=operation,logging`)
**before** launching the operation to avoid a poll race. Chat is an interactive
websocket-class operation: `POST /1.0/chat` returns an operation whose metadata carries the
websocket URL to dial for streamed tokens and `<think>` blocks.

---

## Endpoint reference

| Method & path | Kind | Purpose |
|---|---|---|
| `GET /` | sync | API discovery: version, auth state, `api_extensions` |
| `GET /1.0` | sync | Server info: config summary + backend readiness |
| `GET /1.0/operations`, `GET /1.0/operations/{id}` | sync | List / inspect operations |
| `GET /1.0/operations/{id}/wait` | sync | Long-poll an operation to completion |
| `DELETE /1.0/operations/{id}` | sync | Cancel a cancellable operation |
| `GET /1.0/operations/{id}/websocket` | ws | Chat session stream |
| `GET /1.0/events` | ws | Operation/logging event stream |
| `GET`/`POST /1.0/knowledge` | sync | List / create knowledge bases |
| `GET`/`DELETE /1.0/knowledge/{name}` | sync | Inspect / delete a base |
| `GET`/`DELETE /1.0/knowledge/{name}/sources[/{id}]` | sync | List / inspect / forget sources |
| `POST /1.0/knowledge/{name}/sources` | async | Ingest (file/URL/batch) |
| `POST /1.0/knowledge/{name}/export` | async | Export a base |
| `POST /1.0/knowledge/import` | async | Import a base |
| `POST /1.0/knowledge-engine` | async | Initialise models/pipelines/indexes |
| `POST /1.0/search` | sync | Hybrid search |
| `POST /1.0/chat` | async (ws) | Start an interactive chat session |
| `POST /1.0/answer/batch` | async | Run a prepared batch manifest |

The full, authoritative contract is the generated [`rest-api.yaml`](../rest-api.yaml)
OpenAPI specification at the repository root.
