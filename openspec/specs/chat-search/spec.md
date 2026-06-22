# chat-search Specification

## Purpose
TBD - created by archiving change add-chat-search-command. Update Purpose after archive.
## Requirements
### Requirement: In-chat retrieval-only search command

The chat REPL SHALL provide a `/search <query>` slash command that retrieves matching chunks from the active knowledge bases and displays them, without performing any LLM generation, summarization, or prompt augmentation.

The command SHALL search using the same hybrid retrieval pipeline (BM25 + neural + rerank) that the chat RAG loop uses, over exactly the knowledge bases currently toggled active via `/use-knowledge` (the session's active indexes).

The user's query terms SHALL be passed verbatim to retrieval; the command SHALL NOT invoke query rewriting or keyword expansion and SHALL NOT contact the inference server.

#### Scenario: Retrieving chunks for a query

- **WHEN** a user enters `/search high availability clustering` with one or more knowledge bases toggled active
- **THEN** the command runs the hybrid retrieval pipeline over the active knowledge bases using the verbatim terms
- **AND** it prints the matching chunks ordered by relevance score descending
- **AND** it does not call the inference server or produce any generated answer

#### Scenario: No augmentation or generation occurs

- **WHEN** a `/search` command completes
- **THEN** the session's conversation history is unchanged (no user or assistant message is appended)
- **AND** no augmented RAG prompt is constructed

### Requirement: Search results display full chunk content with metadata

For each retrieved chunk, the command SHALL display its relevance score, originating knowledge base name, source identifier, creation date, and the `[CANONICAL]`/`[UPSTREAM]` provenance tag, together with the chunk's full content.

The chunk content SHALL NOT be truncated.

#### Scenario: Each result shows metadata and full content

- **WHEN** `/search` returns one or more hits
- **THEN** each result shows its score, knowledge base name, source ID, creation date, and provenance tag
- **AND** the complete chunk content is shown without truncation

#### Scenario: No matching chunks

- **WHEN** `/search` runs successfully but the active knowledge bases contain no matching chunks
- **THEN** the command reports that no results were found

### Requirement: Configurable result count via -k

The command SHALL accept an optional `-k N` flag specifying the maximum number of results to return. When omitted, it SHALL use the default chat retrieval result count.

A non-positive or non-integer `N` SHALL be rejected with a usage message and SHALL NOT perform a search.

#### Scenario: Limiting results

- **WHEN** a user enters `/search -k 5 ceph osd recovery`
- **THEN** the command returns at most 5 results for the query `ceph osd recovery`

#### Scenario: Default result count

- **WHEN** a user enters `/search ceph osd recovery` without `-k`
- **THEN** the command returns up to the default chat retrieval result count

#### Scenario: Invalid -k value

- **WHEN** a user enters `/search -k 0 ...` or `/search -k abc ...`
- **THEN** the command prints a usage message and does not perform a search

### Requirement: Preconditions and guidance

The command SHALL validate that retrieval is possible before searching and SHALL give actionable guidance when it is not.

#### Scenario: No active knowledge bases

- **WHEN** a user runs `/search <query>` with no knowledge bases toggled active
- **THEN** the command instructs the user to select knowledge bases with `/use-knowledge` and does not perform a search

#### Scenario: Retrieval unavailable

- **WHEN** a user runs `/search <query>` but no knowledge client or embedding model is available for the session
- **THEN** the command reports that knowledge retrieval is unavailable and does not perform a search

#### Scenario: Empty query

- **WHEN** a user enters `/search` with no query terms (after any flags are removed)
- **THEN** the command prints a usage hint and does not perform a search

### Requirement: Command discoverability

The `/search` command SHALL be registered for the chat REPL's slash-command autocomplete and live hinting alongside the existing slash commands.

When a slash command that takes arguments has been fully typed, the REPL SHALL display the command's argument syntax as a dimmed inline hint, so options such as `-k N` are discoverable without prior knowledge.

#### Scenario: Autocomplete lists the command

- **WHEN** a user types `/` in the chat REPL and triggers autocomplete or hinting
- **THEN** `/search` appears among the available slash commands

#### Scenario: Inline syntax hint reveals arguments

- **WHEN** a user has typed `/search` (the command name, before entering arguments)
- **THEN** the REPL shows a dimmed inline hint of its argument syntax (including the `-k N` option)
- **AND** the hint disappears once the user begins typing the query
- **AND** the hint is display-only and never becomes part of the submitted input

