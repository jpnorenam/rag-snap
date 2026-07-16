<!--
Ordering dependency: this delta MODIFIES "Candidate questions can be extracted
from an uploaded document", which is ADDED by the not-yet-archived change
`add-ui-answer-batch` (Change 4). Change 4 must be synced/archived into the main
rest-api-answer spec before this change is archived, so the MODIFIED base exists.
-->

## MODIFIED Requirements

### Requirement: Candidate questions can be extracted from an uploaded document

The API SHALL provide `POST /1.0/answer/build` that accepts an uploaded RFP/RFI document
(PDF, DOCX, XLSX, or CSV) as `multipart/form-data`. It runs as an asynchronous, cancellable
operation and behaves differently by document kind:

- **Free-text formats (PDF, DOCX):** extraction runs in one pass. The operation detects candidate
  questions from the document text (mirroring the CLI `answer batch --build` behavior) and, when
  requested (the default; a request flag SHALL allow skipping it), applies an LLM semantic-refinement
  pass. On completion the operation SHALL publish the extracted candidate questions on its metadata
  for interactive review.
- **Tabular formats (XLSX, CSV):** the operation SHALL NOT extract questions on this pass, because
  the column that holds the questions cannot be reliably guessed. Instead it SHALL parse the document
  into one or more tables (Tika for XLSX, direct parsing for CSV) and complete with a discriminated
  response indicating a column choice is required: a flag marking that a column is needed, an opaque
  build token identifying the parsed tables staged server-side, and, per table, the header row and —
  for each column — a small sample of cell values and an average cell length. The response SHALL
  include a suggested table and column computed by a scoring heuristic (header synonyms, average cell
  length, question-mark fraction, penalizing identifier/numeric columns), offered only as a default
  selection and never used to extract without an explicit client choice. Actual extraction for
  tabular formats is performed by `POST /1.0/answer/build/extract` (see below).

In all cases the endpoint SHALL NOT persist a manifest and SHALL NOT run the batch — building a
manifest and running it remain distinct client-driven steps. An unsupported file type SHALL surface
a clear error, as SHALL a free-text document from which no questions can be extracted. This
requirement reverses the previous decision that the document-to-manifest build flow is CLI-only, and
its Tika dependency (for XLSX and PDF/DOCX) is the first in this capability.

#### Scenario: Extracting questions from a free-text document

- **WHEN** a client uploads a PDF or DOCX to `POST /1.0/answer/build`
- **THEN** the API returns an asynchronous operation that extracts candidate questions in one pass
- **AND** on completion the extracted questions are available on the operation metadata for review

#### Scenario: Spreadsheet upload requires a column choice

- **WHEN** a client uploads an XLSX or CSV to `POST /1.0/answer/build`
- **THEN** the operation completes without extracting questions, publishing instead the parsed tables (headers and per-column sample cells) plus a staged build token and a suggested column
- **AND** the client must choose a column and call `POST /1.0/answer/build/extract` to obtain questions

#### Scenario: Suggested column is a default, not an auto-run

- **WHEN** a spreadsheet is parsed on the first pass
- **THEN** the heuristic's suggested table and column are returned only as a preselected default
- **AND** no questions are extracted and no batch is run until the client confirms a column

#### Scenario: Optional LLM refinement

- **WHEN** a client requests extraction with refinement enabled (the default)
- **THEN** the applicable extraction pass applies the LLM semantic-refinement pass before publishing the questions
- **AND** when the client disables refinement, the raw extracted questions are published unchanged

#### Scenario: Unsupported file type

- **WHEN** a client uploads a file whose type is not PDF, DOCX, XLSX, or CSV
- **THEN** the operation fails with a clear error rather than producing an empty or invalid manifest

#### Scenario: Build does not run the batch

- **WHEN** a build operation completes
- **THEN** no batch run is started and no manifest is persisted server-side
- **AND** the client must post a prepared manifest to `POST /1.0/answer/batch` to run it

## ADDED Requirements

### Requirement: Column-scoped extraction from a staged spreadsheet

The API SHALL provide `POST /1.0/answer/build/extract` that extracts questions from a single chosen
column of a table that was parsed and staged by a prior `POST /1.0/answer/build` call. The request
SHALL identify the staged tables by their build token and specify the table and column to extract,
an optional identifier column (defaulting to auto-numbered IDs when none is given), a minimum cell
length (cells shorter than which are skipped), and whether to apply LLM refinement. It SHALL run as
an asynchronous operation and SHALL NOT require the document to be re-uploaded or re-parsed.

On completion the operation SHALL publish the extracted questions on its metadata in the same shape
a free-text `POST /1.0/answer/build` publishes, so a client consumes one results shape regardless of
document kind. A build token that is unknown or expired SHALL yield a clear error directing the
client to re-upload the document. When the chosen column yields no questions after the minimum-length
filter, the operation SHALL fail with a message identifying the column so the client can choose a
different column or lower the minimum length and retry — without starting a batch run.

#### Scenario: Extracting from a chosen column

- **WHEN** a client posts a valid build token with a chosen table and column to `POST /1.0/answer/build/extract`
- **THEN** the operation extracts questions from that column of the staged table and publishes them in the standard results shape
- **AND** the document is neither re-uploaded nor re-parsed

#### Scenario: Expired or unknown build token

- **WHEN** a client posts a build token that is unknown or has expired
- **THEN** the API responds with a clear error directing the client to re-upload the document

#### Scenario: Chosen column yields nothing

- **WHEN** the chosen column produces no questions after the minimum-length filter
- **THEN** the operation fails with a message identifying the column
- **AND** no batch run is started, so the client can retry with a different column or minimum length

#### Scenario: Identifier column defaults to auto-numbering

- **WHEN** a client does not specify an identifier column
- **THEN** the extracted questions are assigned sequential identifiers
