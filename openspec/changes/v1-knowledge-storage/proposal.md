# Proposal

## Why

Knowledge sources cannot be kept fresh today. Sources do not record where they came from,
source IDs collide across knowledge bases, and a forced import merges into the existing base
instead of replacing it, leaving removed sources behind and duplicating re-ingested chunks.
The upcoming features (Drive subscriptions, source refresh, health warnings) all depend on
fixing this storage layer first. The snap is still v0, so this is the moment to make a clean
v1 break instead of migrating data.

## What Changes

- **BREAKING:** every knowledge base name becomes an OpenSearch alias that points at a
  *generation* index. Bases from v0 are detected as legacy: they are listed so they can be
  deleted, but cannot be searched, ingested into or exported. Users re-create and re-ingest.
- A knowledge base can be rebuilt into a new generation and switched over atomically. Searches
  keep using the old generation until the new one is complete; a failed rebuild leaves the old
  generation untouched.
- **BREAKING:** `knowledge import` (CLI, REST, Google Drive import) with force now **replaces**
  the target base via a new generation instead of loading on top of it.
- A knowledge base can be marked read-only. Ingest, forget, re-ingest, label changes and delete
  are then refused from the CLI, the REST API and the UI, and OpenSearch blocks writes to its
  generation. Nothing marks a base read-only yet; Drive subscriptions (a later change) will.
- **BREAKING:** source IDs are unique **per knowledge base**, not across the cluster. The same ID
  can exist in two bases; duplicate detection, `metadata`, `forget` and re-ingest are scoped to
  the base.
- Source records gain structured origin (kind, location, remote version marker), freshness
  timestamps, verification fields, a review interval and an empty description field. Repository
  ingests record a source group (repository, branch, path filter, extensions) shared by their
  files. No behavior uses these fields yet beyond recording and returning them.
- **BREAKING:** export archives move to manifest version 2, which records the embedding model
  name and version. Import refuses archives whose model does not match the engine's, and refuses
  version 1 archives with a message to re-export from v1.

## Capabilities

### New Capabilities
- `knowledge-storage`: how knowledge bases are stored — alias-to-generation layout, atomic
  generation replacement, read-only bases, per-base source identity, and legacy (v0) detection.
- `knowledge-source-metadata`: the fields recorded for each ingested source (origin, freshness,
  verification, review interval, description) and repository source groups.
- `knowledge-archive-format`: the export archive manifest (version 2, embedding model) and the
  compatibility checks import applies.

### Modified Capabilities
- `rest-api-knowledge`: listing reports legacy bases; source endpoints and ingest duplicate
  checks are scoped per base; writes to read-only bases are refused; import with force replaces
  the base atomically; source metadata responses include the new fields.

## Impact

- **Services:** OpenSearch only (index layout, aliases, index template, source metadata index,
  index blocks). The inference server and Tika are unaffected.
- **Config:** no new config keys.
- **Code:** `cmd/cli/basic/knowledge/` (indexes, sources, ingest, batch, import, export, labels,
  search), `cmd/cli/basic/knowledge.go`, chat and answer code that maps index names to base names
  (`chat/search.go`, `chat/turn.go`, `chat/history.go`, `analysis/catalogue.go`, `rfp.go`),
  `internal/api` knowledge, ingest, search and chats handlers.
- **User-facing surfaces:** `knowledge list` (legacy marker), `knowledge ingest` / `metadata` /
  `forget` (per-base IDs), `knowledge import` (replace semantics, new refusals), `knowledge
  export` (manifest v2), REST source metadata and knowledge list responses, and the web UI
  knowledge list (legacy bases). Documentation to update: `docs/usage.md`, `docs/rest-api.md`,
  `rest-api.yaml`, `docs/local-ui.md`, `INSTALL.md` (v1 upgrade note), and command `--help`
  text.
- **Upgrade:** no data migration. After upgrading, users delete legacy bases and re-ingest, and
  maintainers re-export archives.
