## MODIFIED Requirements

### Requirement: Inference and Tika entries report service details

When reachable, the `inference` entry SHALL include the LLM model name detected from the
inference server (the same detection the CLI `status` command performs), and the `tika` entry
SHALL include the Tika server version when the server reports one. When the inference backend
cannot list models, because the OpenAI-compatible server answers `GET /models` with 404 or 405
or because `chat.api=anthropic` (which never lists models), the `inference` entry SHALL instead
report the configured `chat.model`. It SHALL NOT spend tokens on a generation request to probe
health. If no `chat.model` is configured in that case, the entry SHALL report `unreachable`
with an error telling the operator to set `chat.model`. A missing detail (probe succeeded but
detail unavailable) SHALL NOT change the service's `running` state.

#### Scenario: LLM model name detected

- **WHEN** the inference server is reachable and reports a model
- **THEN** the `inference` entry reports `running` and includes the detected LLM model name

#### Scenario: Anthropic backend reports configured model

- **WHEN** `chat.api=anthropic`, the chat endpoint accepts a TCP connection, and `chat.model=global.anthropic.claude-sonnet-5-5`
- **THEN** the `inference` entry reports `running` with model `global.anthropic.claude-sonnet-5-5`, and no Messages request was made

#### Scenario: Anthropic backend without a model

- **WHEN** `chat.api=anthropic` and `chat.model` is unset
- **THEN** the `inference` entry reports `unreachable` with an error naming `chat.model`

#### Scenario: Tika version reported

- **WHEN** the Tika server answers its version endpoint
- **THEN** the `tika` entry reports `running` and includes the reported version string
