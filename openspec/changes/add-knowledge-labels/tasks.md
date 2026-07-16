# Tasks — add-knowledge-labels

## 1. Label model in the knowledge package

- [x] 1.1 Add label validation (`^[a-z0-9][a-z0-9-]{0,31}$`) and the single resolver `ResolveLabel(indexName, storedLabel)` (kapa index → `kapa-canonical`, "upstream" substring → `upstream`, else `canonical`; stored label wins) plus a shared uppercase-bracket tag formatter, with unit tests in `pkg`-style `_test.go` files under `cmd/cli/basic/knowledge`
- [x] 1.2 Add `label` keyword field to the index template mapping (`indexes.go`) and a helper that ensures the field exists on an already-created index via `PUT _mapping`
- [x] 1.3 Add `Label` to `knowledge.Document` (`bulk.go`), `SourceMetadata` (`sources.go`), and `SearchHit` (`search.go`, decoding `_source.label` and applying the resolver so every hit carries a final label)
- [x] 1.4 Add base default label storage in index mapping `_meta.default_label`: write on create, read helper returning (label, stored-vs-convention), update via `PUT _mapping`

## 2. Ingest paths carry labels

- [x] 2.1 Add `Label` to `IngestOptions` and store the resolved label on chunks and source metadata in `IngestSource` (`ingest.go`); resolution order: explicit > base `_meta` default > legacy inference
- [x] 2.2 Add `label:` to `BatchJob` and thread it through `ProcessBatch`/`processSingleJob` (`batch.go`)
- [x] 2.3 Thread labels through the remaining ingest callers: CLI `k ingest --label` flag, Google Drive import, and daemon ingest handlers (request `label` field, per-entry for batch), validating before any work starts

## 3. CLI surfaces

- [x] 3.1 Add `--label` to `k create` and create the index with `_meta.default_label`
- [x] 3.2 Add `k label <base> [<label>]` subcommand (show effective default + provenance; set default; `--apply-to-existing` runs mapping-ensure + `_update_by_query` with `conflicts=proceed` over chunks lacking a label, and updates unlabeled source metadata records), preserving the fixed command ordering in the root
- [x] 3.3 Show labels in `k sources` listing and source metadata inspection (fallback label for legacy sources)
- [x] 3.4 Replace `chat.sourceLabel` usage in `formatContext` (`rag.go`) and `/search` rendering (`search.go`) with `hit.Label`; delete `sourceLabel`; set `Label: "kapa-canonical"` on hits in the kapa client; adjust default prompt constants' wording to note tags come from user labels (same default tag names and rules)

## 4. Daemon API and client

- [x] 4.1 Replace `provenance` with `label` in `handlers_search.go` (use `hit.Label`, delete `provenanceLabel`), and update `internal/apiclient` types and `remote.go` (delete `remoteProvenanceLabel`, render the returned label)
- [x] 4.2 Expose `default_label` in base detail/list payloads, accept `default_label` on create (validated), and add the set-default-label API with `apply_to_existing` running backfill as an async operation
- [x] 4.3 Include `label` in source list/inspect payloads; update API handler tests that assert the old `provenance` field

## 5. UI (follow ui-conventions skill)

- [x] 5.1 Update `ui/lib/api` types: search hit `label`, source `label`, base `default_label`, ingest/create request fields
- [x] 5.2 Search page: render the API-returned label in the result card footer
- [x] 5.3 Knowledge pages: optional default-label input in the create modal, optional label input in the ingest modal (effective default as placeholder, client-side validation), label chip column in the sources table, default label on the base detail header, label in the source metadata view
- [x] 5.4 Edit the default label from the base detail: `patchAsync` envelope verb + `setKnowledgeLabel` client, edit-label modal (pre-filled value, apply-to-existing checkbox, validation) opened from the detail header, sync update refreshes detail, backfill runs as a tracked operation

## 6. Verification and docs

- [ ] 6.1 Run `make all`; build the snap, install it, and verify end-to-end on a real install: create a labeled base, ingest with and without `--label`, `k search`/chat `/search` show stored labels, chat context tags custom labels, legacy unlabeled base still shows convention labels
- [ ] 6.2 Verify `_meta.default_label` survives `k export`/`k import` on a real install; if elasticdump/import drops it, restore it explicitly in `import.go`
- [ ] 6.3 Verify `k label <base> <label> --apply-to-existing` backfills a pre-labels base and that an explicitly-labeled source is untouched
- [x] 6.4 Update README command docs (`k create --label`, `k label`, `k ingest --label`, batch manifest `label:`) and note that label semantics are defined in prompt variants; mention the `provenance` → `label` API field change in release notes
