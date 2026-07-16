# rest-api-answer Specification

## Purpose

Expose structured batch question-answering over the API. A client submits a manifest of
questions (and the knowledge bases to ground them in); the daemon runs each question through the
RAG+LLM pipeline as a background operation and makes the JSON results retrievable. This covers
the `rag-cli.rag answer batch` run path.

## Requirements

### Requirement: Batch answering runs as an operation

The API SHALL provide `POST /1.0/answer/batch` that accepts a batch manifest of questions and
the knowledge bases to use, and runs them as an asynchronous operation. Each question SHALL be
answered using the same RAG+LLM pipeline as the chat answer path (keyword rewrite merged with any
manifest keywords, hybrid retrieval, grounded generation). When no context is retrieved for a
question, the answer SHALL be the fixed "not enough information" response rather than an
ungrounded generation.

The prompt templates driving generation SHALL come from the daemon prompt store
(`rest-api-prompts`): the resolved `answer_system_prompt` — the variant named by the request's
`prompt_ref` when one is given, otherwise the slot's active variant, otherwise the built-in
default — and the `source_rules` override (or its default). Prompts SHALL be resolved when the
batch operation starts; changes to stored prompts, variants, or active pointers SHALL apply to
operations started afterwards and SHALL NOT alter an operation already running.

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

#### Scenario: Active variant drives new batch runs

- **WHEN** a variant is active on `answer_system_prompt` (or `source_rules` is customized) and a
  client starts a batch operation without a `prompt_ref`
- **THEN** the operation's generation uses the resolved templates instead of the built-in
  defaults

#### Scenario: Mid-run prompt edits do not affect the running operation

- **WHEN** a stored prompt, variant, or active pointer is updated while a batch operation is running
- **THEN** the running operation continues with the prompts it started with
- **AND** the next batch operation started uses the updated resolution

### Requirement: Batch requests can reference a named prompt variant

The batch request/manifest SHALL accept an optional `prompt_ref` naming a variant of
`answer_system_prompt`. `prompt_ref` and the inline custom `prompt` SHALL be mutually exclusive;
a request carrying both SHALL be rejected with a validation error before any question is
answered. When `prompt_ref` is given, the variant SHALL be resolved when the operation starts
and its head version used as the batch system prompt exactly as the slot's effective value would
be (not the inline-custom path that appends `source_rules`). An unknown variant SHALL fail the
request with a not-found error. The completed results SHALL record the resolved prompt
provenance — variant name and version number, or empty for the built-in default — alongside the
existing per-question fields, for every batch run whether or not a `prompt_ref` was given.

#### Scenario: prompt_ref drives the run

- **WHEN** a client posts a manifest with `prompt_ref` naming a stored variant
- **THEN** the operation's system prompt is that variant's head version at operation start

#### Scenario: prompt_ref and inline prompt conflict

- **WHEN** a client posts a manifest carrying both `prompt_ref` and an inline `prompt`
- **THEN** the API rejects the request with a validation error and no operation is created

#### Scenario: Results carry prompt provenance

- **WHEN** a batch operation completes
- **THEN** its results record which prompt resolution ran — the variant name and version, or the built-in default

### Requirement: Batch results are retrievable

On completion, the operation SHALL make the batch results available in a structured form that
includes, per question, the question and its generated answer, along with the model used and a
generation timestamp — equivalent to the JSON output the CLI writes today.

#### Scenario: Retrieving completed results

- **WHEN** a batch operation completes successfully
- **THEN** the client can retrieve the structured results, including each question, its answer, the model used, and a generation timestamp

### Requirement: Manifest is supplied prepared, not built interactively

The API SHALL accept an already-prepared batch manifest. The interactive document-to-manifest
"build" flow (extracting questions from PDF/DOCX/XLSX/CSV with interactive review and refinement)
is a CLI-client concern and is NOT part of this API capability. If document-derived manifests are
needed over the API in future, they SHALL be added as a separate, explicit capability.

#### Scenario: Prepared manifest is accepted

- **WHEN** a client posts a complete batch manifest
- **THEN** the daemon runs it without requiring any interactive question-extraction or review step

#### Scenario: Interactive build is not exposed

- **WHEN** a client looks for an endpoint that extracts questions from a document with interactive review
- **THEN** no such endpoint exists in this capability
