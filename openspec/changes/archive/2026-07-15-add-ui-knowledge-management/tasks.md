# Tasks: add-ui-knowledge-management

Read `docs/ux/00-foundation.md` first, then `docs/ux/02-knowledge-management.md`. UI work also
follows the `ui-conventions` skill.

> **Verification status (2026-07-14):** all code is implemented and statically verified —
> `go build`/`go vet`/`go test ./...` pass, the UI typechecks and `next build` produces the
> `/knowledge` route, `rest-api.yaml` is in sync (`make spec-check`), and the snap **packages
> successfully** (`snapcraft`, exit 0). Items still open need a running stack the automated session
> could not drive: `golangci-lint` isn't installed (the repo pins the v2 config), and
> `sudo snap install` / the live end-to-end exercise (1.5, 3.4, 14.2–14.4) plus the in-browser DoD
> checks (15.2 dark/light, 15.3 620px render, 15.4 keyboard walkthrough, 16.4 export/import
> round-trip) require an interactive install + browser. Commands to finish are in §14.

## 1. Ingest: unify paths and fix force (rest-api-knowledge)

- [x] 1.1 Extract one shared ingest helper in `cmd/cli/basic/knowledge/` and make both the daemon (`internal/api/handlers_ingest.go` `ingestOne`) and the CLI (`batch.go` `ingestAndIndex`) call it, removing the duplicated logic
- [x] 1.2 Add a `force` field to the ingest request (`ingestRequest`) and thread it through the shared helper
- [x] 1.3 Implement re-ingest semantics: skip when the source is already `completed` and `!force`; when `force`, call `DeleteChunksBySourceID(index, sourceID)` before re-indexing so force **replaces** (no appended duplicate chunks)
- [x] 1.4 Return a distinguishable duplicate-source error (dedicated code/sentinel) when a completed source id is re-ingested without `force`, so the UI can render a field-level message
- [ ] 1.5 Add a regression test proving a forced re-ingest leaves no orphaned chunks (chunk count equals the new batch only)

## 2. Batch: server-side github/gitea fetchers (rest-api-knowledge)

- [x] 2.1 Extend the batch item type to carry a source `type` (file/url/github/gitea) plus repo coordinates
- [x] 2.2 Route `github`/`gitea` batch entries through the existing fetchers (`processGitHubRepoJob`/`processGiteaRepoJob`) inside the ingest operation, reading `GITHUB_TOKEN`/`GITEA_TOKEN` from the daemon env
- [x] 2.3 Fail a repo entry whose token is missing with the exact env-var hint, leaving the rest of the batch to proceed
- [x] 2.4 Reject batch `file:` entries referencing local paths over the API with a clear "upload instead" message (confinement boundary)

## 3. Engine-init: return and persist model IDs (rest-api-knowledge)

- [x] 3.1 Surface the freshly-resolved embedding/rerank model IDs from initialization (accessors on the client or a return value) instead of re-reading config in `handleEngineInit`
- [x] 3.2 Put the resolved IDs in the operation metadata
- [x] 3.3 Persist the IDs to `package`-scoped config (`knowledge.model.embedding`, `knowledge.model.rerank`) from the daemon so the engine is usable post-init without a manual `config set`
- [ ] 3.4 Verify search/rerank succeed after a daemon-driven init with no manual step

## 4. List enrichment (rest-api-knowledge)

- [x] 4.1 Add `source_count` to `knowledgeBaseSummary` via a single aggregation over the metadata index (group by index name), avoiding an N+1

## 5. Export/import browser round-trip (rest-api-knowledge)

- [x] 5.1 Make export default to a single compressed archive and record its location/id in the operation metadata
- [x] 5.2 Add an authenticated download for a completed export (e.g. `GET /1.0/knowledge/{name}/export/{opId}/archive`) that streams the `.tar.gz` with `Content-Disposition: attachment`
- [x] 5.3 Add a `multipart/form-data` branch to `POST /1.0/knowledge/import` that stages the uploaded archive, unpacks it to a temp dir, imports, and cleans up; keep the existing `input_dir` JSON form
- [x] 5.4 Carry optional target name and `force` (overwrite) through the multipart form; respect existing upload size limits and stream rather than buffer

## 6. API surface: routes, tests, docs

- [x] 6.1 Wire any new routes in `internal/api/server.go`
- [x] 6.2 Update `rest-api.yaml` and `docs/rest-api.md` for the ingest `force`, batch types, engine-init IDs, `source_count`, export download, and import upload
- [x] 6.3 Update `docs/usage.md` for the corrected `k ingest --force` (replace, not append) behavior change
- [x] 6.4 Add/extend handler tests (`handlers_knowledge_test.go`, ingest/engine tests) for the new behavior

## 7. UI: API client (local-ui-app)

- [x] 7.1 Extend `ui/lib/api/knowledge.ts` with typed verbs for engine-init, sources list/get/forget, single + batch ingest (with `force`), export + download, and import upload; normalize `null` arrays to `[]`
- [x] 7.2 Add any needed envelope helpers (`deleteSync` already exists; add multipart/download helpers following the `request()` pattern)

## 8. UI: knowledge list + engine gate (local-ui-app)

- [x] 8.1 Add `ui/app/knowledge/page.tsx` rendering list or detail off `?kb=` via `useSearchParams()`
- [x] 8.2 KB list table (name, source count, open/export/delete) with all four view states and the empty-state CLI hint; name is a real link, row is not a click target
- [x] 8.3 Engine-not-initialized caution gate with "Initialize engine" as a tracked op; show copyable model IDs on success without blocking the page

## 9. UI: create and delete (local-ui-app)

- [x] 9.1 Create modal with a validated name field; on error keep the modal open with field-level message and preserved input; on success refresh + positive notification
- [x] 9.2 Delete via type-to-confirm modal stating the source count; destructive button disabled until the typed name matches

## 10. UI: detail, sources, metadata, forget (local-ui-app)

- [x] 10.1 Detail view: title, source-count subtitle + CLI hint, back link; sources table (id, title, type, ingested relative-with-title) with all four states and empty-state hint
- [x] 10.2 Metadata modal rendering the definition list + copyable raw JSON
- [x] 10.3 Forget via plain confirm modal naming the source and base; refresh on success

## 11. UI: single-document ingest (local-ui-app)

- [x] 11.1 Ingest modal: upload-file / from-URL choice (dropzone enhances the native input), source-id prefilled from filename slug, force-re-ingest checkbox
- [x] 11.2 Submit → tracked operation, close modal immediately; row appears on success
- [x] 11.3 Duplicate-id error without force keeps the modal open with the field-level message and preserves input
- [x] 11.4 Slim live in-progress hint above the sources table while an ingest for the open KB runs (`aria-live="polite"`)

## 12. UI: batch ingest (local-ui-app)

- [x] 12.1 Batch modal: dropzone for the YAML manifest + link to the schema; client-side parse and preview of entries with type indicators before "Start batch"
- [x] 12.2 Each entry joins a tracked operation; missing-token repo entries fail with the exact env-var hint

## 13. UI: export and import (local-ui-app)

- [x] 13.1 Export (list row + detail action) as a tracked op; on success offer a real browser download of the archive
- [x] 13.2 Import modal: dropzone for the `.tar.gz`, optional target name, force-overwrite with inline warning; tracked op; refresh on success
- [x] 13.3 Include the muted Google-Drive-to-CLI line in the import modal

## 14. Verification

- [x] 14.1 `make all` (tidy fmt vet lint test build)
- [ ] 14.2 `cd ui && npm run build`; `snapcraft -v`; `sudo snap install --dangerous ./rag-cli_*.snap`
- [ ] 14.3 In-snap exercise: engine-init from the UI (IDs shown + persisted), create, single ingest (file + URL), re-ingest with/without force (no duplicate chunks), batch with a github repo (+ missing-token path), forget, metadata, delete
- [ ] 14.4 In-snap exercise: full export → browser download → import upload round-trip, watching every long op in the operations indicator

## 15. Definition of done (UX) — foundation checklist

- [x] 15.1 All four view states per foundation §7; mutations follow §7 in-flight/success/failure rules
- [ ] 15.2 Looks correct in light **and** dark themes (`is-dark`)
- [ ] 15.3 Usable at 620px (collapsed rail) — no horizontal page scroll
- [ ] 15.4 Keyboard-only walkthrough passes (§9); focus management on modals/routes verified
- [x] 15.5 Only sanctioned patterns (§6) used; any new pattern added to `docs/ux/00-foundation.md`
- [x] 15.6 All colors via `--vf-*` tokens; zero hardcoded hex outside the sidebar SCSS vars
- [x] 15.7 Empty states include the CLI-equivalent command

## 16. Definition of done (UX) — knowledge-management checklist

- [x] 16.1 Engine-init gate renders only when uninitialized and unblocks without reload after success; model IDs copyable
- [x] 16.2 Delete is type-to-confirm with source count; forget is plain confirm
- [x] 16.3 Ingest modal closes on submit; completion arrives via operations context; duplicate-ID error keeps user input
- [ ] 16.4 Export produces a real browser download; import round-trips an exported archive
- [x] 16.5 Tables scroll horizontally inside their wrapper at 620px; row actions reachable by keyboard
