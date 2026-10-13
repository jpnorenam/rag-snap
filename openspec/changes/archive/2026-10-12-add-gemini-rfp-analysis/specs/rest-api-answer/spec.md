## ADDED Requirements

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
