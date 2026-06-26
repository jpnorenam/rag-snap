## ADDED Requirements

### Requirement: Versioned HTTP API served by a daemon

The snap SHALL provide an HTTP API daemon, started by a `rag api serve` command and runnable as a snap service, that serves a versioned REST API under the `/1.0` path prefix. The daemon SHALL read its bind address and port from configuration (`api.http.host`, `api.http.port`).

The daemon SHALL be stateless with respect to workflow data: it SHALL NOT persist uploaded documents, question manifests, or answers between requests. Only transient operation status MAY be held in memory.

#### Scenario: Daemon serves the versioned API

- **WHEN** the daemon is running with a configured host and port
- **THEN** it accepts HTTP requests under `/1.0` at that address
- **AND** it does not store workflow documents, manifests, or answers across requests

#### Scenario: Server discovery and feature detection

- **WHEN** a client requests `GET /1.0`
- **THEN** the response includes server information, the active authentication mode, and an `api_extensions` array of supported named extensions
- **AND** clients can detect optional capabilities from `api_extensions` without relying on a version bump

### Requirement: Uniform error contract

The API SHALL return errors as `application/problem+json` (RFC 7807) with an appropriate HTTP status code. Unknown routes and unsupported methods SHALL produce the same structured error shape.

#### Scenario: Structured error for a bad request

- **WHEN** a client sends a malformed or invalid request
- **THEN** the response uses an appropriate 4xx status code and an `application/problem+json` body describing the problem

#### Scenario: Unknown route

- **WHEN** a client requests a path or method the API does not serve
- **THEN** the response is a structured `application/problem+json` error with a 404 or 405 status code

### Requirement: Asynchronous operations for long-running work

Long-running requests SHALL be modeled as asynchronous operations. Such a request SHALL return `202 Accepted` with a `Location` header pointing at `/1.0/operations/{id}` and an operation object describing the work.

A client SHALL be able to retrieve an operation's current status via `GET /1.0/operations/{id}`, and SHALL receive the operation's result when it has completed successfully, or a structured error when it has failed.

#### Scenario: Long-running request returns an operation

- **WHEN** a client starts a long-running request (such as question extraction or answering)
- **THEN** the response is `202 Accepted` with a `Location` header referencing `/1.0/operations/{id}` and an operation object

#### Scenario: Polling an operation to completion

- **WHEN** a client requests `GET /1.0/operations/{id}` for an operation that has completed successfully
- **THEN** the response reports the completed status and provides (or links to) the operation's result

#### Scenario: Polling a failed operation

- **WHEN** a client requests `GET /1.0/operations/{id}` for an operation that failed
- **THEN** the response reports the failed status and a structured error describing the failure

### Requirement: Operation progress event stream

The API SHALL expose `GET /1.0/events` as a Server-Sent Events stream that emits operation lifecycle and progress events. A client SHALL be able to filter the stream to a single operation.

#### Scenario: Watching progress for an operation

- **WHEN** a client opens `GET /1.0/events` filtered to a running operation's id
- **THEN** it receives progress and lifecycle events for that operation as Server-Sent Events until the operation completes or fails

### Requirement: Authenticated access with selectable mode

The API SHALL require authentication on all `/1.0` routes and SHALL support a selectable authentication mode via `api.auth.mode`: an `oidc` mode that validates a bearer ID token issued by the configured identity provider, and a `token` mode that validates a shared bearer secret supplied via the `RAG_API_TOKEN` environment variable.

In `oidc` mode the API SHALL validate the token against the provider's published keys using the configured issuer and audience, and SHALL authorize the caller by email domain against `api.auth.allowed_domains`. The shared secret for `token` mode SHALL be provided as an environment variable, never as configuration.

#### Scenario: Accepted OIDC token

- **WHEN** `api.auth.mode` is `oidc` and a request presents a valid bearer ID token whose email domain is in `api.auth.allowed_domains`
- **THEN** the request is authenticated and authorized and proceeds

#### Scenario: Rejected token

- **WHEN** a request presents a missing, invalid, or expired bearer token (or, in `oidc` mode, an identity whose domain is not allowlisted)
- **THEN** the API rejects it with a 401 or 403 structured error and does not perform the requested work

### Requirement: Cross-origin and private-network access control for the web UI

The API SHALL enforce CORS using a strict origin allowlist from `api.cors.origins` and SHALL NOT use a wildcard origin. It SHALL correctly answer CORS preflight (`OPTIONS`) requests.

When a request is a Private Network Access preflight (carrying `Access-Control-Request-Private-Network`), the API SHALL respond with `Access-Control-Allow-Private-Network: true` so that a browser on an HTTPS page may reach a `localhost`-bound daemon.

#### Scenario: Allowed origin

- **WHEN** the browser UI on an allowlisted origin sends a request (including its CORS preflight)
- **THEN** the API responds with the matching CORS headers and the request is allowed

#### Scenario: Disallowed origin

- **WHEN** a request originates from an origin not present in `api.cors.origins`
- **THEN** the API does not return permissive CORS headers for that origin and the browser blocks the cross-origin access

#### Scenario: Private Network Access preflight

- **WHEN** a browser on an HTTPS page issues a Private Network Access preflight to the `localhost`-bound daemon
- **THEN** the API responds with `Access-Control-Allow-Private-Network: true` for an allowlisted origin so the subsequent request can proceed
