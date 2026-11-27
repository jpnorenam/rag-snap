## MODIFIED Requirements

### Requirement: In-chat retrieval-only search command

The chat REPL SHALL provide a `/search <query>` slash command that retrieves matching chunks
from the active knowledge bases and the active kapa.ai source groups and displays them, without
performing any LLM generation, summarization, or prompt augmentation.

The command SHALL search using the same hybrid retrieval pipeline (BM25 + neural + rerank) that
the chat RAG loop uses, over exactly the knowledge bases currently toggled active via
`/use-knowledge` (the session's active indexes), and the same kapa.ai retrieval the chat RAG loop
uses, over exactly the source groups currently selected via `/use-kapa`. When both kinds of
source are active, the two retrievals SHALL be issued concurrently and the results SHALL be shown
in the order the chat RAG loop merges them — local hits, ordered by relevance score descending,
followed by kapa.ai hits in kapa.ai's own order — as defined by `kapa-retrieval`. Kapa.ai scores
are rank-derived and not comparable with local scores, so the two sets SHALL NOT be interleaved
by score.

The user's query terms SHALL be passed verbatim to retrieval; the command SHALL NOT invoke query
rewriting or keyword expansion and SHALL NOT contact the inference server.

#### Scenario: Retrieving chunks for a query

- **WHEN** a user enters `/search high availability clustering` with one or more knowledge bases toggled active
- **THEN** the command runs the hybrid retrieval pipeline over the active knowledge bases using the verbatim terms
- **AND** it prints the matching chunks ordered by relevance score descending
- **AND** it does not call the inference server or produce any generated answer

#### Scenario: Retrieving chunks from kapa.ai

- **WHEN** a user enters `/search <query>` with one or more kapa.ai source groups selected via `/use-kapa`
- **THEN** the command retrieves matching chunks from exactly those kapa.ai source groups
- **AND** each kapa.ai chunk is tagged with the `kapa-canonical` label
- **AND** it does not call the inference server or produce any generated answer

#### Scenario: Both kinds of source active

- **WHEN** a user enters `/search <query>` with both a knowledge base and a kapa.ai source group active
- **THEN** the command prints the local chunks ordered by score, followed by the kapa.ai chunks

#### Scenario: No augmentation or generation occurs

- **WHEN** a `/search` command completes
- **THEN** the session's conversation history is unchanged (no user or assistant message is appended)
- **AND** no augmented RAG prompt is constructed

### Requirement: Preconditions and guidance

The command SHALL validate that retrieval is possible before searching and SHALL give actionable
guidance when it is not. A kapa.ai retrieval that fails at request time SHALL be reported and
SHALL NOT discard the local results, as defined by `kapa-retrieval`.

#### Scenario: No active source of either kind

- **WHEN** a user runs `/search <query>` with no knowledge bases toggled active and no kapa.ai source groups selected
- **THEN** the command instructs the user to select a source with `/use-knowledge` or `/use-kapa` and does not perform a search

#### Scenario: Only kapa.ai groups are active

- **WHEN** a user runs `/search <query>` with no knowledge bases active but one or more kapa.ai source groups selected
- **THEN** the command performs the search using kapa.ai alone, without requiring an embedding model

#### Scenario: Retrieval unavailable

- **WHEN** a user runs `/search <query>` with knowledge bases active but no knowledge client or embedding model is available for the session, and no kapa.ai source groups are selected
- **THEN** the command reports that knowledge retrieval is unavailable and does not perform a search

#### Scenario: Kapa.ai fails during a search

- **WHEN** a user runs `/search <query>` with both kinds of source active and the kapa.ai request fails
- **THEN** the command prints the local results and reports the kapa.ai failure

#### Scenario: Empty query

- **WHEN** a user enters `/search` with no query terms (after any flags are removed)
- **THEN** the command prints a usage hint and does not perform a search
