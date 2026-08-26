# local-ui-app Delta

## MODIFIED Requirements

### Requirement: Ingest a document from the UI

The UI SHALL let the user ingest a single source into a base via a modal offering upload-file,
from-URL, GitHub-repo, and Gitea-repo choices. Upload and URL modes SHALL keep a source-identifier
field prefilled from the filename. Repo modes SHALL offer fields for the repository reference
(owner/repo or URL), optional branch (defaulting to the repository's default branch), optional
path prefix, optional file extensions, and label; extensions entered without a leading dot SHALL
be normalized. Repo modes SHALL offer a pre-ingest preview that shows the matched file count and a
sample of matched paths via the repo-preview endpoint, flagging truncated listings; preview SHALL
be advisory and SHALL NOT be required to submit. A missing daemon token SHALL surface the exact
env-var hint (`GITHUB_TOKEN` / `GITEA_TOKEN`) in the modal. All modes SHALL keep the
force-re-ingest option. On submit the UI SHALL start a tracked operation and close the modal
immediately, letting rows appear when the operation reports success. A duplicate-identifier error
without force SHALL keep the modal open with a field-level message and preserve the user's input.

#### Scenario: Ingesting by upload

- **WHEN** the user chooses a file, sets or accepts a source id, and submits
- **THEN** a tracked ingest operation starts and the modal closes immediately

#### Scenario: Ingesting from a URL

- **WHEN** the user enters a valid URL and submits
- **THEN** a tracked ingest operation starts for that URL

#### Scenario: Ingesting a repository

- **WHEN** the user selects a repo mode, enters a repository reference (with optional branch, path, and extensions), and submits
- **THEN** a tracked ingest operation starts that expands the repository server-side into per-file sources

#### Scenario: Previewing repository files before ingesting

- **WHEN** the user requests a preview for a filled-in repo source
- **THEN** the modal shows the matched file count and a sample of paths, with a caution when the listing is truncated

#### Scenario: Missing token surfaced in the modal

- **WHEN** a preview or ingest fails because the daemon lacks the repo token
- **THEN** the modal shows the exact env-var hint (`GITHUB_TOKEN` or `GITEA_TOKEN`) and preserves the user's input

#### Scenario: Duplicate source id without force

- **WHEN** ingestion is rejected because the source id already exists and force is off
- **THEN** the modal stays open with a message telling the user to enable force re-ingest, and input is preserved

#### Scenario: In-progress hint on the detail view

- **WHEN** an ingest operation for the open base is running
- **THEN** the sources table shows a live-updating in-progress hint above it

### Requirement: Batch ingest from the UI

The UI SHALL let the user batch-ingest via a modal with two modes: uploading the YAML manifest the
CLI accepts, or building a manifest visually. Upload mode SHALL parse the manifest client-side and
preview the entries (with a type indicator per entry) before starting, and SHALL offer loading the
parsed jobs into the builder for editing. Builder mode SHALL let the user add, edit, and remove
jobs of type url, github-repo, and gitea-repo with the manifest's per-job fields (name, source,
branch, path, extensions, label), validating as the user edits; repo jobs SHALL offer a matched-
file preview via the repo-preview endpoint. The builder SHALL export the manifest as a downloadable
YAML file compatible with `k ingest --batch` (same schema and job type names), and parsing then
serializing a manifest SHALL round-trip. Local `file` jobs from an uploaded manifest SHALL be
preserved for export but excluded from a UI-run batch with an explanation. Each started entry SHALL
join a single tracked operation. Entries requiring credentials the daemon lacks SHALL fail with the
exact env-var hint (`GITHUB_TOKEN` / `GITEA_TOKEN`).

#### Scenario: Previewing a manifest

- **WHEN** the user uploads a valid manifest
- **THEN** its entries are previewed with type indicators before the batch starts

#### Scenario: Editing an uploaded manifest in the builder

- **WHEN** the user uploads a manifest and chooses to edit it in the builder
- **THEN** the builder is populated with the parsed jobs, including flagged local-file jobs

#### Scenario: Building a batch visually

- **WHEN** the user adds url and repo jobs in the builder and fills their fields
- **THEN** invalid fields are flagged inline as the user edits, and the batch can start once the jobs validate

#### Scenario: Previewing a repo job in the builder

- **WHEN** the user requests a preview on a repo job
- **THEN** the matched file count (and truncation, if any) is shown for that job

#### Scenario: Downloading the built manifest

- **WHEN** the user downloads the built manifest
- **THEN** a YAML file in the CLI batch schema is produced that `k ingest --batch` accepts unchanged

#### Scenario: Running a batch

- **WHEN** the user starts the batch
- **THEN** the entries are ingested as tracked operations with progress

#### Scenario: Missing token for a repo entry

- **WHEN** a github or gitea entry lacks its token on the daemon
- **THEN** that entry fails with the exact env-var hint and the rest proceed
