# Design — add-knowledge-labels

## Context

Provenance labels are currently inferred, not stored. `chat.sourceLabel` (cmd/cli/basic/chat/rag.go), `api.provenanceLabel` (internal/api/handlers_search.go), and `remoteProvenanceLabel` (cmd/cli/basic/chat/remote.go) each re-derive a tag from the index name: the kapa index → `[KAPA-CANONICAL]`, a name containing "upstream" → `[UPSTREAM]`, otherwise `[CANONICAL]`. Chunk documents (`knowledge.Document`) carry only `content`, `source_id`, `created_at`; `SourceMetadata` in the `rag-snap-metadata` index has no label either. The three default prompt slots hardcode these tag names and their priority rules, but prompts are already user-customizable through the daemon's named/versioned prompt-variant store (slots `chat_system_prompt`, `answer_system_prompt`, `source_rules`).

Constraints that shape the design:

- `k export`/`k import` move bases between machines via bundled elasticdump using `--type=data` and `--type=mapping` for both the context index and the sources metadata index — anything that must travel with a base has to live in the index's documents or its mapping.
- The snapctl config backend is unavailable outside the snap and its keys are machine-local; labels describe indexed content, so config is the wrong home for them.
- Existing installs have unlabeled chunks and bases relying on the "-upstream" naming convention; saved prompt variants reference the current tag names.

## Goals / Non-Goals

**Goals:**

- Let users attach exactly one label to each ingested source, with a per-base default, and have that label appear as the `[<LABEL>]` tag on retrieved chunks in RAG context and search displays.
- Make stored labels the single source of truth at read time, with a deterministic fallback for pre-existing data.
- Collapse the three duplicated label resolvers into one.
- Preserve stock behavior: default label set (`canonical`, `kapa-canonical`, `upstream`) and default prompts keep working unchanged.

**Non-Goals:**

- No label registry (descriptions, priority ranks, CRUD API) — label semantics live entirely in user prompt text via the existing variant system.
- No multi-label sources, no retrieval-time boosting/filtering by label, no changes to the prompt-variant machinery, no re-labeling of individual already-ingested sources (forget + re-ingest covers that).

## Decisions

### 1. Labels are denormalized onto chunks at ingest; read path never joins

`knowledge.Document` gains `Label string \`json:"label,omitempty"\``, and the index template mapping gains a `label` keyword field. `SearchHit` gains `Label`, populated during hit decoding: the stored `_source.label` when present, otherwise the legacy index-name inference. All consumers (REPL `formatContext`, `/search` and `k search` rendering, daemon `handleSearch`) read `hit.Label` and never re-derive it.

*Why:* a join against source metadata on every search adds latency and a failure mode; a keyword field is negligible storage. Denormalization also makes export/import correct for free — elasticdump copies `_source` verbatim.

*Alternative rejected:* resolving labels from `SourceMetadata` at query time — requires an extra query per search, and hits from imported bases whose metadata records failed to import would silently lose labels.

### 2. One resolver in the `knowledge` package

A single exported function, e.g. `knowledge.ResolveLabel(indexName, storedLabel string) string`, implements: stored label wins; else kapa index → `kapa-canonical`; else name contains "upstream" → `upstream`; else `canonical`. `chat.sourceLabel`, `api.provenanceLabel`, and `remoteProvenanceLabel` are deleted; rendering as an uppercase bracketed tag (`[UPSTREAM]`) is one shared formatting helper. Kapa hits (fetched live, never indexed) are constructed with `Label: "kapa-canonical"` at the kapa client.

### 3. Base default label lives in the index mapping `_meta`

`k create <name> --label <label>` writes `{"_meta": {"default_label": "<label>"}}` into the index mapping; `k label <base>` reads it, `k label <base> <label>` updates it via `PUT /<index>/_mapping`. When unset, the effective default follows the legacy convention (name contains "upstream" → `upstream`, else `canonical`) so old bases behave identically.

*Why `_meta`:* it is the one per-index key/value slot that elasticdump's `--type=mapping` export already carries, so a base's default label travels with `k export`/`import` without new archive formats. Config (snapctl) is machine-local and unavailable outside the snap; a reserved document in `rag-snap-metadata` would need import-path rewrites and can be lost independently of the index.

*Consequence:* the import path must be verified (and if needed, taught) to restore `_meta` when it recreates the target index — flagged in Risks.

### 4. Ingest label resolution: flag > base default > legacy inference

`IngestOptions` gains `Label`. Callers (CLI `k ingest --label`, batch jobs via a new `label:` field on `BatchJob`, daemon ingest endpoints via a `label` request field, Google Drive import) resolve the label before calling `IngestSource`: explicit value if given, otherwise the base's `_meta` default, otherwise the legacy inference. The resolved label is written to every chunk `Document` and to the source's `SourceMetadata` (new `Label` field), so `k sources` and the sources API can display it.

Label values are validated with `^[a-z0-9][a-z0-9-]{0,31}$` (lowercase, like index names) and rendered uppercase inside brackets. This keeps prompt tags predictable and prevents prompt-injection-shaped labels (`]` or newlines inside a tag).

### 5. Semantics stay in prompts; defaults are unchanged in meaning

`formatContext` keeps its exact structure — only the tag now comes from `hit.Label`. The compiled-in default prompts continue to reference `[CANONICAL]`, `[KAPA-CANONICAL]`, `[UPSTREAM]`; their wording is touched only to note that tags come from user-assigned labels. Users who define custom labels express priority in their own prompt variants (already supported end-to-end: save/activate/`chat --prompt`). No generated preamble, no template placeholders.

*Why:* this was the explicit scope decision (pure prompt-side). It keeps this change orthogonal to the just-shipped variant system and avoids inventing a second place where prompt text is assembled.

### 6. Backfill is explicit and additive

`k label <base> <label> --apply-to-existing` (and the daemon equivalent) first ensures the `label` mapping field exists on the index (`PUT _mapping` — the updated template only applies to newly created indexes), then runs `_update_by_query` with `conflicts=proceed` setting `label` **only on chunks that lack one** (`must_not: exists label`), and updates the matching `SourceMetadata` records the same way. Chunks that already carry a label are never overwritten — an explicit per-source label given at ingest time survives a later base-level backfill.

On the daemon, backfill runs as an async operation (consistent with ingest/export), since `_update_by_query` on a large base is slow.

### 7. REST API: `label` replaces `provenance` (breaking)

`searchResult.Provenance` becomes `Label`; source and base payloads gain `label`/`default_label`; ingest requests accept `label`. `internal/apiclient` and the bundled UI are updated in lockstep. No versioning shim: the API is localhost-only with the UI as its only known consumer.

### 8. UI: minimal additive surface, per ui-conventions

Search result footers and the base-detail sources table show the label as a text chip; the create-KB modal and ingest form gain an optional label input (validated with the same pattern, placeholder showing the effective default). Implementation must follow `.claude/skills/ui-conventions/SKILL.md` (Vanilla tokens, existing form/table patterns). No new pages.

No snapcraft.yaml changes: no new interfaces, binaries, hooks, config keys, or secrets.

## Risks / Trade-offs

- **[`_meta` round-trip through export/import is unverified]** → during implementation, exercise `k export`/`k import` on a labeled base against a real install; if elasticdump's mapping restore or the import path's index-recreation drops `_meta`, copy it explicitly in `import.go` (it already reads the exported mapping file).
- **[Existing indexes lack the `label` mapping field; dynamic mapping would type it wrong]** → every write path that can touch an older index (`k label`, backfill, ingest into an existing base) ensures the keyword mapping via `PUT _mapping` first; adding a field is always mapping-compatible.
- **[User labels the LLM was never told about]** → with a custom label and a stock prompt, chunks carry tags the prompt doesn't explain; the model may under-weight them. Mitigation: `k label`/`k ingest --label` help text and README point to prompt variants as the place to define label meaning. Detecting/warning on prompt–label mismatch is deliberately out of scope.
- **[Backfill semantics surprise]** → `--apply-to-existing` fills only unlabeled chunks; a user expecting wholesale re-labeling must forget and re-ingest. Documented in command help.
- **[Breaking API field rename]** → localhost API with bundled UI; acceptable. Release notes mention it for anyone scripting against `/1.0/search`.
- **[Sparse test culture]** → label resolution, validation, and ingest-time resolution order are pure functions — add unit tests there (`knowledge` package) even though most of the codebase is untested.

## Migration Plan

1. Ship additively: new binary reads old indexes via the read-time fallback; no install-hook or data migration runs.
2. Users opt in per base: `k label <base> <label> --apply-to-existing`, or simply keep relying on the fallback.
3. Rollback is safe: an older binary ignores the `label` field in `_source` and re-derives tags from index names; no data is lost.

## Open Questions

- Should `k create` also accept `--label` interactively (huh prompt) when omitted, or stay flag-only? (Default assumption: flag-only, effective default shown in `k label`.)
- Should the batch answer manifest (`answer batch`) ever select labels for retrieval scoping? Out of scope now; revisit if label-based retrieval filtering is requested.
