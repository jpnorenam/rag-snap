# rest-api-ui-serving Specification

## Purpose

Let the `ragd` daemon serve the embedded browser UI to a local browser. Because a browser
cannot connect to the existing unix socket, the daemon gains a loopback TCP listener that
serves the UI same-origin with the `/1.0` API, mirroring LXD's single-mux model. Remote
exposure stays out of scope: the listener binds loopback only.

## ADDED Requirements

### Requirement: Loopback HTTP listener for the UI

The daemon SHALL be able to open an HTTP listener bound to a loopback address
(`127.0.0.1`/`::1`) in addition to the unix socket. The listener SHALL be opt-in, controlled
by the `api.ui.enabled` config key (default off), with its bind address from `api.ui.address`
(default an OS-assigned loopback port). The daemon SHALL refuse to bind the UI listener to a
non-loopback address in this change.

#### Scenario: Listener opens when enabled

- **WHEN** `api.ui.enabled` is true and the daemon starts
- **THEN** the daemon opens an HTTP listener on the configured loopback address
- **AND** continues to serve the unix socket unchanged

#### Scenario: Listener stays closed by default

- **WHEN** `api.ui.enabled` is false (the default)
- **THEN** the daemon does not open any TCP listener

#### Scenario: Non-loopback bind refused

- **WHEN** `api.ui.address` resolves to a non-loopback address
- **THEN** the daemon refuses to start the UI listener and reports the misconfiguration

### Requirement: UI is served same-origin with the API

The embedded UI SHALL be served under `/ui/` on the same listener and HTTP mux as the
`/1.0/...` API, so the UI and API share one origin and no CORS configuration is required.
A request to `/` SHALL redirect to `/ui/`. The embedded assets SHALL be compiled into the
`ragd` binary (no dependency on files on disk at runtime).

#### Scenario: API and UI on one origin

- **WHEN** a browser loads the UI over the loopback listener
- **THEN** the UI assets are served under `/ui/` and `/1.0/...` API calls hit the same origin
- **AND** no cross-origin request is made

#### Scenario: Root redirects to the UI

- **WHEN** a browser requests `/` on the loopback listener
- **THEN** the daemon redirects to `/ui/`

#### Scenario: Assets embedded in the binary

- **WHEN** the daemon serves a UI asset
- **THEN** it is read from the embedded filesystem, not from a path on disk

### Requirement: SPA fallback for client-side routes

For requests under `/ui/` that do not match an embedded asset, the daemon SHALL serve the
UI's `index.html` so client-side routing works on deep links and reloads.

#### Scenario: Deep link falls back to index

- **WHEN** a browser requests a `/ui/...` path that is not a real asset
- **THEN** the daemon responds with the UI's `index.html`

### Requirement: Static UI assets do not require API authentication

Serving the static UI assets under `/ui/` SHALL NOT require API authentication, so the page
can load and then authenticate its `/1.0/...` data calls. The `/1.0/...` API SHALL remain
authenticated as defined by the auth capabilities.

#### Scenario: UI shell loads unauthenticated

- **WHEN** an unauthenticated browser requests the UI shell under `/ui/`
- **THEN** the static assets are served
- **AND** subsequent `/1.0/...` calls are still subject to authentication

### Requirement: rag ui launch command

The CLI SHALL provide a `rag ui` command that makes the local UI reachable and opens it. It
SHALL ensure the loopback listener is available, determine its URL, apply the localhost
auth token, and open the user's browser at the UI. When the UI is not enabled, the command
SHALL explain how to enable it rather than failing silently.

#### Scenario: Launch opens the browser

- **WHEN** the user runs `rag ui` with the UI enabled
- **THEN** the command resolves the loopback UI URL with the token applied and opens it in a browser

#### Scenario: Guidance when disabled

- **WHEN** the user runs `rag ui` while `api.ui.enabled` is false
- **THEN** the command reports that the UI is disabled and how to enable it
