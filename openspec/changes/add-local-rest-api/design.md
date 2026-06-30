## Context

`rag-cli` is currently a short-lived Cobra CLI. Each command builds clients on demand:
`serverApiUrls(ctx)` (`cmd/cli/basic/common.go:134`) reads `snapctl` config into a
`{openai,opensearch,tika}` URL map, then handlers construct `knowledge.NewClient(url)`
(reads `OPENSEARCH_USERNAME`/`PASSWORD` env, `client.go:71`), `openai.NewClient(...)`
(reads `CHAT_API_KEY` env, `chat/client.go:81`), and `processing.NewTikaClient(url)`.
There is **no daemon, listener, or persistent socket** in the Go code (only an ephemeral
GDrive OAuth loopback). The only snap service is `tika-server` (`daemon: simple`,
`install-mode: disable` — `snapcraft.yaml:163`), which gives us a working precedent for a
strictly-confined snap daemon.

We are introducing `ragd`, a daemon that owns config, secrets, the long-lived clients, and
a registry of async operations, and exposes them over a versioned REST API on a local unix
socket. The design deliberately mirrors LXD: local socket = full trust gated by group
membership; remote HTTPS deferred. This document records the decisions that shape the API
contract and the daemon's place in the snap.

The four pivotal product decisions (already taken):

1. **Chat** is exposed as an interactive **websocket** session; the daemon owns `Session`.
2. **Config** stays on `snapctl`; the daemon reads at boot and on reload. `rag get`/`set`
   keep writing `snapctl` directly. Config does **not** become an API resource in this change.
3. **Long operations** use the full LXD-style **async operations + events websocket** model.
4. **Scope** is the local unix+HTTP API; **HTTPS/remote auth is deferred**.

## Goals / Non-Goals

**Goals:**
- A single `ragd` daemon exposing knowledge management, chat, and batch answering over a
  local unix socket, reachable by the existing CLI and by any local program.
- LXD-faithful conventions: versioned `/1.0` root, `api_extensions` feature detection, the
  sync/async/error envelope, the operations resource, the events websocket, ETag on
  replaceable resources.
- Local auth purely via socket access: peer in `root` or the configured group → full access.
- Better secret hygiene: credentials live on the daemon, not on every client invocation.
- An auto-generated OpenAPI spec that tracks the handler code.

**Non-Goals:**
- **No HTTPS listener, TLS client certs, trust store, tokens, or OIDC** (next change).
- **No fine-grained authorization / per-identity scoping.** Local access is all-or-nothing,
  like LXD's unix socket. (Authorization scaffolding belongs with the remote-auth change.)
- **No config-over-API.** `snapctl` remains the config backend and the package/user
  precedence model is untouched; the daemon is a reader, not a new writer.
- **No change to OpenSearch artifacts** (pipelines/indexes/models) or the RAG algorithm.
- **No new secrets.** Existing env vars move to the daemon service environment.
- Not rewriting business logic — handlers call the existing `knowledge`/`chat`/`processing`/
  `rfp` packages.

## Decisions

### 1. Daemon process & lifecycle — second snap service, socket owned by the daemon

`ragd` is a new `daemon: simple` app in `snapcraft.yaml`, `install-mode: disable` and
`restart-condition: always`, exactly like `tika-server`. It is opt-in: `snap start
rag-cli.ragd`. On start it:

1. Reads config from `snapctl get` (host/port/TLS/model keys + new `api.socket.*` keys).
2. Constructs the long-lived `OpenSearchClient`, inference client, and Tika client, and
   begins readiness polling (reusing the existing `checkServer`/`handshake` logic) without
   blocking the listener — endpoints that need a backend report `503`-style errors until ready.
3. Creates the unix socket at a fixed path under `$SNAP_COMMON` (e.g.
   `$SNAP_COMMON/ragd/unix.socket`), `chown`s it to `root:<api.socket.group>` and `chmod`s
   to `api.socket.mode` (default `0660`), then serves HTTP on it.
4. Re-reads config and rebuilds clients on `SIGHUP` (the "reload signal"). `rag set` writing
   `snapctl` does **not** auto-notify the daemon; a `snap restart` or explicit reload applies
   changes. (A future `POST /1.0/config` could push reloads, but config-over-API is out of scope.)

**Socket path & host visibility (key confinement risk) — RESOLVED by spike 0.1.**
Under strict confinement `$SNAP_COMMON` is `/var/snap/rag-cli/common/`, which is itself
root-owned but world-traversable, so a socket *file* placed there can be reached by host users
provided its own ownership/mode permit it. Findings:

- snapd's `sockets:` activation stanza supports only `listen-stream` and `socket-mode` —
  **`socket-group` was proposed but never implemented in snapd**
  ([PR #3916](https://github.com/snapcore/snapd/pull/3916),
  [forum](https://forum.snapcraft.io/t/socket-activation-support/2050)). So socket activation
  alone cannot give us `root:<group>` ownership; it can only set the mode (default `0666`).
- A world-writable `0666` socket (the "just make `$SNAP_COMMON` world-accessible" route,
  [forum](https://forum.snapcraft.io/t/sharing-a-unix-domain-socket-between-a-daemon-and-an-app/12332))
  removes the DAC group gate our design depends on. **Rejected** — it would force *all*
  access control into the peercred layer with no file-permission backstop.
- `listen-stream` paths are restricted to `$SNAP_DATA/...`, `$SNAP_COMMON/...`, or abstract
  `@snap.<snap>.<name>` names; `/run/...` is rejected at install time.

**Decision:** the daemon **creates the socket itself** (does not use the `sockets:` activation
stanza for ownership) at `$SNAP_COMMON/ragd/unix.socket`, then `chown`s it to
`root:<api.socket.group>` and `chmod`s it to `api.socket.mode` (default `0660`). Group
ownership is set by the daemon at runtime because snapd cannot set it declaratively. This keeps
the DAC group gate (file mode + group) as the first line of defence, with peercred as the
second. The `<group>` must exist on the host; the snap declares it via `system-usernames`
(`snap_daemon`/`_daemon_`) only if we later choose to drop privileges — for now the daemon runs
as root (like LXD and `tika-server`) and just sets group ownership on the socket. Whether the
chosen group is a pre-existing host group the admin populates, or one the snap must create, is a
packaging detail for task 8.x; the API and auth design do not depend on it.

### 2. Local authentication — `SO_PEERCRED` + group membership, nothing else

When a connection is accepted on the unix socket, the daemon reads the peer credentials via
`SO_PEERCRED` (`golang.org/x/sys/unix.GetsockoptUcred` on the underlying `*net.UnixConn`),
yielding the client's uid/gid/pid. Access is granted iff the peer's effective user is `root`
**or** is a member of the configured `api.socket.group`. Membership is resolved from the host
passwd/group databases (`os/user.LookupId` + group membership), not just the socket's gid, so
the check is explicit and auditable. A granted connection has **full access** to every
endpoint — there is no per-route authorization.

**Spike 0.2 — RESOLVED.** Verified `golang.org/x/sys/unix` (already an indirect module dep —
**no new dependency**) exposes everything needed: `type Ucred {Pid int32; Uid, Gid uint32}`,
`GetsockoptUcred(fd, level, opt) (*Ucred, error)`, and the `SO_PEERCRED`/`SOL_SOCKET` constants.
The concrete pattern: from the accepted `*net.UnixConn`, call `SyscallConn()` then `Control(fn)`,
and inside `fn(fd)` call `unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)`.
Resolve membership with `os/user.LookupId(strconv.Itoa(int(ucred.Uid)))` →
`u.GroupIds()` and compare against the gid of `api.socket.group` (`user.LookupGroup`). Because
the HTTP server abstracts away the raw conn, the auth seam captures the `Ucred` at accept time:
wrap the `net.Listener` so `Accept()` stamps each `net.Conn` with its peer creds, then a base
HTTP middleware reads them via the connection (e.g. a `ConnContext`-populated value) — this is
the same approach LXD uses to thread peer identity from the listener into the handler.

Rationale: this is precisely LXD's model ("local access through the unix socket always grants
full access"). The socket file mode (`0660`, `root:<group>`) is the first gate; the peercred
check is defence-in-depth and gives a clean `403` with an actionable message ("user must be in
the <group> group") rather than a raw connection refusal. It also positions us cleanly for the
remote change: remote requests will arrive without valid peercred and must instead present a
cert/token, so the auth seam is a single `authenticate(conn, req) (identity, error)` step that
this change implements only the unix branch of.

### 3. Response envelope — adopt LXD's sync/async/error shapes verbatim

Every JSON response is one of:

```jsonc
// sync (HTTP 200)
{ "type": "sync", "status": "Success", "status_code": 200, "metadata": { ... } }

// async (HTTP 202), Location: /1.0/operations/<uuid>
{ "type": "async", "status": "Operation created", "status_code": 100,
  "operation": "/1.0/operations/<uuid>", "metadata": { <operation object> } }

// error (HTTP 4xx/5xx)
{ "type": "error", "error_code": 404, "error": "Knowledge base not found" }
```

Status codes are LXD's doubled numeric+text scheme (100–199 running state, 200–399 success,
400–599 failure). Clients are told to switch on the numeric `status_code`. PUT-replaceable
resources carry an **ETag**; mutating PUTs honour `If-Match` and fail `412` on mismatch
(needed only where we expose replaceable objects — see knowledge spec). Helper functions
(`respondSync`, `respondAsync`, `respondError`) centralize this so every handler is uniform.

### 4. Operations model — in-memory registry + events hub

A long operation is created by a handler, registered in an in-memory `operations` map keyed by
UUID, and run on a goroutine. The operation object carries: `id`, `class` (`task` |
`websocket` | `token`), `description`, `created_at`/`updated_at`, `status`/`status_code`,
`resources` (affected URLs, e.g. the KB and source being ingested), `metadata` (operation-
specific progress, e.g. `{chunks_done, chunks_total}`), `may_cancel`, and `err`.

- `GET /1.0/operations` lists; `GET /1.0/operations/{uuid}` returns one.
- `GET /1.0/operations/{uuid}/wait?timeout=N` long-polls until terminal or timeout.
- `DELETE /1.0/operations/{uuid}` cancels if `may_cancel` (cooperative: the goroutine watches
  a `context.Context` cancelled by the handler).
- Progress is published to the **events** hub; `GET /1.0/events?type=operation,logging`
  upgrades to a websocket and streams typed events. Clients are advised to subscribe to events
  *before* launching an operation to avoid a poll race (LXD's guidance).

UUIDs and timestamps: workflow/runtime note — the daemon generates these at runtime (no
constraint here; the OpenSpec-script timestamp rule is irrelevant to the daemon itself).

Operations are **not persisted**: a daemon restart drops in-flight operations. Accepted for
this change (ingest/batch are re-runnable; export/import write to disk and can be retried).
Persistence can be added later without changing the API contract.

### 5. Chat — interactive websocket session, daemon owns `Session`

`POST /1.0/chat` creates a chat session and returns an async **websocket-class** operation
whose metadata includes a websocket URL (and a one-time secret), mirroring LXD `exec`. The
client dials `GET /1.0/operations/{uuid}/websocket?secret=...` and then:

- sends `{"type":"prompt","content":"...","active_kbs":[...]}` control frames,
- receives `{"type":"token","content":"..."}` frames as generation streams, plus
  `{"type":"think", ...}` for `<think>` blocks and a terminal `{"type":"done", ...}`.

The **daemon holds the `Session`** (`chat/commands.go:117`) — active indexes, conversation
history, resolved model, the wired `KnowledgeClient`/inference client. This means
`/use-knowledge` semantics become control messages (set active KBs) rather than client-side
REPL state, and a session survives across prompts on the one websocket. The existing RAG turn
logic (`rewriteSearchQuery` → `retrieveContext` → `buildRAGPrompt` → streaming completion) is
reused unchanged behind the socket; only the transport of tokens changes (from terminal writes
to websocket frames). Session lifetime ends when the websocket closes or an idle timeout fires.

Why websocket and not SSE/retrieval-only: chat is intrinsically bidirectional and stateful
(multi-turn, mid-session KB toggles), and LXD already proves the interactive-operation-over-
websocket pattern. SSE would force re-sending history each call; a retrieval-only daemon would
leave the "real" chat in the CLI and defeat the purpose of an API.

### 6. Config stays on snapctl; the daemon is a reader

No `GET/PUT /1.0/config`. The daemon snapshots config at boot via `snapctl get` and on
`SIGHUP`. `rag get`/`set` continue to operate `snapctl` directly (and `set` still requires
root and enforces package/user precedence). The `/1.0` root response **may surface a
read-only `config` summary** (effective host/port/model values, secrets redacted) for
diagnostics — read-only, derived from the daemon's in-memory snapshot, not a writable resource.

Rationale: config-over-API would mean re-expressing the package<user precedence and the
root-gated `set` as API semantics, plus a write path that races with `snapctl`. Out of scope
and unnecessary for the stated goal. The cost is that applying a config change needs a reload/
restart rather than being instant — acceptable for an opt-in daemon.

### 7. API versioning & feature detection

`GET /` returns `{"api_status":"stable","api_version":"1.0","auth":"trusted"|"untrusted",
"api_extensions":[...]}`-style metadata. New backward-compatible capabilities are advertised by
appending to `api_extensions` (e.g. `knowledge_export`, `chat_websocket`, `batch_answer`),
**not** by bumping to `/2.0`. Clients feature-detect. `auth` reports `trusted` once the
peercred check passes (so the CLI can confirm it has access). This is LXD's discipline and it
is what lets the API evolve for years without a version break.

### 8. OpenAPI spec generation

Handlers carry `swagger:route`-style doc comments; a generator (go-swagger or equivalent, as
LXD uses) emits `rest-api.yaml` at build time, validated in `make` so the spec cannot drift
from the code. The spec is published alongside the docs. We do not hand-maintain the YAML.

### 9. Code placement & reuse

- `cmd/ragd/main.go` — daemon entry: load config, build clients, start listener, handle signals.
- `internal/api/` — router, envelope helpers, peercred auth, operations registry, events hub,
  per-resource handlers (`knowledge.go`, `chat.go`, `answer.go`), OpenAPI annotations.
- Business logic is **called, not copied**: `knowledge.OpenSearchClient` and its methods, the
  chat retrieval/stream functions, `chat.ProcessBatchChat`, `processing.*`, `rfp.*`. Where those
  functions currently write to stdout or drive `huh`/`readline` interactively, we factor the core
  out from the presentation (the interactive `huh` review flows in `answer batch --build` are CLI-
  only and are **not** exposed verbatim — the API takes a prepared manifest; see answer spec).

### 10. CLI rewiring sequenced last

The CLI keeps constructing clients directly until the daemon reaches parity. The final phase
adds a client mode (the CLI detects a running `ragd`/socket and calls the API; otherwise falls
back to direct mode), so the daemon can land and be exercised by API clients and tests before
the CLI depends on it. This keeps each phase shippable and avoids a flag-day rewrite.

## Risks / Trade-offs

- **Strict-confinement socket visibility (highest risk).** A unix socket under `$SNAP_COMMON`
  may not be reachable by non-snap host users. Mitigation: spike the LXD approach (host-visible
  socket path + group) before building handlers; the API contract is independent of the outcome.
- **Daemon owns secrets & long-lived connections.** A crash drops in-flight operations and chat
  sessions. Mitigated by `restart-condition: always`, cooperative cancellation, and re-runnable
  operations; persistence deferred.
- **Two config paths during the transition.** `rag set` writes snapctl; the daemon needs a
  reload to see it. Documented; instant config-over-API is explicitly out of scope.
- **Surface area & dependencies.** Adds a websocket lib and an OpenAPI generator, plus a
  meaningful new code surface. Mitigated by reusing existing business logic and landing the
  daemon behind an opt-in service before any CLI dependency.
- **Chat session state moves server-side.** Behavior must match the current REPL (history,
  `/use-knowledge`, `<think>` rendering) but over frames. Risk of subtle divergence; mitigated
  by reusing the exact RAG turn functions and validating against the CLI REPL.
- **No authorization granularity.** Any local user in the group gets everything — by design,
  per LXD. Operators must treat group membership as root-equivalent for the RAG stack; this
  must be stated plainly in the docs.
