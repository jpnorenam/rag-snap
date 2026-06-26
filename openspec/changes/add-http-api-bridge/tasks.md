## 1. Service-layer extraction (prerequisite)

- [ ] 1.1 Create the service package (e.g. `pkg/service/`) with no terminal I/O (no `fmt.Printf`, stdin, spinners, or file writes). Define its dependency inputs (config-derived service URLs, embedding model ID, OpenSearch client) explicitly so both CLI and API can construct it.
- [ ] 1.2 Implement `ListKnowledgeBases(ctx) → []KnowledgeBase` wrapping `OpenSearchClient.ListIndexes` + `KnowledgeBaseNameFromIndex`.
- [ ] 1.3 Implement `ExtractQuestions(ctx, doc, opts) → rfp.Manifest` by lifting the core of `answer.go`'s `runBuild` (Tika extract + `rfp.ExtractFrom*`/`ExtractQuestionsFromText` + optional refine), without writing a manifest file.
- [ ] 1.4 Implement `AnswerQuestions(ctx, manifest, bases, opts) → QAFile` by wrapping `chat.ProcessBatchChat`, returning the result instead of writing JSON. Pick one canonical key (`result` vs `results`) for the `QAFile` JSON and document it for the UI.
- [ ] 1.5 Refactor the existing CLI commands (`answer batch`, `answer batch --build`, `knowledge list`) to call the service layer and format its returned data — preserving current CLI output and error behavior.
- [ ] 1.6 Move/confirm `rfp.Manifest` / `Question` and the `QAFile` types are in a package importable by both the CLI and the API without import cycles.

## 2. HTTP platform: server, discovery, error contract

- [ ] 2.1 Add the `api` command group and `rag api serve` subcommand in a new `cmd/cli/api/` (or `cmd/ragd/`); register it in `cmd/cli/main.go` preserving the fixed command order and `cobra.EnableCommandSorting = false`.
- [ ] 2.2 Build the HTTP server on stdlib `net/http` `ServeMux` (Go 1.22+ method+path patterns), reading bind host/port from `api.http.*` config.
- [ ] 2.3 Implement `GET /1.0` returning server info, the `api_extensions` array, and the active auth mode.
- [ ] 2.4 Implement a uniform error responder using `application/problem+json` (RFC 7807) with appropriate HTTP status codes; route unknown paths/methods to it.

## 3. HTTP platform: async operations + events

- [ ] 3.1 Implement an in-memory operations registry (id, kind, status, created/updated, error, result-ref) with creation, lookup, and TTL cleanup. No workflow data is persisted beyond operation status.
- [ ] 3.2 Long-running handlers return `202 Accepted` with `Location: /1.0/operations/{id}` and the operation object; `GET /1.0/operations/{id}` returns current status and, on completion, the result (or a link to fetch it).
- [ ] 3.3 Implement `GET /1.0/events` as a Server-Sent Events stream emitting operation lifecycle/progress events; support filtering to a single operation id.

## 4. RFP workflow endpoints

- [ ] 4.1 `GET /1.0/knowledge` → `ListKnowledgeBases`, returned as a JSON array of bases for the UI's selection step.
- [ ] 4.2 `POST /1.0/rfps:extract` accepting `multipart/form-data` (document file + optional flags e.g. `no_refine`); runs `ExtractQuestions` as an async operation; result is the `Manifest`.
- [ ] 4.3 `POST /1.0/rfps:answer` accepting JSON `{ manifest, bases, model?, temperature? }`; runs `AnswerQuestions` as an async operation; result is the `QAFile`.
- [ ] 4.4 Validate request bodies (missing file, empty manifest, unknown base names) and surface RFC 7807 errors.

## 5. Auth + CORS/PNA middleware

- [ ] 5.1 Implement an auth middleware interface with two implementations: `oidc` (validate Firebase/Google ID token against provider public keys; authorize by `api.auth.allowed_domains`) and `token` (compare `RAG_API_TOKEN`); select via `api.auth.mode`.
- [ ] 5.2 Implement CORS with a strict origin allowlist from `api.cors.origins` (no `*`); handle `OPTIONS` preflight; include credentials/headers needed by the UI.
- [ ] 5.3 Handle Chrome Private Network Access: answer preflights carrying `Access-Control-Request-Private-Network` with `Access-Control-Allow-Private-Network: true`.
- [ ] 5.4 Apply auth + CORS to all `/1.0/*` routes; confirm a request without a valid token is rejected and a request from a non-allowlisted origin is blocked.

## 6. Snap packaging

- [ ] 6.1 Add a `daemon: simple` app for `ragd`/`rag api serve` in `snap/snapcraft.yaml` with the `network-bind` plug.
- [ ] 6.2 Seed `api.*` package defaults in `snap/hooks/install` (host `127.0.0.1`, port `9210`, `cors.origins` = the Firebase UI origin, `auth.mode=oidc`, OIDC issuer/audience placeholders).
- [ ] 6.3 Add `gorilla/websocket` and the chosen OIDC validation library to `go.mod`; run `make tidy`.

## 7. Documentation (user-facing surface)

- [ ] 7.1 Add an "API / UI bridge" section to `docs/usage.md` (running the daemon locally, config keys, auth modes, the local-phase CORS/PNA caveat, the deployment phases).
- [ ] 7.2 Add `--help`/usage text for `rag api serve` and update `apps/completion.bash`.
- [ ] 7.3 Write the HTTP contract reference (endpoints, request/response JSON, the `Manifest` and `QAFile` shapes, operation/events model) for the `rag-snap-ui` team.

## 8. Cross-repo coordination (out of this repo's scope; track only)

- [ ] 8.1 `rag-snap-ui`: introduce a configurable API base URL (env var) — `http://localhost:9210` for dev.
- [ ] 8.2 `rag-snap-ui`: implement the extract → edit → select-bases → answer flow against the contract, attaching the Firebase ID token as a bearer header and persisting the `Manifest`/`QAFile` to RTDB as today.

## 9. Validation

- [ ] 9.1 Run `make all` (tidy fmt vet lint test build) locally.
- [ ] 9.2 Build and install the snap; confirm the `ragd` daemon starts, binds the configured port, and `GET /1.0` reports the auth mode and `api_extensions`.
- [ ] 9.3 Validate inside the installed snap (config paths depend on snapctl): drive `extract` (upload a sample RFP — e.g. `test-rfp/CLOVIS_RFP_CSR.yaml`-style document) → `answer` end to end, polling an operation and watching `/1.0/events`; confirm the resulting `QAFile` matches what `rag answer batch` produces for the same input.
- [ ] 9.4 Confirm the CLI commands (`answer batch`, `--build`, `knowledge list`) still behave and print as before after the service-layer refactor.
- [ ] 9.5 Auth/CORS checks: a valid Firebase token is accepted and a domain outside `allowed_domains` is rejected; a request from a non-allowlisted origin is blocked; the localhost PNA preflight succeeds from the deployed UI origin.
