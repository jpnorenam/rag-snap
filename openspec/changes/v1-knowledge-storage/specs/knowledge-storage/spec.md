## Purpose

Define how knowledge bases are stored in OpenSearch so they can be replaced safely, protected
from local edits when they are managed elsewhere, and identified independently of each other.

## ADDED Requirements

### Requirement: Knowledge bases are served through a stable name

Each knowledge base SHALL be reachable by a stable name that points at exactly one generation of
its content. Search, ingest, export, listing and chat SHALL address a base by that name and
SHALL attribute results to the base name, never to the generation.

#### Scenario: Search results name the base

- **WHEN** a search over base `commercial` returns hits
- **THEN** each hit reports `commercial` as its originating base

#### Scenario: Listing shows bases, not generations

- **WHEN** a base has a generation being built alongside its current one
- **THEN** listing shows the base once, and no entry for the generation being built

### Requirement: Atomic generation replacement

The system SHALL be able to rebuild a base into a new generation and switch the base's name to
it in one atomic step. Until the switch, all reads SHALL use the previous generation. After the
switch, the previous generation and its source records SHALL be removed.

#### Scenario: Reads use the old generation during a rebuild

- **WHEN** a rebuild of `commercial` is in progress
- **THEN** searches over `commercial` return results from the previous generation only

#### Scenario: Switch on success

- **WHEN** a rebuild completes successfully
- **THEN** searches over `commercial` return results from the new generation only
- **AND** the previous generation and its source records no longer exist

#### Scenario: Failure leaves the base unchanged

- **WHEN** a rebuild fails or is cancelled before the switch
- **THEN** the base still serves the previous generation with its source records unchanged
- **AND** the partially built generation is removed

### Requirement: Read-only knowledge bases

A knowledge base SHALL be markable as read-only. For a read-only base, ingest, re-ingest,
forget, label changes and delete SHALL be refused with an error stating the base is read-only,
whether requested from the CLI, the REST API or the web UI. Search, chat, answer and export
SHALL still work, as SHALL replacement of the base through a generation rebuild.

#### Scenario: Ingest into a read-only base from the CLI

- **WHEN** a user runs `knowledge ingest` against a read-only base
- **THEN** the command fails with an error stating the base is read-only
- **AND** the base's content is unchanged

#### Scenario: Reading a read-only base

- **WHEN** a user searches or chats with a read-only base selected
- **THEN** results from that base are returned as for any other base

#### Scenario: Replacing a read-only base

- **WHEN** a generation rebuild replaces a read-only base
- **THEN** the replacement succeeds and the new generation is also read-only

### Requirement: Storage-level write protection

A read-only base's generation SHALL reject document writes and mapping changes at the
OpenSearch level, so writes that bypass the application are also refused. Deleting the
generation SHALL remain possible.

#### Scenario: Direct write to a read-only generation

- **WHEN** a document write is sent directly to OpenSearch for a read-only base's generation
- **THEN** OpenSearch rejects the write

### Requirement: Source identity is scoped to a knowledge base

A source SHALL be identified by its knowledge base and source ID together. The same source ID
SHALL be usable in different bases without conflict. Duplicate detection, metadata lookup,
forget and forced re-ingest SHALL only consider sources in the targeted base.

#### Scenario: Same source ID in two bases

- **WHEN** source `pricing` is ingested into base `telco` and then into base `commercial`
- **THEN** both ingests succeed and each base holds its own `pricing` source

#### Scenario: Forget affects one base only

- **WHEN** a user forgets source `pricing` in base `telco`
- **THEN** source `pricing` in base `commercial` is unchanged

#### Scenario: Duplicate within a base

- **WHEN** source `pricing` is ingested into base `telco` a second time without force
- **THEN** it is skipped as already ingested

### Requirement: Legacy v0 knowledge bases

Knowledge bases created by v0 SHALL be detected as legacy. Legacy bases SHALL be listed with a
legacy marker and SHALL be deletable. Search, ingest, export and import into a legacy base SHALL
be refused with an error telling the user to delete and re-create the base. Creating a base
whose name matches a legacy base SHALL be refused until the legacy base is deleted.

#### Scenario: Listing after upgrade

- **WHEN** a user lists knowledge bases after upgrading from v0
- **THEN** each v0 base appears with a legacy marker

#### Scenario: Using a legacy base

- **WHEN** a user ingests into, searches or exports a legacy base
- **THEN** the request fails with an error telling the user to delete and re-create it

#### Scenario: Deleting a legacy base

- **WHEN** a user deletes a legacy base
- **THEN** its data and its v0 source records are removed
