# rest-api-localhost-auth Specification

## Purpose

Authenticate browser clients on the loopback listener. The unix socket trusts peers via
`SO_PEERCRED`, but those kernel credentials are unavailable for TCP connections, so the
loopback listener needs an application-layer secret. A daemon-generated localhost bearer
token provides that trust boundary now and is the seam where TLS client certs / OIDC will
attach when the surface becomes remote.

## ADDED Requirements

### Requirement: Daemon generates a localhost access token

When the UI listener is enabled, the daemon SHALL ensure a localhost access token exists,
generating a high-entropy token if none is present. The token SHALL be stored in a file
under the daemon's writable data area (`$SNAP_COMMON`) readable only by the configured
access group, so members of that group — the same trust boundary as the unix socket — can
read it and non-members cannot.

#### Scenario: Token created on first enable

- **WHEN** the UI listener is enabled and no token exists
- **THEN** the daemon generates a high-entropy token and persists it

#### Scenario: Existing token reused

- **WHEN** the daemon restarts and a token already exists
- **THEN** the daemon reuses the existing token rather than regenerating it

#### Scenario: Token file is group-scoped

- **WHEN** the token file is written
- **THEN** its permissions allow the access group to read it and deny other users

### Requirement: Loopback requests authenticate with the token

Requests to `/1.0/...` over the loopback listener SHALL be authenticated by presenting the
localhost token (e.g. as a bearer token or equivalent header/cookie). A request without a
valid token SHALL be rejected with an authentication error. Requests over the unix socket
SHALL continue to authenticate via `SO_PEERCRED` and SHALL NOT require the token.

#### Scenario: Valid token admitted

- **WHEN** a loopback request to `/1.0/...` presents the valid localhost token
- **THEN** the daemon authenticates the request and serves it

#### Scenario: Missing or invalid token rejected

- **WHEN** a loopback request to `/1.0/...` presents no token or an invalid one
- **THEN** the daemon rejects it with an authentication error

#### Scenario: Unix socket unaffected

- **WHEN** a client connects over the unix socket
- **THEN** authentication uses `SO_PEERCRED` as before and no token is required

### Requirement: Token delivered to the browser at launch

The `rag ui` launch flow SHALL supply the token to the browser so the loaded UI can
authenticate its API calls, without the user copying it by hand. The token SHALL NOT be
embedded in the static assets at build time (it is per-installation and must not be baked
into the binary).

#### Scenario: Launch applies the token

- **WHEN** the user launches the UI via `rag ui`
- **THEN** the opened UI is able to authenticate its `/1.0/...` calls using the token

#### Scenario: Token not baked into assets

- **WHEN** the UI assets are built and embedded
- **THEN** they contain no installation-specific token
