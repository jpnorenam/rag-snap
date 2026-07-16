# chat-search Specification (delta)

## MODIFIED Requirements

### Requirement: Search results display full chunk content with metadata

For each retrieved chunk, the command SHALL display its relevance score, originating knowledge base name, source identifier, creation date, and the chunk's resolved label rendered as an uppercase bracketed tag (e.g. `[INTERNAL]`, `[CANONICAL]`), together with the chunk's full content. The label SHALL be resolved by the shared knowledge-labels resolver (stored chunk label, with index-name fallback for unlabeled chunks), not re-derived locally.

The chunk content SHALL NOT be truncated.

#### Scenario: Each result shows metadata and full content

- **WHEN** `/search` returns one or more hits
- **THEN** each result shows its score, knowledge base name, source ID, creation date, and resolved label tag
- **AND** the complete chunk content is shown without truncation

#### Scenario: Stored labels are displayed

- **WHEN** a hit's chunk carries a stored label `internal`
- **THEN** the result shows `[INTERNAL]` regardless of the base's name

#### Scenario: No matching chunks

- **WHEN** `/search` runs successfully but the active knowledge bases contain no matching chunks
- **THEN** the command reports that no results were found
