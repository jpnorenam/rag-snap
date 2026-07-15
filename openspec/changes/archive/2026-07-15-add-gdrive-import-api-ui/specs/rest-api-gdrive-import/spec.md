## ADDED Requirements

### Requirement: Report Google Drive connection status

The daemon SHALL expose `GET /1.0/knowledge/gdrive/status` (authenticated) returning whether Drive import is `configured` (both `gdrive.client.id` and `gdrive.client.secret` are set), whether a valid token is `connected`, whether an OAuth flow is `pending`, and, when available, the connected `account` and any last-flow `error`. The response SHALL NOT include the access token, refresh token, or any OAuth URL containing a secret.

#### Scenario: Credentials not configured

- **WHEN** `gdrive.client.id` or `gdrive.client.secret` is unset and the UI requests status
- **THEN** the response reports `configured: false` so the UI can render the not-configured state

#### Scenario: Connected

- **WHEN** a valid Drive token is stored and the UI requests status
- **THEN** the response reports `configured: true` and `connected: true`, with the account when the API exposes it

#### Scenario: No token stored

- **WHEN** credentials are configured but no valid token is stored and no flow is pending
- **THEN** the response reports `configured: true`, `connected: false`, `pending: false`

### Requirement: Start the Google Drive OAuth flow

The daemon SHALL expose `POST /1.0/knowledge/gdrive/connect` (authenticated) that starts an OAuth2 Authorization-Code-with-PKCE loopback flow in the background and returns a Google `consent_url` for the UI to open in a new tab. The handler SHALL return promptly without blocking for user consent. The daemon SHALL run its own loopback callback listener, validate a `state` nonce on the redirect, exchange the authorization code, and persist the resulting token. At most one flow SHALL be pending at a time; starting a new flow SHALL supersede any prior pending flow.

#### Scenario: Flow started

- **WHEN** credentials are configured and the UI posts to connect
- **THEN** the daemon returns a `consent_url` and begins awaiting the callback in the background

#### Scenario: Credentials not configured

- **WHEN** `gdrive.client.id`/`gdrive.client.secret` are unset and the UI posts to connect
- **THEN** the daemon responds with a client error indicating Drive is not configured

#### Scenario: Consent completes

- **WHEN** the user completes consent and the browser redirects to the daemon's callback with a matching `state`
- **THEN** the daemon exchanges the code, stores the token, and a subsequent status request reports `connected: true`

#### Scenario: Consent denied or times out

- **WHEN** the user denies consent or does not complete it within the flow timeout
- **THEN** the pending flow ends in an error state surfaced through the status endpoint, and the UI can retry

### Requirement: Disconnect Google Drive

The daemon SHALL expose `POST /1.0/knowledge/gdrive/disconnect` (authenticated) that deletes the stored Drive token. After disconnect, status SHALL report `connected: false`.

#### Scenario: Disconnecting

- **WHEN** a token is stored and the UI posts to disconnect
- **THEN** the daemon deletes the token and status reports `connected: false`

### Requirement: Resolve a Google Drive URL into archives

The daemon SHALL expose `POST /1.0/knowledge/gdrive/resolve` (authenticated) accepting a Drive folder or file URL in the forms the CLI accepts, returning the resource `kind` (`file` or `folder`) and the list of discovered `.tar.gz` archives with `id`, `name`, and `size`. A single-file URL SHALL resolve to exactly one archive. Resolution failures SHALL return specific, actionable errors distinguishing not found, no access with the connected account, and not a recognised Drive URL.

#### Scenario: Folder with archives

- **WHEN** the UI resolves a Drive folder URL containing archives
- **THEN** the daemon returns `kind: folder` and the list of archives with names and sizes

#### Scenario: Single file

- **WHEN** the UI resolves a Drive single-file URL
- **THEN** the daemon returns `kind: file` and one archive, skipping the picker

#### Scenario: Unrecognised URL

- **WHEN** the UI resolves a string that is not a recognised Drive URL
- **THEN** the daemon returns a client error explaining the accepted URL forms

#### Scenario: Not connected

- **WHEN** the UI resolves a URL while no valid token is stored
- **THEN** the daemon returns an error indicating the account is not connected

### Requirement: Import selected Drive archives as tracked operations

The daemon SHALL expose `POST /1.0/knowledge/gdrive/import` (authenticated) accepting the archive identifiers to import, an optional target name, and a force flag. The daemon SHALL start one tracked async operation per selected archive — downloading the archive to a temporary file and importing it via the shared import core — so that partial failures are reported per archive through the operations surface. A missing target name SHALL derive the knowledge-base name from the archive filename. Force SHALL carry the same overwrite semantics as local import.

#### Scenario: Importing multiple archives

- **WHEN** the UI imports several selected archives
- **THEN** the daemon starts one tracked operation per archive and each completes or fails independently

#### Scenario: Derived name

- **WHEN** an archive is imported without a target name
- **THEN** the knowledge-base name is derived from the archive filename

#### Scenario: Force overwrite

- **WHEN** an archive would overwrite an existing base and force is set
- **THEN** the import proceeds with the same overwrite semantics as local import

#### Scenario: Temporary files are cleaned up

- **WHEN** a per-archive import operation finishes, whether it succeeds or fails
- **THEN** the daemon removes the downloaded temporary archive
