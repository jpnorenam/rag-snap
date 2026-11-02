## MODIFIED Requirements

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

When the model's response is truncated, or empty because reasoning consumed the output budget, the
operation SHALL fail rather than completing with a short manifest, and that failure SHALL NOT be
acceptable by any request.

When the generated question count falls materially short of the triage estimate from a questions call
that finished normally, the operation SHALL fail with a structured shortfall error carrying the expected
count, the produced count and that the call finished normally. The questions produced SHALL be retained
on the analysis session for as long as the session lives. A client SHALL be able to post the same token
again with the interview answers and an explicit acceptance of the shortfall. Generation then SHALL
continue from the retained questions without a second questions call, and the resulting manifest SHALL
record the acceptance. An acceptance posted for a session that holds no retained shortfall SHALL be
rejected rather than ignored.

The structured shortfall error SHALL also carry the per-section produced and estimated counts. As an
alternative to accepting, a client SHALL be able to post the same token and interview answers with a
request to re-run the extraction. The daemon then SHALL repeat only the questions call, at the next
higher reasoning effort up to the highest, over the staged documents, and SHALL continue as for a fresh
generation. A re-run request for a session that holds no retained shortfall SHALL be rejected.

#### Scenario: Generating a manifest from a triage token

- **WHEN** a client posts a valid triage token and interview answers to the generation endpoint
- **THEN** the request returns a tracked asynchronous operation
- **AND** on completion the metadata carries the generated manifest in the shape the batch endpoint accepts, its check findings, and the generating model

#### Scenario: Expired token

- **WHEN** a client posts an expired triage token to the generation endpoint
- **THEN** the request fails with an error stating the session expired and the documents must be uploaded again

#### Scenario: Truncated generation

- **WHEN** generation is truncated against the model's output ceiling
- **THEN** the operation fails, no manifest is returned, and no acceptance is offered

#### Scenario: Shortfall is reported with both counts

- **WHEN** generation produces 111 questions against a triage estimate of 168 from a call that finished normally
- **THEN** the operation fails with a structured shortfall error carrying 168, 111 and that the call finished normally

#### Scenario: Accepting a shortfall

- **WHEN** a client posts the same token and interview answers with an explicit acceptance after a shortfall
- **THEN** generation continues from the retained questions without another questions call, and the completed manifest records the acceptance with both counts

#### Scenario: Re-running the extraction after a shortfall

- **WHEN** a client posts the same token and interview answers with a re-run request after a shortfall from a low-effort questions call
- **THEN** the questions call is repeated at medium effort over the staged documents, without a re-upload, and generation continues as for a fresh generation

#### Scenario: Acceptance without a shortfall

- **WHEN** a client posts an acceptance for a session that holds no retained shortfall
- **THEN** the request is rejected with an error saying there is no shortfall to accept

#### Scenario: Generated manifest is directly runnable

- **WHEN** a client submits a generated manifest from the operation metadata to the batch endpoint unchanged
- **THEN** it is accepted, with no field dropped in transit
