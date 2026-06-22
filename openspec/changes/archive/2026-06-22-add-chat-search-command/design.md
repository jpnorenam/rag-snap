## Context

The chat REPL (`cmd/cli/basic/chat/`) already contains every piece of the retrieval path:

- `Session` holds `KnowledgeClient`, `EmbeddingModelID`, and `ActiveIndexes`.
- `knowledge.OpenSearchClient.Search(ctx, indexes, query, lexicalQuery, embeddingModelID, k)` runs a hybrid search (BM25 `match` + `neural` KNN + rerank `ext`) per index, merges, and sorts by score — returning `[]knowledge.SearchHit{Index, Score, Content, SourceID, CreatedAt}`.
- `retrieveContext` / `formatContext` (rag.go) consume those hits but are tuned for **LLM injection**: `formatContext` emits only the provenance tag, content, source, and score, concatenated with `---` separators.
- Slash commands are dispatched by `handleSlashCommand` (commands.go), which currently matches the **entire input line** against `cmdUseKnowledge`. `slashCommands` drives both readline autocomplete (`PcItem`) and the live hinter (`slashHinter`).

`/search` is a new presentation layer over the existing `Search` call. The design work is in the dispatcher seam, argument parsing, and a human-facing formatter — not in the retrieval engine.

## Goals / Non-Goals

**Goals:**
- Let a user, mid-chat, see exactly what the retriever returns for arbitrary terms against the toggled knowledge bases.
- Use the **same hybrid pipeline** the RAG loop uses, so results faithfully reflect what the LLM would have been fed.
- Display rich, human-readable metadata and the full chunk content.
- Cost nothing extra: no inference-server calls, no new OpenSearch artifacts, no new config.

**Non-Goals:**
- No LLM generation, summarization, or prompt augmentation.
- No query rewriting / keyword expansion (`rewriteSearchQuery` is intentionally not called).
- No lexical-only / BM25-only mode — `/search` reuses the hybrid pipeline, which requires the embedding model. (A models-down keyword-grep mode was considered and rejected; see Decisions.)
- No new persisted state or config keys; the snapctl config backend and package/user precedence model are untouched.
- No new secrets — `/search` adds no credentials; it relies on the same `OPENSEARCH_*` env vars already used by the session's `KnowledgeClient`.

## Decisions

### 1. Reuse the existing hybrid `Search` (Reading A), not a new lexical path
`/search` calls `session.KnowledgeClient.Search` unchanged. Rationale: the primary value is *inspecting what RAG retrieves*, so the command must run the identical pipeline (BM25 + neural + rerank). A separate BM25-only path would diverge from real retrieval behavior and require a new query builder and client method. Consequence: `/search` requires `EmbeddingModelID != ""` (the embedding ML model loaded), exactly like the RAG loop. When it is empty, `/search` reports that retrieval is unavailable rather than silently returning nothing.

### 2. Verbatim terms, no augmentation
The user's terms are passed as **both** `query` (neural + rerank) and `lexicalQuery` (BM25): `Search(ctx, ActiveIndexes, terms, terms, EmbeddingModelID, k)`. `rewriteSearchQuery` is not invoked, so there is no inference-server round-trip and no keyword expansion. This keeps the command honest to "retrieve from the provided keywords" and lets it work even when the chat backend is unreachable.

### 3. Dedicated human-facing formatter, not `formatContext`
`formatContext` hides metadata for LLM consumption. `/search` gets its own formatter that leads with provenance and shows **full, untruncated** content:

```
[1] score 0.8421  ·  openstack-upstream  [UPSTREAM]
    source: nova-docs/compute.md   created: 2026-03-11
    ────────────────────────────────────────────────
    <full chunk content, no truncation>

[2] score 0.7903  ·  canonical-kb  [CANONICAL]
    source: ubuntu-pro/faq         created: 2026-05-02
    ────────────────────────────────────────────────
    <full chunk content, no truncation>
```

KB display names come from `knowledge.KnowledgeBaseNameFromIndex(hit.Index)` (as `selectActiveContext` already does); the provenance tag from the existing `sourceLabel(hit.Index)`. Results are already sorted by score descending by `Search`.

### 4. Dispatcher splits verb from args
`/search` is the first slash command with an argument. `handleSlashCommand` switches from whole-line matching to `verb, args, _ := strings.Cut(strings.TrimSpace(input), " ")` and matches on `verb`. `/use-knowledge` continues to match with an empty `args`. `cmdSearch` is added to `slashCommands` so both autocomplete and the hinter pick it up.

### 5. `-k N` parsing, with a sensible default
`/search` parses an optional `-k N` flag from `args`; the remaining tokens are the query. Default reuses `defaultRAGTopK` (15). Invalid or non-positive `N` → usage error, no search. Empty query (after stripping flags) → usage hint, no search. Parsing is a small hand-rolled token scan (the command runs inside the REPL, not through Cobra), kept minimal: recognize `-k`/`-k=`, treat everything else as query terms.

### 6. Guard rails mirror `retrieveContext`
Before searching: if `KnowledgeClient == nil` → "knowledge base not available"; if `len(ActiveIndexes) == 0` → tell the user to toggle bases with `/use-knowledge`; if `EmbeddingModelID == ""` → retrieval unavailable. Zero hits → print "no results" rather than nothing.

## Risks / Trade-offs

- **Embedding model dependency:** by reusing the hybrid pipeline, `/search` is unavailable when the embedding model is not loaded. Accepted: it matches RAG behavior and the command's purpose is to mirror RAG retrieval. Mitigated by a clear message.
- **Unbounded output:** printing full chunk content for up to `k` hits can flood the terminal for large `k` or long chunks. Accepted per requirements (no truncation); `-k` gives the user control, and chunks are bounded in size by the ingestion chunker.
- **Dispatcher refactor regression:** changing `handleSlashCommand` from whole-line to verb/args matching could affect `/use-knowledge`. Low risk (it simply matches `verb` with empty `args`); covered by manual REPL validation.
- **Hinter cosmetics:** `slashHinter`'s `strings.HasPrefix(cmd, input)` assumes no-arg commands; once the user types `/search ` the hint stops matching. Acceptable — the autocomplete still surfaces the command, and the behavior degrades gracefully.
