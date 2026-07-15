## MODIFIED Requirements

### Requirement: Export and import knowledge bases from the UI

The UI SHALL let the user export a base as a tracked operation and, on success, download the
resulting archive through the browser. The UI SHALL let the user import a base via a modal whose
source is chosen between **From file** and **From Google Drive**. For **From file**, the user
uploads an archive with an optional target name and a force-overwrite option that warns inline.
Import SHALL run as a tracked operation and refresh the list on success. The import modal SHALL NOT
tell the user to drop to the CLI for Google Drive; the Drive source is provided in-modal.

#### Scenario: Exporting and downloading

- **WHEN** the user exports a base and the operation completes
- **THEN** the UI offers a browser download of the archive

#### Scenario: Importing an archive

- **WHEN** the user uploads an archive in the import modal and submits
- **THEN** a tracked import operation runs and the list refreshes on success

#### Scenario: Round-trip

- **WHEN** a base is exported and its downloaded archive is re-imported
- **THEN** the base is restored without re-embedding

#### Scenario: Choosing a source

- **WHEN** the user opens the import modal
- **THEN** the UI offers a From file / From Google Drive source chooser and shows no CLI-only Drive hint

## ADDED Requirements

### Requirement: Connect and disconnect Google Drive from the import modal

The UI SHALL let the user connect a Google account from the Drive source of the import modal. When
the daemon reports Drive is not configured, the UI SHALL render an information state naming the
`gdrive.client.id`/`gdrive.client.secret` package config keys and linking the Status page, instead of
a connect action. When configured but not connected, the UI SHALL offer a connect action that opens
the daemon-provided consent URL in a **new tab** (never navigating the app away) and shows a waiting
state with a focusable cancel while it polls for completion. On denial or timeout the UI SHALL show a
recoverable error with retry. Once connected, the UI SHALL show the connected account when available
and a disconnect action guarded by a confirm that deletes the stored token. The UI SHALL never render
the token or raw OAuth URLs containing secrets.

#### Scenario: Drive not configured

- **WHEN** the daemon reports Drive is not configured
- **THEN** the UI shows an information state pointing at the config keys and the Status page, with no connect button

#### Scenario: Connecting in a new tab

- **WHEN** the user starts the connect flow
- **THEN** the UI opens the consent URL in a new tab, keeps app state, and shows a waiting state with cancel

#### Scenario: Consent completes

- **WHEN** consent completes in the other tab
- **THEN** the modal advances automatically to locating archives

#### Scenario: Consent denied or times out

- **WHEN** consent is denied or times out
- **THEN** the modal shows a recoverable error with retry, distinct from the timeout case

#### Scenario: Disconnecting

- **WHEN** the user disconnects behind the confirm
- **THEN** the stored token is deleted and the connect state returns

### Requirement: Locate and pick Google Drive archives

The UI SHALL let the connected user enter a Drive folder or file URL, validate its shape client-side,
and resolve it via the daemon. Resolution errors SHALL be shown as specific, actionable messages
(not found / no access / not a rag-cli archive). For a single-file URL the UI SHALL skip picking and
go to confirm. For a folder the UI SHALL present the discovered archives as a real checkbox group
(fieldset + legend) showing name, size, and modified time, with a tri-state **Select all** checkbox
that is equivalent to the CLI `--all`, a selected count, and a **Force** checkbox with the same
overwrite semantics as local import.

#### Scenario: Single-file URL skips picking

- **WHEN** the resolved URL is a single archive file
- **THEN** the UI skips the picker and proceeds to confirm

#### Scenario: Folder select-all

- **WHEN** the resolved URL is a folder and the user checks Select all
- **THEN** all discovered archives are selected, matching the CLI `--all`

#### Scenario: Resolution error

- **WHEN** resolution fails because the account cannot access the URL
- **THEN** the UI shows a specific no-access message with an actionable next step

### Requirement: Import Google Drive archives via the operations panel

The UI SHALL start a Drive import of the selected archives, closing the modal immediately, with each
archive tracked as an operation surfaced in the global operations panel. The knowledge-base list SHALL
refresh as imports land. Partial failure SHALL be reported per archive in the operations panel rather
than as all-or-nothing messaging.

#### Scenario: Importing selected archives

- **WHEN** the user imports the selected archives
- **THEN** the modal closes and each archive appears as a tracked operation; the list refreshes as each lands

#### Scenario: Partial failure

- **WHEN** some archives import successfully and others fail
- **THEN** each archive's success or failure is visible individually in the operations panel
