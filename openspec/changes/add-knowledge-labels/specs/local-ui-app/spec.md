# local-ui-app Specification (delta)

## ADDED Requirements

### Requirement: Default label can be edited from the base detail

The base detail view SHALL offer an edit affordance next to the displayed default label that
opens a modal with the label value pre-filled (validated against the knowledge-labels format)
and an "apply to existing" checkbox explaining that only chunks and sources without a label are
backfilled. Submitting without backfill SHALL update synchronously and refresh the detail;
submitting with backfill SHALL start a tracked operation and notify on completion. Validation
errors SHALL keep the modal open with the entered value preserved.

#### Scenario: Editing the default label

- **WHEN** the user opens the edit-label modal, enters `internal`, and submits without backfill
- **THEN** the base's default label is updated and the detail header shows `internal`

#### Scenario: Backfilling from the UI

- **WHEN** the user submits the edit-label modal with "apply to existing" checked
- **THEN** a tracked operation backfills unlabeled chunks and sources, and a notification reports completion

#### Scenario: Invalid label is rejected client-side

- **WHEN** the user enters a label that fails the format check
- **THEN** the modal stays open with a field-level error and the entered value is preserved

## MODIFIED Requirements

### Requirement: Create and delete a knowledge base from the UI

The UI SHALL let the user create a knowledge base via a modal with a validated name field and an
optional default-label field (validated against the knowledge-labels format, with the effective
default shown as placeholder), and delete a base via a type-to-confirm modal whose body states
the source count and that the action cannot be undone. Deletion SHALL match CLI semantics (§8 of
the UX foundation). Validation errors on create SHALL keep the modal open with a field-level
message and preserve the user's input.

#### Scenario: Creating a base

- **WHEN** the user submits a valid name in the create modal
- **THEN** the base is created, the list refreshes, and a success notification is shown

#### Scenario: Creating a base with a default label

- **WHEN** the user enters `partner` in the default-label field and submits
- **THEN** the base is created with `partner` as its default label

#### Scenario: Create validation error

- **WHEN** creation fails validation (name or label) or conflicts with an existing name
- **THEN** the modal stays open with a field-level error and the entered values are preserved

#### Scenario: Deleting a base

- **WHEN** the user opens delete for a base
- **THEN** a type-to-confirm modal states the source count and requires typing the base name
- **AND** the destructive action stays disabled until the typed name matches exactly

### Requirement: Knowledge base detail with sources

The UI SHALL provide a `/knowledge/?kb=<name>` detail view (query-param routing, read via
`useSearchParams()`) rendered by the same page as the list, with a back link to the list. The
detail view SHALL show the base's effective default label and SHALL list the base's ingested
sources in a table (source id, title/filename, type, label as a non-interactive chip, ingested
time as relative-with-absolute-title), with per-source actions to view metadata and to forget
the source. Forgetting SHALL use a plain confirm modal naming the source and base. The metadata
view SHALL render the stored metadata (including the label) and expose the raw JSON in a
copyable block.

#### Scenario: Viewing sources

- **WHEN** the user opens a base's detail view
- **THEN** its ingested sources are listed with id, title, type, label, and ingested time
- **AND** the base's effective default label is shown

#### Scenario: No sources ingested

- **WHEN** a base has no sources
- **THEN** an empty state is shown with an ingest action and the CLI ingest hint

#### Scenario: Inspecting source metadata

- **WHEN** the user opens a source's metadata
- **THEN** the stored metadata is shown with the raw JSON available in a copyable block

#### Scenario: Forgetting a source

- **WHEN** the user confirms forget for a source
- **THEN** the source's chunks and metadata are removed and the list refreshes

### Requirement: Ingest a document from the UI

The UI SHALL let the user ingest a single source into a base via a modal offering an upload-file
or from-URL choice, a source-identifier field prefilled from the filename, an optional label
field (validated against the knowledge-labels format, with the base's effective default label
shown as placeholder), and a force-re-ingest option. On submit the UI SHALL start a tracked
operation and close the modal immediately, letting the row appear when the operation reports
success. A duplicate-identifier error without force SHALL keep the modal open with a field-level
message and preserve the user's input.

#### Scenario: Ingesting by upload

- **WHEN** the user chooses a file, sets or accepts a source id, and submits
- **THEN** a tracked ingest operation starts and the modal closes immediately

#### Scenario: Ingesting with an explicit label

- **WHEN** the user enters `internal` in the label field and submits
- **THEN** the ingest request carries `label: internal` and the resulting source row shows that label

#### Scenario: Ingesting from a URL

- **WHEN** the user enters a valid URL and submits
- **THEN** a tracked ingest operation starts for that URL

#### Scenario: Duplicate source id without force

- **WHEN** ingestion is rejected because the source id already exists and force is off
- **THEN** the modal stays open with a message telling the user to enable force re-ingest, and input is preserved

#### Scenario: In-progress hint on the detail view

- **WHEN** an ingest operation for the open base is running
- **THEN** the sources table shows a live-updating in-progress hint above it

### Requirement: Search results render full chunks with score and provenance

Each hit SHALL render as one card in ranked order showing: a header with the rank number,
the source ID, the knowledge base name as a non-interactive chip, and the relevance score
right-aligned to 3 decimals; the chunk's full content preserving paragraph breaks and
without truncation; and a footer with the hit's resolved label and other provenance details in
small text. The label SHALL be the `label` field returned by the search API (stored chunk
label, with index-name fallback), not derived client-side. The results region
SHALL be announced via `aria-live="polite"` as "N results", be preceded by an off-screen
"Results" heading, and focus SHALL remain in the query input after submit. The source ID
SHALL render as plain text until a knowledge-detail route exists to link to.

#### Scenario: Result card contents

- **WHEN** a search returns hits
- **THEN** each card shows rank, source ID, KB name chip, and the score to 3 decimals
- **AND** the complete chunk content renders untruncated with paragraph breaks preserved
- **AND** the footer shows the hit's label as returned by the API
- **AND** the output matches what `k search` prints for the same query (chunks, scores, labels)

#### Scenario: Results announced to assistive tech

- **WHEN** a search completes with N hits
- **THEN** a polite live region announces "N results" and focus is still in the query input
