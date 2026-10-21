## ADDED Requirements

### Requirement: Analysis requests carry operator instructions and prompt parts

The analysis triage endpoint SHALL accept an optional `instructions` form field carrying the operator's
free-text instructions. Instructions over the length limit SHALL be rejected with a 400 naming the
limit, before any model call. Accepted instructions SHALL be kept on the analysis session and applied
by the generation endpoint for the same token, without being sent again.

The triage operation metadata SHALL echo the instructions it was given, so a client can show what the
session is working to.

On completion, the generation operation metadata SHALL carry, in addition to the manifest, the
model-written part of the prompt and the appended rules block as separate fields, such that their
concatenation is the manifest's prompt.

#### Scenario: Triage with instructions

- **WHEN** a client uploads an RFP with an `instructions` field to the triage endpoint
- **THEN** the triage request to the model carries the instructions, and the operation metadata echoes them

#### Scenario: Generation reuses the session's instructions

- **WHEN** a client posts a triage token from a session started with instructions to the generation endpoint
- **THEN** generation applies the same instructions without the client sending them again

#### Scenario: Over-long instructions are rejected

- **WHEN** the `instructions` field exceeds the limit
- **THEN** the request fails with a 400 naming the limit and no operation is started

#### Scenario: Prompt parts in generation metadata

- **WHEN** a generation operation completes
- **THEN** its metadata carries the model-written prompt and the appended rules as separate fields whose concatenation is the manifest's prompt
