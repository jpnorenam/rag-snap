## ADDED Requirements

### Requirement: Answer batch screen with a step-state flow

The UI SHALL provide an Answer section at `/answer/` that turns the previously-inert
"Answer RFPs" sidebar placeholder into a shipped screen. The section SHALL be a single page
driven by a client-side step state — landing, running, and review — rather than separate
routes, because the flows share one review surface. The sidebar `answer` entry SHALL become an
enabled navigation link, and navigating to the section SHALL update the header title and
`document.title` per the app-shell navigation rules.

The landing state SHALL present entry cards for the three flows: **Run a manifest**,
**Build from a document**, and **Review results**.

#### Scenario: Navigating to the Answer section

- **WHEN** a user selects the enabled "Answer RFPs" entry in the sidebar
- **THEN** the `/answer/` page renders its landing state with entry cards for running a manifest, building from a document, and reviewing results
- **AND** the header title and `document.title` reflect the Answer section

### Requirement: Manifest is previewed and validated before any run

Before running, the UI SHALL accept a YAML batch manifest from the user, parse it client-side,
and present a preview showing the manifest name, the target knowledge bases, and the questions
as a numbered read-only list. A manifest that fails to parse SHALL surface a field-level
validation error and SHALL NOT be sent to the API; the user SHALL be able to select a different
file after a parse failure. The UI SHALL expose a temperature input defaulting to `0.1` to match
the CLI.

#### Scenario: Valid manifest shows a preview

- **WHEN** a user supplies a well-formed YAML manifest
- **THEN** the UI shows the manifest name, target knowledge bases, and a numbered list of its questions before any run is started

#### Scenario: Invalid manifest is rejected client-side

- **WHEN** a user supplies a file that is not a valid batch manifest
- **THEN** the UI shows a validation error and does not call the batch API
- **AND** the user can select a different file

### Requirement: Running a prepared manifest as a tracked operation

The UI SHALL run a previewed manifest by posting it to `POST /1.0/answer/batch` as an
asynchronous operation and registering the returned operation with the shared operations
tracker so it appears in the global operations indicator and can be cancelled from the
operations panel. While running, the UI SHALL show a running view that reflects per-question
progress from the operation's progress metadata when available, and a single progress
indicator otherwise. A batch run SHALL survive navigation away from and back to `/answer/`:
returning while a run is in progress SHALL restore the running view from the operations tracker.
On successful completion, the UI SHALL transition into the review surface with the run's results.

#### Scenario: Starting a batch run

- **WHEN** a user runs a previewed manifest
- **THEN** the UI posts the manifest to the batch endpoint, registers the operation with the tracker, and shows a running view
- **AND** the run appears in the global operations indicator and can be cancelled from the operations panel

#### Scenario: Progress reflects per-question metadata

- **WHEN** the running operation reports how many of its questions are done and the total
- **THEN** the running view reflects that per-question progress
- **AND** falls back to a single progress indicator when no per-question metadata is available

#### Scenario: A run survives navigation

- **WHEN** a user navigates away from `/answer/` while a batch is running and later returns
- **THEN** the running view is restored from the operations tracker rather than lost

#### Scenario: Completion opens the review surface

- **WHEN** a tracked batch operation completes successfully
- **THEN** the UI transitions into the review surface populated with that run's results

### Requirement: Build a manifest from a document via a three-step wizard

The UI SHALL provide a three-step build wizard for deriving a manifest from an uploaded RFP
document, with a visible step indicator that does not use the brand accent color for emphasis.

Step one (**Upload**) SHALL accept a PDF, DOCX, XLSX, or CSV file, show the detected type, and
offer a "Refine questions with the model" option (enabled by default). Submitting SHALL send the
document to `POST /1.0/answer/build` as a tracked asynchronous operation, showing a running state
while extraction proceeds.

Step two (**Review questions**) SHALL present the extracted candidate questions as an editable
list: each question SHALL be individually selectable (selected by default) and editable, the user
SHALL be able to add a new question, and a running count of selected-of-total questions SHALL
always be visible. Reordering is not required.

Step three (**Configure & run**) SHALL let the user choose target knowledge bases, a temperature,
and a manifest name, and SHALL offer two actions: **Download manifest**, which saves a YAML
manifest the CLI's `answer batch` accepts without running it; and **Run batch**, which runs the
selected questions through the run flow and continues into the review surface.

Wizard state SHALL be held in memory only, and the UI SHALL warn the user before navigating away
while a built manifest is unsaved.

#### Scenario: Uploading a document extracts questions

- **WHEN** a user uploads a supported document in the wizard's first step
- **THEN** the UI sends it to the build endpoint as a tracked operation and shows a running state
- **AND** the extracted candidate questions populate the review step on completion

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

### Requirement: Batch results review surface

The UI SHALL provide a review surface, shared by a completed run and by opening a previously
exported results file, that renders results using the retained `QAItem`/`QAFile`/`ParsedQAFile`
contract (tolerating both `result` and `results` keys). The surface SHALL show a header with the
manifest name, the run timestamp, and the knowledge bases used, and SHALL offer an **Export JSON**
action that downloads the results verbatim in the same format the CLI writes. Each question and
answer SHALL render as a card: the question as the title, the answer as pre-wrapped text, and the
provenance as a collapsible section listing each source's knowledge base, source identifier, and
score. A failed or empty answer SHALL render with a caution treatment and its error text rather
than blank. When there are more than ten questions, the surface SHALL show a jump-list index that
links to each question card.

#### Scenario: Reviewing results

- **WHEN** results are available from a completed run
- **THEN** the review surface shows the manifest name, timestamp, and knowledge bases used, and one card per question with its answer and collapsible sources

#### Scenario: Exporting results verbatim

- **WHEN** a user selects Export JSON on the review surface
- **THEN** the UI downloads the results file in the same JSON format the CLI produces

#### Scenario: Failed answers are surfaced, not blank

- **WHEN** a question's answer failed or is empty
- **THEN** its card renders with a caution treatment and the associated error text

#### Scenario: Jump list for long result sets

- **WHEN** a result set has more than ten questions
- **THEN** the review surface shows a jump-list index linking to each question card

### Requirement: Reviewing a previously exported results file

The UI SHALL let a user open a previously exported batch results JSON file from disk directly
into the review surface, without running a batch. A file that does not match the results
contract SHALL surface a validation error rather than rendering a broken surface.

#### Scenario: Opening a results file

- **WHEN** a user opens a previously exported results JSON file
- **THEN** the review surface renders it using the results contract, without requiring a run

#### Scenario: Malformed results file is rejected

- **WHEN** a user opens a file that does not match the results contract
- **THEN** the UI surfaces a validation error instead of rendering a broken review surface

## MODIFIED Requirements

### Requirement: Preserve the answer-review data contract

The UI codebase SHALL retain the `QAItem` and `ParsedQAFile` type contract from
`rag-snap-ui` (tolerating both `result` and `results` keys in batch output). This contract
SHALL be the shape the shipped Answer batch review surface renders; no divergent results shape
SHALL be introduced for that screen.

#### Scenario: Contract backs the review surface

- **WHEN** the Answer batch review surface renders results from a run or an opened file
- **THEN** it consumes the `QAItem`/`ParsedQAFile` contract rather than a newly invented shape
