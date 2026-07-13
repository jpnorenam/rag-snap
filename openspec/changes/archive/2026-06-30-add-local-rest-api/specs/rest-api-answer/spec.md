# rest-api-answer Specification

## Purpose

Expose structured batch question-answering over the API. A client submits a manifest of
questions (and the knowledge bases to ground them in); the daemon runs each question through the
RAG+LLM pipeline as a background operation and makes the JSON results retrievable. This covers
the `rag-cli.rag answer batch` run path.

## ADDED Requirements

### Requirement: Batch answering runs as an operation

The API SHALL provide `POST /1.0/answer/batch` that accepts a batch manifest of questions and
the knowledge bases to use, and runs them as an asynchronous operation. Each question SHALL be
answered using the same RAG+LLM pipeline as the chat answer path (keyword rewrite merged with any
manifest keywords, hybrid retrieval, grounded generation). When no context is retrieved for a
question, the answer SHALL be the fixed "not enough information" response rather than an
ungrounded generation.

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
