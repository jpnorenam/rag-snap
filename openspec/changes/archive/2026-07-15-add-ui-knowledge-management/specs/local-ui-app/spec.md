## ADDED Requirements

### Requirement: Knowledge bases list screen

The UI SHALL provide a `/knowledge/` screen that lists the user's knowledge bases in a semantic
table showing each base's name and source count, with per-row actions to open, export, and delete
the base. The screen SHALL implement all four standard view states (loading, empty, loaded, error)
per the UX foundation, and its empty state SHALL include the CLI-equivalent command. The base name
SHALL be a real link into the detail screen; the whole row SHALL NOT be a click target.

#### Scenario: Listing knowledge bases

- **WHEN** the user opens `/knowledge/` and bases exist
- **THEN** each base is listed with its name and source count and row actions

#### Scenario: No knowledge bases yet

- **WHEN** the user opens `/knowledge/` and no bases exist
- **THEN** an empty state is shown with a create action and the `rag-cli.rag k create <name>` hint

#### Scenario: Opening a base

- **WHEN** the user activates a base's name link
- **THEN** the detail screen for that base is shown

### Requirement: Knowledge engine initialization gate

When the knowledge engine is uninitialized, the `/knowledge/` screen SHALL surface a caution
notification with an action to initialize the engine, without blocking the rest of the page. The
initialization SHALL run as a tracked asynchronous operation; on success the UI SHALL show the
resulting embedding and rerank model identifiers in a copyable form.

#### Scenario: Engine uninitialized

- **WHEN** the engine is reported uninitialized on load
- **THEN** a caution notification with an "Initialize engine" action is shown
- **AND** the rest of the page remains usable

#### Scenario: Initializing the engine from the UI

- **WHEN** the user triggers "Initialize engine"
- **THEN** the work runs as a tracked operation
- **AND** on success the embedding and rerank model identifiers are shown in a copyable snippet

### Requirement: Create and delete a knowledge base from the UI

The UI SHALL let the user create a knowledge base via a modal with a validated name field, and
delete a base via a type-to-confirm modal whose body states the source count and that the action
cannot be undone. Deletion SHALL match CLI semantics (§8 of the UX foundation). Validation errors
on create SHALL keep the modal open with a field-level message and preserve the user's input.

#### Scenario: Creating a base

- **WHEN** the user submits a valid name in the create modal
- **THEN** the base is created, the list refreshes, and a success notification is shown

#### Scenario: Create validation error

- **WHEN** creation fails validation or conflicts with an existing name
- **THEN** the modal stays open with a field-level error and the entered name is preserved

#### Scenario: Deleting a base

- **WHEN** the user opens delete for a base
- **THEN** a type-to-confirm modal states the source count and requires typing the base name
- **AND** the destructive action stays disabled until the typed name matches exactly

### Requirement: Knowledge base detail with sources

The UI SHALL provide a `/knowledge/?kb=<name>` detail view (query-param routing, read via
`useSearchParams()`) rendered by the same page as the list, with a back link to the list. The
detail view SHALL list the base's ingested sources in a table (source id, title/filename, type,
ingested time as relative-with-absolute-title), with per-source actions to view metadata and to
forget the source. Forgetting SHALL use a plain confirm modal naming the source and base. The
metadata view SHALL render the stored metadata and expose the raw JSON in a copyable block.

#### Scenario: Viewing sources

- **WHEN** the user opens a base's detail view
- **THEN** its ingested sources are listed with id, title, type, and ingested time

#### Scenario: No sources ingested

- **WHEN** a base has no sources
- **THEN** an empty state is shown with an ingest action and the CLI ingest hint

#### Scenario: Inspecting source metadata

- **WHEN** the user opens a source's metadata
- **THEN** the stored metadata is shown with the raw JSON available in a copyable block

#### Scenario: Forgetting a source

- **WHEN** the user confirms forget for a source
- **THEN** the source's chunks and metadata are removed and the list refreshes

### Requirement: Ingest a document from the UI

The UI SHALL let the user ingest a single source into a base via a modal offering an upload-file
or from-URL choice, a source-identifier field prefilled from the filename, and a force-re-ingest
option. On submit the UI SHALL start a tracked operation and close the modal immediately, letting
the row appear when the operation reports success. A duplicate-identifier error without force SHALL
keep the modal open with a field-level message and preserve the user's input.

#### Scenario: Ingesting by upload

- **WHEN** the user chooses a file, sets or accepts a source id, and submits
- **THEN** a tracked ingest operation starts and the modal closes immediately

#### Scenario: Ingesting from a URL

- **WHEN** the user enters a valid URL and submits
- **THEN** a tracked ingest operation starts for that URL

#### Scenario: Duplicate source id without force

- **WHEN** ingestion is rejected because the source id already exists and force is off
- **THEN** the modal stays open with a message telling the user to enable force re-ingest, and input is preserved

#### Scenario: In-progress hint on the detail view

- **WHEN** an ingest operation for the open base is running
- **THEN** the sources table shows a live-updating in-progress hint above it

### Requirement: Batch ingest from the UI

The UI SHALL let the user batch-ingest by uploading the YAML manifest the CLI accepts, parse it
client-side, and preview the entries (with a type indicator per entry) before starting. Each entry
SHALL join a tracked operation. Entries requiring credentials the daemon lacks SHALL fail with the
exact env-var hint (`GITHUB_TOKEN` / `GITEA_TOKEN`).

#### Scenario: Previewing a manifest

- **WHEN** the user uploads a valid manifest
- **THEN** its entries are previewed with type indicators before the batch starts

#### Scenario: Running a batch

- **WHEN** the user starts the batch
- **THEN** the entries are ingested as tracked operations with progress

#### Scenario: Missing token for a repo entry

- **WHEN** a github or gitea entry lacks its token on the daemon
- **THEN** that entry fails with the exact env-var hint and the rest proceed

### Requirement: Export and import knowledge bases from the UI

The UI SHALL let the user export a base as a tracked operation and, on success, download the
resulting archive through the browser. The UI SHALL let the user import a base by uploading an
archive via a modal, with an optional target name and a force-overwrite option that warns inline.
Import SHALL run as a tracked operation and refresh the list on success. The import modal SHALL
include a muted line pointing Google Drive users to the CLI (Drive import is out of scope here).

#### Scenario: Exporting and downloading

- **WHEN** the user exports a base and the operation completes
- **THEN** the UI offers a browser download of the archive

#### Scenario: Importing an archive

- **WHEN** the user uploads an archive in the import modal and submits
- **THEN** a tracked import operation runs and the list refreshes on success

#### Scenario: Round-trip

- **WHEN** a base is exported and its downloaded archive is re-imported
- **THEN** the base is restored without re-embedding
