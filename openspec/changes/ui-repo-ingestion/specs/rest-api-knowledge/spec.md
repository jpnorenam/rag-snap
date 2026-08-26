# rest-api-knowledge Delta

## ADDED Requirements

### Requirement: Preview repository files before ingestion

The API SHALL provide `POST /1.0/knowledge/repo-preview` that lists the files a `github` or
`gitea` source entry would ingest, without starting an ingestion. The request SHALL accept the
same descriptive fields as a repo batch item (`type`, `source`, `branch`, `path`, `extensions`)
and the response SHALL be synchronous, carrying the matched file paths (capped to a bounded
sample), the total match count, and a truncation flag when the upstream listing was incomplete.
The listing SHALL use the same source parsing and filtering as ingestion, and the same daemon
environment tokens (`GITHUB_TOKEN` / `GITEA_TOKEN`); a missing token SHALL fail the request with
an error naming the required env var. The endpoint SHALL NOT touch OpenSearch and SHALL NOT
require an existing knowledge base.

#### Scenario: Previewing a GitHub repository

- **WHEN** a client posts a `github` source with a branch, path prefix, and extensions
- **THEN** the API returns the matching file paths (sampled), the total count, and no operation is created

#### Scenario: Preview honours the same filters as ingestion

- **WHEN** a preview and a subsequent ingest use the same source, branch, path, and extensions
- **THEN** both resolve the same file set (barring upstream repository changes in between)

#### Scenario: Missing token

- **WHEN** a preview is requested for a `github` or `gitea` source and the corresponding token env var is not set on the daemon
- **THEN** the request fails with an error naming the required env var (`GITHUB_TOKEN` or `GITEA_TOKEN`)

#### Scenario: Truncated upstream listing

- **WHEN** the upstream repository tree listing is truncated
- **THEN** the response marks the preview as truncated so the client can warn the user

#### Scenario: Invalid source

- **WHEN** the source reference cannot be parsed or the type is not `github` or `gitea`
- **THEN** the request fails with a validation error describing the expected format
