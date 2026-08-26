# Tasks: Repository ingestion in the web UI

## 1. Daemon: repo-preview endpoint

- [x] 1.1 Add `internal/api/handlers_repo_preview.go`: `POST /1.0/knowledge/repo-preview` accepting `{type, source, branch, path, extensions}`, dispatching to `processing.ListGitHubRepoFiles` / `ListGiteaRepoFiles` with the daemon's `GITHUB_TOKEN`/`GITEA_TOKEN`; return sync `{files (capped ~200), total, truncated}`; 400 on unparsable source, unknown type, or missing token (exact env-var hint)
- [x] 1.2 Surface the truncation flag: extend the processing list helpers to return truncated status instead of printing a warning (keep CLI behavior by printing at the CLI call sites)
- [x] 1.3 Register the route in `internal/api/server.go` following the existing knowledge routes
- [x] 1.4 Add `internal/api/handlers_repo_preview_test.go` covering: happy path against a stubbed forge API, path/extension filtering parity with ingest, missing token error text, invalid source, truncation flag
- [x] 1.5 Update `rest-api.yaml` swagger with the new endpoint

## 2. UI: API client

- [x] 2.1 Add `previewRepo(item)` to `ui/lib/api/knowledge.ts` via `postSync`, with typed `RepoPreview {files, total, truncated}` response, normalizing `null` files to `[]`

## 3. UI: single-repo ingestion in IngestModal

- [x] 3.1 Extend `IngestModal.tsx` mode radio group with "GitHub repo" and "Gitea repo"; repo modes show source, branch (optional), path prefix (optional), extensions, and reuse label + force; submit as a one-item `ingestBatch`
- [x] 3.2 Extensions input: comma-separated text normalized on blur (prefix missing dot, lowercase, dedupe), rendered as chips showing what will be sent
- [x] 3.3 "Preview files" secondary button calling `previewRepo`: inline count + capped path list, spinner while in flight, truncation caution, advisory only (submit never gated on preview)
- [x] 3.4 Error handling: missing-token / parse errors from preview or submit render as form-level `p-form-validation` messages preserving input; keep the existing 409-duplicate flow for non-repo modes
- [x] 3.5 Add any new styles to `ui/app/globals.scss` under the existing `// --- kb ingest ---` feature group (BEM, `--vf-*` tokens only)

## 4. UI: manifest builder in BatchIngestModal

- [x] 4.1 Add `serializeBatchManifest(jobs)` to `ui/lib/batchManifest.ts` (inverse of `parseBatchManifest`; CLI job type names `github-repo`/`gitea-repo`, `version: "1"`, inline extension lists) and export a builder-friendly job model
- [x] 4.2 Round-trip unit tests in `ui/lib/manifest.test.ts` style: parse(serialize(jobs)) === jobs, quoting/edge cases (colons, hashes, empty optionals)
- [x] 4.3 Rework `BatchIngestModal.tsx` into two modes ("Upload manifest" / "Build manifest") using the same radio-tab pattern as IngestModal; upload mode keeps current behavior plus an "Edit in builder" action mapping parsed jobs (flagging local `file` jobs) into builder state
- [x] 4.4 Builder mode: job list with add/remove, type select (url / github-repo / gitea-repo), per-job fields (name, source, branch, path, extensions, label), inline validation (required source, label pattern, ≥1 runnable job); `file` jobs preserved-but-excluded from run with explanation
- [x] 4.5 Per-repo-job "Preview" affordance showing matched file count (+ truncation) via `previewRepo`
- [x] 4.6 "Download YAML" footer action producing the serialized manifest as a client-side Blob download; "Start batch" submits runnable jobs via existing `ingestBatch`
- [x] 4.7 Styles for the builder in `globals.scss` under the `// --- kb batch ---` group

## 5. Verification & docs

- [x] 5.1 Verify a downloaded builder manifest is accepted unchanged by `rag-cli.rag k ingest <kb> --batch <file>`
- [ ] 5.2 UI-conventions compliance pass on both modals: light + dark themes, keyboard-only walkthrough, 620px width without horizontal page scroll, all colors via `--vf-*` tokens, four view states where applicable
- [x] 5.3 Update `docs/usage.md` UI section for repo ingestion and the manifest builder (no CLI/completion changes — CLI surface untouched)
- [ ] 5.4 Run `make all` and the UI tests; build and install the snap, then exercise end-to-end against a real GitHub repo (preview with/without `GITHUB_TOKEN`, single-repo ingest, builder batch run)
