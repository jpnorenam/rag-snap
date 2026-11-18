# Tasks

## 1. Spikes (verify assumptions before building)

- [x] 1.1 Against the bundled OpenSearch version, compare `index.blocks.read_only` and `index.blocks.write` on a test generation (document writes, mapping/`_meta` updates, search, `GET _mapping`, alias changes, delete); delete the test indexes afterwards and note the results and the chosen block in design.md (Decision 7)
- [x] 1.2 Using the bundled elasticdump, export data from an alias that points at a generation and import it into a fresh generation with no ingest pipeline; verify chunk counts match and the same query returns the same top hit; note the results in design.md (Decision 9)
- [x] 1.3 Confirm `POST /_aliases` with a remove + add pair switches searches with no window where the alias resolves to zero or two indexes, and verify with a script that searches continuously during the switch

## 2. Generation layout and name resolution

- [ ] 2.1 Change the index template to pattern `rag-snap-gen-*` without the shared `rag-snap-context` alias, and make `knowledge init` replace the old template; verify with `GET _index_template` after init
- [ ] 2.2 Add generation naming (`rag-snap-gen-<kb>-<unix-millis>`) and base creation that creates the first generation with its alias in one request; unit-test the name builder and verify `knowledge create` produces an alias and one generation
- [ ] 2.3 Add the alias resolver (`kb → generation`, `generation → kb`) with unit tests over a recorded `GET _alias` response
- [ ] 2.4 Switch listing (`knowledge list`, REST list, `analysis/catalogue.go`, `rfp.go`, chat `/use-knowledge`) to aliases with stats from the generations; verify a base appears once and an unaliased generation does not appear
- [ ] 2.5 Map search hit `_index` to the base through the resolver in `chat/search.go`, `chat/turn.go`, `chat/history.go`, `internal/api/handlers_search.go` and `internal/api/chats.go`; verify hits report the base name in CLI search and `POST /1.0/search`
- [ ] 2.6 Make delete resolve and delete the generation(s), and make label read/write use the resolved generation; verify `knowledge delete` and `knowledge label` on a v1 base
- [ ] 2.7 Update `docs/usage.md` (knowledge storage overview, `create`/`list`/`delete`) and command `--help` text to match; verify the documented commands run as written

## 3. Source records and origin

- [ ] 3.1 Create the `rag-snap-sources` index with the mapping from design.md Decision 4 (record type, kb, generation, origin object, timestamps, verification, `review_interval_days`, description fields) and create it in `knowledge init`; verify the mapping with `GET rag-snap-sources/_mapping`
- [ ] 3.2 Rewrite source metadata CRUD to key by generation + source ID and to read only the base's current generation; unit-test ID derivation and verify the same source ID ingests into two bases and `forget` in one leaves the other
- [ ] 3.3 Record origin for CLI `--file`/`--url`, REST upload/URL and batch jobs, and drop `file_path`; verify with `knowledge metadata` for each kind
- [ ] 3.4 Add the blob hash to `RepoEntry` from the GitHub and Gitea tree listings, record it as the origin version, and record repository source groups (resolved branch, path filter, extensions); unit-test the tree parsing and verify a repo ingest creates one group referenced by every file
- [ ] 3.5 On forced re-ingest, read the previous record first: preserve verification and description fields, always update `last_checked_at`, update `last_changed_at` only when the checksum changes; unit-test the merge logic
- [ ] 3.6 Show origin, group and freshness fields in `knowledge metadata` and `knowledge list --sources`; update `docs/usage.md` (source IDs are now per base: remove the "Source IDs are global" and repository-ID upgrade notes, document the new metadata fields) and verify the examples match real output

## 4. Read-only knowledge bases

- [ ] 4.1 Store `read_only` in the generation `_meta` and add `ErrReadOnly` checks to ingest, batch ingest, forget, set label, delete and user-initiated import in the shared `knowledge` package; unit-test that each function refuses a read-only base
- [ ] 4.2 Apply `index.blocks.write` to read-only generations as described in design.md Decision 7; verify a direct `curl` document write to a read-only generation is rejected and that swapping and deleting it still work
- [ ] 4.3 Map `ErrReadOnly` to a `409` with a clear message in the REST handlers and to a clear CLI error; verify with a base marked read-only in a test (set `_meta` directly)
- [ ] 4.4 Document read-only bases in `docs/usage.md` (what is refused and what still works); verify the wording matches the CLI error text

## 5. Rebuild-and-swap primitive

- [ ] 5.1 Implement the per-base lock in `rag-snap-sources` (`op_type=create`, owner, start time, one-hour expiry); unit-test acquire, conflict and stale-lock takeover
- [ ] 5.2 Implement rebuild-and-swap (create generation, load, refresh, count check, carry read-only, atomic alias switch, 5 s grace, delete old records and generation; full cleanup on failure or cancellation); verify with an integration test that a failed load leaves the base serving its old content
- [ ] 5.3 Add the `knowledge init` sweep of unaliased, unlocked generations and their records; verify an orphaned generation created by hand is removed and a locked one is kept

## 6. Archive format v2 and import

- [ ] 6.1 Write manifest version 2 with `embedding_model` and `settings.default_label`, write `sources.json` with source and group records without `kb`/`generation`, and stop writing `mapping.json`; verify by exporting a base and inspecting the archive
- [ ] 6.2 Validate the manifest before any write (version must be `2`, embedding model must match) with errors naming both models or telling the user to re-export; unit-test each refusal
- [ ] 6.3 Route import through generations: new base → first generation then alias; existing base with force → rebuild-and-swap; apply `settings.default_label`; verify a forced import of an archive missing a source removes that source from the base
- [ ] 6.4 Verify Google Drive import (`knowledge import` with a Drive URL and `POST /1.0/knowledge/gdrive/import`) uses the same path and refusals
- [ ] 6.5 Update `docs/usage.md` (export/import: replace semantics, manifest v2, refusals) and verify the documented commands run as written

## 7. Legacy (v0) bases

- [ ] 7.1 Detect concrete `rag-snap-context-*` indexes as legacy; list them with a legacy marker in the CLI and REST list; refuse every operation except delete with the "delete and re-create" error; verify against a hand-created legacy index
- [ ] 7.2 Delete legacy bases with their `rag-snap-metadata` records, and delete `rag-snap-metadata` when no legacy index remains; refuse creating a base whose name matches a legacy index; verify both
- [ ] 7.3 Add the v1 upgrade note to `INSTALL.md` (run `knowledge init`, delete legacy bases, re-ingest or import v2 archives, maintainers re-export, no downgrade) and verify the steps against an upgraded snap

## 8. REST API surface

- [ ] 8.1 Add `read_only` and `legacy` to knowledge list and detail responses, and the new source fields to source responses; verify with `curl` against ragd
- [ ] 8.2 Update `rest-api.yaml` and `docs/rest-api.md` for the new fields, the `409` read-only and legacy-conflict errors, import replace semantics and archive refusals; verify the OpenAPI file still validates

## 9. Web UI

- [ ] 9.1 Show "Legacy (v0)" bases with only a Delete action, and "Read-only" bases without ingest, forget or delete actions, in the knowledge list and detail; verify in the running UI with a legacy and a read-only base
- [ ] 9.2 Show origin and freshness fields in the source metadata modal; verify for an uploaded file, a URL and a repository file
- [ ] 9.3 Verify compliance with the `ui-conventions` skill for the changed views: light and dark themes, keyboard-only pass, `--vf-*` tokens only, all four view states, 620px width with no horizontal page scroll
- [ ] 9.4 Update `docs/local-ui.md` for the legacy and read-only badges and the metadata modal fields

## 10. Integration

- [ ] 10.1 Run `make all` and the UI tests, and verify they pass
- [ ] 10.2 Build and install the snap, then exercise end to end: `knowledge init`, create a base, ingest a file, a URL and a GitHub repo, search from CLI and UI, export, force-import the archive into the same base while searching continuously, and confirm the base never returns an error or mixed results

## Workflow follow-up

- Archive this change with `/opsx:archive` once implemented and verified.
- Next change from the roadmap in `PLAN.md`: `drive-subscriptions`.
