## Why

Today the snap and the web UI (`rag-snap-ui`, hosted on Firebase) are wired together by hand. The snap produces a RFP answer batch as a JSON file (`rag answer batch`), and a human uploads that file into the UI, which then stores everything in Firebase and lets a team review and edit the answers. There is no programmatic bridge between the two halves.

The desired workflow runs entirely from the UI: upload an RFP document → **extract** its questions → review/edit the extracted questions → select knowledge bases → **answer** the questions → review and collaborate. To make that possible, the snap must expose its two compute steps — question extraction and batch answering — over an HTTP API the browser can call, plus a way to list the available knowledge bases for selection.

The deployment trajectory is also known: the knowledge store (OpenSearch) moves off the laptop to a centralized GCP VM, and later to a Canonical-managed server; the UI stays on Firebase. The RAG retrieval/rerank loop is chatty with OpenSearch, so the API service should be deployable **next to** OpenSearch rather than pinned to a laptop. We therefore design a single networked service (`ragd`) that runs locally today (single box, for development) and is deployed onto the OpenSearch host as centralization proceeds — the same binary, the same contract, only the bind address and auth mode change.

## What Changes

- **Extract a transport-agnostic service layer** (`pkg/service`, package name TBD in design) holding `ExtractQuestions`, `AnswerQuestions`, and `ListKnowledgeBases`. These are lifted out of the Cobra `RunE` closures (`answer.go`'s `runBuild`, `chat.ProcessBatchChat`, `knowledge` list) so both the CLI and the new API call the same logic. The service returns data and errors and performs **no terminal I/O** (no `fmt.Printf`, no stdin prompts, no spinners).
- **Add a new HTTP API daemon** served by a new command (`rag api serve`) and wired as a snap `daemon`. It exposes a versioned REST API under `/1.0`, with:
  - `GET /1.0` — server info + `api_extensions` (feature detection) + auth mode.
  - `GET /1.0/knowledge` — list knowledge bases (for the UI's "select knowledge bases" step).
  - `POST /1.0/rfps:extract` — upload a document, extract its questions; returns a `Manifest` of questions. **Async** (long-running, Tika + extraction + optional refine).
  - `POST /1.0/rfps:answer` — run the RAG+LLM batch over a (UI-edited) `Manifest` against selected bases; returns a `QAFile`. **Async** (long-running).
  - `GET /1.0/operations/{id}` — poll an async operation.
  - `GET /1.0/events` — Server-Sent Events stream for operation progress.
- **Keep the daemon stateless.** Extract returns the `Manifest`; the UI persists and edits it in Firebase; answer takes the edited `Manifest` back. The daemon stores no workflow state, which also makes the centralized service safe under concurrent runners.
- **Authenticate with Google/Firebase OIDC bearer tokens** — the same identity the UI already holds via Firebase Auth — validated by a pluggable auth middleware. A simpler shared-token mode is available for local development. Cross-origin access from the Firebase UI is gated by a strict CORS origin allowlist and the Private Network Access preflight (needed only while a browser calls a `localhost` daemon).

### Out of scope (deferred)

- Exposing chat, search, ingest, export/import, or `knowledge init` over HTTP — this change covers only the extract → answer RFP workflow plus knowledge-base listing. The platform (`/1.0`, operations, events, auth) is built to accommodate them later.
- The feedback → model-retraining loop. Note the training signal (model answer vs. human-edited answer, plus ratings) is **already collected in Firebase** (`editedAnswers`, `ratings` in the UI's session state); harvesting it is a later, largely UI-side concern.
- Changes inside the `rag-snap-ui` repository (configurable API base URL, calling the endpoints). Tracked as cross-repo coordination, not part of this snap change.
- A WebSocket channel for streamed chat tokens (the platform reserves room for it; not built here).

### External services touched

- **OpenSearch** (`knowledge` store): yes — reused via the extracted service layer for `ListKnowledgeBases` and the answer batch's retrieval/rerank. No new pipelines, indexes, or models. OpenSearch is reached by its existing configurable URL (`knowledge.http.*`), so centralizing it to a GCP VM is a config change, not a code change.
- **Inference server** (`chat` backend): yes — reused by `AnswerQuestions` (the RAG+LLM batch) and the extraction refine step. No change to how it is configured; remains a local Inference snap or a third-party OpenAI-compatible endpoint.
- **Tika**: yes — reused by `ExtractQuestions` for document text extraction. No change to how it is bundled/run.

### New config keys

All under a new `api.*` namespace (dot-flattened, snapctl-backed), seeded as **package** keys by the install hook; origins/auth are expected to be overridden per deployment:

- `api.http.host` (package) — bind address; default `127.0.0.1` (local). Set to `0.0.0.0` on the centralized host.
- `api.http.port` (package) — bind port; default `9210`.
- `api.cors.origins` (package) — comma-separated allowed origins; default the Firebase UI origin.
- `api.auth.mode` (package) — `oidc` | `token`; default `oidc`. `token` is for local development.
- `api.auth.oidc.issuer` (package) — OIDC issuer (Firebase/Google) for token validation.
- `api.auth.oidc.audience` (package) — expected token audience (the Firebase project).
- `api.auth.allowed_domains` (user) — email-domain allowlist for authorization (e.g. `canonical.com`).

### New secrets (environment variables, never config)

- `RAG_API_TOKEN` — shared bearer secret for `token` auth mode (local development only). `oidc` mode requires **no** server-side secret; it validates against the provider's public keys.

### User-facing surface and documentation

This change adds a user-facing surface: a new `rag api serve` command (run as a snap daemon) and the HTTP API itself. Documentation that must change: `docs/usage.md` (a new "API / UI bridge" section), the new command's `--help`/usage text, and `apps/completion.bash`. The HTTP contract (endpoints, request/response shapes) should be documented for the `rag-snap-ui` team.

## Capabilities

### New Capabilities

- `http-api`: the HTTP API platform — a versioned `/1.0` REST surface served by a daemon, with feature-detection via `api_extensions`, a uniform JSON success/error contract, async operations with a progress event stream, OIDC/token authentication, and CORS/PNA access control for the browser-based UI.
- `rfp-api`: the RFP workflow endpoints — list knowledge bases, extract questions from an uploaded document, and answer an (edited) question manifest against selected bases, with the question `Manifest` as the contract carried by the UI between the two steps.

### Modified Capabilities

<!-- None. No existing spec-level behavior changes; the CLI commands keep their behavior, refactored onto the shared service layer. -->

## Impact

- **Affected code:**
  - New `pkg/service/` (or equivalent) — transport-agnostic `ExtractQuestions`, `AnswerQuestions`, `ListKnowledgeBases`; reused by both CLI and API.
  - New `cmd/cli/api/` (or `cmd/ragd/`) — HTTP server, routing (`net/http` ServeMux), `/1.0` handlers, operations registry, SSE events, auth + CORS/PNA middleware, response/error envelope helpers.
  - `cmd/cli/main.go` — register the new `api` command group (preserving the fixed command order).
  - `cmd/cli/basic/answer.go`, `cmd/cli/basic/chat/batch.go` — refactor `runBuild` / `ProcessBatchChat` to call the service layer; the CLI keeps its current behavior and output.
  - `cmd/cli/basic/rfp/extractor.go` — `Manifest`/`Question` become the API's extract response / answer request contract (may move to a shared package).
  - `snap/snapcraft.yaml` — new `daemon` app for `ragd`, `network-bind` plug; `snap/hooks/install` — seed `api.*` package defaults.
- **Reused as-is:** `rfp.ExtractFrom*` / `ExtractQuestionsFromText` / `WriteManifest`, `chat.ProcessBatchChat`, `chat.LoadBatchManifest`, `knowledge.OpenSearchClient` (list + search), `processing` (Tika), the snapctl config backend.
- **New config keys:** the `api.*` keys listed above (package, except `api.auth.allowed_domains` which is user).
- **New secrets:** `RAG_API_TOKEN` (env var, local token mode only).
- **New snap interfaces / changes:** `network-bind` plug; a new `daemon` app entry; install-hook seeding of `api.*` defaults. No new bundled binaries.
- **Dependencies:** add `gorilla/websocket` (reserved for the later chat stream; SSE for progress uses stdlib). A JWT/OIDC validation library for the auth middleware (e.g. `coreos/go-oidc`); exact choice decided in design.
