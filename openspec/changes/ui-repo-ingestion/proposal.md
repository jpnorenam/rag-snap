# Proposal: Repository ingestion in the web UI

## Why

Repository ingestion (GitHub/Gitea) is a first-class CLI feature — `k ingest --batch` with a YAML
manifest — and the daemon's ingest endpoint already accepts `github`/`gitea` batch items. But the
web UI only exposes file-upload and URL ingestion in its single-source modal, and its batch modal
requires the user to hand-write a YAML manifest elsewhere and upload it. Users who live in the UI
cannot ingest a repository without dropping to the CLI or a text editor, and they cannot see which
files a repo job would match before committing to a potentially long ingestion.

## What Changes

- **Single-repo ingestion from the UI**: the ingest modal gains "GitHub repo" and "Gitea repo"
  source modes alongside upload/URL, with fields for repository (owner/repo or URL), branch
  (optional, defaults to the repo's default branch), path prefix, file extensions, and label.
  Submits through the existing batch ingest API as a one-item batch — no new ingest path.
- **Pre-ingest file preview**: a new REST endpoint lists the files a repo job would match
  (reusing the daemon's existing `ListGitHubRepoFiles`/`ListGiteaRepoFiles`), so the UI can show
  a file count and sample paths before the user starts ingestion. This touches the inference
  server and OpenSearch not at all — it is a read-only listing against the GitHub/Gitea APIs.
- **Visual YAML builder for batch ingestion**: the batch modal grows a "build" mode next to the
  existing "upload manifest" mode: a form-based editor to add/remove jobs (url, github-repo,
  gitea-repo), edit per-job fields, validate as you type, preview matched files per repo job, and
  either run the batch directly or download the manifest as a CLI-compatible YAML file
  (`version` + `jobs`, same schema as `k ingest --batch`).
- **Round-trip**: an uploaded manifest can be loaded into the builder for editing before running
  or re-downloading.

Explicitly *not* changing:

- Authentication stays as-is: repo tokens are read from the daemon's `GITHUB_TOKEN` /
  `GITEA_TOKEN` environment variables (project rule: secrets via env vars, never config). The UI
  surfaces the exact env-var hint when a token is missing; it never collects or stores tokens.
- Incremental re-runs already work (completed sources are skipped unless force is set) — the UI
  keeps exposing the existing force toggle; no new state is introduced.
- No new config keys (package or user scoped).
- The CLI is untouched; the manifest schema is unchanged, so builder output is CLI-compatible by
  construction.

Services touched: none of the three backends changes behavior. OpenSearch and Tika are exercised
only through the existing ingest operation; the inference server is not involved. The only new
server surface is the read-only repo-preview endpoint in the daemon (`internal/api`).

## Capabilities

### New Capabilities

(none — all changes extend existing capabilities)

### Modified Capabilities

- `rest-api-knowledge`: ADDED requirement — a repo file-preview endpoint that lists the files a
  `github`/`gitea` source would ingest (count + paths), using the daemon's env-var tokens, without
  starting an ingestion.
- `local-ui-app`: MODIFIED "Ingest a document from the UI" — the single-source modal adds GitHub
  and Gitea repo modes with a pre-ingest file preview. MODIFIED "Batch ingest from the UI" — the
  batch modal adds a visual manifest builder with validation, per-repo-job preview, YAML
  export/download, and load-into-builder for uploaded manifests.

## Impact

- `internal/api/`: new preview handler (route + handler + tests), reusing
  `cmd/cli/basic/processing` repo listing; `rest-api.yaml` swagger updated.
- `ui/components/knowledge/IngestModal.tsx`: two new source modes + preview call.
- `ui/components/knowledge/BatchIngestModal.tsx` + `ui/lib/batchManifest.ts`: builder mode,
  YAML serialization (export), manifest → builder loading.
- `ui/lib/api/knowledge.ts`: preview client function.
- User-facing surface changes: UI-only (modal affordances). Documentation to update:
  `docs/usage.md` (UI ingest section, if present) and the swagger spec `rest-api.yaml`. No CLI
  command, flag, or slash-command changes.
- Dependencies: none added. The UI keeps its zero-YAML-dependency stance (the existing
  purpose-built parser gains a matching serializer).
