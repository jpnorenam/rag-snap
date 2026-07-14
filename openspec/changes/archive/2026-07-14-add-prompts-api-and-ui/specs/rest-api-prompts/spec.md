# rest-api-prompts Specification (delta)

## ADDED Requirements

### Requirement: Prompt templates are readable with defaults and customized state

The API SHALL provide `GET /1.0/prompts` returning the three prompt templates —
`chat_system_prompt`, `answer_system_prompt`, `source_rules` — in that fixed order, and
`GET /1.0/prompts/{name}` returning a single one. Each prompt view SHALL include the prompt
name, the effective value (the stored override when one exists, otherwise the built-in
default), the built-in default text, and a flag indicating whether the prompt is customized.
Requests naming an unknown prompt SHALL be rejected with an error identifying the valid names.

#### Scenario: Listing the prompts

- **WHEN** a client sends `GET /1.0/prompts`
- **THEN** the response contains exactly the three prompt templates in the fixed order
- **AND** each entry carries the effective value, the built-in default, and the customized flag

#### Scenario: Reading a customized prompt

- **WHEN** a prompt has a stored override and a client reads it
- **THEN** the effective value is the override, the default field still carries the built-in
  text, and the customized flag is true

#### Scenario: Unknown prompt name

- **WHEN** a client requests a prompt name other than the three defined templates
- **THEN** the API returns a not-found error naming the valid prompt names

### Requirement: Prompt templates can be updated

The API SHALL provide `PUT /1.0/prompts/{name}` accepting a new value for the named prompt and
persisting it as the stored override. Empty or whitespace-only values SHALL be rejected — reset
is an explicit operation, not a side effect of clearing the value. A submitted value identical
to the built-in default SHALL clear the override instead of storing a copy, so the customized
flag reflects a real divergence from the default.

#### Scenario: Storing a customization

- **WHEN** a client PUTs a non-empty value that differs from the built-in default
- **THEN** subsequent reads return that value as the effective prompt with the customized flag
  set

#### Scenario: Empty value rejected

- **WHEN** a client PUTs an empty or whitespace-only value
- **THEN** the API rejects the request with a validation error and the stored prompt is
  unchanged

#### Scenario: Value equal to the default clears the override

- **WHEN** a client PUTs a value byte-identical to the built-in default
- **THEN** the prompt is no longer customized and subsequent reads report the customized flag
  as false

### Requirement: Prompt templates can be reset to their defaults

The API SHALL provide `DELETE /1.0/prompts/{name}` removing the stored override so the
effective value returns to the built-in default. Resetting a prompt that has no override SHALL
succeed as a no-op.

#### Scenario: Resetting a customized prompt

- **WHEN** a client DELETEs a customized prompt
- **THEN** subsequent reads return the built-in default as the effective value with the
  customized flag false

#### Scenario: Reset is idempotent

- **WHEN** a client DELETEs a prompt that is not customized
- **THEN** the request succeeds and the prompt remains at its default

### Requirement: Prompt customizations persist across daemon restarts

Stored prompt overrides SHALL be persisted by the daemon (under its `$SNAP_COMMON` state
directory) so they survive daemon restarts and snap refreshes. The persisted form SHALL store
only overrides: a prompt without an override always resolves to the built-in default of the
running release. A persisted store that cannot be parsed SHALL NOT fail prompt resolution; the
daemon SHALL fall back to the built-in defaults and report the prompts as not customized.

#### Scenario: Customization survives a restart

- **WHEN** a prompt is customized and the daemon restarts
- **THEN** reads after the restart return the customized value

#### Scenario: Defaults track the installed release

- **WHEN** a prompt has no override and a snap refresh changes that prompt's built-in default
- **THEN** the effective value after the refresh is the new release's default

#### Scenario: Corrupt store degrades to defaults

- **WHEN** the persisted prompt store exists but cannot be parsed
- **THEN** prompt resolution yields the built-in defaults and reads report no prompt as
  customized

### Requirement: Prompt endpoints require authentication

The prompt endpoints SHALL be exposed on both the unix-socket and loopback listeners and SHALL
require the same authentication as the other `/1.0` resources (trusted socket peer or localhost
bearer token).

#### Scenario: Unauthenticated access refused

- **WHEN** a loopback client calls a prompt endpoint without a valid token
- **THEN** the API refuses the request with an authentication error
