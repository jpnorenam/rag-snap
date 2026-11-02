# Spec Delta

## MODIFIED Requirements

### Requirement: Search results display full chunk content with metadata

For each retrieved chunk, the command SHALL display its relevance score, originating knowledge base name, source identifier, creation date, and the `[CANONICAL]`/`[UPSTREAM]` provenance tag, together with the chunk's full content.

The source SHALL be shown as the chunk's resolved citation: its label, followed by its link when the citation has one. When the label differs from the source identifier, the source identifier SHALL also be shown.

The chunk content SHALL NOT be truncated.

#### Scenario: Each result shows metadata and full content

- **WHEN** `/search` returns one or more hits
- **THEN** each result shows its score, knowledge base name, source ID, creation date, and provenance tag
- **AND** the complete chunk content is shown without truncation

#### Scenario: Result shows the resolved citation

- **WHEN** `/search` returns a chunk whose source has title `Ubuntu Pro Service Description` and link `https://ubuntu.com/pro`
- **THEN** the result's source line shows `Ubuntu Pro Service Description`, the link `https://ubuntu.com/pro`, and the source ID

#### Scenario: Result without a link

- **WHEN** `/search` returns a chunk whose citation has no link
- **THEN** the result's source line shows the citation label and no link placeholder

#### Scenario: No matching chunks

- **WHEN** `/search` runs successfully but the active knowledge bases contain no matching chunks
- **THEN** the command reports that no results were found
