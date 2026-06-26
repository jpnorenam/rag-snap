## Context

The snap is a thin orchestrator over three networked services (OpenSearch, an OpenAI-compatible inference server, Tika). Its domain logic — the RFP extract step and the answer batch — currently lives **inside Cobra `RunE` closures**, interleaved with `fmt.Printf`, stdin prompts, file writes, and spinners. For example, `answer.go`'s `runBuild` extracts questions and writes a YAML manifest, and `chat.ProcessBatchChat` runs the batch and writes JSON. There is no layer an HTTP handler could call.

The consumer is `rag-snap-ui`: a Next.js app deployed on **Firebase** whose entire data/collaboration layer is the Firebase Realtime Database. It authenticates users with **Google via Firebase Auth**. Today it ingests data only by manual JSON file upload (`FileLoader` → `parseQAFile` → RTDB). It currently makes **no** backend API calls of any kind, so the API contract is greenfield and we own both repositories.

The data contract already exists in two types:
- `rfp.Manifest { Questions []Question }` — produced by extraction, the thing the UI will edit between the two steps.
- The `QAFile { generated_at, model, result[]: {id, question, answer} }` shape — produced by the answer batch and parsed by the UI ([`lib/types.ts`](https://github.com/Ivannicus/rag-snap-ui/blob/master/lib/types.ts)).

Deployment trajectory (given): OpenSearch is local now → a GCP VM → a Canonical server; the UI stays on Firebase. OpenSearch is already reached via a configurable URL (`knowledge.http.*`), so "centralizing OpenSearch" is a config change for the existing code.

## Goals / Non-Goals

**Goals:**
- Let the UI drive the full extract → edit → answer workflow over HTTP, replacing the manual file handoff.
- One service (`ragd`), built once, deployable progressively closer to OpenSearch (laptop → GCP VM → Canonical server) with only bind-address and auth-mode changes.
- Reuse the exact RAG pipeline the CLI uses, so API results match CLI results.
- Keep the snap's confinement, snapctl config model, and env-var secret model intact.

**Non-Goals:**
- A general "expose the whole CLI over REST" API. Only the RFP workflow + KB listing here.
- Server-side persistence of RFP/workflow state — the UI/Firebase owns it.
- The retraining feedback loop (data already accrues in Firebase).
- gRPC, and a WebSocket chat stream (room is reserved; not built).

## Decisions

### 1. `ragd` is co-located with OpenSearch; "local" is the single-box degenerate case
The RAG loop makes several OpenSearch round-trips per question, across many questions per batch. Running `ragd` on a laptop while OpenSearch sits on a GCP VM would push that chatty loop over the internet. So `ragd` is designed as a **networked service deployed next to OpenSearch**: locally during development (`--bind 127.0.0.1`), then onto the OpenSearch host (`--bind 0.0.0.0` + TLS) as centralization proceeds. The same `rag-cli` snap is the deployment unit everywhere — installed on a laptop for dev (CLI + daemon) and on the GCP VM as the centralized service. This keeps the snapctl config backend available in every deployment (the snap provides `snapctl` on the VM too).

Consequence: only *runners* of an answer batch ever need the snap; *reviewers* keep using the Firebase UI only. Once `ragd` is centralized, runners need nothing but a browser.

### 2. Authenticate with Firebase/Google OIDC across all phases; pluggable middleware
The UI already signs users in with Google via Firebase Auth, so it can attach the current user's ID token: `Authorization: Bearer <await auth.currentUser.getIdToken()>`. `ragd` validates that JWT against the provider's public keys (offline) and authorizes by email domain (`api.auth.allowed_domains`, e.g. `canonical.com`). This:
- Reuses the identity the UI already has — no second login.
- Is identical local → GCP → Canonical server; only the bind address/TLS changes.
- Defeats local-phase CSRF for free: a malicious website the user happens to visit cannot forge a Firebase Google token, so **no separate pairing-token scheme is needed**.

Auth is a middleware interface with two implementations: `oidc` (default) and `token` (a shared `RAG_API_TOKEN` for local development convenience). The mode is `api.auth.mode`. Secrets stay in env vars per project convention; `oidc` mode needs no server-side secret.

### 3. Stateless daemon; Firebase owns workflow state
`extract` returns a `Manifest`; the UI persists and edits it (in RTDB, exactly as it persists everything else); `answer` takes the edited `Manifest` back in its request body. `ragd` stores nothing between calls. This matches how the UI already works and makes a shared, centralized daemon safe under concurrent runners (each request is independent). The `/1.0/operations/{id}` registry holds only in-flight/recent operation status, not workflow data.

### 4. Conventional REST + RFC 7807 errors; borrow LXD's *operations* and *extensions*, not its envelope
The consumer is a web frontend whose developers expect ordinary REST: resource objects returned directly, standard HTTP status codes, and `application/problem+json` for errors. We therefore **do not** adopt LXD's idiosyncratic sync/async envelope (`{"type":"sync","status_code":100,…}`). We **do** borrow two LXD ideas that genuinely fit:
- **Async operations**: long-running calls (`extract`, `answer`) return `202 Accepted` with a `Location: /1.0/operations/{id}` and an operation object; the client polls that URL or watches `/1.0/events`.
- **`/1.0` + `api_extensions[]`**: a single stable version, extended via a named-extensions array for feature detection, rather than version bumps.

Progress uses **Server-Sent Events** (`/1.0/events`, browser `EventSource`) — lighter than a WebSocket for one-way server→client updates. A WebSocket is reserved for the future bidirectional chat stream only.

### 5. The two compute steps map to existing functions, lifted into a service layer
`ExtractQuestions(doc) → Manifest` is the core of `answer.go`'s `runBuild` (Tika extract + `rfp.ExtractFrom*`/`ExtractQuestionsFromText` + optional LLM refine) minus file writing and printing. `AnswerQuestions(manifest, bases) → QAFile` is `chat.ProcessBatchChat` (already a standalone function) minus file writing. `ListKnowledgeBases()` wraps `OpenSearchClient.ListIndexes` + `KnowledgeBaseNameFromIndex`. The service package returns data/errors only; the CLI keeps its current printing by formatting the returned data, and the API handlers serialize it to JSON.

### 6. Transport stack: lean stdlib, matching the Canonical/LXD idiom
Router: stdlib `net/http` `ServeMux` with Go 1.22+ method+path patterns (e.g. `"POST /1.0/rfps:extract"`) — the project is on Go 1.24, so no third-party router is needed. JSON via `encoding/json`. `gorilla/websocket` is added but used only for the reserved chat stream (same library LXD uses). No web framework (gin/echo/fiber). The CLI keeps Cobra.

### 7. Daemon lifecycle via a snap `daemon` app
`rag api serve` starts the HTTP server using the in-process service layer and the snapctl-backed config. It is wired in `snapcraft.yaml` as a `daemon: simple` app with a `network-bind` plug. The install hook seeds `api.*` package defaults (host `127.0.0.1`, port `9210`, CORS origin = the Firebase UI origin, `auth.mode=oidc`). On a centralized host the operator overrides host/TLS/origins via config. Command order in `cmd/cli/main.go` is preserved when adding the group.

### 8. Document upload shape
`POST /1.0/rfps:extract` accepts `multipart/form-data` (the RFP document file, plus optional flags mirroring `--no-refine`). This is the natural browser file-upload encoding and keeps Tika extraction server-side.

## Ruled-out alternatives

- **Firebase as a job bus** (UI writes a job to RTDB; a local snap-worker subscribed to RTDB runs it and writes results back). Elegant for a pure-local tool and consonant with the UI's real-time model, but it is a **dead end relative to centralization**: it would require building a job-queue protocol over RTDB and coupling the snap to Firebase credentials + egress, then deleting it once `ragd` has a network endpoint. Rejected.
- **Laptop-resident daemon as the permanent home** (only OpenSearch centralizes; `ragd` stays local forever). Pushes the chatty RAG loop over the internet and forces every runner to keep a heavy local install. Rejected in favor of co-locating `ragd` with OpenSearch (Decision 1).
- **gRPC surface.** No typed-client consumer exists (the consumer is a browser), and it diverges from the lean REST+WS stack. Rejected.
- **LXD's response envelope.** Idiosyncratic for web developers; conventional REST + RFC 7807 is friendlier (Decision 4). We keep LXD's operations/extensions ideas only.
- **A bespoke pairing-token auth scheme.** Made unnecessary by reusing Firebase OIDC (Decision 2).

## Risks / Trade-offs

- **Browser → `localhost` friction during the local phase.** An HTTPS page (Firebase) calling `http://localhost` works only via the browser's localhost secure-context exception, and Chrome's Private Network Access gate requires answering a preflight with `Access-Control-Allow-Private-Network: true` (easy to miss; fails silently). Accepted and bounded — this friction exists only while `ragd` runs on a laptop; it disappears once `ragd` is on the GCP VM behind real TLS. Mitigated by explicit CORS + PNA handling and dev docs.
- **Service-layer extraction is the bulk of the work and risks behavior drift in the CLI.** The extract/answer logic is entangled with I/O; lifting it could subtly change CLI output or error handling. Mitigated by keeping the CLI commands as thin formatters over the service results and validating CLI behavior is unchanged.
- **OIDC validation correctness.** Misconfigured issuer/audience or clock skew can reject valid tokens or (worse) accept wrong ones. Mitigated by using a vetted OIDC library, pinning issuer/audience via config, and domain-based authorization.
- **Long answer batches over a request/response API.** A big RFP can run for minutes; the async operation + SSE model handles this, but a client that only polls must handle long-running operations and reconnects. Accepted (async is the right model); durable, tab-independent runs are a later concern if needed.
- **Config surface growth.** A new `api.*` namespace adds package keys an operator must understand per deployment. Mitigated by sensible install-hook defaults and documentation.

## Migration / Deployment notes

- **Phase 0 (dev/local):** install the snap on the laptop; `ragd` binds `127.0.0.1:9210`; `auth.mode` may be `token` for convenience; the Firebase UI (with a configurable API base URL pointing at `http://localhost:9210`) drives the workflow. Requires CORS + PNA handling.
- **Phase 1 (centralize OpenSearch + ragd to a GCP VM):** install the same snap on the VM; OpenSearch runs on/with the VM; `ragd` binds `0.0.0.0` behind TLS; `auth.mode=oidc`; CORS origin = the Firebase UI; the UI repoints its API base URL at the VM. No code change.
- **Phase 2 (Canonical server):** same service and config moved to the managed host; UI repoints again.
- The single UI-side seam that makes this smooth: the API base URL is a UI configuration value (one env var), not hard-coded.
