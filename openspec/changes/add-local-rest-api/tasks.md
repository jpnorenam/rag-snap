## 0. Spikes / de-risking (do first)

- [ ] 0.1 Spike strict-confinement unix-socket visibility: confirm how a `ragd` socket under
  `$SNAP_COMMON` (or a snapd `sockets:` activation path) can be reached by non-snap host users in
  the configured group. Reference LXD's socket path + `lxd` group approach. Record the chosen
  approach in `design.md` Decision 1 before building handlers.
- [ ] 0.2 Spike `SO_PEERCRED` retrieval from a Go `net.Listener` on a unix socket (uid/gid/pid),
  and host group-membership resolution, under strict confinement (`os/user`, nsswitch availability).
- [ ] 0.3 Choose the websocket library (e.g. `gorilla/websocket` vs `nhooyr.io/websocket`) and the
  OpenAPI generator (e.g. go-swagger as LXD uses); add to `go.mod` and `make`.

## 1. Daemon skeleton & unix socket (rest-api-server)

- [ ] 1.1 Add `cmd/ragd/main.go`: load snapctl config, build OpenSearch/inference/Tika clients,
  start the listener, handle `SIGHUP` (reload/rebuild) and `SIGTERM` (graceful shutdown).
- [ ] 1.2 Add `internal/api/` router and the unix-socket listener; create the socket at the
  decided path; set ownership `root:<api.socket.group>` and mode `api.socket.mode`.
- [ ] 1.3 Begin backend readiness polling without blocking the listener; endpoints needing an
  unready backend return a backend-unavailable error.
- [ ] 1.4 Add `api.socket.group` and `api.socket.mode` config keys; seed them as package keys in
  the snap `install` hook with sensible defaults (group e.g. `rag`, mode `0660`).

## 2. Auth, envelope, versioning (rest-api-server)

- [ ] 2.1 Implement `SO_PEERCRED`-based authentication: grant access iff peer is `root` or a member
  of `api.socket.group`; otherwise `403` with a "join the <group> group" message.
- [ ] 2.2 Implement response-envelope helpers `respondSync`/`respondAsync`/`respondError` with the
  doubled numeric+text status codes and the `Location` header on async.
- [ ] 2.3 Implement `GET /` (versions, `auth` trusted/untrusted, `api_extensions`) and the `/1.0`
  root (server info + read-only redacted config summary). Establish the `api_extensions` list.
- [ ] 2.4 Add ETag support (`If-Match` → `412`) for any replaceable resource exposed later.

## 3. Operations & events (rest-api-operations)

- [ ] 3.1 Implement the in-memory operations registry and the operation object (id, class,
  description, timestamps, status/status_code, resources, metadata, may_cancel, err).
- [ ] 3.2 Implement `GET /1.0/operations`, `GET /1.0/operations/<uuid>`,
  `GET /1.0/operations/<uuid>/wait?timeout=N`, and `DELETE /1.0/operations/<uuid>` (cooperative
  cancel via `context.Context`).
- [ ] 3.3 Implement the events hub and `GET /1.0/events` websocket with type filtering; publish
  operation lifecycle/progress and logging events.

## 4. Knowledge endpoints (rest-api-knowledge)

- [ ] 4.1 Sync: `GET/POST /1.0/knowledge`, `GET/DELETE /1.0/knowledge/<name>` (no interactive
  confirm at API layer). Reuse `OpenSearchClient` create/list/delete.
- [ ] 4.2 Sync: `GET /1.0/knowledge/<name>/sources`, `GET .../sources/<id>`, `DELETE .../sources/<id>`.
- [ ] 4.3 Async: `POST /1.0/knowledge/<name>/sources` ingest (file upload, URL crawl, batch) as an
  operation with progress + cancel; reuse `processing.*` and `BulkIndex`/status updates.
- [ ] 4.4 Sync: `POST /1.0/search` hybrid search; reuse `OpenSearchClient.Search`; embedding-model-
  unavailable → error.
- [ ] 4.5 Async: knowledge-engine init operation (model deploy/pipelines/indexes); report model IDs.
- [ ] 4.6 Async: export and import operations; reuse the existing elasticdump-based export/import.
  Google Drive auth stays CLI-only.

## 5. Chat over websocket (rest-api-chat)

- [ ] 5.1 Implement `POST /1.0/chat` returning a websocket-class operation; implement the chat
  websocket endpoint and control protocol (prompt, set-active-kbs, token/think/done frames).
- [ ] 5.2 Move the `Session` state server-side (active indexes, history, resolved model); reuse the
  existing RAG turn logic (`rewriteSearchQuery` → `retrieveContext` → `buildRAGPrompt` → stream).
- [ ] 5.3 Stream tokens and `<think>` blocks as frames; enforce idle timeout and clean teardown on
  disconnect.

## 6. Batch answering (rest-api-answer)

- [ ] 6.1 Async: `POST /1.0/answer/batch` accepting a prepared manifest; run via
  `chat.ProcessBatchChat` as an operation with progress + cancel.
- [ ] 6.2 Make the structured results retrievable on completion (per-question answer, model,
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
