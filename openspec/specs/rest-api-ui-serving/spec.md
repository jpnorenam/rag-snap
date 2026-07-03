# rest-api-ui-serving Specification

## Purpose

Let the `ragd` daemon serve the embedded browser UI to a local browser over the loopback
listener that already exists on this branch. The loopback TCP listener and its localhost
bearer-token authentication are defined by the `rest-api-loopback` and
`rest-api-localhost-auth` capabilities and are reused unchanged; this capability adds the
UI assets, same-origin serving under `/ui/`, the SPA fallback, the token-handoff endpoint,
and the `rag ui` launch command. Remote exposure stays out of scope: the listener binds
loopback only.

## Requirements

### Requirement: UI is served same-origin with the API on the loopback listener

The embedded UI SHALL be served under `/ui/` on the same loopback listener and HTTP mux as
the `/1.0/...` API, so the UI and API share one origin and no CORS configuration is required.
A request to `/` on the loopback listener SHALL redirect to `/ui/`. The embedded assets SHALL
be compiled into the `ragd` binary (no dependency on files on disk at runtime). The unix
socket routes SHALL be unchanged — `/ui/` serving is added to the loopback routes only.

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

#### Scenario: Unix socket routes unchanged

- **WHEN** a client connects over the unix socket
- **THEN** it is served the `/1.0/...` API and discovery root only, with no `/ui/` routes

### Requirement: SPA fallback for client-side routes

For requests under `/ui/` that do not match an embedded asset, the daemon SHALL serve the
UI's `index.html` so client-side routing works on deep links and reloads.

#### Scenario: Deep link falls back to index

- **WHEN** a browser requests a `/ui/...` path that is not a real asset
- **THEN** the daemon responds with the UI's `index.html`

### Requirement: Static UI assets do not require API authentication

Serving the static UI assets under `/ui/` SHALL NOT require the localhost bearer token, so
the page can load and then authenticate its `/1.0/...` data calls. The `/1.0/...` API SHALL
remain authenticated as defined by the `rest-api-localhost-auth` capability.

#### Scenario: UI shell loads unauthenticated

- **WHEN** an unauthenticated browser requests the UI shell under `/ui/`
- **THEN** the static assets are served
- **AND** subsequent `/1.0/...` calls are still subject to authentication

### Requirement: Token handoff into the UI

The daemon SHALL provide a `/ui/login` endpoint on the loopback listener that accepts the
localhost token (e.g. as a query parameter), establishes it as a loopback-scoped credential
for the browser session (such as a cookie), and redirects into the SPA. This keeps the token
out of the SPA's JavaScript source and out of the persistent address bar. The handoff
endpoint SHALL be reachable without prior authentication so a freshly launched browser can
present the token it was given.

#### Scenario: Login endpoint accepts the token and redirects

- **WHEN** a browser requests `/ui/login` with a valid localhost token
- **THEN** the daemon establishes the loopback-scoped credential and redirects into `/ui/`

#### Scenario: Subsequent API calls are authenticated by the handoff

- **WHEN** the UI issues a `/1.0/...` request after the login handoff
- **THEN** the request is authenticated by the credential established at `/ui/login`

### Requirement: rag ui launch command

The CLI SHALL provide a `rag ui` command that makes the local UI reachable and opens it. It
SHALL contact the daemon over the trusted unix socket to discover the loopback listener's URL
and localhost token, build the `/ui/login` handoff URL with the token applied, and open the
user's browser at it. A `--no-browser` flag SHALL print the handoff URL instead of opening a
browser. When the loopback listener is disabled, the command SHALL explain how to enable it
(via `api.loopback.enabled`) rather than failing silently.

#### Scenario: Launch opens the browser

- **WHEN** the user runs `rag ui` with the loopback listener enabled
- **THEN** the command resolves the loopback UI URL with the token applied and opens it in a browser

#### Scenario: Print instead of open

- **WHEN** the user runs `rag ui --no-browser` with the loopback listener enabled
- **THEN** the command prints the handoff URL instead of opening a browser

#### Scenario: Guidance when disabled

- **WHEN** the user runs `rag ui` while `api.loopback.enabled` is false
- **THEN** the command reports that the UI is disabled and how to enable it