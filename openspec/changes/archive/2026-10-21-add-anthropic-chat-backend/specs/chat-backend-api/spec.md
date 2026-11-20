## ADDED Requirements

### Requirement: The chat wire protocol is selected by chat.api
The system SHALL select the inference wire protocol from the package-scoped config key `chat.api`, accepting `openai` (OpenAI Chat Completions) and `anthropic` (Anthropic Messages API), and SHALL treat an unset or empty value as `openai`. An unrecognised value SHALL fail the command or daemon request with an error naming the key and the accepted values. It SHALL NOT fall back silently to either protocol. The CLI direct path and the `ragd` daemon SHALL resolve the key identically, and every LLM call made by chat, answer batch, query rewrite and question refinement SHALL use the selected protocol.

#### Scenario: Unset key keeps today's behaviour
- **WHEN** `chat.api` is unset and the user runs `chat`
- **THEN** requests go to `{chat base URL}/chat/completions` exactly as before this change

#### Scenario: Anthropic selected
- **WHEN** `chat.api=anthropic` and the user runs `chat`
- **THEN** every request for that session goes to `{chat base URL}/v1/messages`

#### Scenario: Invalid value is rejected
- **WHEN** `chat.api=bedrock`
- **THEN** `chat` fails before any network call with an error naming `chat.api` and the values `openai` and `anthropic`

#### Scenario: Install and refresh seed the key
- **WHEN** the snap is freshly installed, or refreshed over an install that predates this change
- **THEN** a package-layer `chat.api` exists with value `openai`, so a `user` set of `chat.api` is accepted

### Requirement: Anthropic requests authenticate only with CHAT_API_KEY
With `chat.api=anthropic` the system SHALL send `CHAT_API_KEY` as the `x-api-key` header on every request and SHALL use the base URL built from `chat.http.host`, `chat.http.port`, `chat.http.path` and `chat.http.tls`, with `/v1/messages` appended. The system MUST NOT authenticate with, or change its endpoint because of, ambient `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL`, Anthropic CLI profiles, or workload-identity environment variables.

#### Scenario: Bedrock bearer token
- **WHEN** `chat.http.host=bedrock-runtime.us-east-2.amazonaws.com`, `chat.http.port=443`, `chat.http.path=anthropic`, `chat.http.tls=true` and `CHAT_API_KEY` is set
- **THEN** requests are sent to `https://bedrock-runtime.us-east-2.amazonaws.com:443/anthropic/v1/messages` with `x-api-key` equal to `CHAT_API_KEY`

#### Scenario: Ambient Anthropic credentials are ignored
- **WHEN** `ANTHROPIC_AUTH_TOKEN` and `ANTHROPIC_BASE_URL` are set in the environment alongside `CHAT_API_KEY`
- **THEN** the request carries no `Authorization` header derived from `ANTHROPIC_AUTH_TOKEN` and is sent to the configured chat base URL, not to `ANTHROPIC_BASE_URL`

#### Scenario: Missing key surfaces guidance
- **WHEN** the backend answers the readiness ping with 401 or 403
- **THEN** the error tells the user to set `CHAT_API_KEY`

### Requirement: Anthropic requests use the Messages request shape
With `chat.api=anthropic` the system SHALL send the system prompt in the top-level `system` field, SHALL send only `user` and `assistant` turns in `messages`, and SHALL set `max_tokens` explicitly on every request. The system MUST NOT send `temperature`, `top_p` or `top_k`. A temperature supplied by the `--temperature` flag, a batch manifest, or a REST request body SHALL be accepted and ignored, and verbose mode SHALL say that it was ignored.

#### Scenario: System prompt placement
- **WHEN** a chat turn is sent with the configured system prompt and two prior turns
- **THEN** the request body has the prompt in `system` and `messages` holds only the user/assistant turns plus the new user turn

#### Scenario: Temperature is not forwarded
- **WHEN** the user runs `answer batch manifest.yaml --temperature 0.1` with `chat.api=anthropic`
- **THEN** no request body contains `temperature`, and the run does not fail on account of it

#### Scenario: Output ceiling is explicit
- **WHEN** any Anthropic request is built (chat turn, batch answer, rewrite, refine, readiness ping)
- **THEN** the body contains a positive `max_tokens`

### Requirement: Auxiliary calls budget for reasoning
With `chat.api=anthropic` the query-rewrite and readiness-ping calls SHALL request low reasoning effort and SHALL set an output ceiling large enough that reasoning cannot leave the text empty under normal operation. A rewrite that returns no text SHALL fall back to the original query, as it does today. A readiness ping that stops on `max_tokens` SHALL count as success.

#### Scenario: Rewrite is not starved by reasoning
- **WHEN** the query-rewrite call runs against a reasoning model with `chat.api=anthropic`
- **THEN** the request sets low effort and a `max_tokens` well above the rewrite's expected output, and a non-empty rewrite is used for lexical retrieval

#### Scenario: Ping truncation is healthy
- **WHEN** the readiness ping returns `stop_reason: max_tokens`
- **THEN** the server is treated as ready

### Requirement: Anthropic streaming maps onto existing token kinds
With `chat.api=anthropic` the system SHALL stream chat turns and SHALL emit text deltas as answer tokens and thinking deltas as `think` tokens. These are the same kinds the REPL renders and the daemon websocket carries. Only text content SHALL be stored in conversation history or saved chats; thinking content SHALL NOT be stored. The `<think>`-tag detection of the OpenAI backend SHALL remain in effect for that backend only.

#### Scenario: Text streams as answer tokens
- **WHEN** the model streams text deltas for a daemon chat turn
- **THEN** each delta reaches the websocket client as an answer token, followed by the terminal message

#### Scenario: Thinking is labelled and not persisted
- **WHEN** the model streams thinking deltas before its answer
- **THEN** they reach the client as `think` tokens, and the saved chat contains only the answer text

### Requirement: Refusals are errors, not empty answers
With `chat.api=anthropic` a response whose `stop_reason` is `refusal` SHALL be reported as an error that includes the refusal category when one is present. It MUST NOT be returned as an empty answer or as the no-context answer. In `answer batch` a refused question SHALL go through the existing per-question failure hook and the run SHALL continue. A response that stops on `max_tokens` with no text SHALL also be reported as an error that names the output ceiling.

#### Scenario: Batch question refused
- **WHEN** the model refuses one question in a batch
- **THEN** that question is reported through the failure hook with the refusal category, and the remaining questions are answered

#### Scenario: Empty truncated answer
- **WHEN** a batch answer stops on `max_tokens` with no text content
- **THEN** the question is reported as failed with an error mentioning the output ceiling, not recorded with an empty answer

### Requirement: Anthropic backend requires a configured model
With `chat.api=anthropic` the system SHALL resolve the model only from an explicit source (the `--model` flag where present, the manifest `model`, the REST request, or `chat.model`) and SHALL NOT query a model-listing endpoint. When no model is configured, the command or daemon request SHALL fail with an error telling the operator to set `chat.model` (for example `global.anthropic.claude-sonnet-5-5`).

#### Scenario: Model configured
- **WHEN** `chat.api=anthropic` and `chat.model=global.anthropic.claude-sonnet-5-5`
- **THEN** chat and answer batch send `model: global.anthropic.claude-sonnet-5-5` and make no `GET /models` request

#### Scenario: Model missing
- **WHEN** `chat.api=anthropic` and no model is configured anywhere
- **THEN** `chat` fails with an error naming `chat.model`

### Requirement: Both backends produce the same pipeline behaviour
The RAG pipeline SHALL be identical across backends: query rewrite, retrieval, augmentation with the grounding prompt, the no-context short-circuit in answer batch, domain routing, `StripThinkTags` on final answers, history kept as the original prompt rather than the augmented one, saved-chat round-trips, and the batch JSON output (with `model` set to the model actually used). Switching `chat.api` SHALL NOT change the format of saved chats or batch output.

#### Scenario: Saved chat resumes across backends
- **WHEN** a chat saved under `chat.api=openai` is resumed after switching to `chat.api=anthropic`
- **THEN** its turns load as prior user/assistant messages and the conversation continues

#### Scenario: No-context question is not sent to the model
- **WHEN** a batch question retrieves no context with `chat.api=anthropic`
- **THEN** its answer is the fixed no-context response and no Messages request is made for it
