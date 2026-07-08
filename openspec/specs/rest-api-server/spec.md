# rest-api-server Specification

## Purpose

Define the `ragd` daemon, its local unix-socket HTTP listener, the versioned `/1.0` API
root with feature detection, the uniform sync/async/error response envelope, local
authentication by socket group membership, and the auto-generated OpenAPI specification.
This is the foundation the knowledge, chat, answer, and operations capabilities build on.

## Requirements

### Requirement: Daemon serves the API over a local unix socket

The system SHALL provide a `ragd` daemon that listens for HTTP requests on a local unix
domain socket. The daemon SHALL be packaged as an opt-in snap service that is disabled on
install and managed via snap service controls.

The daemon SHALL create the socket and set its file mode to `api.socket.mode` (default
`0666`). The daemon SHALL NOT attempt to change the socket's group owner: under strict
confinement the seccomp profile denies chowning to an arbitrary group, which would crash the
daemon. Access SHALL therefore be gated by the peer-credential check (see "Local
authentication"), not by the socket's file ownership.

The unix socket SHALL be the only listener opened by default. The daemon MAY additionally
open an opt-in loopback TCP listener when `api.loopback.enabled` is set, as defined by the
`rest-api-loopback` capability; that listener binds a loopback address only. The daemon SHALL
NOT open any non-loopback TCP or HTTPS listener; remote access is out of scope.

#### Scenario: Socket is created with the configured mode

- **WHEN** the `ragd` daemon starts with `api.socket.mode=0666`
- **THEN** it creates the unix socket and sets its mode to `0666`
- **AND** it begins serving HTTP requests on that socket without attempting to chown it

#### Scenario: Daemon is opt-in

- **WHEN** the snap is installed
- **THEN** the `ragd` service is present but not started automatically
- **AND** it starts only when explicitly started via snap service controls

#### Scenario: No listener opened beyond the socket by default

- **WHEN** the `ragd` daemon is running and `api.loopback.enabled` is false (the default)
- **THEN** it accepts connections only on the local unix socket
- **AND** it does not bind any TCP port or TLS listener

#### Scenario: Only a loopback TCP listener may be added

- **WHEN** `api.loopback.enabled` is true
- **THEN** the only additional listener the daemon opens is bound to a loopback address
- **AND** it still opens no non-loopback TCP or TLS listener

### Requirement: Versioned API root with feature detection

The API SHALL expose a root endpoint `GET /` that reports the supported API version(s), the
authentication state of the caller, and a list of `api_extensions` naming backward-compatible
features. All resources SHALL live under the `/1.0/` prefix.

Backward-compatible additions SHALL be advertised by appending to `api_extensions` rather than
by introducing a new major version path.

#### Scenario: Root advertises version and extensions

- **WHEN** a client requests `GET /`
- **THEN** the response reports API version `1.0` and an `api_extensions` list
- **AND** all functional resources are addressed under `/1.0/`

#### Scenario: Root reports caller authentication state

- **WHEN** an authenticated local client requests `GET /`
- **THEN** the response indicates the caller is trusted
- **WHEN** the caller has not passed authentication
- **THEN** the response indicates the caller is untrusted

### Requirement: Uniform response envelope

Every API response SHALL be one of three shapes: a synchronous result, an asynchronous
operation reference, or an error. Each SHALL carry a numeric `status_code` (or `error_code`)
that clients use in preference to any text status.

- A synchronous response SHALL use `{"type":"sync"}` with HTTP 200 and a `metadata` object.
- An asynchronous response SHALL use `{"type":"async"}` with HTTP 202, an `operation` URL of
  the form `/1.0/operations/<uuid>`, and the operation URL also in the `Location` header.
- An error response SHALL use `{"type":"error"}` with an `error_code` matching the HTTP status
  (one of 400, 401, 403, 404, 409, 412, 500) and a human-readable `error` string.

#### Scenario: Synchronous result

- **WHEN** a handler completes a request immediately
- **THEN** it returns HTTP 200 with `{"type":"sync", "status_code":200, "metadata":{...}}`

#### Scenario: Asynchronous result

- **WHEN** a handler starts a long-running operation
- **THEN** it returns HTTP 202 with `{"type":"async", "operation":"/1.0/operations/<uuid>"}`
- **AND** the `Location` header is set to the same operation URL

#### Scenario: Error result

- **WHEN** a request fails or targets a missing resource
- **THEN** the response is `{"type":"error", "error_code":<code>, "error":"<message>"}`
- **AND** the HTTP status equals the `error_code`

### Requirement: Local authentication via socket group membership

The daemon SHALL authenticate every connection using a transport-aware check. Connections on
the unix socket SHALL be authenticated by the peer's operating-system credentials
(`SO_PEERCRED`): access is granted if and only if the peer's effective user is `root` or is a
member of the configured `api.socket.group`. Connections on the loopback listener SHALL be
authenticated by the localhost bearer token (see the `rest-api-localhost-auth` capability),
because `SO_PEERCRED` is unavailable for TCP peers.

A granted connection SHALL have access to all endpoints; there SHALL be no per-endpoint
authorization in this capability. A denied unix-socket connection SHALL receive a `403` error
with a message naming the group the user must join.

#### Scenario: Member of the access group is granted access

- **WHEN** a process owned by a user in the `api.socket.group` connects to the socket
- **THEN** the daemon grants the connection full access to the API

#### Scenario: Root is granted access

- **WHEN** a process owned by `root` connects to the socket
- **THEN** the daemon grants the connection full access to the API

#### Scenario: Non-member is denied with guidance

- **WHEN** a process owned by a user not in the access group and not `root` connects
- **THEN** the daemon returns a `403` error
- **AND** the error message states the user must be a member of the configured access group

#### Scenario: Loopback connection uses token authentication

- **WHEN** a request arrives on the loopback listener rather than the unix socket
- **THEN** the daemon authenticates it by the localhost token, not by `SO_PEERCRED`

### Requirement: Configuration is read from snapctl, not the API

The daemon SHALL read all of its configuration (service hosts, ports, TLS flags, model IDs,
and the `api.socket.*` keys) from the snapctl config store at startup, and SHALL re-read it
when it receives a reload signal. The API SHALL NOT expose a writable configuration resource.

Secrets (`OPENSEARCH_USERNAME`, `OPENSEARCH_PASSWORD`, `CHAT_API_KEY`) SHALL be provided to the
daemon via environment variables, never via config or the API.

#### Scenario: Config is loaded at startup

- **WHEN** the daemon starts
- **THEN** it reads its configuration from the snapctl config store
- **AND** it constructs its OpenSearch, inference, and Tika clients from those values

#### Scenario: Config is re-read on reload

- **WHEN** the daemon receives its reload signal after the snapctl config has changed
- **THEN** it re-reads the configuration and rebuilds its service clients

#### Scenario: No writable config endpoint exists

- **WHEN** a client attempts to write configuration through the API
- **THEN** no such endpoint is available

### Requirement: Backend readiness does not block the listener

The daemon SHALL begin serving the API as soon as the socket is ready, independently of
whether the OpenSearch, inference, or Tika backends are reachable. Endpoints that require a
backend that is not yet ready SHALL return an error indicating the backend is unavailable,
rather than causing the daemon to fail to start.

#### Scenario: API is available before backends are ready

- **WHEN** the daemon starts while OpenSearch is still initializing
- **THEN** the API root and operations endpoints respond normally
- **AND** a knowledge endpoint that needs OpenSearch returns an error stating the backend is unavailable

### Requirement: Auto-generated OpenAPI specification

The project SHALL produce an OpenAPI/Swagger specification for the API, generated from
annotations on the handler code, so that the published specification tracks the implementation.
The build SHALL validate that the specification is in sync with the handlers.

#### Scenario: Specification is generated from handlers

- **WHEN** the API specification is generated
- **THEN** it is derived from annotations on the handler code, not hand-maintained
- **AND** every implemented endpoint appears in the specification

#### Scenario: Build detects spec drift

- **WHEN** a handler is added or changed without regenerating the specification
- **THEN** the build's specification check fails
