# Design: Repository ingestion in the web UI

## Context

The ingest stack is already layered for this feature:

- `cmd/cli/basic/processing/github.go` / `gitea.go` expose `ParseGitHubSource`,
  `ListGitHubRepoFiles`, `ParseGiteaSource`, `ListGiteaRepoFiles`, and `FetchRepoFile` — pure
  listing/fetching helpers with no OpenSearch coupling.
- `internal/api/handlers_ingest.go` already accepts batch items of type `github`/`gitea` (with
  `source`, `branch`, `path`, `extensions`, `label`) on `POST /1.0/knowledge/{name}/sources`,
  expands them server-side, and requires `GITHUB_TOKEN`/`GITEA_TOKEN` from the daemon
  environment, failing an entry with the exact env-var hint when absent.
- `ui/components/knowledge/IngestModal.tsx` offers upload/URL modes only.
  `ui/components/knowledge/BatchIngestModal.tsx` uploads a YAML manifest, parses it with the
  purpose-built reader in `ui/lib/batchManifest.ts` (no YAML dependency), previews entries, and
  submits via `ingestBatch` in `ui/lib/api/knowledge.ts`.

So the change is UI composition plus one small read-only daemon endpoint. No snapcraft changes:
the daemon already makes outbound requests to GitHub/Gitea during repo ingestion, so no new
interfaces/plugs are needed; no new bundled binaries or hooks. No new config keys (snapctl layers
untouched) and no new secrets — tokens remain the existing `GITHUB_TOKEN`/`GITEA_TOKEN` daemon
environment variables.

## Goals / Non-Goals

**Goals:**

- Ingest a single GitHub or Gitea repository from the UI with branch/path/extensions/label.
- Show the user what a repo job matches (file count + paths) before ingesting.
- Build, validate, run, and export a CLI-compatible batch manifest visually.
- Keep the uploaded-manifest flow working and let it feed the builder (round-trip).

**Non-Goals:**

- Collecting or storing repo tokens in the UI, API requests, or config (env vars only).
- New retry/scheduling semantics — an operation's per-entry behavior is unchanged.
- Changing the manifest schema or any CLI behavior.
- A general YAML editor; the builder covers exactly the `version` + `jobs[]` schema.

## Decisions

### 1. Preview endpoint: `POST /1.0/knowledge/repo-preview`

A new handler in `internal/api` (e.g. `handlers_repo_preview.go`) accepting
`{type: "github"|"gitea", source, branch?, path?, extensions?}` and returning a **sync** response
`{files: [...paths], total, truncated}` (paths capped at ~200 for payload size, `total` always the
full count; `truncated` reflects the GitHub trees-API truncation flag).

- **Why POST, not GET**: the input is a structured object (extensions array); mirrors the ingest
  body shape so the UI submits the same object it previews. Listing is read-only and fast, so a
  sync envelope (not an operation) is right.
- **Why not scoped under `{name}`**: the listing doesn't touch OpenSearch or any base; the
  builder previews jobs before a target base is relevant. A base-independent path avoids a
  pointless existence check. (Kept under `/1.0/knowledge/` because it belongs to the knowledge
  ingest surface and the spec capability `rest-api-knowledge`.)
- **Token behavior**: identical to ingest — missing env var yields a 400 with the exact
  `GITHUB_TOKEN`/`GITEA_TOKEN` hint, so preview surfaces the problem *before* a failed operation.
- **Alternative considered**: client-side calls to the GitHub/Gitea APIs. Rejected: CORS,
  token exposure in the browser, and divergence from the daemon's parsing/filtering logic.

### 2. Single-repo mode lives in `IngestModal`

Extend the existing modal's radio group ("Upload file" / "From URL") with "GitHub repo" and
"Gitea repo". Repo modes swap the source-id field for repo fields: source (owner/repo or URL),
branch (optional, help text: repo default), path prefix (optional), extensions (comma-separated
text input, normalized to `[".md", ...]`), and reuse the existing label + force controls. A
"Preview files" secondary button calls the preview endpoint and renders count + a capped path
list inline; the primary button stays enabled without preview (preview is advisory, not a gate).
Submit sends a one-item `ingestBatch` (the API's single-URL/file shapes don't carry repo fields).

- **Alternative considered**: a separate `RepoIngestModal`. Rejected: the KB detail already has
  two ingest buttons; a third entry point fragments the flow, and the modal's mode-switch
  pattern already exists.

### 3. Builder mode inside `BatchIngestModal`, manifest logic in `ui/lib/batchManifest.ts`

The batch modal gains a two-mode header ("Upload manifest" / "Build manifest") following the
same radio-tab pattern as `IngestModal`. Build mode is a job-list editor: add job (type select:
url / github-repo / gitea-repo), per-job fields matching `BatchJob` (name, source, branch, path,
extensions, label — `target_kb` is intentionally omitted: the modal is opened on a base and the
API path fixes the target), remove job, inline validation (required source, label pattern,
non-empty jobs). Per repo job, a preview affordance shows matched file count via the preview
endpoint. Footer actions: "Download YAML" (client-side Blob download, `type` values are the CLI
names `github-repo`/`gitea-repo`) and the existing "Start batch".

`batchManifest.ts` gains `serializeBatchManifest(jobs): string` — the inverse of
`parseBatchManifest` — emitting the documented flat schema (`version: "1"`, `jobs:` with quoted
scalars and inline extension lists) so parse(serialize(x)) round-trips. Uploading a manifest
offers "Edit in builder", mapping parsed jobs into builder state (unsupported `file` entries are
carried into the builder but flagged, since the CLI supports them even though the API cannot run
them — they survive a download untouched but are excluded from a run).

- **Why no YAML library**: the UI's existing zero-dependency stance (`ui-conventions`: do not
  add UI dependencies); the schema is flat and already has a purpose-built parser to mirror.
- **Alternative considered**: a raw-YAML textarea with live validation. Rejected: hand-writing
  YAML in a textarea is the exact workflow the proposal is removing.

### 4. UI conventions applied

Vanilla-only markup per the `ui-conventions` skill: `p-form p-form--stacked`, validation via
`p-form-validation is-error` + `__message`, chips (`p-chip`) for job types in previews, tables
with `aria-label` in an `overflow-x: auto` wrapper, one `p-button--positive` per view, spinner
pattern for in-flight preview calls, all new styles in `globals.scss` under feature-prefixed BEM
(`.kb-batch__*`, `.kb-ingest__*` groups already exist), colors via `--vf-*` tokens only, both
themes verified. New API client code goes through `envelope.ts` (`postSync`) in
`ui/lib/api/knowledge.ts` — no direct fetch.

## Risks / Trade-offs

- [Preview and ingest can disagree if the repo changes between calls] → Preview is advisory
  only; ingestion re-lists at run time (existing behavior). The UI labels the preview as a
  point-in-time match.
- [Large repos: trees API truncation (>100k files) and huge preview payloads] → The daemon caps
  returned paths and passes through the `truncated` flag; the UI shows "and N more…" plus a
  truncation caution.
- [Hand-rolled YAML serializer drifts from the parser] → Serializer and parser live in the same
  module with round-trip unit tests (`manifest.test.ts` pattern already exists in `ui/lib`).
- [Missing daemon tokens discovered late] → Preview fails fast with the exact env-var hint
  before any operation is started; the modal renders it as a form-level error with guidance on
  setting the variable for the snap service.
- [Comma-separated extensions input is ambiguous (".md" vs "md")] → Normalize on blur: prefix a
  missing dot, lowercase, dedupe; show the normalized chips so the user sees what will be sent.

## Migration Plan

Pure addition: new endpoint + UI affordances; no data, config, or schema migrations. Rollback =
revert. Existing manifests and CLI flows are untouched. Deploy via the normal snap build; verify
with `make all`, `ui` tests, and an installed snap exercising a real GitHub repo preview + ingest.

## Open Questions

- None blocking. (Possible follow-up, out of scope: a `target_kb` column in the builder to emit
  multi-base manifests for CLI use; today the builder targets the open base like the upload flow.)
