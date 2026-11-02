# Design

## Context

See `proposal.md` for motivation and `specs/` for the behavior contract. Current state that shapes
the approach:

- **Two stores, one join key.** Chunks (`rag-snap-<kb>`) carry `content`, `source_id`, `label`,
  `created_at`. Per-source metadata lives in the global `rag-snap-metadata` index, **document
  `_id` = `source_id`** (`sources.go:151`), with `file_path`, `file_name`, `title`, `author`,
  `content_type`, … Export/import already carry the metadata docs for a base (`export.go:138`);
  archives from before metadata existed are rebuilt by `synthesizeSourcesFromIndex` with
  `source_id` only.
- **Provenance is dropped at one point.** `retrieveContext` (`chat/rag.go:49`) returns a formatted
  string; its three callers (REPL `client.go:367`, `LiveSession.Prompt` `turn.go:174`, batch
  `batch.go:330`) never see the hits.
- **`file_path`/`file_name` are inconsistent**, depending on how the source was ingested:

  | Path | `file_path` | `file_name` |
  |---|---|---|
  | CLI `--url` | URL ✅ | `rag-crawl-123.html` ❌ |
  | CLI `--file` | local path | basename ✅ |
  | CLI batch repo (`knowledge/batch.go:230`) | `/tmp/rag-github-123.md` ❌ | temp ❌ |
  | UI upload (`handlers_ingest.go:271`) | `/tmp/ragd-upload-123.pdf` ❌ | temp ❌ |
  | UI URL | URL ✅ | temp ❌ |
  | UI repo (`handlers_ingest.go:336`) | repo-relative path | temp ❌ |

  All temp names come from a small, known set of `os.CreateTemp` patterns: `rag-crawl-*`,
  `rag-github-*`, `ragd-upload-*`.
- **Kapa hits** set `SourceID = SourceURL` and have no title (`kapa.go:51`).
- **Saved chats** (`internal/chatstore.Turn`) are `{role, content}`, converted from and to openai
  message params in `chat/history.go:211`.
- **The UI Markdown renderer is hand-rolled** (`ui/components/common/Markdown.tsx`), and UI
  conventions forbid new UI dependencies.
- No new config keys, secrets, snap plugs, hooks, or bundled binaries are needed.

## Goals / Non-Goals

**Goals:**
- A single resolution point, so every consumer of `SearchHit` gets the same citation.
- A resolver chain shaped so that a future chunk-level reference (document / section / cell) is
  one new resolver at the head of the chain, with no change to callers or wire formats.
- No migration and no forced re-ingest. Re-ingest only buys links that were never recorded.

**Non-Goals:**
- Chunk-level references or a populated `locator` (the field is reserved, always empty).
- User-editable citation instructions: the instruction is a fixed suffix, not a fourth prompt
  template. It can be made editable later.
- Backfilling `source_uri` for existing sources, or guessing links from `source_id`.
- Citation-aware export formats beyond the JSON `sources` field (e.g. XLSX columns).

## Decisions

### D1. `Citation` lives on `SearchHit`, resolved inside retrieval

```
knowledge.Citation {
  Key      string  // document identity; today = source_id
  Label    string  // never empty
  Href     string  // omitempty
  Locator  string  // omitempty; reserved
  Base     string  // knowledge-base name (or "kapa")
  Tag      string  // resolved label tag, e.g. CANONICAL
  Via      string  `json:"-"`  // which resolver produced Label/Href (verbose output)
}
SearchHit { ...existing fields..., Citation Citation `json:"citation"` }
```

`OpenSearchClient.Search` and `KapaClient.Search` both run the resolver chain before they return,
so `/search`, `k search`, `POST /1.0/search`, chat and batch all get citations without changes at
the call sites. *Alternative:* resolve in `retrieveContext`. Rejected because search surfaces call
`Search` directly and would need their own resolution.

### D2. Resolver chain: batch-oriented, fill-only-empty

```go
type CitationResolver interface {
    Resolve(ctx context.Context, hits []SearchHit) // fills empty Citation fields in place
}
chain: [ KapaResolver, MetadataResolver(client), SourceIDResolver ]
        └─ future: ChunkRefResolver goes first
```

- Resolvers take the whole slice, so `MetadataResolver` makes **one `_mget`** on
  `rag-snap-metadata` for the distinct `source_id`s (at most `k` ids) instead of one request per
  hit.
- Each resolver fills only fields that are still empty, so the order expresses priority.
- Resolvers never return errors. A failed `_mget` is logged in verbose mode and the chain carries
  on, because the spec says citation resolution must not fail retrieval.
- `SourceIDResolver` is total: `Key = Label = source_id`.

*Alternative:* a per-field priority table. Rejected as harder to extend with the chunk-ref tier,
which supplies several fields at once.

### D3. Field rules (per spec "link" and "label" requirements)

- **Href:** Kapa `SourceURL` → `source_uri` → `file_path` if it parses as an absolute http(s) URL
  → empty.
- **Label:** title if it passes the sanity check → `file_name` if it is not a temp name → (Kapa)
  URL → `source_id`.
- **Title sanity check:** after trimming, length 3–200; not equal to the file name, compared
  case-insensitively and with or without extension; not matching
  `^Microsoft (Word|Excel|PowerPoint) - ` or `^untitled`.
- **Temp-name check:** matches `^(rag-crawl|rag-github|ragd-upload)-\d+`. The pattern list lives
  next to the `CreateTemp` calls as a shared constant, so a new temp prefix can't silently produce
  junk labels.

### D4. New `source_uri` field instead of reusing `file_path`

`SourceMetadata.SourceURI` (keyword, `omitempty`) means "what we cite"; `file_path` keeps meaning
"what we read". The existing metadata index gets the mapping through an idempotent put-mapping
(`EnsureSourceURIMapping`, following `EnsureLabelMapping`), called on ingest and at engine init.
Export/import carry the field unchanged, since elasticdump copies the documents as they are.
*Alternative:* overload `file_path`. Rejected because its meaning already varies by ingest path,
which is the debt this change pays down.

### D5. Ingest records the URI; temp paths stop leaking

- `IngestOptions.SourceURI` is new. `IngestSource` sets `FileName` from the **original** input:
  the basename of `MetadataPath`, the last URL path segment, or empty. It never uses the temp
  file's name.
- `processing.RepoEntry` gains `WebURL`, built where the branch is already resolved (`github.go:50`,
  `gitea.go:43`). GitHub uses `https://github.com/<o>/<r>/blob/<branch>/<path>`; Gitea uses
  `<base>/<o>/<r>/src/branch/<branch>/<path>`. The branch is used rather than the SHA so links stay
  readable. Links follow the branch, which is accepted for now.
- CLI batch (`knowledge/batch.go`) passes `MetadataPath = entry.Path` and `SourceURI = entry.WebURL`.
  The UI upload passes `header.Filename` as `MetadataPath`, plus the optional form `source_uri`.
- `k ingest --uri <url>` is allowed only with `--file`, since `--url` records its URL
  automatically. It is validated as absolute http(s) before Tika runs.

### D6. Markers, numbering, and a context format centred on documents

`formatContext(hits, reg)` keeps hits in score order (no regrouping) and prefixes each chunk with
its document's marker:

```
[2] [CANONICAL] guide.pdf
…chunk…
---
[1] [KAPA-CANONICAL] https://documentation.ubuntu.com/…
```

The score is dropped from the LLM context; it was noise for the model. A `CitationRegistry`
(`key → n`, `next`) assigns numbers:

- **Chat:** one registry per `Session` / `LiveSession`, kept for the life of the session. On
  `Restore` it is rebuilt from the saved turns' sources: same `key → n` mappings, `next = max+1`.
- **Batch:** a fresh registry per question, so each answer's numbering is self-contained.

`retrieveContext` returns `(contextText string, retrieved []CitedSource)`, where
`CitedSource = {Marker int, Citation}` holds the distinct documents in context.

### D7. Post-generation validation

`citeAnswer(text, retrieved) → (text', cited []CitedSource, uncited bool)`:

1. Parse markers with a tolerant pattern covering `[1]`, `[1][2]`, `[1, 2]` and `[1,2]` (an `S`
   before a number, as in `[S1]`, is tolerated), and normalize them to `[1][2]`. Numbers have at most
   three digits, so `[2024]` is not a marker.
2. Bare numbers are more ambiguous than the earlier `[S1]` form, so brackets inside inline or fenced
   code, or glued to a preceding word (`arr[1]`), are not markers. A marker followed by `(…)` is
   still a marker, never Markdown link text, so a citation cannot pose as a link to an arbitrary URL.
3. Drop markers whose number is not among `retrieved`.
4. `cited` = the remaining markers in first-cited order, deduplicated.
5. If retrieval ran (`len(retrieved) > 0`) and `cited` is empty: `uncited = true`. `text'` is still
   the normalized answer, and the caller decides what to do with it:
   - **Chat** keeps the answer and flags it unsourced ("No relevant sources").
   - **Batch** replaces it with the no-answer response, because its prose goes into procurement
     documents.

*Revised during testing.* The first iteration withheld uncited chat answers as the no-answer
response, and markers were `[S1]`. In practice small models often skip or reshape the markers, so
most chat answers became "The provided context does not contain enough information…" even when the
answer was good. Chat now keeps the answer and says it has no relevant sources, and markers are
plain numbers.

For batch, markers are then stripped from `text'`. Whitespace before punctuation is collapsed and
double spaces are trimmed.

The validation only runs when context was retrieved. The "no context retrieved" path keeps
today's behavior: chat injects an empty-context note, batch skips the LLM call.

### D8. Citation instruction travels with the retrieved context

`ragCitationRules` is added to the turn's user prompt by `buildRAGPrompt`, right after the
marker-tagged context and only when context was retrieved; the empty-context note and turns without
retrieval never carry it. It asks the model to end each paragraph that uses context with its `[#]`
markers, to use only markers present in the context, and never to cite from memory. System prompts
(default or custom, chat or batch) are sent exactly as resolved.

*Revised during testing.* The first iteration appended the rules to the system prompt. With no
knowledge base active, small models (gemma3-4b) copied the rules' `[1]` example into answers that
had nothing to cite, and nothing removed it, because validation only ran when something was
retrieved. Two changes close that gap: the rules now reach the model only alongside citable context,
and `citeAnswer` removes every marker from an answer to a turn without retrieval (D7).

Batch keeps "flowing prose, no headers"; the markers are stripped afterwards (D7). The batch system
prompt stays byte-identical across a run, as the existing domain design requires.

### D8a. Chat starts with the `default` base active

The direct REPL always activated `default`; the remote REPL and the UI started with none, so a
first question was silently ungrounded. The remote REPL now starts its daemon session with
`default` (and prints the active set), and the UI preselects the `default` chip when that base
exists and nothing else is selected. Resumed chats keep their saved set.

### D9. Streaming surfaces

- **REPL (direct):** streams as today. After the stream, it runs D7 and prints a dimmed
  `Sources` block. When `uncited` it prints a "No relevant sources" notice instead; history keeps
  the answer.
  *Alternative:* buffer the whole answer and print it only once validated. Rejected because it
  loses streaming, which matters on slow local models.
- **Websocket:** a new frame `{type:"sources", sources:[CitedSource]}` is sent before the first
  `token`. The `done` frame gains `sources` (the cited list) and `unsourced` (the answer cited
  none of them). The streamed text is never replaced. `chatServerMessage` gets
  `Sources []CitedSource` and `Unsourced bool`.
- **REPL (remote, `remote.go`):** consumes the same frames and renders them the same way as
  direct mode.

### D10. Persistence: chatstore owns its own shape

`chatstore.Turn` gains `Sources []Source` (`json:"sources,omitempty"`), where `chatstore.Source`
mirrors `CitedSource` field for field. `internal/chatstore` stays free of `cmd/cli/...` imports,
and the conversion lives in `chat/history.go`.

Since openai message params can't carry sources, the session keeps a slice parallel to its
assistant messages: `historyToTurns(messages, sources)` / `turnsToHistory(...)`. Assistant content
keeps its markers in history. Session-wide numbering keeps them consistent, so the model can refer
to `[1]` again later.

### D11. UI rendering

- **Chat screen:** `Markdown` gains an optional `citations: Map<number, CitedSource>` prop. The
  inline parser turns `[<n>]` tokens it finds in that map into a chip; unknown markers render as
  literal text and are already removed server-side.
  - The chip uses `p-chip` styling, is an `<a target="_blank" rel="noopener noreferrer">` when the
    source has an `href` and a `<span>` otherwise, has `aria-label` set to the label and `title`
    set to the label.
  - The map is seeded from the `sources` frame, so chips render while the answer streams.
  - On `done`, the message's sources are replaced by the cited list and a `Sources` list is
    rendered under the answer.
  - When unsourced, the answer stays and a muted "No relevant sources" line takes the Sources
    list's place.
- **Search page:** the header label is linked when there is an `href`; the footer shows the
  `source_id`.
- **Ingest modal:** an optional Source URL field for uploads.
- **Batch review:** a sources list on each card.
- **Types:** `ui/lib/types.ts` gains `Citation` / `CitedSource`; `QAItem.sources?` is optional, so
  older result files still parse.

## Risks / Trade-offs

- **Weak local models skip markers.** In chat the answer is still shown, with a "No relevant
  sources" notice, so the user judges it rather than losing it. In batch it is still withheld;
  verbose mode logs "withheld … no valid citations" so that is diagnosable.
- **Chat clarifying questions are flagged unsourced.** They cite nothing by nature. → Cosmetic, so
  accepted.
- **Saved chats do not record the unsourced flag**, so a resumed unsourced turn shows neither
  sources nor the notice. → Accepted for now; it would need a `Turn` field.
- **Bare `[n]` collides with ordinary text** more than `[Sn]` did (for example "the list [1, 2]"
  in prose). → Code and word-glued brackets are excluded, and numbers above three digits are
  ignored. A rare prose false positive at worst drops or relinks a bracketed number.
- **Branch-based repo links drift** as the branch moves. → Readable links were chosen over
  permalinks; switching to the SHA is a single-line change in `WebURL`.
- **The heuristic title check** may reject a good title or accept a bad one. → A wrong label only
  costs readability, never correctness. The `source_id` stays visible in `/search` and the
  sources lists.
- **An extra `_mget` per retrieval.** → It's one small request for at most `k` ids, against the
  same cluster; it doesn't matter next to the embedding and rerank cost.
- **Markers vs. Markdown:** `[1]` next to `(…)` could read as a Markdown link (`[1](x)`). →
  The prompt asks for markers at the end of paragraphs. Both the daemon and the UI parser treat it
  as a marker before link syntax, and the UI only renders numbers present in the map.

## Migration Plan

- **No data migration.** `source_uri` mapping is added idempotently. Existing sources resolve
  through the fallbacks, and old saved chats and batch result files load unchanged (all new fields
  are `omitempty` or optional).
- To get links for previously ingested repo, upload or file sources, re-ingest with `--force`
  (file sources with `--uri`). Document this in the README.
- **Rollback:** the previous binary ignores the new JSON fields and the extra mapping.
