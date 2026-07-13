# rest-api-server Specification

## MODIFIED Requirements

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