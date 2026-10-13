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

The manifest SHALL accept the optional `domains` routing list and the optional `questions[].source`
field, and the daemon SHALL apply domain routing — resolution, injection into the question turn, and
inheritance of domain keywords into retrieval — identically to the CLI's direct run path, as defined
by `answer-batch-domains`. Neither field SHALL be dropped in transit. A manifest carrying an invalid
`domains` entry SHALL fail the request with a validation error before any question is answered.

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
includes, per question, the question, its generated answer, and the domain applied to it (absent when
no domain resolved), along with the model used and a generation timestamp — equivalent to the JSON
output the CLI writes today.

#### Scenario: Retrieving completed results

- **WHEN** a batch operation completes successfully
- **THEN** the client can retrieve the structured results, including each question, its answer, the model used, and a generation timestamp

#### Scenario: Results carry the applied domain

- **WHEN** a batch operation completes and a question resolved to a domain
- **THEN** that question's result records the applied domain
- **AND** a question that resolved to no domain carries no domain value

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

### Requirement: Document analysis triage runs as an operation

The API SHALL provide an endpoint that accepts an RFP document and zero or more additional context
documents as a multipart upload and runs the first analysis phase as an asynchronous operation, in the
same shape as the existing document build endpoint.

On completion the operation metadata SHALL carry the triage: the RFP identity as stated in the
documents, the section or sheet structure found, the estimated question count, whether the RFP requires
OpenStack to be addressed, whether questions carry more than one field to be answered each, and the
suggested knowledge bases. It SHALL also carry, per uploaded document, the format detected and whether
the document was conveyed natively or converted, so a client can show what happened to each file.

The metadata SHALL carry a token identifying the conveyed documents, for the generation phase to
reference. The token SHALL expire, and the metadata SHALL convey when it does, so a client can tell
the operator their session has a lifetime. Tokens SHALL be staged in memory, in the same manner as the
existing build tokens, and SHALL NOT be persisted.

Because the staged entry holds the conveyed document content rather than a reference to it, the store
SHALL bound both the size accepted per request and the total it will hold across sessions, and SHALL
reject an over-large request before conveying anything rather than after. A rejection SHALL name the
limit and the measured value.

The endpoint SHALL reject a request when analysis is not configured or no API key is present in the
environment, with an error naming what is missing, and SHALL NOT answer the questions instead.

The API key SHALL NOT appear in any response, in operation metadata, or in any error message.

#### Scenario: Uploading documents for triage

- **WHEN** a client uploads an RFP document and two context documents to the analysis triage endpoint
- **THEN** the request returns a tracked asynchronous operation
- **AND** on completion the operation metadata carries the triage, the per-document conveyance report, and a token with its expiry

#### Scenario: Analysis not configured

- **WHEN** a client posts to the analysis triage endpoint with no API key present in the environment
- **THEN** the request fails with an error naming the environment variables consulted, and no model call is made

#### Scenario: Upload exceeding the staging bound

- **WHEN** a client uploads a document set larger than the staging store accepts
- **THEN** the request is rejected naming the limit and the measured value, before any document is conveyed

#### Scenario: Key is never returned

- **WHEN** any analysis request succeeds or fails
- **THEN** neither the response body nor the operation metadata nor the error message contains the API key

### Requirement: Manifest generation runs as an operation against a triage token

The API SHALL provide an endpoint that accepts a triage token together with the interview answers —
the target answer length in words, whether that length is a hard limit or a soft target, and the
OpenStack flavour when one was chosen — and runs the second analysis phase as an asynchronous
operation.

The endpoint SHALL NOT require the documents to be uploaded again; it SHALL generate from the documents
the token refers to.

On completion the operation metadata SHALL carry the generated manifest in the same JSON shape the
batch endpoint accepts, so a client can run it without a further conversion, and SHALL carry the check
findings for that manifest. The metadata SHALL also carry the model that generated it.

The endpoint SHALL reject a request carrying an unknown or expired token with an error stating that the
analysis session expired and the documents must be uploaded again.

When the model's response is truncated, when it is empty because reasoning consumed the output budget,
or when the generated question count falls materially short of the triage estimate, the operation SHALL
fail with an error naming the expected and produced counts, rather than completing with a short manifest.

#### Scenario: Generating a manifest from a triage token

- **WHEN** a client posts a valid triage token and interview answers to the generation endpoint
- **THEN** the request returns a tracked asynchronous operation
- **AND** on completion the metadata carries the generated manifest in the shape the batch endpoint accepts, its check findings, and the generating model

#### Scenario: Expired token

- **WHEN** a client posts an expired triage token to the generation endpoint
- **THEN** the request fails with an error stating the session expired and the documents must be uploaded again

#### Scenario: Truncated generation

- **WHEN** generation is truncated against the model's output ceiling
- **THEN** the operation fails naming the expected and produced question counts and no manifest is returned

#### Scenario: Generated manifest is directly runnable

- **WHEN** a client submits a generated manifest from the operation metadata to the batch endpoint unchanged
- **THEN** it is accepted, with no field dropped in transit

### Requirement: Manifests can be checked without being run

The API SHALL provide an endpoint that accepts a batch manifest and returns its check findings
synchronously, without answering any question and without calling any model.

The response SHALL carry the question and domain counts, the per-domain match counts, and the findings
at their two severities, so a client can distinguish a clean manifest from one carrying warnings and
from one carrying defects.

The endpoint SHALL accept any manifest the batch endpoint accepts, whether or not analysis produced it,
and SHALL NOT modify or store it.

A manifest whose references cannot be resolved because the knowledge store is unreachable SHALL still
be checked for its structural, routing and prompt findings, with the reference checks reported as not
performed rather than as passing.

#### Scenario: Checking a manifest

- **WHEN** a client posts a manifest to the check endpoint
- **THEN** the response carries the counts, the per-domain match counts, and the findings by severity, and no question was answered

#### Scenario: Checking a hand-written manifest

- **WHEN** a client posts a manifest that analysis did not produce
- **THEN** it is checked and reported normally

#### Scenario: Reference checks unavailable

- **WHEN** a manifest is checked while the knowledge store is unreachable
- **THEN** the response reports the structural, routing and prompt findings and marks the reference checks as not performed

### Requirement: Analysis endpoints are advertised as capabilities

The daemon SHALL advertise the analysis and check endpoints in the capability list it reports, in the
same manner as the existing batch and build capabilities, so a client can discover whether the daemon
it is talking to supports them rather than probing.

Analysis SHALL be advertised as available only when the daemon can actually run it, so a client can
present the path as unavailable rather than offering it and failing at submit time.

#### Scenario: Capabilities include analysis

- **WHEN** a client reads the daemon's capability list on a daemon that can run analysis
- **THEN** the analysis and check capabilities are listed

#### Scenario: Analysis unconfigured is not advertised as available

- **WHEN** a client reads the capability list on a daemon with no analysis API key present
- **THEN** analysis is not advertised as available
