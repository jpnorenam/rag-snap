# Proposal

## Why

RAG answers today give no way to trace a claim back to the document it came from. Retrieval
knows each chunk's `source_id`, and the `rag-snap-metadata` index knows each source's input URI,
title, and file name, but `retrieveContext` flattens hits into a prompt string and discards them,
so neither the user nor a reviewer of an `answer batch` deliverable can verify an answer. Citations
are the prerequisite for trusting the tool's output in RFP/RFI work, and the data to produce them
largely exists already.

## What Changes

- Introduce a **citation** for every retrieved hit: a document-level identity (`key`), a
  human-readable `label`, an optional clickable `href`, an empty-for-now `locator` (reserved for
  future section/cell references), and display context (knowledge base, label tag).
- Resolve citations through an ordered **resolver chain** over data that exists today — Kapa
  source URL, then the source-metadata record (`source_uri`, `file_path` when it is an http(s)
  URL, sanity-checked `title`, `file_name`), then the chunk's `source_id` as a never-failing
  fallback. No re-ingest is required; no link is ever guessed (repo sources ingested before this
  change get no `href` until re-ingested).
- `SearchHit` carries its resolved citation, so every consumer (chat, batch, search, API) gets
  provenance from one place.
- Context chunks are tagged with **per-document markers** (`[1]`, `[2]`, …) and the system
  prompts require the model to cite the markers it relied on at the end of each paragraph. Markers
  are numbered **session-wide** in chat so a document keeps its number across turns and in saved
  history.
- **Uncited grounded answers are flagged, not hidden, in chat**: when retrieval ran but the chat
  answer cites no valid marker, the answer is kept and shown with a "No relevant sources" notice.
  Batch answers stay strict and are withheld as the no-answer response. Invented markers are
  dropped.
- **Ingest records a canonical source URI** in a new `source_uri` metadata field: the URL for URL
  ingests, a full web URL built from known repo coordinates for GitHub/Gitea ingests, and an
  optional user-supplied `--uri` for file ingests. Fixes CLI batch ingest recording a temp path as
  the source's `file_path`.
- Surfaces:
  - **Chat REPL** (direct and remote): a "Sources" block after each answer listing cited sources.
  - **Web UI chat**: a new `sources` websocket frame; paragraphs render their `[#]` markers as
    source chips.
  - **`answer batch`** (CLI and API): markers are stripped from `answer`; a per-result `sources`
    field lists the cited documents.
  - **Saved chats**: assistant turns persist their sources; older saved chats load unchanged.
  - **Search** (`/search`, `k search`, `POST /1.0/search`, `/search/` page): show the resolved
    citation instead of the raw `source_id`.
- **New user-facing surfaces**: a `--uri` flag on `k ingest`, a Sources block in REPL output, a
  source-URI field in the UI ingest modal, source chips in the UI chat, and a `sources` field in
  batch output. Docs to update: `README.md` (chat and ingest usage), `docs/local-ui.md` (chat
  screen, ingest modal), and the `answer batch` output description.

External services touched: **OpenSearch** (new `source_uri` mapping on `rag-snap-metadata`; one
`_mget` per retrieval) and the **inference server** (system prompt and per-turn context format
change). **Tika** is untouched. No new config keys.

## Capabilities

### New Capabilities
- `source-citations`: how retrieved hits resolve to document-level citations (identity, label,
  link, locator), the resolver fallback chain, session-wide marker numbering, citation
  enforcement on grounded answers, and the source URI recorded at ingest.

### Modified Capabilities
- `chat-search`: each displayed hit shows its resolved citation (label and link) in place of the
  bare source identifier.
- `chat-history`: a saved chat's assistant turns carry their cited sources, restored on resume.
- `rest-api-chat`: the chat websocket emits a `sources` frame per answer; answers follow the
  citation enforcement rule.
- `rest-api-answer`: batch results carry per-question `sources` and a marker-free `answer`.
- `rest-api-knowledge`: search hits include their citation; source ingest accepts an optional
  source URI.
- `local-ui-app`: the chat screen renders per-paragraph source chips, the search page shows the
  citation, the ingest modal accepts a source URI, and the batch review shows per-answer sources.

## Impact

- **Code**: `cmd/cli/basic/knowledge/` (`search.go` SearchHit, new citation resolver,
  `sources.go` mapping + `SourceMetadata.SourceURI`, `ingest.go`, `batch.go` MetadataPath fix,
  `kapa.go`); `cmd/cli/basic/processing/` (GitHub/Gitea web URL construction);
  `cmd/cli/basic/chat/` (`rag.go` retrieval returns hits, `prompts.go` citation rules,
  `client.go`, `turn.go`, `remote.go`, `batch.go`, `search.go`, `history.go`);
  `internal/chatstore` (`Turn.Sources`); `internal/api` (chat frames, ingest, search, answer);
  `ui/` (ChatScreen, search page, ingest modal, batch review, `lib/types.ts`).
- **APIs**: additive JSON fields only (`sources` on batch results and chat turns, `citation` on
  search hits, `source_uri` on source metadata and ingest requests, a new `sources` websocket
  frame type). Existing clients ignore unknown fields.
- **Behavior**: grounded chat/batch answers that cite nothing are replaced by the no-answer
  response — a deliberate tightening that may surface as more "not enough information" answers on
  weaker models.
- **Data**: no migration. Existing sources resolve via fallbacks; re-ingesting with `--force` is
  only needed to obtain clickable links for previously ingested repo and file sources.
- **Customized prompts**: users with a custom `chat_system_prompt`/`answer_system_prompt` keep
  their text; the citation instruction is appended alongside the mandatory source rules.
