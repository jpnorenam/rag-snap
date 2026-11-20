# Spec Delta

## ADDED Requirements

### Requirement: Search page hides similar results behind a toggle

The page SHALL show only hits the response marks as matching (hits without the flag count as matching). When other hits exist, the page SHALL show below the matching results a message giving their number and saying they do not match the query, with a toggle that shows or hides them. Similar results SHALL start hidden on every search, and shown ones SHALL render as cards marked as not matching.

#### Scenario: Similar results start hidden

- **WHEN** a search returns 2 matching hits and 8 hits with `matched: false`
- **THEN** the 2 matching cards render
- **AND** a message states that 8 similar results do not match the query, with a control to show them
- **AND** no similar-result card is visible

#### Scenario: Toggling similar results

- **WHEN** the user activates the show control
- **THEN** the 8 similar-result cards render below the message, each marked as not matching
- **AND** the control now hides them again, and its expanded state is exposed to assistive tech

#### Scenario: New search collapses similar results

- **WHEN** similar results are shown and the user submits another query
- **THEN** the new search's similar results start hidden

## MODIFIED Requirements

### Requirement: Search results render full chunks with score and provenance

Each matching hit SHALL render as one card in ranked order showing: a header with the rank number,
the source ID, the knowledge base name as a non-interactive chip, and the relevance score
right-aligned to 3 decimals; the chunk's full content preserving paragraph breaks and
without truncation; and a footer with provenance details in small text. The results region
SHALL be announced via `aria-live="polite"` as "N results", where N counts matching hits only, be preceded by an off-screen
"Results" heading, and focus SHALL remain in the query input after submit. The source ID
SHALL render as plain text until a knowledge-detail route exists to link to.

#### Scenario: Result card contents

- **WHEN** a search returns matching hits
- **THEN** each card shows rank, source ID, KB name chip, and the score to 3 decimals
- **AND** the complete chunk content renders untruncated with paragraph breaks preserved
- **AND** the output matches what `k search` prints for the same query (chunks, scores, provenance, and the matching/similar split)

#### Scenario: Results announced to assistive tech

- **WHEN** a search completes with N matching hits
- **THEN** a polite live region announces "N results" and focus is still in the query input

### Requirement: Search page distinguishes initial, loading, no-hits, no-KBs, and error states

The page SHALL implement distinct states: **initial** (no query yet) — an empty state
explaining hybrid semantic + lexical retrieval with reranking, no LLM, including the CLI
hint `rag-cli.rag k search "<query>"`; **loading** — a spinner replaces the results area and
the submit control is disabled to prevent double-submit; **no hits** — shown when no hit matches, a message naming the
searched bases, stating that no results match the query, and suggesting widening the base selection, followed by the similar-results toggle when similar results exist; **no knowledge
bases exist** — a caution notification linking to create/ingest a knowledge base first;
**error** — the standard error notification, with the standard daemon-unreachable message
for connection failures. No-hits, no-KBs, and error SHALL be visually and semantically
distinct states.

#### Scenario: Initial state

- **WHEN** the page loads without a query in the URL
- **THEN** an empty state explains retrieval-only search and shows the CLI equivalent command

#### Scenario: No hits vs error are distinct

- **WHEN** a search succeeds with zero matching hits
- **THEN** the UI shows the no-hits message naming the searched bases — not an error notification
- **AND** offers the similar-results toggle when the response contains similar results
- **AND** a failed request instead shows a negative notification with the daemon error message

#### Scenario: No knowledge bases exist

- **WHEN** the page loads and `GET /1.0/knowledge` returns no bases
- **THEN** a caution notification explains a knowledge base must be created and ingested first
