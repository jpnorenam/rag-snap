# chat-search Specification

## Purpose
Retrieval-only search from the CLI (`k search` and the in-chat `/search` command): shows the knowledge-base chunks that match a query, keeps near-but-not-matching chunks hidden as similar results, and presents results the same way as the UI search page.

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

- **WHEN** `/search` runs successfully but no returned hit is matching
- **THEN** the command reports that no results match the query
- **AND** if similar results exist, it states how many and that `--show-similar` reveals them

### Requirement: Configurable result count via -k

The command SHALL accept an optional `-k N` flag specifying the maximum number of hits to return, counted across all active knowledge bases together. When omitted, it SHALL use the default chat retrieval result count.

A non-positive or non-integer `N` SHALL be rejected with a usage message and SHALL NOT perform a search.

#### Scenario: Limiting results

- **WHEN** a user enters `/search -k 5 ceph osd recovery` with two knowledge bases active
- **THEN** the command returns at most 5 hits in total for the query `ceph osd recovery`, the highest-scoring across both bases

#### Scenario: Default result count

- **WHEN** a user enters `/search ceph osd recovery` without `-k`
- **THEN** the command returns up to the default chat retrieval result count of hits

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

When a slash command that takes arguments has been fully typed, the REPL SHALL display the command's argument syntax as a dimmed inline hint, so options such as `-k N` and `--show-similar` are discoverable without prior knowledge.

#### Scenario: Autocomplete lists the command

- **WHEN** a user types `/` in the chat REPL and triggers autocomplete or hinting
- **THEN** `/search` appears among the available slash commands

#### Scenario: Inline syntax hint reveals arguments

- **WHEN** a user has typed `/search` (the command name, before entering arguments)
- **THEN** the REPL shows a dimmed inline hint of its argument syntax (including the `-k N` and `--show-similar` options)
- **AND** the hint disappears once the user begins typing the query
- **AND** the hint is display-only and never becomes part of the submitted input

### Requirement: Matching hits are defined by the server

A hit SHALL count as **matching** when the search service marks it `matched`, which it SHALL do exactly when the hit's relevance score after reranking is strictly greater than 0 (the search match threshold of `rest-api-knowledge`). Clients SHALL use the flag and SHALL NOT apply a score threshold of their own; a hit without the flag SHALL be treated as matching. The chat RAG loop SHALL NOT use the flag and SHALL keep its existing retrieval.

#### Scenario: Positive score matches

- **WHEN** a search retrieves a chunk whose relevance score is 2.37
- **THEN** the hit is marked as matching

#### Scenario: Zero or negative score does not match

- **WHEN** a search retrieves chunks whose relevance scores are 0 and -4.10
- **THEN** neither hit is marked as matching

#### Scenario: Hit without a match flag

- **WHEN** a client receives a hit that carries no match flag, such as from an older daemon
- **THEN** the client treats the hit as matching

#### Scenario: Chat answers are unaffected

- **WHEN** a user asks a question in chat with knowledge bases active
- **THEN** the context sent to the inference server is retrieved exactly as before this change, including chunks scoring 0 or below

### Requirement: Similar results are hidden by default

Every returned hit that does not match SHALL be a **similar result**. Similar results SHALL NOT be shown by default. When at least one exists, the command SHALL print a message giving their number, saying they do not match the query, and naming the `--show-similar` option that reveals them. With `--show-similar`, they SHALL follow the matching hits under a separator labelling them as not matching.

#### Scenario: Similar results summarized, not shown

- **WHEN** a user enters `/search kafka` and the search returns 2 matching hits and 13 similar results
- **THEN** the 2 matching hits are printed
- **AND** a message states that 13 similar results do not match the query and can be shown with `--show-similar`
- **AND** the similar results' content is not printed

#### Scenario: Revealing similar results

- **WHEN** a user enters `/search --show-similar kafka`
- **THEN** the matching hits are printed first
- **AND** the similar results follow under a separator labelling them as not matching the query, each with its score and metadata

#### Scenario: Every hit matches

- **WHEN** every returned hit matches
- **THEN** no similar-results message is printed

### Requirement: CLI `k search` shows only matching hits

The `k search` command SHALL apply the same matching and similar-result rules as `/search`: print matching hits, report when none match, summarize hidden similar results, and reveal them under a labelled separator with `--show-similar`. `--top` SHALL cap the total number of hits across all searched bases. Its output format, `--bases` behaviour, and `--top` default of 10 SHALL be otherwise unchanged.

#### Scenario: Term absent from the knowledge base

- **WHEN** a user runs `rag-cli.rag k search kafka` and no returned hit matches
- **THEN** the command reports that no results match `kafka`
- **AND** states how many similar results exist and that `--show-similar` reveals them

#### Scenario: Showing similar results from the CLI

- **WHEN** a user runs `rag-cli.rag k search kafka --show-similar`
- **THEN** the command prints any matching hits, then the similar results under a separator labelling them as not matching

#### Scenario: Top caps hits across bases

- **WHEN** a user runs `rag-cli.rag k search ceph --bases a,b --top 3` and each base holds more than 3 relevant chunks
- **THEN** at most 3 hits are returned in total, the highest-scoring across both bases

### Requirement: Search surfaces present results identically

The CLI (`k search`, `/search`) and the UI search page SHALL split the same set of hits into the same matching hits and similar results, in the same order, and SHALL report the same counts. This SHALL be enforced by a shared test fixture of hits and expected splits, checked by the tests of both the CLI and the UI.

#### Scenario: Same hits, same split

- **WHEN** the CLI and the UI render the same search response
- **THEN** both show the same matching hits in the same order
- **AND** both report the same number of hidden similar results

#### Scenario: Divergence is caught by tests

- **WHEN** a change makes either the CLI or the UI split a fixture case differently from its expected result
- **THEN** that client's test suite fails
