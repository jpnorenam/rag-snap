## Purpose

Record, for every ingested source, where it came from and how fresh it is, so later features can
refresh sources, show their health and describe what a knowledge base contains.

## ADDED Requirements

### Requirement: Source origin

Every source record SHALL include its origin: a kind (`upload`, `url`, `github` or `gitea`), a
location (the uploaded file name, the URL, or the repository and in-repository path), and a
remote version marker when the origin provides one (the repository file's content hash).

#### Scenario: Uploaded file

- **WHEN** a user ingests a local file `sku-book.pdf`
- **THEN** the source's origin kind is `upload` and its location is `sku-book.pdf`

#### Scenario: Web page

- **WHEN** a user ingests from `https://example.com/pricing`
- **THEN** the source's origin kind is `url` and its location is that URL

#### Scenario: Repository file

- **WHEN** a GitHub repository ingest indexes `docs/install.md`
- **THEN** that source's origin kind is `github`, its location names the repository and
  `docs/install.md`, and its version marker is the file's content hash

### Requirement: Repository source groups

A repository ingest SHALL record a source group holding the repository, the branch it was read
from, the path filter and the extension list. Every file source from that ingest SHALL reference
its group. Groups SHALL be scoped to their knowledge base.

#### Scenario: Group recorded

- **WHEN** a user ingests repository `owner/repo` on branch `main` with path filter `docs/` and
  extensions `.md`
- **THEN** a source group records `owner/repo`, `main`, `docs/` and `.md`
- **AND** each ingested file's source record references that group

#### Scenario: Default branch is resolved

- **WHEN** a repository ingest does not name a branch
- **THEN** the group records the branch that was actually read, not an empty value

### Requirement: Freshness timestamps

Every source record SHALL include when it was ingested, when its origin was last checked and
when its content last changed. A forced re-ingest whose content checksum matches the previous
one SHALL update the last-checked time but not the last-changed time.

#### Scenario: First ingest

- **WHEN** a source is ingested for the first time
- **THEN** its ingested, last-checked and last-changed times equal the ingest time

#### Scenario: Unchanged re-ingest

- **WHEN** a source is force re-ingested and its content checksum is unchanged
- **THEN** its last-checked time is updated and its last-changed time is not

#### Scenario: Changed re-ingest

- **WHEN** a source is force re-ingested and its content checksum differs
- **THEN** both its last-checked and last-changed times are updated

### Requirement: Verification and review fields

Every source record SHALL include a last-verified time, a verified-by value and a review
interval. All three SHALL be empty after ingest; a forced re-ingest SHALL preserve them.

#### Scenario: Fields present but empty

- **WHEN** a user inspects a newly ingested source
- **THEN** its last-verified time, verified-by value and review interval are present and empty

### Requirement: Source description field

Every source record SHALL include a description and the description's provenance (`manual` or
`ai`). Both SHALL be empty after ingest; a forced re-ingest SHALL preserve them.

#### Scenario: Description preserved on re-ingest

- **WHEN** a source with a description is force re-ingested
- **THEN** its description and provenance are unchanged

### Requirement: Metadata is exposed

`knowledge metadata` and the REST source metadata endpoints SHALL return the origin, source group
reference, freshness, verification, review interval and description fields.

#### Scenario: Inspecting a source from the CLI

- **WHEN** a user runs `knowledge metadata <base> <source-id>`
- **THEN** the output includes the origin kind and location and the freshness times

### Requirement: Imported sources keep their recorded metadata

Importing an archive SHALL keep each source's recorded origin, group, freshness, verification
and description fields as they were exported, scoped to the base it is imported into.

#### Scenario: Freshness survives export and import

- **WHEN** a maintainer exports a base and a user imports the archive
- **THEN** each imported source shows the same last-changed time as on the maintainer's machine
