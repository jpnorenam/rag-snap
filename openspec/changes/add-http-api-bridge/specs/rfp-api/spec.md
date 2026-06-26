## ADDED Requirements

### Requirement: List knowledge bases for selection

The API SHALL provide `GET /1.0/knowledge` returning the available knowledge bases, so the UI can present them for selection before answering. Each entry SHALL identify the knowledge base by its user-facing name (not the raw index name).

#### Scenario: Listing knowledge bases

- **WHEN** a client requests `GET /1.0/knowledge`
- **THEN** the response is a list of available knowledge bases identified by their user-facing names
- **AND** the list reflects the same knowledge bases the CLI's `knowledge list` reports

### Requirement: Extract questions from an uploaded document

The API SHALL provide `POST /1.0/rfps:extract` that accepts an uploaded RFP/RFI document via `multipart/form-data` and extracts its questions, returning a question manifest. The extraction SHALL use the same logic as the CLI's `answer batch --build`, including optional LLM refinement that the caller MAY disable.

Because extraction is long-running, the endpoint SHALL behave as an asynchronous operation, and the operation's result SHALL be the extracted question manifest.

#### Scenario: Extracting questions from a document

- **WHEN** a client uploads a supported document to `POST /1.0/rfps:extract`
- **THEN** the API starts an asynchronous operation that extracts the document's questions
- **AND** the completed operation's result is a manifest containing the extracted questions

#### Scenario: Disabling refinement

- **WHEN** a client requests extraction with refinement disabled
- **THEN** the extracted questions are returned without the LLM refinement step

#### Scenario: Missing document

- **WHEN** a client calls `POST /1.0/rfps:extract` without a document file
- **THEN** the API returns a structured error and does not start an extraction

### Requirement: Answer a question manifest against selected knowledge bases

The API SHALL provide `POST /1.0/rfps:answer` that accepts a question manifest and a set of selected knowledge bases, runs each question through the same RAG + LLM batch pipeline the CLI uses, and returns the question-and-answer result. The supplied manifest MAY be one the UI has edited after extraction.

Because answering is long-running, the endpoint SHALL behave as an asynchronous operation, and the operation's result SHALL be the answered question-and-answer document in the shape the UI consumes.

#### Scenario: Answering an edited manifest

- **WHEN** a client posts a (possibly edited) question manifest and one or more selected knowledge bases to `POST /1.0/rfps:answer`
- **THEN** the API starts an asynchronous operation that answers each question via the RAG + LLM pipeline over the selected bases
- **AND** the completed operation's result is a question-and-answer document matching what `rag answer batch` produces for the same manifest and bases

#### Scenario: Empty or invalid manifest

- **WHEN** a client posts an empty manifest or references unknown knowledge bases
- **THEN** the API returns a structured error and does not run the batch

### Requirement: The question manifest is the contract carried between extract and answer

The question manifest returned by extraction SHALL be the same shape the answer endpoint accepts, so a client can extract, edit the questions, and submit them for answering without transformation. The API SHALL NOT require server-side persistence of the manifest between the two calls; the client carries it.

#### Scenario: Round-tripping a manifest

- **WHEN** a client takes the manifest returned by `POST /1.0/rfps:extract`, edits its questions, and submits it to `POST /1.0/rfps:answer`
- **THEN** the answer endpoint accepts the edited manifest without any separate server-side stored copy
