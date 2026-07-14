# rest-api-config Specification

## Purpose

Expose the snapctl-backed configuration over the API so clients can read the merged
configuration with its layer provenance (`package` vs `user`), write user-layer overrides, and
revert them — the API-side equivalent of the CLI `config get` / `config set` commands, with the
same validation. Secret-shaped keys are write-only through the API. Service credentials
(`OPENSEARCH_USERNAME`, `OPENSEARCH_PASSWORD`, `CHAT_API_KEY`) are environment variables rather
than config keys and stay out of this capability entirely.

## Requirements

### Requirement: Config is readable with layer provenance

The daemon SHALL expose `GET /1.0/config`, gated by the same authentication as the rest of the
`/1.0` API, returning the merged snapctl-backed configuration as a list of entries sorted by
key, each carrying the dot-namespaced key, its effective value, and the layer it is effective
from: `user` when a user-layer override exists for the key, otherwise `package`. Layer
provenance SHALL be determined by reading the layers individually, not by comparing values (a
user override equal to the package value still reports `user`). Deprecated keys — the same list
the CLI `config get` hides — SHALL be omitted. The response SHALL include a `writable` boolean
stating whether the caller may write config.

#### Scenario: Package key listed

- **WHEN** an authenticated client requests `GET /1.0/config` and `tika.http.port` is set only in the package layer
- **THEN** the response lists `tika.http.port` with its value and layer `package`

#### Scenario: User override reported as user layer

- **WHEN** a user-layer value exists for `chat.http.host`
- **THEN** the response lists `chat.http.host` with the user-layer value and layer `user`

#### Scenario: Deprecated keys hidden

- **WHEN** a key on the CLI's deprecated-configuration list has a value in the store
- **THEN** it does not appear in the `GET /1.0/config` response

### Requirement: Secret-shaped config values are redacted

`GET /1.0/config` SHALL redact the value of any key whose final segment is `secret`, `password`,
or `token` (today this matches `gdrive.client.secret`), replacing the value with a fixed
redaction marker while keeping the key, its layer, and its writability — the key is write-only
through the API. Service credentials (`OPENSEARCH_USERNAME`, `OPENSEARCH_PASSWORD`,
`CHAT_API_KEY`) are environment variables, not config keys, and MUST NOT appear in any config
response.

#### Scenario: Google Drive client secret redacted

- **WHEN** `gdrive.client.secret` has a non-empty value in any layer
- **THEN** `GET /1.0/config` lists the key with the redaction marker in place of the value

#### Scenario: Redacted key remains writable

- **WHEN** an authenticated client sends `PUT /1.0/config/gdrive.client.secret` with a new value
- **THEN** the write succeeds and subsequent reads still return the redaction marker

### Requirement: Config writes target the user layer with CLI-identical validation

The daemon SHALL expose `PUT /1.0/config/{key}` accepting a string value and writing it to the
**user** config layer through the same storage path as CLI `config set`. A key that does not
already exist in the merged config SHALL be rejected as unknown, and a key on the deprecated
list SHALL be rejected, both as client errors carrying a message suitable for field-level
display. A successful write SHALL be effective immediately for subsequent merged reads.

#### Scenario: Valid write persists to the user layer

- **WHEN** an authenticated client sends `PUT /1.0/config/chat.http.port` with value `9000`
- **THEN** the daemon writes the user layer and responds with success
- **AND** a subsequent `GET /1.0/config` reports `chat.http.port` with value `9000` and layer `user`

#### Scenario: Unknown key rejected

- **WHEN** a client sends `PUT /1.0/config/not.a.key`
- **THEN** the daemon responds with a client error identifying the key as unknown
- **AND** nothing is written

#### Scenario: Deprecated key rejected

- **WHEN** a client sends `PUT /1.0/config/{key}` for a key on the deprecated list
- **THEN** the daemon rejects the write as a client error

### Requirement: User overrides can be reverted to the package value

The daemon SHALL expose `DELETE /1.0/config/{key}`, removing the key's user-layer value so the
package value becomes effective. When the key has no user-layer value, the daemon SHALL respond
with a client error and change nothing.

#### Scenario: Revert restores the package value

- **WHEN** `chat.http.host` has both a package value and a user override and a client sends `DELETE /1.0/config/chat.http.host`
- **THEN** the user-layer value is removed
- **AND** a subsequent `GET /1.0/config` reports the package value with layer `package`

#### Scenario: Revert without an override fails

- **WHEN** a client sends `DELETE /1.0/config/{key}` for a key with no user-layer value
- **THEN** the daemon responds with a client error and the config is unchanged

### Requirement: Write access follows API authentication and is advertised

Config mutations SHALL be gated by the same authentication as other mutating endpoints (socket
peercred or the localhost bearer token) — both listener's authenticated callers may write. The
`writable` field in `GET /1.0/config` SHALL report the caller's effective write permission so
clients can render a read-only view instead of offering writes that would fail.

#### Scenario: Authenticated caller may write

- **WHEN** a caller authenticated via the loopback token requests `GET /1.0/config`
- **THEN** `writable` is true and `PUT`/`DELETE` requests from the same caller are accepted

#### Scenario: Unauthenticated caller rejected

- **WHEN** a client without valid credentials sends `PUT /1.0/config/{key}`
- **THEN** the daemon responds with the standard authentication error and nothing is written

### Requirement: Config capability is feature-detectable

The daemon SHALL advertise the config resource by appending a `config` entry to
`api_extensions`.

#### Scenario: Extension advertised

- **WHEN** a client requests `GET /1.0`
- **THEN** `api_extensions` includes `config`
