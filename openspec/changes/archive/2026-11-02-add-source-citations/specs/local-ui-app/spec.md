# Spec Delta

## MODIFIED Requirements

### Requirement: Search results render full chunks with score and provenance

Each hit SHALL render as one card in ranked order showing: a header with the rank number,
the source ID, the knowledge base name as a non-interactive chip, and the relevance score
right-aligned to 3 decimals; the chunk's full content preserving paragraph breaks and
without truncation; and a footer with provenance details in small text. The results region
SHALL be announced via `aria-live="polite"` as "N results", be preceded by an off-screen
"Results" heading, and focus SHALL remain in the query input after submit. The source ID
SHALL render as plain text until a knowledge-detail route exists to link to.

The header SHALL show the hit's citation label in place of the bare source ID; when the label
differs from the source ID, the source ID SHALL appear in the footer. When the citation has a
link, the label SHALL be an external link opening in a new tab with `rel="noopener noreferrer"`;
otherwise the label SHALL render as plain text.

#### Scenario: Result card contents

- **WHEN** a search returns hits
- **THEN** each card shows rank, source ID, KB name chip, and the score to 3 decimals
- **AND** the complete chunk content renders untruncated with paragraph breaks preserved
- **AND** the output matches what `k search` prints for the same query (chunks, scores, provenance)

#### Scenario: Citation with a link

- **WHEN** a hit's citation has label `Ubuntu Pro Service Description` and link `https://ubuntu.com/pro`
- **THEN** the card header shows the label as an external link to `https://ubuntu.com/pro` and the footer shows the source ID

#### Scenario: Citation without a link

- **WHEN** a hit's citation has no link
- **THEN** the card header shows the label as plain text

#### Scenario: Results announced to assistive tech

- **WHEN** a search completes with N hits
- **THEN** a polite live region announces "N results" and focus is still in the query input

## ADDED Requirements

### Requirement: Chat answers show sources on the paragraphs that cite them
The chat screen SHALL render each citation marker in an answer as a source chip showing the marker
number, with the source's label as its accessible name and tooltip, linking to the source when it has
a link. Chips SHALL render while the answer streams. Each completed answer SHALL end with a Sources
list of its cited documents.

#### Scenario: Paragraph with two markers
- **WHEN** an answer paragraph ends with `[1][3]`
- **THEN** that paragraph ends with two source chips, `1` and `3`, each naming its source's label to assistive tech

#### Scenario: Chip for a linked source
- **WHEN** a chip's source has a link
- **THEN** activating the chip opens the link in a new tab

#### Scenario: Chips during streaming
- **WHEN** a marker arrives in the token stream after the turn's `sources` frame
- **THEN** it renders as a chip before the answer completes

#### Scenario: Unknown marker
- **WHEN** an answer contains a marker that is not among the turn's sources
- **THEN** the marker is not rendered as a chip

### Requirement: The chat screen preselects the default knowledge base
When the knowledge-base list includes `default` and no selection is in place yet, the chat screen
SHALL select `default`, matching the CLI chat. A selection already in place (such as a resumed chat's)
SHALL be kept.

#### Scenario: Default base exists
- **WHEN** the chat screen loads and `GET /1.0/knowledge` lists `default`
- **THEN** the `default` chip is selected and the first prompt's session starts with `default` active

#### Scenario: No default base
- **WHEN** the knowledge-base list has no `default` base
- **THEN** no base is preselected

### Requirement: Markers that refer to nothing are not shown
The chat screen SHALL render only markers for documents the turn retrieved (while streaming) or cited
(once done); any other marker SHALL be removed from the displayed answer rather than shown as literal
`[n]` text.

#### Scenario: Marker without retrieval
- **WHEN** an answer streamed without a preceding `sources` frame contains `[1]`
- **THEN** the answer is shown without the `[1]` text and without a chip

### Requirement: Unsourced chat answers say they have no relevant sources
When the terminal frame flags an answer as unsourced, the chat screen SHALL keep the streamed text and
show, in place of the Sources list, a muted "No relevant sources" notice.

#### Scenario: Unsourced answer
- **WHEN** an answer finishes and its terminal frame is flagged unsourced
- **THEN** the streamed text stays, no Sources list is shown, and a "No relevant sources" notice appears under the answer

### Requirement: Restored chats show their sources
When a saved chat is resumed in the chat screen, restored assistant turns SHALL render their markers as
source chips and their Sources list from the saved sources. Turns saved without sources SHALL render as
plain answers.

#### Scenario: Resuming a chat with cited turns
- **WHEN** the user resumes a saved chat whose assistant turns carry sources
- **THEN** those turns render with source chips and a Sources list

### Requirement: Ingest modal accepts a source URI
The single-source ingest modal SHALL offer an optional Source URL field for file uploads, validated
client-side as an absolute http(s) URL, and sent with the ingest request. The field SHALL NOT be shown
for URL ingests, whose URL is recorded automatically.

#### Scenario: Upload with a source URL
- **WHEN** the user uploads a file and enters `https://example.com/spec.pdf` as the Source URL
- **THEN** the ingest request carries that source URI

#### Scenario: Invalid source URL
- **WHEN** the user enters a Source URL that is not an absolute http(s) URL
- **THEN** the modal shows a validation error on the field and does not submit

### Requirement: Batch results review shows per-answer sources
The batch results review SHALL show each answer's `sources` as a list of labels, linked when the
source has a link, distinct from the answer text. Results without `sources` SHALL render unchanged.

#### Scenario: Answer with sources
- **WHEN** a result records two sources
- **THEN** that question's card lists both sources by label

#### Scenario: Older results file
- **WHEN** a user opens a results file produced before sources existed
- **THEN** the review renders it without error and with no sources list
