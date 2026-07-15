# Design: add-ui-knowledge-management

UX authority: [`docs/ux/00-foundation.md`](../../../docs/ux/00-foundation.md) (read first) and
[`docs/ux/02-knowledge-management.md`](../../../docs/ux/02-knowledge-management.md) (this change's
doc). Where this design and those docs overlap, the UX docs win; UI conventions are also codified
in the `ui-conventions` skill.

## Context

`rest-api-knowledge` already covers list/create/get/delete, sources list/get/delete, ingest,
engine-init, and export/import (`internal/api/handlers_knowledge.go`, `handlers_ingest.go`,
`handlers_engine.go`; routes in `server.go`). The UI consumes almost none of it — only the KB
list feeds the chat KB-selector chips today. Change 1 shipped the shell, the operations context
(`useOperations`), and the shared primitives (`EmptyState`, `Spinner`, `ConfirmModal`).

Reading `docs/ux/02-knowledge-management.md` against the live endpoints surfaced five gaps, four of
which require server work and two of which are latent correctness bugs affecting the CLI too. This
design records how each is closed. Constraints that shape everything:

- **Strict confinement**: `ragd` cannot read arbitrary user filesystem paths and the browser cannot
  read daemon-side paths. Any file crossing that boundary must go over HTTP (multipart up,
  streamed download).
- **snapctl-only config**: model IDs live in `package`-scoped config keys
  (`knowledge.model.embedding`, `knowledge.model.rerank`); the daemon runs as root and can
  `snapctl set --package`.
- **Static export UI**: `ui/app/knowledge/page.tsx`, client-only, no dynamic path segments — detail
  is `?kb=<name>` read via `useSearchParams()`.
- **Global source metadata**: source metadata lives in one shared index keyed by `source_id`
  (`getSourceMetadata` → `/{sourcesIndexName}/_doc/{sourceID}`, `sources.go`), **not** scoped by KB.

## Goals / Non-Goals

**Goals:**

- Full `/knowledge/` list + detail UI at parity with the `k` subcommands the PLAN lists.
- Make export/import work end-to-end in the browser.
- One correct ingest path shared by CLI and daemon, with `force` that truly replaces.
- Batch ingest that handles github/gitea with token hints.
- Engine-init that returns and persists model IDs so the engine works post-init.

**Non-Goals:**

- Google Drive import (Change 7) — the import modal only carries the muted CLI-hint line.
- Re-scoping source metadata per-KB (see D7 / Open Questions) — out of scope; the UI works within
  the global-`source_id` model and duplicate detection is global.
- Any inference-server work; ingest touches OpenSearch + Tika only.

## Decisions

### D1: Export stages daemon-side, then streams to the browser

Keep the async export operation (elasticdump into `$SNAP_COMMON`), defaulting to a single
compressed `.tar.gz`. Add a browser-reachable fetch for the finished artifact — preferred shape: the
export operation records the archive path/id in its metadata, and a new
`GET /1.0/knowledge/{name}/export/{opId}/archive` streams the file with
`Content-Disposition: attachment`. The UI's "Download archive" affordance (operations-panel row +
page notification) hits that URL. *Why not stream the archive as the POST response:* export is a
tracked long-running operation per the UX doc — the browser must be free to close the modal and
watch progress, so download is a second, on-demand step keyed off completion.

### D2: Import accepts a multipart upload, stages, unpacks, imports

Add a `multipart/form-data` branch to `POST /1.0/knowledge/import` (mirroring how
`collectUploadedItems` stages ingest uploads): stream the uploaded `.tar.gz` to a temp file, unpack
to a temp dir, run `ImportKnowledgeBase` against it, clean up. Keep the existing `input_dir` JSON
body for CLI/daemon-local callers. Optional target-name and `force` (overwrite existing KB) ride the
multipart form. Reconcile the archive contract with D1: export emits a compressed archive; import
accepts that same archive and unpacks before handing the importer a directory (the importer reads a
directory of elasticdump files today).

### D3: One ingest helper; `force` deletes-then-indexes

Collapse the daemon's `ingestOne` (`handlers_ingest.go`) and the CLI's `ingestAndIndex`
(`batch.go`) into a single shared function in `cmd/cli/basic/knowledge/`. Semantics:

```
ingest(sourceID, force):
  existing = GetSourceMetadata(sourceID)          // global lookup
  if existing.completed && !force:  skip           // parity with CLI today
  if existing exists && force:      DeleteChunksBySourceID(index, sourceID)   // the missing "replace"
  Ingest(tika) → IndexSourceMetadata → BulkIndex → mark completed
```

The `DeleteChunksBySourceID` call is exactly what `handleSourceDelete` (forget) already runs, so the
building block exists — it was simply never wired into the force path. This fixes the
append-not-replace corruption for **both** entry points. Add `force` to `ingestRequest`; a duplicate
`source_id` without `force` returns a distinguishable error (dedicated code / sentinel) the UI maps
to the field-level message "Source `<id>` already exists. Enable force re-ingest to replace it."
*Why unify rather than patch the daemon copy:* the divergence exists **because** the logic was
duplicated; leaving two copies invites the next drift.

### D4: Batch gains github/gitea fetchers over the API

Extend the batch item type beyond `{source_id, url}` to carry a source `type`
(file/url/github/gitea) and repo coordinates, and route github/gitea entries through the existing
CLI fetchers (`processGitHubRepoJob`/`processGiteaRepoJob`, `batch.go`) inside the ingest operation.
Tokens come from the daemon env (`GITHUB_TOKEN`/`GITEA_TOKEN`); a missing token fails **only that
entry** with the exact env-var hint, leaving the rest of the batch to proceed. The UI parses the
YAML client-side, previews entries with type chips, then submits — each entry joins the tracked
operation. **Batch file entries** referencing local paths cannot cross the confinement boundary
(the path is on the user's machine, not the daemon's); v1 treats manifest `file:` entries as
unsupported over the API with a clear message, steering users to per-file upload or url/github/gitea.

### D5: Engine-init surfaces and persists model IDs

`Init` resolves `embeddingModelID`/`rerankModelID` and currently only prints a `rag set` hint
(`client.go`). Change the daemon init path to (a) read the freshly-resolved IDs off the client
(add accessors or return them from the init call) rather than re-reading empty config, put them in
the operation metadata, **and** (b) persist them to `package`-scoped config
(`knowledge.model.embedding` / `knowledge.model.rerank`) so chat-rerank/search — which read model
IDs from config — actually work after a UI-driven init with no human `rag set`. The UI shows the
returned IDs in a copyable `p-code-snippet` on success. *Why persist server-side:* over the daemon
there is no operator watching stdout to copy the printed command; the init is only "done" if the
engine is usable afterward.

### D6: List gains `source_count`; detail already has it

Add `source_count` to `knowledgeBaseSummary` via a single aggregation over the metadata index
(grouping by `index_name`), so the list renders the "Sources" column and the delete confirm's
"all N ingested sources" without an N+1 fan-out. The per-KB `GET /1.0/knowledge/{name}` already
returns `source_count`, so KB detail is unchanged.

### D7: UI works within the global-`source_id` model

Because metadata is keyed by `source_id` globally (not per-KB), the ingest source-ID prefill (a
filename slug) can collide across KBs, and duplicate detection in D3 is inherently global. The UI
therefore: (a) surfaces the source ID's role in helper text ("stable identifier used by forget and
metadata"), (b) treats the duplicate-ID error as global, and (c) does not promise per-KB isolation
of source IDs. Re-scoping metadata per-KB is a larger data-model change deferred out of this change
(Open Questions).

### D8: Screens, routing, and operations wiring (per the UX doc)

`/knowledge/` renders list or detail off `?kb=` (`useSearchParams()`); detail shows a breadcrumb
back link. All mutations follow foundation §7 (four states, in-flight/success/failure). Long-running
work (ingest, batch, export, import, engine-init) is `postAsync` + `track()` through the Change-1
operations context — no hand-rolled polling. Deletion is type-to-confirm (`ConfirmModal` type
variant) with the source count; forget is plain confirm. New API verbs reuse the envelope pattern
(`getSync`/`postSync`/`deleteSync`, adding `putSync`/multipart helpers only as needed).

## Risks / Trade-offs

- **[Ingest correctness fix changes existing CLI behavior]** Making `force` delete-then-index
  changes what `k ingest --force` does today (append → replace). This is the intended fix, but it
  is a behavior change for CLI users. → Call it out in the proposal's Why and `docs/usage.md`;
  verify re-ingest leaves no orphaned chunks in the installed snap.
- **[Export/import contract drift]** Compressed-archive-out vs directory-in must round-trip
  exactly. → Test the full loop (export → download → import upload) against a real KB in the snap,
  not just unit-level.
- **[Batch file entries]** Silently succeeding on unsupported `file:` manifest entries would
  mislead. → Fail them explicitly with guidance; only url/github/gitea are batch-supported over the
  API in v1.
- **[Model-ID persistence at package scope]** Writing `package`-scoped keys from the daemon must
  match the CLI/install-hook expectations for those keys. → Confirm the daemon's snapctl write path
  and that a subsequent `config get` shows them layered correctly.
- **[Large uploads/downloads]** Multipart import and archive download can be large; respect the
  existing `MaxIngestFileSize`-style limits and stream rather than buffer.
- **[Global source_id collisions]** Two KBs sharing a `source_id` share one metadata doc and a
  forget in one can strand the other's chunks. → Documented as a known constraint (D7); not
  introduced by this change.

## Migration Plan

1. Land the `rest-api-knowledge` deltas first (ingest unify+force, engine-init IDs, list
   `source_count`, export download, import upload, batch fetchers) with tests, then the UI on top.
2. `make all`; `cd ui && npm run build`; `snapcraft -v`; `sudo snap install --dangerous`.
3. Exercise in the installed snap: engine-init from the UI (model IDs shown + persisted), create,
   single ingest (file + URL), re-ingest with/without force (no duplicate chunks), batch with a
   github repo (+ token-missing path), forget, metadata, delete, and the full export→download→
   import-upload round-trip — watching every long op in the operations indicator.
4. Rollback is `git revert`; no snap-packaging/hook changes. Config writes are additive to existing
   keys.

## Open Questions

- **Download contract**: dedicated `GET .../export/{opId}/archive` endpoint vs. a download token in
  the operation metadata — pick in the propose phase; both satisfy the browser-download DoD.
- **Per-KB source metadata**: should source IDs be scoped per-KB (schema change) rather than global?
  Deferred; flagged so it is a conscious non-goal, not an oversight.
- **Batch `file:` entries**: confirm "unsupported over API, upload instead" is acceptable UX vs.
  investing in a staged-upload manifest form.
