## MODIFIED Requirements

### Requirement: Batch answering runs as an operation

The API SHALL provide `POST /1.0/answer/batch` that accepts a batch manifest of questions and
the knowledge bases to use, and runs them as an asynchronous operation. Each question SHALL be
answered using the same RAG+LLM pipeline as the chat answer path (keyword rewrite merged with any
manifest keywords, hybrid retrieval, grounded generation). When no context is retrieved for a
question, the answer SHALL be the fixed "not enough information" response rather than an
ungrounded generation.

The manifest SHALL accept the optional `domains` routing list, the optional `questions[].source`
field, and the optional `kapa_source_groups` list, and the daemon SHALL apply domain routing —
resolution, injection into the question turn, and inheritance of domain keywords into retrieval —
identically to the CLI's direct run path, as defined by `answer-batch-domains`. None of these fields
SHALL be dropped in transit. A manifest carrying an invalid `domains` entry SHALL fail the request
with a validation error before any question is answered.

When the manifest carries `kapa_source_groups`, the daemon SHALL perform kapa.ai retrieval against
exactly those groups alongside retrieval over the active knowledge bases, as defined by
`kapa-retrieval`, resolving credentials from the daemon's own configuration and environment. The
daemon SHALL NOT run a batch without kapa retrieval when the manifest selects source groups and the
integration is configured. When source groups are selected but the integration is unconfigured, the
operation SHALL report that kapa grounding was requested and could not be applied, and SHALL still
answer from the active knowledge bases.

The prompt templates driving generation SHALL come from the daemon prompt store
(`rest-api-prompts`): the resolved `answer_system_prompt` — the variant named by the request's
`prompt_ref` when one is given, otherwise the slot's active variant, otherwise the built-in
default — and the `source_rules` override (or its default). Prompts SHALL be resolved when the
batch operation starts; changes to stored prompts, variants, or active pointers SHALL apply to
operations started afterwards and SHALL NOT alter an operation already running. Domain routing SHALL
NOT alter the resolved system prompt, which SHALL remain identical for every question in the run.

The operation's metadata SHALL convey progress across the questions, and the operation SHALL be
cancellable.

#### Scenario: Running a batch manifest

- **WHEN** a client posts a manifest of questions to `POST /1.0/answer/batch`
- **THEN** the API returns an asynchronous operation
- **AND** the operation answers each question via the RAG+LLM pipeline and reports progress

#### Scenario: A question with no retrieved context

- **WHEN** a question in the batch retrieves no grounding context
- **THEN** its answer is the fixed "not enough information" response, not an ungrounded generation

#### Scenario: Cancelling a batch run

- **WHEN** a client cancels a running batch operation
- **THEN** processing stops cooperatively and the operation reports cancellation

#### Scenario: Posted domains are applied

- **WHEN** a client posts a manifest carrying a `domains` list
- **THEN** the operation resolves each question against it and applies the matched domain to that question's turn and retrieval query

#### Scenario: Posted source is not dropped

- **WHEN** a client posts a manifest whose questions carry `source`
- **THEN** the value reaches the run and is available as a fallback match key

#### Scenario: Posted kapa source groups are not dropped

- **WHEN** a client posts a manifest carrying `kapa_source_groups`
- **THEN** the value reaches the run and kapa retrieval is performed against exactly those groups

#### Scenario: Kapa selection with an unconfigured integration

- **WHEN** a client posts a manifest carrying `kapa_source_groups` and the daemon has no kapa credentials
- **THEN** the operation reports that kapa grounding was requested but is unconfigured
- **AND** the batch still answers from the active knowledge bases

#### Scenario: Invalid domains entry is rejected

- **WHEN** a client posts a manifest containing a `domains` entry with neither `context` nor `keywords`
- **THEN** the API rejects the request with a validation error and no operation is created

#### Scenario: Active variant drives new batch runs

- **WHEN** a variant is active on `answer_system_prompt` (or `source_rules` is customized) and a
  client starts a batch operation without a `prompt_ref`
- **THEN** the operation's generation uses the resolved templates instead of the built-in
  defaults

#### Scenario: Mid-run prompt edits do not affect the running operation

- **WHEN** a stored prompt, variant, or active pointer is updated while a batch operation is running
- **THEN** the running operation continues with the prompts it started with
- **AND** the next batch operation started uses the updated resolution
