## Context

Every LLM call in rag-cli goes through `openai-go/v3` Chat Completions. The calls live in
`cmd/cli/basic/chat`:

| Call site | File | Shape today |
|---|---|---|
| REPL turn | `client.go` `handlePrompt` / `processStream` | streaming, `<think>` colouring |
| Daemon chat turn | `turn.go` `LiveSession.Prompt` / `streamTurn` | streaming via `StreamFunc` |
| Batch answer | `batch.go` `RunBatch` | non-streaming, `temperature` |
| Query rewrite | `rag.go` `rewriteSearchQuery` | non-streaming, `max_tokens=256` |
| Question refine | `refine.go` `RefineQuestions` | non-streaming JSON |
| Readiness ping | `client.go` `checkServer` | `max_tokens=1`, retries on 503 |
| Model lookup | `client.go` `FindModelName` / `findModelName`, `ModelListingUnsupported` | `GET /models` |

Conversation state is `openai.ChatCompletionNewParams.Messages`, a
`[]openai.ChatCompletionMessageParamUnion`. It is threaded through `history.go`
(save/resume), `rag.go` (`formatConversationForRewrite`) and `LiveSession`.

Callers pass a bare `baseURL string`. The CLI builds it in `cmd/cli/basic/common.go` and the
daemon in `internal/api/config.go`, both from `chat.http.{host,port,path,tls}`. `CHAT_API_KEY`
is the bearer token.

What we know about the target (researched 2026-10-07 and again now):

- Claude Sonnet 5.5 on Bedrock supports the Anthropic Messages API, not Chat Completions. That
  is why the config-only swap on `…/openai/v1` failed. *Corrected after the first live run
  (2026-10-21):* in `us-east-2` the working route is
  `https://bedrock-runtime.{region}.amazonaws.com/anthropic/v1/messages`, with an inference
  profile as the model (`global.anthropic.claude-sonnet-5-5` or `us.anthropic.claude-sonnet-5-5`;
  the bare id has no in-region support). The `bedrock-mantle` endpoint the design first assumed
  serves Sonnet 5.5 only in `us-gov-west-1`. In `us-east-2` it answered 404 "The model
  'anthropic.claude-sonnet-5-5' does not exist" (source: the AWS model card for Claude
  Sonnet 5.5).
- Anthropic's and AWS's docs both show the standard client with base URL
  `…/anthropic` and the Bedrock bearer token as API key (`x-api-key`). SigV4 is only needed for IAM credentials, which we do not use.
- Sonnet 5.5 rejects non-default `temperature`/`top_p`/`top_k` with a 400. It always runs
  adaptive thinking (`{type: "disabled"}` is a 400). Its default effort is `high`, and reasoning
  tokens count against `max_tokens`.
- Bedrock's Messages route has no Anthropic Models API and no server-side `fallbacks`; the
  design never calls either. Explicit cache breakpoints are supported (minimum 512 tokens per
  checkpoint on Sonnet 5.5).

Constraints: config is snapctl-only with package/user layers, and a `user` set needs an
existing package key. Secrets come only from environment variables. The daemon (`ragd`) and
the CLI direct path must behave identically.

## Goals / Non-Goals

**Goals:**
- Sonnet 5.5 on Bedrock works for `chat` (REPL and daemon websocket), `answer batch`
  (CLI direct and daemon), query rewrite and question refinement, selected purely by config.
- The OpenAI backend (local Inference snap, Bedrock Mistral) keeps its current behaviour.
- One seam in the code, so a future backend is one more file rather than another rewire.

**Non-Goals:**
- SigV4 / IAM / service-role auth. Bearer token only, as today.
- Tool use, structured outputs, prompt-level migration of the system prompts for Sonnet.
  Prompts are reused as they are; tuning them is follow-up work.
- Client-side refusal fallback to another model (see Open Questions).
- UI changes. The UI's temperature controls are left in place; the server ignores them under
  this backend.
- Gemini analysis. It has its own client and key and is untouched.
- Auto-detecting the protocol from host or model id.

## Decisions

### D1. Explicit `chat.api` key, default `openai`

A package-scoped `chat.api` takes `openai` or `anthropic`. Unset means `openai`, so existing
installs are unaffected.

*Alternatives:* inferring the protocol from `chat.http.host` containing `bedrock-mantle`, or
from `chat.model` starting with `anthropic.`. Both are rejected as fragile. Proxies, gateways
and Claude Platform on AWS (bare ids, different host) would all be misclassified, and the
failure would be an opaque 404. An explicit key fails loudly with a clear message.

Seeding: `snap/hooks/install` sets `config.package.chat.api=openai`, and
`snap/hooks/post-refresh` adds `seed chat.api "openai"`. `seed()` never overwrites, so a
maintainer's `--package` value survives a refresh.

### D2. A provider-neutral `Backend` interface inside `cmd/cli/basic/chat`

New file `backend.go`:

```go
type Role string // "user" | "assistant"
type Message struct{ Role Role; Content string }

type Request struct {
    Model       string
    System      string
    Messages    []Message
    Temperature *float64 // honoured by openai, ignored by anthropic
    Purpose     Purpose  // PurposeChat | PurposeAnswer | PurposeRewrite | PurposeRefine
}

type Backend interface {
    Complete(ctx context.Context, r Request) (string, error)
    Stream(ctx context.Context, r Request, emit StreamFunc) (string, error) // returns final text
    Ping(ctx context.Context, model string) error
    ListModels(ctx context.Context) ([]string, error) // ErrModelListingUnsupported
}

type BackendConfig struct{ API, BaseURL string }
func NewBackend(c BackendConfig) (Backend, error)
```

Errors: `ErrModelListingUnsupported`, `ErrServerLoading` (OpenAI 503 "Loading model"),
`*StatusError{StatusCode}`, `*RefusalError{Category}` and `*OutputCeilingError{MaxTokens}`.
Output ceilings are chosen per `Purpose` inside each backend; there is no per-request
`MaxTokens` field.

`backend_openai.go` moves today's code behind this interface unchanged: `clientOptions`,
`<think>` detection in streaming, the 503 "Loading model" retry, `MaxCompletionTokens` plus
`MaxTokens` on the ping, and the temperature pass-through. `backend_anthropic.go` implements
the Messages API.

`Purpose` keeps per-call tuning (ceilings, effort, thinking display) inside each backend, so
call sites never branch on provider.

The public entry points that take `baseURL string` today take `BackendConfig` instead:
`Client`, `NewLiveSession`, `RunBatch`, `ProcessBatchChat`, `RefineQuestions` and
`FindModelName`. `ModelListingUnsupported(err)` becomes
`errors.Is(err, ErrModelListingUnsupported)`; the OpenAI backend maps 404/405 to that error.

*Alternative:* an `if anthropic {…}` branch at each of the seven call sites. Rejected because it
duplicates the request-building logic seven times and keeps history typed to OpenAI.

### D3. History becomes `[]Message` with the system prompt held separately

`LiveSession` and the REPL hold `system string` plus `history []Message`. Each backend turns
these into its own wire shape: OpenAI prepends a system message, Anthropic sets the top-level
`system`. `history.go` converts `chatstore.Turn` to and from `Message` directly; the store
format is already role/content, so saved chats stay compatible across backends.
`formatConversationForRewrite` takes `[]Message`.

### D4. Anthropic client built from explicit options only

```go
anthropic.NewClient(
    option.WithBaseURL(cfg.BaseURL),           // …/anthropic ; SDK appends /v1/messages
    option.WithAPIKey(os.Getenv("CHAT_API_KEY")),
    option.WithMaxRetries(2),
)
```

The Anthropic SDK reads `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL`, CLI
profiles and workload-identity variables by default. An explicit base URL and API key override
only the first and third; `ANTHROPIC_AUTH_TOKEN` would still add an `Authorization` header. The
client is therefore built with `option.WithoutEnvironmentDefaults()` (anthropic-sdk-go v1.84.0),
which skips the whole autoloader. A unit test against `httptest.Server` with
`ANTHROPIC_AUTH_TOKEN` and `ANTHROPIC_BASE_URL` set proves it (spec: *Ambient Anthropic
credentials are ignored*). The test fails without the option. This follows the same never-fall-back rule the Gemini analysis client uses for
`CHAT_API_KEY`.

We use the core `anthropic` package, not `bedrock.NewMantleClient`. The Mantle client exists
for SigV4 and would pull in `aws-sdk-go-v2`, which bearer auth does not need.

Config for Sonnet 5.5:
`chat.api=anthropic`, `chat.http.host=bedrock-runtime.us-east-2.amazonaws.com`, `chat.http.port=443`,
`chat.http.path=anthropic`, `chat.http.tls=true`, `chat.model=global.anthropic.claude-sonnet-5-5`.

### D5. Request shape and per-purpose tuning (Anthropic)

| Purpose | Transport | `max_tokens` | Effort | Thinking display |
|---|---|---|---|---|
| Chat (REPL, daemon) | stream | 32 000 | model default | `summarized` → `think` tokens |
| Answer (batch) | stream, accumulate | 32 000 | model default | omitted |
| Refine | stream, accumulate | 64 000 | model default | omitted |
| Rewrite | stream, accumulate | 2 048 | `low` | omitted |
| Ping | non-stream | 16 | `low` | omitted |

- **Stream internally, even for `Complete`.** Large `max_tokens` on a non-streaming request
  risks the SDK's long-request guard and HTTP timeouts, and reasoning at default effort can run
  long. `Complete` streams and accumulates into a final `Message`.
- **Generous ceilings.** Reasoning shares the output budget, so today's 256-token rewrite would
  often return empty text and silently fall back to the raw query. The rewrite gets 2 048 tokens
  at `low` effort.
- **No sampling parameters ever.** Sonnet 5.5 rejects them. A non-nil `Temperature` is dropped,
  with a single verbose-mode notice per session or run.
- **No `thinking` field.** Thinking is adaptive by default. `{type:"disabled"}` is a 400 on
  Sonnet 5.5, and `between_tools` is Sonnet-5.5-specific (it would 400 on Opus 5.5). Effort is
  the portable control. The exception is the chat display, which sends
  `thinking:{type:"adaptive", display:"summarized"}` so the REPL shows reasoning instead of a
  silent pause, the same experience as `<think>` models today.
- **Batch prompt caching.** The batch system prompt is identical for every question, so the
  Anthropic backend puts an explicit `cache_control: ephemeral` breakpoint on the `system` block
  for `PurposeAnswer`. Bedrock accepts explicit breakpoints, and a prompt below the minimum
  cacheable size is simply not cached, so this cannot break a request.
- **Stop reasons.** `refusal` → `ErrRefused{Category}`. `max_tokens` with no text →
  `ErrOutputCeiling`. `max_tokens` with text is returned as is. Ping treats any 2xx as healthy,
  including `max_tokens`.
- **History content.** Only text blocks go into `history`. Thinking blocks are not replayed:
  the turn is stored as plain text, as with OpenAI, which keeps history append-only and portable
  across models.

### D6. Model resolution and status

The Anthropic `ListModels` always returns `ErrModelListingUnsupported`. Callers that today fall
back to `FindModelName` when `chat.model` is empty (`Client`, `NewLiveSession`, `RunBatch`,
`status`) get a clear error: "set `chat.model` (e.g. `global.anthropic.claude-sonnet-5-5`)".

`probeInference` (daemon) and `status` (CLI) already handle "listing unsupported → use
`chat.model`". For `chat.api=anthropic` they skip the listing call, do a bounded TCP dial to the
endpoint (as `handshake` does), and report `chat.model`. Status never spends tokens.

`checkServer`, the REPL's pre-flight, does spend a 16-token ping. That is deliberate: it is the
only way to surface a 403 (no model access) or a bad key before the user types a question.

### D7. Daemon plumbing

`internal/api/config.go` resolves `chat.api` next to the URL and validates it at startup, so a
bad value fails `ragd` the same way a missing `chat.http.host` does. `clientCache` gains
`chatBackend() chat.BackendConfig`. `handlers_chat.go`, `handlers_answer.go`,
`handlers_answer_build.go` and `handlers_status.go` pass it through. REST request bodies keep
their optional `temperature`; under Anthropic it is ignored (spec), so the API contract and the
UI are unchanged.

## Risks / Trade-offs

- **The Bedrock API key may lack permission to invoke the inference profile, or the account may
  lack Sonnet 5.5 access.** Mitigation: the D6 ping surfaces 401/403 with guidance. The docs tell operators to
  grant model access, and the first live call is the acceptance test.
- **Wrong host or model id fails as a 404 "model does not exist".** The first live run hit this
  on `bedrock-mantle`. Mitigation: the docs name the `bedrock-runtime` host and inference-profile
  ids, and explain the 404.
- **SDK default `anthropic-version`.** Checked during implementation: anthropic-sdk-go v1.84.0
  sends `anthropic-version: 2023-06-01`, which the Messages endpoint accepts. The SDK is pinned
  in `go.mod`, so an upgrade that changes the header is deliberate.
- **Ignoring temperature changes output determinism for anyone who relied on `--temperature
  0.1`.** It never guaranteed identical output. Batch answers are now steered by the prompt only.
  We accept this; it is documented, and verbose mode reports it.
- **Prompts written for Mistral.** The grounding rules ("answer only from context") should carry
  over, but Sonnet 5.5 may be more verbose or format differently (markdown in answers, see the
  Telekom v2 notes). Mitigation: compare one existing batch manifest on both backends before
  switching production runs. Prompt tuning is follow-up work.
- **Refactor blast radius.** History type changes touch the REPL, daemon and save/resume paths,
  and most of this code is untested. Mitigation: the OpenAI backend keeps its logic verbatim;
  add unit tests for history conversion and both backends' request bodies (httptest); run the
  existing `chat` package tests.
- **Cost.** Sonnet 5.5 at default effort reasons on every batch answer, and it is pricier per
  token than Mistral Large 3. Mitigation: the batch system-prompt cache breakpoint. An effort
  knob is deferred (Open Questions).

## Migration Plan

1. Ship with default `chat.api=openai`. Installs keep working unchanged after refresh; the
   post-refresh hook seeds the key.
2. To switch an install to Sonnet:
   ```bash
   sudo rag-cli.rag set --package chat.api=anthropic
   sudo rag-cli.rag set --package chat.http.host=bedrock-runtime.us-east-2.amazonaws.com
   sudo rag-cli.rag set --package chat.http.path=anthropic
   sudo rag-cli.rag set --package chat.model=global.anthropic.claude-sonnet-5-5
   # port 443 / tls true unchanged; CHAT_API_KEY unchanged (Bedrock API key)
   sudo snap restart rag-cli.ragd
   ```
3. Rollback: set the four keys back (`chat.api=openai`, `bedrock-runtime…`, `openai/v1`, Mistral
   id). No data migration is needed; saved chats and batch outputs are backend-neutral.

## Resolved Questions

- **Refusal fallback (resolved 2026-10-21): report as failed.** A refused batch question goes
  through the per-question failure hook with its refusal category, and a refused chat turn is
  an error. No retry on another model.
- **Effort knob (resolved 2026-10-21): not now.** Answer, chat and refine calls use the model
  default (`high` on Sonnet 5.5). Only the rewrite and ping calls pin `low` (D5). No
  `chat.effort` key.

## Open Questions

- **Global vs regional pricing.** Resolved by the model id: the `global.` profile carries no
  premium, while `us.` keeps traffic in US regions at the regional rate. Pick `us.` if data
  residency matters for an RFP.
