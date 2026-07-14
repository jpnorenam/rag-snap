# local-ui-app Delta

## ADDED Requirements

### Requirement: Search page runs retrieval-only queries

The UI SHALL provide a `/search/` page that runs hybrid retrieval (via `POST /1.0/search`)
over selected knowledge bases and displays the matching chunks, without any LLM generation —
parity with `k search` and the in-chat `/search` slash command. The page SHALL be a single
column under the app shell: a Vanilla `p-search-box` query bar (input + submit,
`aria-label="Search knowledge bases"`, Enter submits), a scope row, and the results list.
The sidebar's Search entry SHALL become a live route to this page, marked active with
`aria-current="page"` when current.

#### Scenario: Submitting a query returns ranked chunks

- **WHEN** the user enters a query with at least one knowledge base selected and submits
- **THEN** the UI issues `POST /1.0/search` with the verbatim query, the selected bases, and the chosen top-k count
- **AND** the results render in ranked order without contacting the inference server

#### Scenario: Search entry is a live route

- **WHEN** the user activates the sidebar's Search entry
- **THEN** the UI navigates to `/search/` and the entry carries `aria-current="page"`

### Requirement: Search scope is selectable via KB chips and a top-k select

The scope row SHALL offer a knowledge-base multi-select rendered as toggle chips
(`p-chip`/`p-chip--positive`, the exact pattern from the chat screen) and a compact
`<select>` labeled "Results" with options 5 / 10 / 15 / 25, defaulting to **10** (parity
with `k search --top`). Default base selection: all bases selected when exactly one exists;
otherwise the base named `default` when it exists, else all bases. Submitting with no base
selected SHALL be prevented client-side rather than surfacing the daemon's 400 error. Chips
and the select SHALL sit in tab order between the query input and the results.

#### Scenario: Default scope with multiple bases

- **WHEN** the page loads and the knowledge bases include one named `default`
- **THEN** only the `default` base chip starts selected and the Results select shows 10

#### Scenario: Toggling scope chips

- **WHEN** the user toggles a base chip off so that no base remains selected
- **THEN** submission is prevented and the UI indicates at least one base is required

### Requirement: Search query and scope round-trip through the URL

The query, selected bases, and top-k SHALL persist in the URL (`/search/?q=…`) so a search
is shareable and reloadable. Loading a URL containing a query SHALL restore the scope and
re-run the search automatically.

#### Scenario: Reload reproduces the search

- **WHEN** the user reloads a URL of the form `/search/?q=<query>&…` produced by a previous search
- **THEN** the query bar, base chips, and Results select restore the encoded state
- **AND** the same search runs and renders results without further input

### Requirement: Search results render full chunks with score and provenance

Each hit SHALL render as one card in ranked order showing: a header with the rank number,
the source ID, the knowledge base name as a non-interactive chip, and the relevance score
right-aligned to 3 decimals; the chunk's full content preserving paragraph breaks and
without truncation; and a footer with provenance details in small text. The results region
SHALL be announced via `aria-live="polite"` as "N results", be preceded by an off-screen
"Results" heading, and focus SHALL remain in the query input after submit. The source ID
SHALL render as plain text until a knowledge-detail route exists to link to.

#### Scenario: Result card contents

- **WHEN** a search returns hits
- **THEN** each card shows rank, source ID, KB name chip, and the score to 3 decimals
- **AND** the complete chunk content renders untruncated with paragraph breaks preserved
- **AND** the output matches what `k search` prints for the same query (chunks, scores, provenance)

#### Scenario: Results announced to assistive tech

- **WHEN** a search completes with N hits
- **THEN** a polite live region announces "N results" and focus is still in the query input

### Requirement: Search page distinguishes initial, loading, no-hits, no-KBs, and error states

The page SHALL implement distinct states: **initial** (no query yet) — an empty state
explaining hybrid semantic + lexical retrieval with reranking, no LLM, including the CLI
hint `rag-cli.rag k search "<query>"`; **loading** — a spinner replaces the results area and
the submit control is disabled to prevent double-submit; **no hits** — a message naming the
searched bases and suggesting widening the base selection or raising top-k; **no knowledge
bases exist** — a caution notification linking to create/ingest a knowledge base first;
**error** — the standard error notification, with the standard daemon-unreachable message
for connection failures. No-hits, no-KBs, and error SHALL be visually and semantically
distinct states.

#### Scenario: Initial state

- **WHEN** the page loads without a query in the URL
- **THEN** an empty state explains retrieval-only search and shows the CLI equivalent command

#### Scenario: No hits vs error are distinct

- **WHEN** a search succeeds with zero hits
- **THEN** the UI shows the no-hits message naming the searched bases — not an error notification
- **AND** a failed request instead shows a negative notification with the daemon error message

#### Scenario: No knowledge bases exist

- **WHEN** the page loads and `GET /1.0/knowledge` returns no bases
- **THEN** a caution notification explains a knowledge base must be created and ingested first
