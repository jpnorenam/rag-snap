# 02 — UX guidelines: `add-ui-knowledge-management`

The largest surface: `/knowledge/` list + per-KB detail. Parity with `k init/create/list/delete/ingest/forget/metadata/export/import`. Read `00-foundation.md` and `01-app-shell.md` first.

## Page structure

Two levels, both under the standard shell:

1. **`/knowledge/`** — KB list (the section landing page)
2. **`/knowledge/?kb=<name>`** — KB detail. Static export makes dynamic path segments awkward; use a query param read via `useSearchParams()` on the same page, rendering list or detail. Detail shows a breadcrumb-style back link ("← Knowledge bases", `p-button--base`).

### Engine-not-initialized gate
On mount, if the engine is uninitialized (surface this from the API), the list page shows a full-width `p-notification--caution`: "The knowledge engine is not initialized. Chat and ingestion need embedding models and pipelines." with an **Initialize engine** `p-button--positive`. Runs `POST /1.0/knowledge-engine` as a tracked operation; on success show a positive notification containing the embedding/rerank **model IDs** in a copyable `p-code-snippet` (parity: `k init` prints them). Keep the rest of the page usable behind the banner — don't block browsing.

## KB list

- Header row: page intro sentence (muted) + **Create knowledge base** (`p-button--positive`) + **Import** (`p-button`).
- Semantic `<table>`: Name · Sources (count, `u-align--right`) · row actions (`p-button--base`): **Open**, **Export**, **Delete**.
- Row click / Enter opens detail (make the name a real link; don't make the whole `<tr>` a click target).
- Empty state (`EmptyState`): "No knowledge bases yet." + create action + CLI hint `rag-cli.rag k create <name>`.

### Create
Inline `p-modal` with one `p-form--stacked` field (name). Validate the daemon's naming rules client-side where cheap; on error keep the modal open with `p-form-validation is-error` under the field. On success: close, refresh list, positive notification "Knowledge base `<name>` created."

### Delete
Type-to-confirm `ConfirmModal` (foundation §8). Body must state the source count: "Deletes the index and all N ingested sources. This cannot be undone." (fetch sources count before opening, or degrade to "all ingested sources").

## KB detail (sources)

- Title = KB name; subtitle line: source count + muted CLI hint.
- Actions row: **Ingest document** (`p-button--positive`), **Batch ingest** (`p-button`), **Export** (`p-button`).
- Sources `<table>`: Source ID · Title/filename · Type · Ingested (relative time, absolute in `title`) · actions: **Metadata**, **Forget**.
- **Metadata** opens a side-of-modal view (`p-modal`) rendering the stored metadata as a definition list; raw JSON available in a `p-code-snippet` block (copyable). Parity: `k metadata`.
- **Forget** = plain confirm modal ("Removes all chunks and the metadata record for `<source-id>` from `<kb>`."). Parity: `k forget`.
- Empty state: "No sources ingested yet." + ingest action + CLI hint `rag-cli.rag k ingest <kb> <id> --file <path>`.

## Ingest flows

### Single document (modal)
`p-form--stacked` inside a `p-modal`:
1. Source: two-tab choice (radio group or `p-tabs`) — **Upload file** | **From URL**.
   - Upload: an `.app-dropzone` (custom, tokens: dashed `--vf-color-border-default`, hover → positive border) wrapping a native `<input type="file">`; show chosen filename + size. Keyboard/AT path is the native input — the dropzone is enhancement only.
   - URL: text input with basic URL validation.
2. Source ID: text input, prefilled from filename slug; helper text explains it's the stable identifier used by forget/metadata.
3. **Force re-ingest** checkbox (unchecked default) with helper "Replace an existing source with the same ID." (parity: `--force`).
4. Submit → `postAsync`, `track()` the operation, close the modal immediately. The row appears when the operations context reports success and the list refreshes. Do not keep the user staring at a blocked modal.

Duplicate-ID error without `--force`: keep the modal open, field-level validation message "Source `<id>` already exists. Enable force re-ingest to replace it."

### Batch ingest (modal)
Upload the same YAML manifest `k ingest --batch` accepts. Dropzone for the `.yaml` + a muted link/summary of the expected schema (point to `docs/usage.md`). After parse (client-side), show a preview list of entries (id · type chip: file/url/github/gitea) before **Start batch**; each entry becomes/joins a tracked operation. GitHub/Gitea entries needing tokens: if the daemon reports missing credentials, fail that entry with the exact env-var hint (`GITHUB_TOKEN`/`GITEA_TOKEN`).

## Export / import

- **Export** (list row or detail action): confirm-free; starts a tracked operation. On success the operations panel row and a positive notification on the page offer **Download archive** (browser download of the `.tar.gz`). Name shown verbatim.
- **Import** (list header): modal with dropzone for a directory archive (`.tar.gz`) + optional target name field + **Force** checkbox (overwrite existing KB — if set, require type-to-confirm semantics by warning inline). Tracked operation; list refreshes on success.
- Google Drive import is **out of scope here** — the import modal includes a muted line "Importing from Google Drive? Use `rag-cli.rag k import --url <drive-url>` (UI support planned)." Remove that line in Change 7.

## States
All foundation §7 states on both list and detail. Additionally: while any ingest operation for the open KB is running, show a slim inline hint above the sources table ("1 ingest in progress…", `aria-live="polite"`) so users don't need the ops panel to know why the list is about to change.

## Definition of done (UX)
Foundation checklist, plus:
- [ ] Engine-init gate renders only when uninitialized and unblocks without reload after success; model IDs copyable
- [ ] Delete is type-to-confirm with source count; forget is plain confirm
- [ ] Ingest modal closes on submit; completion arrives via operations context; duplicate-ID error keeps user input
- [ ] Export produces a real browser download; import round-trips an exported archive
- [ ] Tables scroll horizontally inside their wrapper at 620px; row actions reachable by keyboard
