## Why

The chat REPL only exposes its retrieval layer indirectly: a user asks a question, the LLM rewrites it into keywords, the hybrid pipeline runs, and the retrieved chunks are folded into the prompt and never shown. There is no way to see *what the retriever actually returns* for a given query.

This makes the knowledge base hard to debug and inspect. When an answer is wrong or thin, the user cannot tell whether the problem is retrieval (the right chunks were not found) or generation (the chunks were found but the LLM ignored them). Operators tuning a knowledge base — checking ingestion quality, source coverage, or chunk boundaries — have no lightweight way to query it from inside the chat session they are already in.

A `/search` slash command closes this gap: it runs the same retrieval the chat loop uses and prints the matching chunks with their metadata, performing **no augmentation and no generation**.

## What Changes

- Add a new in-chat slash command `/search <query>` to `rag-cli.rag chat`.
- It searches the currently toggled knowledge bases (the same `ActiveIndexes` set via `/use-knowledge`) using the **existing hybrid pipeline** (BM25 + neural + rerank) — identical to what the RAG loop runs.
- It performs **no query rewriting / keyword expansion** (no inference-server round-trip) and **no prompt augmentation or LLM generation**. The user's terms are passed verbatim as both the lexical and neural query.
- Results are printed for human reading: each hit shows its score, knowledge base name, source ID, creation date, the `[CANONICAL]`/`[UPSTREAM]` provenance tag, and the **full, untruncated chunk content**.
- Support an optional `-k N` flag to control how many results are returned (default reuses the chat retrieval top-K).
- Register `/search` for autocomplete and the slash-command hinter alongside `/use-knowledge`.

### External services touched

- **OpenSearch** (`knowledge` store): yes — reuses the existing `Search` / hybrid search pipeline and the embedding ML model. No new pipelines, indexes, or models.
- **Inference server** (`chat` backend): no — `/search` deliberately bypasses the LLM entirely. (It still requires the chat session to have started, which is where `KnowledgeClient` and `EmbeddingModelID` are wired.)
- **Tika**: no.

## Capabilities

### New Capabilities

- `chat-search`: in-chat retrieval-only inspection of the active knowledge bases — query the toggled bases and display matching chunks with metadata, without augmentation or generation.

### Modified Capabilities

<!-- None. No existing spec-level behavior changes. -->

## Impact

- **Affected code:**
  - `cmd/cli/basic/chat/commands.go` — register `cmdSearch`, split verb/args in `handleSlashCommand`, add `slashCommands` entry, new handler.
  - `cmd/cli/basic/chat/client.go` — autocomplete `PcItem` for `/search` (driven off `slashCommands`).
  - `cmd/cli/basic/chat/` — new file (e.g. `search.go`) for the handler, argument/`-k` parsing, and the human-facing result formatter.
- **Reused as-is:** `knowledge.OpenSearchClient.Search`, `knowledge.SearchHit`, `knowledge.KnowledgeBaseNameFromIndex`, `sourceLabel`, `common.StartProgressSpinner`.
- **No new config keys** (no `package`/`user` keys added).
- **No new secrets, snap interfaces, bundled binaries, or hook changes.**
- **Dependencies:** none added.
