<!--
Ordering dependency: this delta MODIFIES "Build a manifest from a document via a
three-step wizard", ADDED by the not-yet-archived change `add-ui-answer-batch`
(Change 4). Change 4 must be synced/archived into the main local-ui-app spec
before this change is archived.
-->

## MODIFIED Requirements

### Requirement: Build a manifest from a document via a three-step wizard

The UI SHALL provide a build wizard for deriving a manifest from an uploaded RFP document, with a
visible step indicator that does not use the brand accent color for emphasis. Free-text uploads
(PDF, DOCX) follow the three-step path (upload → review questions → configure & run); spreadsheet
uploads (XLSX, CSV) insert a column-selection step between upload and review, because the daemon
returns parsed tables rather than questions for those formats.

Step one (**Upload**) SHALL accept a PDF, DOCX, XLSX, or CSV file, show the detected type, and offer
a "Refine questions with the model" option (enabled by default). Submitting SHALL send the document
to `POST /1.0/answer/build` as a tracked asynchronous operation, showing a running state while the
document is inspected.

For **spreadsheet/CSV** uploads, when the build operation reports that a column choice is required,
the wizard SHALL present a **column-selection step**: when the document has more than one table the
user SHALL be able to choose the table; the user SHALL choose the column that holds the questions
from among the document's columns, each shown with sample cell values, with the daemon's suggested
column preselected; and a **minimum cell length** control SHALL be available, defaulting to 20.
Continuing SHALL run `POST /1.0/answer/build/extract` for the chosen column as a tracked operation.
A wrong column or minimum length SHALL be recoverable by re-running the extraction alone — the UI
SHALL NOT start a batch run from a guessed or unconfirmed column. Free-text uploads SHALL skip the
column-selection step entirely and proceed directly to review.

Step **Review questions** SHALL present the extracted candidate questions as an editable list: each
question SHALL be individually selectable (selected by default) and editable, the user SHALL be able
to add a new question, and a running count of selected-of-total questions SHALL always be visible.
Reordering is not required.

Step **Configure & run** SHALL let the user choose target knowledge bases, a temperature, and a
manifest name, and SHALL offer two actions: **Download manifest**, which saves a YAML manifest the
CLI's `answer batch` accepts without running it; and **Run batch**, which runs the selected questions
through the run flow and continues into the review surface.

Wizard state SHALL be held in memory only, and the UI SHALL warn the user before navigating away
while a built manifest is unsaved. While a build operation (document inspection or column
extraction) is in flight, the in-place exit SHALL be a cancel action whose confirmation copy is
**phase-appropriate**: cancelling an inspection or extraction SHALL NOT use the batch run's
"progress will be lost" warning, since no batch work has been performed.

#### Scenario: Uploading a free-text document extracts questions

- **WHEN** a user uploads a PDF or DOCX in the wizard's first step
- **THEN** the UI sends it to the build endpoint as a tracked operation and, on completion, the extracted questions populate the review step
- **AND** the column-selection step is skipped

#### Scenario: Spreadsheet upload inserts a column-selection step

- **WHEN** a user uploads an XLSX or CSV and the build operation reports that a column is required
- **THEN** the wizard shows a column-selection step listing the document's columns with sample cells and the suggested column preselected
- **AND** a minimum cell length control is available, defaulting to 20

#### Scenario: Choosing a column extracts its questions

- **WHEN** a user confirms a column (and table, when there is more than one) and continues
- **THEN** the UI runs column extraction as a tracked operation and populates the review step with that column's questions

#### Scenario: A wrong column is recoverable without a batch run

- **WHEN** the chosen column or minimum length yields the wrong questions or none
- **THEN** the user can pick a different column or adjust the minimum length and re-extract
- **AND** no batch run is started until the user runs the reviewed manifest

#### Scenario: Reviewing and editing extracted questions

- **WHEN** a user is on the review step
- **THEN** each candidate question can be deselected, edited, and new questions can be added
- **AND** a count of selected-of-total questions is always visible

#### Scenario: Download a manifest without running

- **WHEN** a user chooses Download manifest on the configure step
- **THEN** the UI saves a YAML manifest that the CLI's `answer batch` command accepts, without starting a run

#### Scenario: Run the built manifest

- **WHEN** a user chooses Run batch on the configure step
- **THEN** the selected questions are run through the batch run flow and the UI continues into the review surface

#### Scenario: Unsaved built manifest warns on navigation

- **WHEN** a user attempts to navigate away with a built but unsaved manifest
- **THEN** the UI warns before discarding the in-memory wizard state

#### Scenario: Cancelling a build phase uses phase-appropriate copy

- **WHEN** a user cancels while the document is being inspected or a column is being extracted
- **THEN** the confirmation does not warn that batch progress will be lost, because no batch run has started
