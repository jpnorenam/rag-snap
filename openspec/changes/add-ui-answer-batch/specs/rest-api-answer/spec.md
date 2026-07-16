## ADDED Requirements

### Requirement: Candidate questions can be extracted from an uploaded document

The API SHALL provide `POST /1.0/answer/build` that accepts an uploaded RFP/RFI document
(PDF, DOCX, XLSX, or CSV) as `multipart/form-data` and extracts candidate questions from it as an
asynchronous, cancellable operation. Extraction SHALL use text/table extraction via Tika for
PDF/DOCX/XLSX and direct parsing for CSV, mirroring the CLI `answer batch --build` behavior. When
requested (the default), the operation SHALL additionally apply an LLM semantic-refinement pass
to the extracted questions; a request flag SHALL allow skipping refinement. On completion, the
operation SHALL publish the extracted candidate questions on its metadata so the client can
retrieve them for interactive review.

The endpoint SHALL NOT persist a manifest, and SHALL NOT run the batch — building a manifest and
running it remain distinct client-driven steps. An unsupported file type or a document from which
no questions can be extracted SHALL surface a clear error.

This requirement reverses the previous decision that the document-to-manifest build flow is
CLI-only. It is the first path in this capability to depend on Tika as an external service.

#### Scenario: Extracting questions from a document

- **WHEN** a client uploads a supported RFP document to `POST /1.0/answer/build`
- **THEN** the API returns an asynchronous operation that extracts candidate questions via Tika (or CSV parsing) and reports progress
- **AND** on completion the extracted questions are available on the operation metadata for review

#### Scenario: Optional LLM refinement

- **WHEN** a client requests extraction with refinement enabled (the default)
- **THEN** the operation applies the LLM semantic-refinement pass to the extracted questions before publishing them
- **AND** when the client disables refinement, the raw extracted questions are published unchanged

#### Scenario: Unsupported or empty document

- **WHEN** a client uploads an unsupported file type, or a document from which no questions can be extracted
- **THEN** the operation fails with a clear error rather than producing an empty or invalid manifest

#### Scenario: Build does not run the batch

- **WHEN** a build operation completes
- **THEN** no batch run is started and no manifest is persisted server-side
- **AND** the client must post a prepared manifest to `POST /1.0/answer/batch` to run it

## MODIFIED Requirements

### Requirement: Manifest is supplied prepared, not built interactively

The `POST /1.0/answer/batch` run endpoint SHALL accept an already-prepared batch manifest and
SHALL NOT perform interactive question extraction or review as part of running a batch. Document
question extraction is exposed separately as `POST /1.0/answer/build` (see "Candidate questions
can be extracted from an uploaded document"); the interactive review and selection of extracted
questions remains a client concern. The daemon SHALL NOT fold extraction into the run endpoint,
and SHALL NOT persist manifests on the client's behalf.

#### Scenario: Prepared manifest is accepted by the run endpoint

- **WHEN** a client posts a complete batch manifest to `POST /1.0/answer/batch`
- **THEN** the daemon runs it without requiring any interactive question-extraction or review step

#### Scenario: Extraction is a separate endpoint, not part of running

- **WHEN** a client wants to derive a manifest from a document
- **THEN** it uses `POST /1.0/answer/build` to extract candidate questions, reviews them client-side, and posts the resulting prepared manifest to `POST /1.0/answer/batch` to run it
