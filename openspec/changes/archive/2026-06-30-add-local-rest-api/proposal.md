## Why

Today `rag-cli` is a stateless request/response CLI. Every command rebuilds its own
clients from `snapctl` config plus secret env vars (`OPENSEARCH_USERNAME`/`PASSWORD`,
`CHAT_API_KEY`), does one thing, and exits. There is no long-running Go process — the
only daemon in the snap is the bundled `tika-server`, and the only `net.Listen` is the
ephemeral Google Drive OAuth loopback.

This has three consequences worth fixing:

- **No programmatic surface.** Anything that wants to drive knowledge management, chat,
  or batch answering has to shell out to the CLI and scrape stdout. There is no stable
  API contract.
- **Secrets live in the client.** Every invocation that touches OpenSearch or the
  inference server needs the credentials in its own environment, so the secrets are
  spread across whatever process launches `rag-cli`.
- **No shared, long-lived state.** Connection pools, model readiness, and chat session
  state are rebuilt per command; long operations (ingest, batch answer, export/import,
  model deploy) have nowhere to run in the background and no way to report progress,
  be polled, or be cancelled.

LXD solves exactly this shape of problem with a daemon that exposes a versioned REST API
over a local unix socket (full access, gated only by socket group membership) and,
optionally, over HTTPS to remote clients (TLS client certs / tokens / OIDC). We adopt
LXD's local-first model: a `ragd` daemon owns config, secrets, clients, and operations;
the existing CLI becomes a thin client over the unix socket. **This change scopes the
local unix+HTTP API only. The HTTPS/remote-auth surface is explicitly deferred** to a
follow-up change, mirroring LXD where remote access is disabled by default.

## What Changes

- **Add a `ragd` daemon** (new snap `daemon: simple` app alongside `tika-server`) that
  serves a RESTful HTTP API over a local **unix socket**. The daemon owns the long-lived
  OpenSearch / inference / Tika clients and reads its configuration from `snapctl` at
  startup and on a reload signal (config stays out-of-band; `rag get`/`set` continue to
  hit `snapctl` directly — see Design).
- **Versioned API surface** rooted at `GET /` (lists API versions + `api_extensions`)
  with all resources under `/1.0/`. A uniform response envelope: `{"type":"sync",...}`
  (HTTP 200), `{"type":"async","operation":"/1.0/operations/<uuid>",...}` (HTTP 202),
  and `{"type":"error","error_code":...}` — modelled on LXD.
- **Local authentication = unix socket access.** A connection over the socket is granted
  full access if the peer's effective user is `root` or a member of the configured access
  group (verified via `SO_PEERCRED`). There is no second auth layer for local clients —
  the socket file ownership/mode plus group membership *is* the trust boundary, exactly
  as in LXD. No certificates, tokens, or OIDC in this change.
- **Async operations model.** Long-running work — `knowledge init` (model deploy),
  `ingest` (download→Tika→chunk→embed), `ingest --batch`, `export`/`import`, and
  `answer batch` — returns `202` with an operation UUID. Clients poll
  `GET /1.0/operations/{uuid}`, long-poll `.../wait`, cancel via `DELETE`, and watch
  progress on a `GET /1.0/events` websocket.
- **Knowledge management endpoints** covering the current `k` subcommands: list/create/
  delete bases, list/ingest/forget/inspect sources, search, export, import.
- **Interactive chat over a websocket.** A chat session is an interactive resource: the
  client opens a websocket, sends prompts plus the active knowledge-base set, and receives
  streamed tokens (and `<think>` blocks) back. The **daemon owns the `Session` state**
  (active indexes, history, resolved model), matching LXD's interactive `exec`/`console`
  operations.
- **Batch answering endpoint** that runs a YAML manifest as an async operation and exposes
  the JSON results.
- **Auto-generated OpenAPI/Swagger spec** (`rest-api.yaml` equivalent) produced from Go
  handler annotations, so the published spec follows the code and never drifts.

This change adds the daemon and API. **Rewiring the CLI commands to call the API instead
of constructing clients directly is sequenced as the final phase** (the CLI keeps working
against backends directly until the daemon reaches parity); see `tasks.md`.

### External services touched

- **OpenSearch** (`knowledge` store): yes — the daemon owns the `OpenSearchClient`; all
  knowledge, search, and batch-answer endpoints proxy to it. No new pipelines/indexes/models.
- **Inference server** (`chat` backend): yes — the daemon owns the `openai-go` client for
  chat and batch answering.
- **Tika**: yes — the daemon calls Tika during ingest and `answer batch --build` document
  extraction.

### New config keys (snapctl, package-scoped with user override)

- `api.socket.group` — host group whose members may access the unix socket (default e.g.
  `rag`; package key, user-overridable). Used for socket ownership/mode and the peercred
  membership check.
- `api.socket.mode` — octal permission mode for the socket file (default `0660`; package key).

No new secrets. The daemon reuses the existing `OPENSEARCH_USERNAME`/`OPENSEARCH_PASSWORD`/
`CHAT_API_KEY` env vars, now set on the **daemon's** snap service environment rather than on
each CLI invocation (a net improvement to secret hygiene — local API clients never need them).

### User-facing surface

- New snap service `ragd` (managed via `snap start/stop rag-cli.ragd`), `install-mode:
  disable` like `tika-server`, opt-in.
- The HTTP/websocket API itself is a new programmatic surface and must be documented
  (`docs/usage.md` API section, the generated `rest-api.yaml`, and a quick-start for the
  unix socket). No new end-user CLI commands are introduced by this change; CLI rewiring in
  the final phase preserves existing command behavior and `--help` text.

## Capabilities

### New Capabilities

- `rest-api-server`: the `ragd` daemon, its unix socket listener, the versioned `/1.0`
  root and `api_extensions`, the sync/async/error response envelope, local authentication
  via socket group membership + peercred, and OpenAPI spec generation.
- `rest-api-operations`: the async operations resource (create/poll/wait/cancel) and the
  events websocket for progress notifications.
- `rest-api-knowledge`: REST endpoints for knowledge-base and source management and search.
- `rest-api-chat`: interactive chat sessions over a websocket, with session state owned by
  the daemon.
- `rest-api-answer`: the batch-answering endpoint, run as an async operation.

### Modified Capabilities

<!-- None at spec level in this change. The existing chat-search capability is unaffected;
     CLI rewiring (final phase) preserves current command behavior. -->

## Impact

- **Affected code:**
  - New `cmd/ragd/` (daemon entry point) and `internal/api/` (or `pkg/api/`): HTTP router,
    response envelope helpers, unix socket listener + peercred auth, operations registry,
    events websocket hub, chat websocket handler, OpenAPI annotations.
  - Reuse of existing logic: `knowledge.OpenSearchClient` and all its methods, the chat
    RAG loop (`chat/rag.go`, `chat/client.go` retrieval/stream), `chat.ProcessBatchChat`,
    `processing.*` ingestion, `rfp.*` extraction — moved/called behind handlers, not rewritten.
  - `cmd/cli/` (final phase only): commands gain a client mode that calls the API.
- **New config keys:** `api.socket.group`, `api.socket.mode` (package-scoped, user-overridable).
- **No new secrets** (existing env vars move to the daemon service environment).
- **Snap packaging:** new `ragd` app (`daemon: simple`, `install-mode: disable`) in
  `snapcraft.yaml`; a `sockets:`/socket-activation stanza or daemon-managed socket with
  configured group/mode; plugs for `network`, `network-bind`. Confinement note: exposing a
  unix socket to non-snap host users under strict confinement is the key packaging risk —
  see Design.
- **Dependencies:** a websocket library (e.g. `gorilla/websocket` or `nhooyr.io/websocket`)
  and an OpenAPI generation approach (Go annotations + generator, as LXD uses with
  `swagger`/`go-swagger`).
- **Docs to update:** `docs/usage.md` (new API section + `ragd` service management), a
  published `rest-api.yaml`, and a unix-socket quick-start.
