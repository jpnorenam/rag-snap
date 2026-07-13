# rest-api-localhost-auth Specification

## Purpose

Authenticate clients on the loopback listener. The unix socket trusts peers via `SO_PEERCRED`, but those kernel credentials are unavailable for TCP connections, so the loopback listener needs an application-layer secret. A daemon-generated localhost bearer token provides that trust boundary now and is the seam where TLS client certs / OIDC will attach when the surface becomes remote.

## Requirements

### Requirement: Daemon generates a localhost access token

When the loopback listener is enabled, the daemon SHALL ensure a localhost access token exists, generating a high-entropy token (at least 256 bits) if none is present. The token SHALL be persisted under the daemon's writable data area (`$SNAP_COMMON`, with a temp-dir fallback outside a snap) so it survives daemon restarts.

#### Scenario: Token created on first enable

- **WHEN** the loopback listener is enabled and no token exists
- **THEN** the daemon generates a high-entropy token and persists it

#### Scenario: Existing token reused

- **WHEN** the daemon restarts and a non-empty token already exists
- **THEN** the daemon reuses the existing token rather than regenerating it

### Requirement: Token file is owner-only

The persisted token file SHALL be written owner-only (`0600`). The daemon SHALL NOT attempt to make it group-readable: under strict confinement snapd's seccomp profile denies the daemon chowning the file to an arbitrary group, and a failed chown MUST NOT crash the daemon. Clients obtain the token value over the trusted socket (see "Token discoverable by trusted clients"), not by reading this file.

#### Scenario: Token persisted owner-only

- **WHEN** the daemon writes a newly generated token
- **THEN** the file mode is `0600` and no group-chown is attempted

#### Scenario: Confinement does not crash the daemon

- **WHEN** the daemon runs under strict confinement and cannot change file group ownership
- **THEN** it still starts and serves the loopback listener using the owner-only token

### Requirement: Loopback requests authenticate with the token

Requests to `/1.0/...` over the loopback listener SHALL be authenticated by presenting the localhost token as an `Authorization: Bearer <token>` header, or as the `rag_ui_token` cookie (which a browser websocket upgrade can carry when it cannot set headers). The token comparison SHALL be constant-time. A request with no token or an invalid token SHALL be rejected with an authentication error. Requests over the unix socket SHALL continue to authenticate via `SO_PEERCRED` and SHALL NOT require the token.

#### Scenario: Valid bearer token admitted

- **WHEN** a loopback request to `/1.0/...` presents the valid token as a bearer header
- **THEN** the daemon authenticates the request and serves it

#### Scenario: Valid token via cookie admitted

- **WHEN** a loopback request presents the valid token as the `rag_ui_token` cookie
- **THEN** the daemon authenticates the request and serves it

#### Scenario: Missing or invalid token rejected

- **WHEN** a loopback request to `/1.0/...` presents no token or an invalid one
- **THEN** the daemon rejects it with an authentication error

#### Scenario: Unix socket unaffected

- **WHEN** a client connects over the unix socket
- **THEN** authentication uses `SO_PEERCRED` as before and no token is required

### Requirement: Token discoverable by trusted clients

The daemon SHALL return the token value in the `GET /1.0` config summary when the loopback listener is enabled, so a trusted client reaching the daemon over the unix socket can obtain it without reading the owner-only file. Returning the token there SHALL be safe because `GET /1.0` is peercred-gated to `root` and members of the access group — exactly the principals the token is scoped to grant.

#### Scenario: Trusted client reads the token over the socket

- **WHEN** a peercred-authorized client requests `GET /1.0` and the loopback listener is enabled
- **THEN** the config summary includes the current localhost token value

#### Scenario: Token absent when listener disabled

- **WHEN** the loopback listener is disabled
- **THEN** the config summary does not include a token value