## Why

The chat backend speaks only OpenAI Chat Completions (`openai-go/v3`), and the Bedrock setup
points at `bedrock-runtime…/openai/v1` with `chat.model=mistral.mistral-large-3-675b-instruct`.
Changing `chat.model` to Claude Sonnet 5.5 does not work, for two reasons:

- Claude Sonnet 5.5 on Bedrock supports the Anthropic Messages API
  (`bedrock-runtime.{region}.amazonaws.com/anthropic/v1/messages`, model
  `global.anthropic.claude-sonnet-5-5`), not the OpenAI-compatible `…/openai/v1` route.
- Every chat, answer and rewrite call sends a non-default `temperature` (0.3 / 0.1), which
  Sonnet 5.5 rejects with a 400 even on the right endpoint.

We want Sonnet 5.5 as the answering model for chat and `answer batch`. That needs a second
wire protocol, not a config tweak.

## What Changes

- Add an **Anthropic Messages** chat backend next to the existing OpenAI-compatible one.
  A new package config key `chat.api` selects it: `openai` (default, today's behaviour) or
  `anthropic`.
- Move every LLM call in `cmd/cli/basic/chat` behind one small backend interface: the REPL
  turn, the daemon `LiveSession`, the batch answer, the query rewrite, question refinement, the
  readiness ping and model lookup. Conversation history becomes provider-neutral; it is no
  longer `[]openai.ChatCompletionMessageParamUnion`.
- With `chat.api=anthropic`:
  - requests go to `{chat.http base}/v1/messages` with `CHAT_API_KEY` sent as the Bedrock
    bearer token in `x-api-key`;
  - the system prompt goes in the top-level `system` field;
  - `max_tokens` is always set explicitly;
  - sampling parameters are never sent. **BREAKING for this backend only:** `--temperature`
    and the manifest/REST `temperature` are accepted but ignored, with a verbose-mode notice.
  - Reasoning deltas surface as the existing `think` token kind.
  - A `refusal` stop reason is reported as an error. It is never treated as an empty answer.
- Model listing is unsupported on Bedrock's Messages route, so `chat.model` is **required** with
  `chat.api=anthropic`. `status` and `GET /1.0/status` report the configured model without
  spending tokens.
- Ambient `ANTHROPIC_*` environment variables and on-disk Anthropic profiles must never
  authenticate a chat request. The only credential is `CHAT_API_KEY`.
- The `openai` backend keeps its current behaviour: endpoints, temperature, `<think>` handling,
  `/models` lookup and the local Inference snap all stay as they are.
- Seed `chat.api=openai` in the install hook, and in the post-refresh hook so existing installs
  can set it.

Services touched: **inference server only** (the `chat` backend). OpenSearch and Tika are
unaffected. Gemini analysis is unaffected; it keeps its own client and key.

New config key: `chat.api`, **package** scoped, seeded to `openai`; a `user` override is allowed.
No new secrets.

User-facing surfaces: no new commands or flags. Behaviour changes for `--temperature` on
`chat` / `answer batch` under the new backend, and `chat.model` becomes mandatory there.
Docs to update: `INSTALL.md` (Bedrock section), `docs/bedrock_guide.md`, `docs/usage.md`
(temperature notes, manifest `model` example), `docs/rest-api.md` (status / temperature),
the `--temperature` flag help text, and `CLAUDE.md` / `openspec/config.yaml` (no longer
"OpenAI-compatible only").

## Capabilities

### New Capabilities
- `chat-backend-api`: selecting the inference wire protocol (`chat.api`) and the Anthropic
  Messages backend contract. Covers endpoint and auth, credential isolation, system prompt
  placement, explicit output ceilings, no sampling parameters, reasoning-stream mapping,
  refusal handling, required model id, and parity with the OpenAI backend for chat, answer
  batch, rewrite and refine.

### Modified Capabilities
- `rest-api-status`: the `inference` entry's model detail comes from the configured `chat.model`
  when the backend has no model listing, including all `chat.api=anthropic` setups. In that
  case `running` means the endpoint is reachable and a model is configured. It is not a
  model-listing result.

## Impact

- **Code:** `cmd/cli/basic/chat/` (client.go, turn.go, rag.go, batch.go, refine.go,
  history.go, plus new backend files), `cmd/cli/basic/` (chat.go, answer.go, status.go,
  rfp_refine.go, common.go), and `internal/api/` (config.go, clients.go, handlers_chat.go,
  handlers_answer.go, handlers_answer_build.go, handlers_status.go).
- **Dependencies:** adds `github.com/anthropics/anthropic-sdk-go`. Only its core client is
  used (bearer token plus base URL), so the AWS SDK is not pulled in.
- **Snap:** `snap/hooks/install` and `snap/hooks/post-refresh` seed `chat.api`. No new plugs,
  interfaces or bundled binaries; the existing `network` plug covers it.
- **Operators:** switching to Sonnet requires changing `chat.http.host`, `chat.http.path` and
  `chat.api` as well as `chat.model`. The Bedrock API key must be allowed to invoke
  the inference profile, and Sonnet 5.5 model access must be granted in the account.
