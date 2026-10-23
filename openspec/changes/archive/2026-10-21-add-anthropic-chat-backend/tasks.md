## 1. Backend seam (no behaviour change)

- [x] 1.1 Add `cmd/cli/basic/chat/backend.go`: `Role`, `Message`, `Purpose`, `Request`, `Backend` interface, `BackendConfig{API, BaseURL}`, `NewBackend` (validates `openai`/`anthropic`, empty → `openai`), and sentinel errors `ErrModelListingUnsupported`, `ErrRefused{Category}`, `ErrOutputCeiling`
- [x] 1.2 Add `backend_openai.go` by moving today's logic verbatim: `clientOptions`, the `<think>`-aware stream loop (from `processStream`/`streamTurn`), the 503 "Loading model" ping retry and 401 guidance (from `checkServer`), `/models` listing with 404/405 → `ErrModelListingUnsupported`, temperature pass-through
- [x] 1.3 Convert history to `system string` + `[]Message`: update `history.go` (`turnsToHistory`, `historyToTurns`, `saveDirectChat`, `resumeDirectChat`), `rag.go` (`formatConversationForRewrite`, `rewriteSearchQuery` takes a `Backend`), and `LiveSession` fields
- [x] 1.4 Route every call site through `Backend`: REPL `Client`/`handlePrompt`, `LiveSession.Prompt`, `RunBatch`, `ProcessBatchChat`, `RefineQuestions`, `checkServer`, `FindModelName`/`findModelName`. Swap their `baseURL string` parameters for `BackendConfig`, and keep the REPL's blue `<think>` rendering driven by `TokenThink`
- [x] 1.5 Replace `ModelListingUnsupported(err)` callers with `errors.Is(err, chat.ErrModelListingUnsupported)`
- [x] 1.6 Update existing `chat` package tests to the new types; add a history round-trip test (`chatstore.Turn` ↔ `Message`)
- [x] 1.7 Checkpoint: `go build ./... && go test ./cmd/cli/basic/chat/...` pass with the OpenAI backend only

## 2. Config key and wiring

- [x] 2.1 Add `confChatAPI = "chat.api"` in `cmd/cli/basic/common.go` and a helper that resolves `chat.BackendConfig` from `chat.api` + the existing URL; use it in `chat.go`, `answer.go`, `rfp_refine.go`, `status.go`
- [x] 2.2 In `internal/api/config.go`, resolve and validate `chat.api` at startup (an invalid value fails like a missing `chat.http.host`); expose `clientCache.chatBackend()` in `clients.go`
- [x] 2.3 Pass `chatBackend()` through `handlers_chat.go` (`NewLiveSession`), `handlers_answer.go` (`RunBatch`), `handlers_answer_build.go` (`RefineQuestions`)
- [x] 2.4 Seed `config.package.chat.api="openai"` in `snap/hooks/install` (with a comment documenting `anthropic`) and `seed chat.api "openai"` in `snap/hooks/post-refresh`
- [x] 2.5 Test: `NewBackend` rejects `chat.api=bedrock` with an error naming the key and both accepted values; empty → openai

## 3. Anthropic backend

- [x] 3.1 `go get github.com/anthropics/anthropic-sdk-go` (core package only; confirm `aws-sdk-go-v2` is not added to `go.mod`), pin the version, `go mod tidy`
- [x] 3.2 Add `backend_anthropic.go`: build the client from `option.WithBaseURL(cfg.BaseURL)` + `option.WithAPIKey(CHAT_API_KEY)` only, and neutralise ambient `ANTHROPIC_AUTH_TOKEN`/profile/WIF auth (design D4)
- [x] 3.3 Map `Request` → `MessageNewParams`: top-level `System`, user/assistant `Messages`, `MaxTokens` from the D5 table by `Purpose`, `output_config.effort=low` for rewrite/ping, `thinking: adaptive + display summarized` for `PurposeChat` only; never set Temperature/TopP/TopK; emit one verbose notice when a non-nil Temperature is dropped
- [x] 3.4 Implement `Stream` (text deltas → `TokenAnswer`, thinking deltas → `TokenThink`, accumulate the final `Message`) and `Complete` as stream+accumulate; return only text blocks
- [x] 3.5 Stop-reason handling: `refusal` → `ErrRefused{Category}`; `max_tokens` with empty text → `ErrOutputCeiling`; map `*anthropic.Error` 401/403 to `CHAT_API_KEY` guidance
- [x] 3.6 `Ping`: one non-streaming 16-token request at low effort; any 2xx (including `max_tokens`) is healthy. `ListModels` always returns `ErrModelListingUnsupported`
- [x] 3.7 Explicit `cache_control: ephemeral` on the `system` block for `PurposeAnswer`
- [x] 3.8 Model resolution: when the backend can't list models and no model is configured, fail with "set `chat.model` (e.g. `global.anthropic.claude-sonnet-5-5`)" in `Client`, `NewLiveSession`, `RunBatch`
- [x] 3.9 httptest unit tests for the Anthropic backend: path is `/anthropic/v1/messages` for path `anthropic`; `x-api-key` = `CHAT_API_KEY`; no `Authorization` header and correct host with `ANTHROPIC_AUTH_TOKEN`/`ANTHROPIC_BASE_URL` set; body has `system`, positive `max_tokens`, no `temperature`; SSE text+thinking deltas map to the right `TokenKind`; refusal and empty-`max_tokens` responses yield the sentinel errors
- [x] 3.10 Batch test: a refused question goes through `hooks.failure` and the run continues; a no-context question makes no Messages request

## 4. Status

- [x] 4.1 `internal/api/handlers_status.go` `probeInference`: for `chat.api=anthropic`, skip listing, do a bounded TCP dial, report `chat.model` or `unreachable` with `chat.model` guidance; keep the OpenAI 404/405 path as is
- [x] 4.2 Same logic in `cmd/cli/basic/status.go`
- [x] 4.3 Add status tests for both anthropic scenarios (model set / unset) in `internal/api`

## 5. Docs

- [x] 5.1 `INSTALL.md` Bedrock section: add the Sonnet 5.5 config block (`chat.api`, `bedrock-runtime` host, `anthropic` path, `global.anthropic.claude-sonnet-5-5`), keep Mistral as the OpenAI-route alternative, and note the model-access and invoke-permission prerequisites
- [x] 5.2 `docs/bedrock_guide.md`: the bearer key must allow invoking the inference profile, and the bare id 404s; how to enable Sonnet 5.5 model access
- [x] 5.3 `docs/usage.md`: temperature is ignored with `chat.api=anthropic`; `chat.model` is required there; update the manifest `model` example; `docs/rest-api.md`: same notes for status and `temperature` fields
- [x] 5.4 `--temperature` help text on `chat` and `answer batch` mentions it is ignored by the Anthropic backend
- [x] 5.5 Update `CLAUDE.md` and `openspec/config.yaml` project context: the chat backend is OpenAI-compatible or Anthropic Messages (`chat.api`), via `openai-go/v3` / `anthropic-sdk-go`

## 6. Verification

- [x] 6.1 `make all` (tidy fmt vet lint test build); if the local golangci-lint panics, run `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run --new-from-rev=origin/main`
- [x] 6.2 `snapcraft -v` and `sudo snap install --dangerous ./rag-cli_*.snap`; confirm `rag-cli.rag get chat.api` shows `openai` after the refresh (post-refresh seed)
- [x] 6.3 Regression on `chat.api=openai` (Bedrock Mistral): one `rag-cli.rag chat` turn with a knowledge base active, and `status` reports running
- [x] 6.4 Switch to Sonnet per the design's Migration Plan; `rag-cli.rag status` reports `global.anthropic.claude-sonnet-5-5` without a generation call; REPL `chat` streams an answer with think output; `/save` then `/history` resume works
- [x] 6.5 Daemon path: `sudo snap restart rag-cli.ragd`, chat through the UI or websocket, and `answer batch` on a small existing manifest via ragd; confirm no 400s and answers are grounded
- [x] 6.6 Run one existing RFP manifest on both backends and compare answers for grounding/format regressions (markdown leakage, no-context rate); confirm the `global.` profile is billed at the global rate
