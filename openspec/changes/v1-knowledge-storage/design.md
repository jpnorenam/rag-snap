# Design

## Context

See proposal.md for motivation and specs/ for the required behavior. The wider roadmap (Drive
subscriptions, refresh, health) and the decisions behind it are in the local `PLAN.md`.

Current storage, as relevant here:

- Each base is a concrete index `rag-snap-context-<kb>` (`FullIndexName`, indexes.go). The index
  template matches `rag-snap-context-*` and also attaches a shared `rag-snap-context` alias that
  no query uses.
- Source metadata lives in one index, `rag-snap-metadata`, with documents keyed by `source_id`
  alone (sources.go), so IDs collide across bases.
- Listing uses `_cat/indices` on the pattern; delete uses `DELETE /<index>`; the default label is
  read from the mapping `_meta`, keyed by the index name in the response (labels.go).
- Search hits carry `_index`, which callers convert to a base name with
  `KnowledgeBaseNameFromIndex` (chat/search.go, handlers_search.go, chat/turn.go, chats.go).
- Import (import.go) loads mapping, data and sources with elasticdump into the base's index and
  only refuses a non-empty target without `--force`; with `--force` it merges.
- The CLI talks to OpenSearch directly through the shared `knowledge` package; ragd uses the
  same package. Both can run at once.

No config keys, secrets, snap plugs, hooks or bundled binaries change. elasticdump stays bundled.

## Goals / Non-Goals

**Goals:**
- One storage layout for every base (no separate Local and Drive layouts).
- A reusable "rebuild into a new generation, then swap" primitive that import uses now and Drive
  sync will use later.
- Read-only enforcement in the shared package plus OpenSearch.
- Source records with every field later phases need, so v1 data never needs a migration.

**Non-Goals:**
- Drive subscriptions, sync UI, syncing warnings, refresh, health states, review intervals in
  use, "Looks good to me", AI descriptions, publishing. These are later changes.
- Any way for users to mark a base read-only. The mechanism exists; Drive subscriptions will use
  it.
- Migrating v0 data.

## Decisions

### 1. Base name = alias; content = generation index

The alias keeps today's name, `rag-snap-context-<kb>`, so everything that stores base names (chat
history, sessions, batch manifests, UI state) keeps working. Generations are concrete indexes
named `rag-snap-gen-<kb>-<unix-millis>`. Code never parses a generation name to find its base; it
resolves through the alias.

A base is created by creating its first generation with the alias in the same create-index
request, so a base never exists without content or vice versa.

*Alternatives:* keep concrete indexes and replace with delete-then-reload (loses data on failure,
empty during import); add generations only for Drive bases (two code paths, and Local bases lose
safe rebuilds).

### 2. Index template moves to the generation prefix

The template pattern becomes `rag-snap-gen-*`, and the unused shared `rag-snap-context` alias is
dropped from it. A half-built generation therefore never matches the base pattern, so it cannot
appear in listings. `knowledge init` replaces the old template.

### 3. Resolving names

A single helper resolves aliases with `GET /_alias/rag-snap-context-*` and returns
`kb → generation` and `generation → kb` maps. It is used by:

- listing (bases are the aliases; per-base stats come from `_cat/indices` on the generations),
- search hit attribution (map `_index` back to the base),
- delete, labels and import (operate on the resolved generation).

`KnowledgeBaseNameFromIndex` is kept for alias names only. Callers that pass `_index` from hits
switch to the resolver. Search resolves once per request; no cache, so a swap is visible
immediately.

### 4. Source records: new index, keyed by generation

A new index, `rag-snap-sources`, replaces `rag-snap-metadata` for v1. Document `_id` is a hash of
`generation + source_id`. Each record stores `kb`, `generation`, `source_id` and the fields from
the source-metadata spec. Readers resolve the base's current generation and query by it, so
records from a generation being built are invisible until the swap. Swapping removes the old
generation's records after the alias moves.

Repository source groups are documents in the same index with `record_type: group` (sources have
`record_type: source`), keyed by `generation + group_id`; `group_id` is derived from repository,
branch, path filter and extensions. Counts and listings filter on `record_type`.

Field types are fixed now to avoid a later mapping change: timestamps are dates;
`review_interval_days` is an integer (absent = use the default, decided in a later change);
`verified_by` and `description_source` are keywords; `description` is text; origin is an object
(`kind` keyword, `location` keyword, `version` keyword, `group_id` keyword).

*Alternative:* key records by `kb + source_id`. Rejected: during a rebuild the new records would
overwrite the live ones before the swap.

### 5. Rebuild-and-swap primitive

```
lock(kb) → create generation G2 (writable, from template)
        → load content (import data / sources / groups, or future sync)
        → refresh, verify counts against the manifest
        → if base is read-only: apply the write block to G2
        → POST /_aliases { remove G1, add G2 }          ← atomic switch
        → wait 5 s (searches already running on G1 finish)
        → delete records of G1, delete G1 → unlock(kb)
on any failure before the switch: delete G2 and its records, unlock; G1 untouched
```

The lock is a document in `rag-snap-sources` created with `op_type=create` (so the CLI and ragd
cannot rebuild the same base at once), holding owner and start time; a lock older than one hour
is treated as stale. `knowledge init` sweeps generations that no alias points at and that hold no
live lock.

### 6. Ingest writes through the alias

Ingest, forget and re-ingest keep writing to the base name; OpenSearch routes writes through a
single-index alias. The forced re-ingest reads the previous record first, so it can preserve
verification and description fields and decide whether `last_changed_at` moves (checksum
comparison).

### 7. Read-only: package check + `index.blocks.write`

The read-only flag is stored in the generation's mapping `_meta` (`read_only: true`) and copied
to each new generation by the rebuild primitive. Every mutating function in the `knowledge`
package (ingest, batch ingest, forget, set label, delete base, import without the rebuild path)
checks it first and returns a typed `ErrReadOnly`; the CLI prints it and the API maps it to
`409`. The generation also gets `index.blocks.write: true`, which rejects every document write,
update and delete while still allowing search, mapping reads, alias changes and deleting the
index, so swaps and deletes need no extra steps. Label and `_meta` edits are not blocked by
OpenSearch; the package check guards them, and a sync rewrites them anyway.

*Alternatives:* `index.blocks.read_only` (also blocks mapping changes, but blocks alias changes
and delete, so every swap would need unblock → switch → block, with a window where the new
generation relies on the package check alone; rejected after the spike below);
`read_only_allow_delete` (can be released automatically by the disk watermark logic);
`index.blocks.metadata` (also blocks reading the mapping, which breaks label reads).

*Spike result (task 1.1, OpenSearch 2.19.6):*

| Request on the blocked index | `blocks.read_only: true` | `blocks.write: true` |
|---|---|---|
| Document write, bulk, delete-by-query | 403 | 403 |
| Mapping `_meta` update, new field | 403 | allowed |
| Settings change (other than blocks) | not tested | allowed |
| Mapping read, search | allowed | allowed |
| Add or remove an alias | **403** | allowed |
| Delete the index | **403** (allowed after clearing the block) | allowed |

`read_only` blocks alias changes, so the swap in Decision 5 would fail on a blocked generation
unless the old generation's block is cleared first and the new generation's block applied after
the switch. Decision: use `blocks.write`.

### 8. Legacy (v0) detection

A legacy base is a concrete index matching `rag-snap-context-*` (not an alias). Listing reports it
with `legacy: true`. Every operation except delete refuses it with "delete and re-create this
knowledge base". Deleting a legacy base deletes the index and its records from
`rag-snap-metadata`; when no legacy indexes remain, `rag-snap-metadata` is deleted too. Creating a
base whose alias name matches a legacy index is refused (OpenSearch would reject the alias
anyway; the check gives a clear message).

### 9. Archive format v2

`manifest.json` gains `version: "2"`, `embedding_model: {name, version}` (from the compiled-in
model constants) and `settings: {default_label}`. The archive holds `data.json` (chunks),
`sources.json` (source and group records without `kb`/`generation`) and `manifest.json`;
`mapping.json` is no longer written or read, since the template defines the mapping and the
manifest carries the settings. Import checks the manifest before any write: version must be `2`,
the embedding model must match exactly. Data is loaded into the new generation with elasticdump
(no ingest pipeline, embeddings kept); records are written by Go with the new `kb` and
`generation`.

*Spike result (task 1.2):* the bundled elasticdump (node 20.19.0) exported a 39-chunk base
through an alias and imported it into a fresh generation with no ingest pipeline: 39 chunks, the
same document IDs, and identical top-3 k-NN hits and scores for the same vector. Exported
records carry the generation name in `_index`; elasticdump ignores it and writes to the output
index. Search hits through an alias report the generation in `_index`, confirming Decision 3.

*Spike result (task 1.3):* 4 threads searched continuously (1,316 searches) while the alias was
switched 40 times with a remove + add pair: no empty results, no results mixing generations, no
errors.

Import paths: new base → build first generation, then add the alias; existing base without force
→ refuse (unchanged); existing base with force → rebuild-and-swap. A read-only base can only be
replaced through the rebuild primitive by its owner feature (Drive sync, later); a user-initiated
import into a read-only base is refused.

### 10. Origin recording

Each ingest path fills origin at the point it knows it: CLI `--file` / API upload → `upload` +
file name; `--url` → `url` + URL; repository jobs → `github`/`gitea` + `owner/repo:<path>` (Gitea
includes the host) + the file's blob hash from the tree listing, plus the group. The existing
`file_path` field is dropped in favor of origin.

### 11. UI

The web UI knowledge list shows legacy bases with a "Legacy (v0)" badge and only a Delete action,
and read-only bases with a "Read-only" badge and no ingest, forget or delete actions. The
source metadata modal shows origin and freshness fields. Follows the `ui-conventions` skill
(Vanilla components and `--vf-*` tokens, both themes, keyboard access).

## Risks / Trade-offs

- [Two generations exist during a rebuild: disk and k-NN memory briefly double] → acceptable at
  the expected size; document it; one rebuild per base at a time (lock).
- [A search running when the old generation is deleted fails] → 5 s grace before deletion.
- [elasticdump behavior against generations and aliases is assumed] → spike task first: export
  from an alias, import into a generation, compare chunk counts and a search result.
- [Block semantics differ by OpenSearch version] → verified on 2.19.6 (Decision 7); re-check
  when the OpenSearch snap moves to a new major version.
- [`blocks.write` does not stop direct mapping edits] → the package check guards labels and
  `_meta`; a direct `curl` edit is possible but a sync rewrites them.
- [Crash between switch and cleanup leaves an orphaned old generation] → `knowledge init` sweep
  of unaliased, unlocked generations.
- [Stale lock blocks rebuilds after a crash] → one-hour expiry; the error names the lock owner and
  age.
- [Every user must re-ingest; every archive must be re-exported] → accepted (v0 → v1); upgrade
  note in INSTALL.md and release notes; legacy listing makes the leftover data visible.
- [Downgrading to v0 after creating v1 bases] → not supported; stated in the upgrade note.

## Migration Plan

No data migration. On upgrade:

1. Run `knowledge init` (replaces the template, creates `rag-snap-sources`, sweeps orphans).
2. Legacy bases show as legacy; delete each and re-create and re-ingest, or import a v2 archive.
3. Maintainers re-export shared archives with v1 so they carry manifest version 2.

Rollback: reinstall the previous snap revision; v1 bases are not readable by v0.

## Open Questions

None blocking. The review interval default and health thresholds are decided in the
source-health change; this change only reserves the fields.
