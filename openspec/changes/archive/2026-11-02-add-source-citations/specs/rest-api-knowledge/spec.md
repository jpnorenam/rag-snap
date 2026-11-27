# Spec Delta

## ADDED Requirements

### Requirement: Search hits include their citation
Each hit returned by `POST /1.0/search` SHALL include its resolved citation — citation key, label,
link (absent when none), and locator — in addition to the fields it already carries.

#### Scenario: Hit with a recorded link
- **WHEN** a search returns a chunk whose source was ingested from `https://ubuntu.com/pro`
- **THEN** the hit's citation carries a label and the link `https://ubuntu.com/pro`

#### Scenario: Hit without a link
- **WHEN** a search returns a chunk whose source has no recorded URL
- **THEN** the hit's citation carries a label and no link

### Requirement: Ingest accepts an optional source URI
`POST /1.0/knowledge/<name>/sources` SHALL accept an optional source URI for file uploads, recorded
as the source's `source_uri`. URL ingests SHALL record their URL as `source_uri`, and `github` and
`gitea` batch entries SHALL record each file's full web URL. Source metadata returned by the sources
endpoints SHALL include `source_uri` when recorded.

#### Scenario: File upload with a source URI
- **WHEN** a client uploads a file supplying source URI `https://example.com/spec.pdf`
- **THEN** the source's metadata includes `source_uri` `https://example.com/spec.pdf`

#### Scenario: Invalid source URI
- **WHEN** a client uploads a file supplying a source URI that is not an absolute http(s) URL
- **THEN** the API rejects the request with a client error before starting an operation

#### Scenario: Repo batch entry
- **WHEN** a `github` batch entry ingests `docs/install.md` from `canonical/k8s-snap`
- **THEN** that source's metadata includes a `source_uri` linking to the file on github.com at the fetched ref
