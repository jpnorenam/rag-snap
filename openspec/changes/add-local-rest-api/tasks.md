## 0. Spikes / de-risking (do first)

- [x] 0.1 Spike strict-confinement unix-socket visibility: confirmed snapd's `sockets:` stanza
  supports only `listen-stream`/`socket-mode` (no `socket-group`), and world-writable `0666`
  defeats the group gate. Decision: daemon creates the socket at `$SNAP_COMMON/ragd/unix.socket`
  and `chown`/`chmod`s it to `root:<api.socket.group>` `0660` itself. Recorded in `design.md`
  Decision 1.
- [x] 0.2 Spike `SO_PEERCRED`: confirmed `golang.org/x/sys/unix` (already an indirect dep) exposes
  `Ucred`, `GetsockoptUcred`, `SO_PEERCRED`/`SOL_SOCKET`; concrete listener-wrap + `os/user`
  membership pattern recorded in `design.md` Decision 2. No new dependency.
- [x] 0.3 Library choices made: **`github.com/coder/websocket`** for the chat/events websockets
  (minimal, context-native successor to nhooyr.io/websocket) and **go-swagger** (`swagger:route`
  annotations → `rest-api.yaml`) for the OpenAPI spec, matching LXD. Added to `go.mod`/`make` when
  first used (websocket in phase 5, swagger in phase 7).

## 1. Daemon skeleton & unix socket (rest-api-server)

- [x] 1.1 `cmd/ragd/main.go`: reads snapctl config via `common.Context`, resolves backend URLs +
  socket config, serves; `SIGTERM`/`SIGINT` → graceful shutdown, `SIGHUP` → reload (re-resolve +
  re-serve). Backend *clients* are built lazily by feature handlers (later phases); phase 1 only
  tracks reachability.
- [x] 1.2 `internal/api/` (`server.go` router + `GET /` and `GET /1.0`; `socket.go` listener).
  Socket created at `$SNAP_COMMON/ragd/unix.socket`, `chown`ed `root:<api.socket.group>` and
  `chmod`ed to `api.socket.mode` by the daemon itself (per spike 0.1; snapd can't set the group).
  Verified end-to-end over the socket with a throwaway test (sync envelope, version, backends map).
- [x] 1.3 `internal/api/backend.go`: background TCP-reachability poll, non-blocking; `GET /1.0`
  reports per-backend readiness. (Per-endpoint "backend unavailable" errors wire in with the
  feature handlers in phases 4–6, when those endpoints exist.)
- [x] 1.4 `api.socket.group`/`api.socket.mode` seeded as package keys in `snap/hooks/install`
  (defaults `rag` / `0660`); daemon reads them via `ResolveSocketConfig` with the same defaults.
  NOTE: the `ragd` snap *app/build* stanza in `snapcraft.yaml` is intentionally deferred to
  task 8.1 (phase 8) — phase 1 is the Go daemon + config only.

## 2. Auth, envelope, versioning (rest-api-server)

- [x] 2.1 Implement `SO_PEERCRED`-based authentication: grant access iff peer is `root` or a member
  of `api.socket.group`; otherwise `403` with a "join the <group> group" message.
- [x] 2.2 Implement response-envelope helpers `respondSync`/`respondAsync`/`respondError` with the
  doubled numeric+text status codes and the `Location` header on async.
- [x] 2.3 Implement `GET /` (versions, `auth` trusted/untrusted, `api_extensions`) and the `/1.0`
  root (server info + read-only redacted config summary). Establish the `api_extensions` list.
- [x] 2.4 Add ETag support (`If-Match` → `412`) for any replaceable resource exposed later.

## 3. Operations & events (rest-api-operations)

- [x] 3.1 Implement the in-memory operations registry and the operation object (id, class,
  description, timestamps, status/status_code, resources, metadata, may_cancel, err).
- [x] 3.2 Implement `GET /1.0/operations`, `GET /1.0/operations/<uuid>`,
  `GET /1.0/operations/<uuid>/wait?timeout=N`, and `DELETE /1.0/operations/<uuid>` (cooperative
  cancel via `context.Context`).
- [x] 3.3 Implement the events hub and `GET /1.0/events` websocket with type filtering; publish
  operation lifecycle/progress and logging events.

## 4. Knowledge endpoints (rest-api-knowledge)

- [x] 4.1 Sync: `GET/POST /1.0/knowledge`, `GET/DELETE /1.0/knowledge/<name>` (no interactive
  confirm at API layer). Reuse `OpenSearchClient` create/list/delete.
- [x] 4.2 Sync: `GET /1.0/knowledge/<name>/sources`, `GET .../sources/<id>`, `DELETE .../sources/<id>`.
- [x] 4.3 Async: `POST /1.0/knowledge/<name>/sources` ingest (file upload, URL crawl, batch) as an
  operation with progress + cancel; reuse `processing.*` and `BulkIndex`/status updates.
- [x] 4.4 Sync: `POST /1.0/search` hybrid search; reuse `OpenSearchClient.Search`; embedding-model-
  unavailable → error.
- [x] 4.5 Async: knowledge-engine init operation (model deploy/pipelines/indexes); report model IDs.
- [x] 4.6 Async: export and import operations; reuse the existing elasticdump-based export/import.
  Google Drive auth stays CLI-only.

## 5. Chat over websocket (rest-api-chat)

- [x] 5.1 Implement `POST /1.0/chat` returning a websocket-class operation; implement the chat
  websocket endpoint and control protocol (prompt, set-active-kbs, token/think/done frames).
- [x] 5.2 Move the `Session` state server-side (active indexes, history, resolved model); reuse the
  existing RAG turn logic (`rewriteSearchQuery` → `retrieveContext` → `buildRAGPrompt` → stream).
- [x] 5.3 Stream tokens and `<think>` blocks as frames; enforce idle timeout and clean teardown on
  disconnect.

## 6. Batch answering (rest-api-answer)

- [x] 6.1 Async: `POST /1.0/answer/batch` accepting a prepared manifest; run via
  `chat.ProcessBatchChat` as an operation with progress + cancel.
- [x] 6.2 Make the structured results retrievable on completion (per-question answer, model,
  timestamp); confirm the interactive `--build` flow is intentionally not exposed.

## 7. OpenAPI spec generation (rest-api-server)

- [ ] 7.1 Annotate handlers; wire the generator to emit `rest-api.yaml`.
- [ ] 7.2 Add a `make` target/check that fails the build when the spec is out of sync with handlers.

## 8. Snap packaging

- [ ] 8.1 Add the `ragd` app to `snapcraft.yaml` (`daemon: simple`, `install-mode: disable`,
  `restart-condition: always`), with the chosen socket stanza and `network`/`network-bind` plugs.
- [ ] 8.2 Set `OPENSEARCH_USERNAME`/`OPENSEARCH_PASSWORD`/`CHAT_API_KEY` on the daemon service
  environment (not on the `rag` CLI app). Confirm no secret reaches API clients.
- [ ] 8.3 Update the `install` hook to seed the new `api.socket.*` package keys.

## 9. CLI rewiring (final phase — keep each prior phase shippable)

- [ ] 9.1 Add an API client to the CLI that talks to the unix socket; detect a running daemon and
  prefer it, falling back to direct backend mode when absent. Preserve the fixed command order in
  `cmd/cli/main.go` and all existing `--help`/usage text.
- [ ] 9.2 Route knowledge, search, chat, and `answer batch` commands through the API client when the
  daemon is present; map async operations to CLI progress (poll/wait or events).

## 10. Docs & validation

- [ ] 10.1 Update `docs/usage.md` with an API section, `ragd` service management, the unix-socket
  quick-start, and the group-membership-is-root-equivalent security note.
- [ ] 10.2 Publish the generated `rest-api.yaml`.
- [ ] 10.3 Run `make all` (tidy fmt vet lint test build) locally.
- [ ] 10.4 Validate inside an installed snap: start `ragd`, exercise the socket from a host user in
  the group (granted) and one outside it (`403`); run an ingest/batch operation end-to-end with
  progress over the events websocket; hold a chat session over the websocket; confirm config changes
  apply after a daemon reload/restart.
