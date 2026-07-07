# rest-api-loopback Specification

## Purpose

Let the `ragd` daemon serve the `/1.0` REST API over an opt-in loopback TCP listener, in addition to the unix socket, so local clients that cannot dial a unix socket (browsers, scripts, other processes) can reach the same API on the same mux. Remote exposure stays out of scope: the listener binds loopback only.

## ADDED Requirements

### Requirement: Opt-in loopback HTTP listener

The daemon SHALL be able to open an HTTP listener bound to a loopback address (`127.0.0.1`/`::1`) in addition to the unix socket. The listener SHALL be opt-in, controlled by the `api.loopback.enabled` config key (default off), with its bind address from `api.loopback.address` (default an OS-assigned loopback port, `127.0.0.1:0`). When enabled, the daemon SHALL continue to serve the unix socket unchanged.

#### Scenario: Listener opens when enabled

- **WHEN** `api.loopback.enabled` is true and the daemon starts
- **THEN** the daemon opens an HTTP listener on the configured loopback address
- **AND** continues to serve the unix socket unchanged

#### Scenario: Listener stays closed by default

- **WHEN** `api.loopback.enabled` is false (the default)
- **THEN** the daemon does not open any TCP listener and serves only the unix socket

### Requirement: Non-loopback binds are refused

The daemon SHALL refuse to bind the loopback listener to any non-loopback address. It SHALL reject a configured host that is empty (binds all interfaces) or that is not a loopback IP (or `localhost`) before listening, and SHALL additionally verify after binding that the resolved address is loopback, closing the listener otherwise. A refused bind SHALL be a fatal startup error, reported clearly, rather than silently downgraded.

#### Scenario: Non-loopback host rejected before binding

- **WHEN** `api.loopback.address` has a non-loopback host (e.g. `0.0.0.0:8080` or `192.168.1.10:8080`)
- **THEN** the daemon refuses to open the listener and reports the misconfiguration
- **AND** does not bind any TCP port

#### Scenario: All-interfaces bind rejected

- **WHEN** `api.loopback.address` has an empty host (e.g. `:8080`)
- **THEN** the daemon refuses the bind and reports that a loopback address is required

#### Scenario: Post-bind loopback verification

- **WHEN** the configured host resolves to a mix that includes a non-loopback address
- **THEN** the daemon closes the listener and refuses to serve on it

### Requirement: Loopback listener serves the same /1.0 API

The `/1.0/...` API endpoints SHALL be served identically on the loopback listener and the unix socket, registered from one shared handler set so the two transports never drift. The loopback listener SHALL NOT serve any endpoint the unix socket does not, other than transport bookkeeping (authentication is defined by `rest-api-localhost-auth`).

#### Scenario: API reachable over loopback

- **WHEN** an authenticated client calls a `/1.0/...` endpoint over the loopback listener
- **THEN** it is served by the same handler that serves the unix socket
- **AND** returns the same response envelope

#### Scenario: Handlers shared across transports

- **WHEN** a new `/1.0/...` endpoint is added to the API
- **THEN** it is reachable on both the unix socket and the loopback listener without per-transport registration

### Requirement: Resolved loopback address is discoverable

Because the default bind uses an OS-assigned port (`:0`), the daemon SHALL expose the resolved listen address once the listener is open, so a trusted client can discover the actual port. The address SHALL be reported in the `GET /1.0` config summary under a `loopback` section alongside its enabled state.

#### Scenario: Config summary reports the resolved address

- **WHEN** the loopback listener is enabled and a trusted client requests `GET /1.0`
- **THEN** the config summary reports `loopback.enabled` true and the resolved `loopback.address`

#### Scenario: Disabled state reported

- **WHEN** the loopback listener is disabled and a trusted client requests `GET /1.0`
- **THEN** the config summary reports `loopback.enabled` false and no address