# Proposal: add-ui-knowledge-management

## Why

The `/knowledge/` section is the largest UI parity gap: the sidebar entry is a
disabled "Soon" placeholder and there is no browser path to any of
`k init / create / list / delete / ingest / forget / metadata / export / import`.
Change 1 (`add-ui-app-shell`) shipped the shell, the operations UX, and the shared
primitives (`EmptyState`, `Spinner`, `ConfirmModal`) this change consumes, so the UI
foundation is ready.

The `docs/ux/PLAN.md` sized this as "MODIFIED `local-ui-app`; **likely** MODIFIED
`rest-api-knowledge`", with the UI as the bulk of the work. Investigating the
existing `rest-api-knowledge` surface against `docs/ux/02-knowledge-management.md`
shows the API modifications are **not optional** — the UX the doc describes cannot be
built on today's endpoints, and two of the gaps are latent **correctness bugs** that
also affect CLI users:

1. **Export/import has no browser round-trip.** `POST .../export` writes to a
   daemon-side directory (`output_dir`) and `POST .../import` reads a daemon-side
   directory (`input_dir`) — both server paths the browser cannot reach under strict
   confinement. The doc's "Download archive" button and upload dropzone need a
   download endpoint and a multipart upload the API does not have.
2. **"Force re-ingest" does not exist, and re-ingest corrupts the index.** The ingest
   request has no `force` field, and the daemon's ingest path
   (`ingestOne`, `internal/api/handlers_ingest.go`) omits the "skip if already
   completed" guard the CLI's canonical `ingestAndIndex`
   (`cmd/cli/basic/knowledge/batch.go`) has — a daemon/CLI parity divergence.
   Deeper: **neither** path deletes prior chunks before re-indexing, so `BulkIndex`
   *appends*. Re-ingesting a `source_id` leaves orphaned duplicate chunks while the
   metadata's `ChunkCount` reflects only the newest batch. The doc's checkbox copy
   ("Replace an existing source with the same ID") describes behavior that exists
   nowhere yet. This affects CLI `k ingest --force` today, independent of the UI.
3. **Batch ingest cannot handle github/gitea.** The daemon batch item type is
   `{source_id, url}` only; the CLI supports `github`/`gitea` repo jobs with
   `GITHUB_TOKEN`/`GITEA_TOKEN` (`batch.go`). The doc's batch preview shows
   file/url/github/gitea type chips — three of four are unsupported over the API.
4. **Engine-init returns blank model IDs over the daemon.** `Init`
   (`cmd/cli/basic/knowledge/client.go`) does **not** persist the resolved model IDs
   to config despite its doc-comment saying so — it `fmt.Printf`s a
   `sudo rag set --package …` instruction to stdout. Over the daemon that print goes
   to the daemon log, config stays empty, and `handleEngineInit` reads the IDs back
   *from that empty config* — so the UI success notification shows blank IDs
   (breaking story 2.5's DoD), and because chat-rerank/search read model IDs from
   config, the engine may not function until a human runs `rag set` by hand.
5. **The KB list has no source count.** `handleKnowledgeList` returns chunk-level
   `docs_count`/`store_size`, not a source count — but the list's "Sources" column and
   the delete confirm ("all N ingested sources") both need it.

## What Changes

### REST API (`rest-api-knowledge`)

- **Export download**: after the export operation stages the archive daemon-side,
  expose a way for the browser to fetch the resulting `.tar.gz` (download endpoint or
  operation-scoped download token). Export defaults to a single compressed archive.
- **Import upload**: accept a `multipart/form-data` archive upload, stage it, unpack to
  a temp dir, then run the existing importer against that dir. Keep the existing
  `input_dir` JSON form for CLI/daemon-local callers.
- **Ingest unification + force**: collapse `ingestOne` and `ingestAndIndex` into one
  shared helper; add a `force` field to the ingest request. Semantics: skip if the
  source is already `completed` and `!force`; when `force`, **delete existing chunks**
  (`DeleteChunksBySourceID`, already used by forget) before re-indexing so force truly
  *replaces*. Fixes the append-not-replace bug for CLI and daemon alike. Duplicate
  `source_id` without `force` returns a distinguishable error the UI maps to a
  field-level validation message.
- **Batch fetchers**: extend the batch item type and the daemon batch path with
  github/gitea repo jobs, reusing the CLI fetchers and honoring
  `GITHUB_TOKEN`/`GITEA_TOKEN`; a fetcher lacking its token fails that entry with the
  exact env-var hint. (batch file entries referencing local paths remain
  daemon-boundary-limited — see design.)
- **Engine-init model IDs**: the init operation surfaces the freshly-resolved
  embedding/rerank model IDs directly (from the client, not a config re-read) and
  persists them to `package`-scoped config so the engine actually works post-init over
  the daemon.
- **List enrichment**: add `source_count` to the KB list response (single aggregation),
  killing the per-row N+1 the UI would otherwise need.

### UI (`local-ui-app`)

- `/knowledge/` list + `/knowledge/?kb=<name>` detail (query-param routing per the doc),
  under the standard shell.
- Engine-not-initialized caution gate with an **Initialize engine** action showing
  copyable model IDs on success.
- KB list (table, create modal, type-to-confirm delete with source count, import),
  KB detail (sources table, metadata modal, plain-confirm forget), single + batch
  ingest modals, export download and import upload — all long-running work tracked
  through the Change-1 operations context.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `rest-api-knowledge`: export gains a browser-reachable download; import gains a
  multipart upload path; ingest gains `force` with replace (delete-then-index)
  semantics and a unified code path; batch gains github/gitea fetchers with token
  handling; engine-init surfaces and persists model IDs; the list response gains
  `source_count`.
- `local-ui-app`: adds the `/knowledge/` list and detail screens and all their flows
  (init gate, create, delete, ingest single/batch, forget, metadata, export/import).

## Impact

- **Code**: `internal/api/` (`handlers_ingest.go`, `handlers_engine.go`,
  `handlers_knowledge.go`, route wiring in `server.go`), the shared ingest helper and
  batch/fetcher glue in `cmd/cli/basic/knowledge/`, and `ui/` (new
  `ui/app/knowledge/page.tsx`, `ui/lib/api/knowledge.ts` extensions, new components,
  styles in `ui/app/globals.scss`).
- **APIs consumed by UI**: existing `GET/POST /1.0/knowledge`,
  `DELETE /1.0/knowledge/{name}`, `GET .../sources[/{id}]`,
  `DELETE .../sources/{id}`, `POST .../sources`, `POST /1.0/knowledge-engine`, plus the
  modified export/import/batch surface above.
- **External services**: **OpenSearch** yes (indexes, chunks, metadata, models);
  **Tika** yes (ingest extraction path); **inference** no.
- **Config**: no new keys. Engine-init *writes* the existing
  `knowledge.model.embedding` / `knowledge.model.rerank` keys at `package` scope
  (the daemon runs as root and can `snapctl set --package`); the CLI already owns those
  keys.
- **Secrets/env**: batch github/gitea fetchers read `GITHUB_TOKEN` / `GITEA_TOKEN`
  from the daemon environment (consistent with the existing secrets-via-env rule).
- **User-facing surface**: new browser screens; CLI commands/flags unchanged (the
  ingest force fix corrects existing `--force` behavior without changing its
  signature). Docs to update: `docs/local-ui.md`, `docs/rest-api.md`, `rest-api.yaml`,
  and `docs/usage.md` if the corrected ingest semantics are documented.
- **UX guidelines**: governed by `docs/ux/00-foundation.md` and
  `docs/ux/02-knowledge-management.md`; the design links them and tasks carry their
  Definition of done checklist.
- **Dependencies**: none added.
