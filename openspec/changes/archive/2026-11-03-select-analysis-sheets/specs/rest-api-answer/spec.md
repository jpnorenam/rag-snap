## ADDED Requirements

### Requirement: A spreadsheet's sheets can be listed before analysis

The API SHALL provide an endpoint, `POST /1.0/answer/analyze/sheets`, that accepts one document as a
multipart upload and returns its sheet inventory: for each sheet, its 1-based position in workbook order, its name,
and its data-row count.

The endpoint SHALL answer synchronously. It SHALL make no model call and SHALL NOT require analysis to
be configured or an API key to be present. It SHALL stage nothing beyond the request, so the
inventory holds no session and has no expiry.

For a CSV the inventory SHALL hold one sheet. For a PDF or DOCX the endpoint SHALL return an empty
inventory together with the detected format, so a client can tell that sheet scope does not apply.
An unsupported file type SHALL be a 400 naming the supported types. A spreadsheet that cannot be
converted because the tika service is unavailable SHALL be an error saying so.

The endpoint SHALL apply the same per-request size bound as the analysis triage endpoint.

#### Scenario: Listing a workbook's sheets

- **WHEN** a client uploads an eight-tab workbook to the sheets endpoint
- **THEN** the response lists eight sheets in workbook order with positions 1 to 8, names and data-row counts
- **AND** no model call is made

#### Scenario: Listing a PDF

- **WHEN** a client uploads a PDF to the sheets endpoint
- **THEN** the response carries format `pdf` and an empty sheet list

#### Scenario: Analysis not configured

- **WHEN** no Gemini API key is present and a client uploads a workbook to the sheets endpoint
- **THEN** the inventory is returned

### Requirement: Analysis triage requests carry a sheet scope

The analysis triage endpoint SHALL accept an optional multipart field `sheets`, a JSON object that maps
sheet names or 1-based positions to `answer`, `context` or `exclude`, applying to the primary document. Its
meaning, defaults and validation SHALL be those of the analysis sheet scope. A malformed scope — not a
JSON object, or a value other than the three — and a scope given for a primary document that is not a
spreadsheet SHALL be a 400 before any operation starts. A scope naming a sheet the workbook does not
have, or leaving no sheet to answer, SHALL fail the operation after conversion and before any model
call, with the same message the CLI gives, including the list of available sheets.

The completed triage operation's metadata SHALL carry the resolved scope as a list of every sheet
with its position, name, data-row count and scope. The scope SHALL be staged with the analysis token,
so generation against that token applies it without the client sending it again.

#### Scenario: Scoped triage

- **WHEN** a client posts a workbook to the analysis triage endpoint with `sheets` setting positions 2 to 7 to `answer` and position 1 to `context`
- **THEN** the triage counts only positions 2 to 7
- **AND** the operation metadata lists all eight sheets with their resolved scopes, position 8 — which the field does not name — marked `exclude`

#### Scenario: Generation reuses the staged scope

- **WHEN** a client generates against a token whose triage was scoped
- **THEN** generation receives the same scoped document without a `sheets` field in the generation request

#### Scenario: Malformed scope

- **WHEN** a client posts a `sheets` field that maps a sheet to `skip`
- **THEN** the request is rejected with a 400 and no operation starts

#### Scenario: Unknown sheet

- **WHEN** a client posts a `sheets` field naming a sheet the workbook does not have
- **THEN** the operation fails with an error listing the available sheets, and no model call is made
