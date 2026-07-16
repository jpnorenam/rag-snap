# rest-api-prompts Delta Specification

## ADDED Requirements

### Requirement: Generation slots support named prompt variants

The API SHALL support named variants on the two generation slots (`chat_system_prompt`,
`answer_system_prompt`): `GET /1.0/prompts/{slot}/variants` listing variant summaries (name,
last-updated, version count), `POST /1.0/prompts/{slot}/variants` creating a variant from a name
and a non-empty value, `GET /1.0/prompts/{slot}/variants/{name}` returning a variant's head
value and metadata, and `DELETE /1.0/prompts/{slot}/variants/{name}` removing it. Variant names
SHALL match `^[a-z0-9][a-z0-9-]{0,63}$` and SHALL be validated on lookup so a name taken from a
request path cannot escape the store. The name `default` SHALL be reserved — it denotes the
built-in default and cannot be created, edited, or deleted. The `source_rules` slot SHALL NOT
support variants; its variant endpoints SHALL return not-found. Deleting the currently active
variant SHALL be rejected with a conflict error instructing the client to activate another
selection first.

#### Scenario: Creating and listing variants

- **WHEN** a client POSTs `{name: "presales-call", value: "..."}` to a generation slot's variants collection
- **THEN** the variant is stored and appears in the variants listing with its metadata

#### Scenario: Invalid or reserved names rejected

- **WHEN** a client creates a variant named `Default`, `../x`, or `default`
- **THEN** the API rejects the request with a validation error

#### Scenario: Deleting the active variant is a conflict

- **WHEN** a client DELETEs the variant a slot's active pointer references
- **THEN** the API returns a conflict error and the variant remains stored

#### Scenario: No variants on the guardrail slot

- **WHEN** a client calls a variant endpoint on `source_rules`
- **THEN** the API returns a not-found error

### Requirement: Variant edits build an append-only version history

Each save to a variant (`PUT /1.0/prompts/{slot}/variants/{name}`) SHALL append an immutable
version carrying a monotonically increasing version number, a timestamp, and the full value; the
latest version (the head) SHALL always be the variant's effective value. A save whose value is
byte-identical to the head SHALL be a no-op that creates no version. Empty or whitespace-only
values SHALL be rejected. `GET /1.0/prompts/{slot}/variants/{name}/versions` SHALL return the
full history. The `source_rules` override SHALL keep the same version history on its single
override.

#### Scenario: Saves append versions

- **WHEN** a client saves a variant three times with distinct values
- **THEN** the version history holds three versions in order and the head is the last value

#### Scenario: Identical save is a no-op

- **WHEN** a client saves a value byte-identical to the variant's head
- **THEN** no new version is created and the history length is unchanged

### Requirement: Old versions can be restored

`POST /1.0/prompts/{slot}/variants/{name}/restore` with a version number SHALL append a new head
version whose value is that version's content — history SHALL remain linear and immutable; no
pointer moves backwards. Restoring the current head SHALL be a no-op. An unknown version number
SHALL be rejected with a not-found error.

#### Scenario: Restore appends a new head

- **WHEN** a client restores version 1 of a variant whose head is version 3
- **THEN** a version 4 is appended with version 1's content and becomes the effective value
- **AND** versions 1 through 3 are unchanged

### Requirement: A slot's active variant selects the effective prompt

Each generation slot SHALL carry an active pointer naming one variant or the built-in default,
changed via `PATCH /1.0/prompts/{slot}` with `{"active": "<name>"}` (empty string selects the
default). The slot's effective value — what new sessions and batches resolve when no explicit
selection is made — SHALL be the active variant's head, or the built-in default when the pointer
is unset. Activating an unknown variant SHALL fail with a not-found error and leave the pointer
unchanged.

#### Scenario: Activation changes the effective prompt

- **WHEN** a client PATCHes a slot's active pointer to a stored variant
- **THEN** the slot's effective value becomes that variant's head version
- **AND** sessions started afterwards without an explicit selection use it

#### Scenario: Activating an unknown variant fails

- **WHEN** a client PATCHes the active pointer to a name with no stored variant
- **THEN** the API returns a not-found error and the previous pointer remains in effect

### Requirement: The legacy override store is migrated once

On first prompt-store access after upgrade, an existing legacy single-file override store SHALL
be migrated: each non-empty generation-slot override becomes a variant named `custom` with one
version, activated for its slot; a non-empty `source_rules` override becomes version 1 of its
versioned override. The legacy file SHALL then be moved aside so migration never re-runs and the
pre-migration content remains recoverable. A migration failure SHALL degrade to built-in
defaults with a logged warning, never a daemon failure.

#### Scenario: Legacy override becomes an active custom variant

- **WHEN** the daemon first accesses the store after upgrade and the legacy file holds a `chat_system_prompt` override
- **THEN** a `custom` variant exists for `chat_system_prompt` with that content as version 1 and is active
- **AND** the effective prompt observed by clients is unchanged

#### Scenario: Migration runs once

- **WHEN** the daemon restarts after a completed migration
- **THEN** no second migration occurs and stored variants are untouched

## MODIFIED Requirements

### Requirement: Prompt templates are readable with defaults and customized state

The API SHALL provide `GET /1.0/prompts` returning the three prompt templates —
`chat_system_prompt`, `answer_system_prompt`, `source_rules` — in that fixed order, and
`GET /1.0/prompts/{name}` returning a single one. Each prompt view SHALL include the prompt
name, the effective value (the active variant's head for a generation slot with a variant
active, the stored override for `source_rules`, otherwise the built-in default), the built-in
default text, and a flag indicating whether the effective value differs from the built-in
default. Generation-slot views SHALL additionally include the active variant name (empty for the
default) and the list of stored variant names. Requests naming an unknown prompt SHALL be
rejected with an error identifying the valid names.

#### Scenario: Listing the prompts

- **WHEN** a client sends `GET /1.0/prompts`
- **THEN** the response contains exactly the three prompt templates in the fixed order
- **AND** each entry carries the effective value, the built-in default, and the customized flag
- **AND** generation-slot entries carry the active variant name and the stored variant names

#### Scenario: Reading a slot with an active variant

- **WHEN** a generation slot has a variant active and a client reads it
- **THEN** the effective value is that variant's head, the default field still carries the
  built-in text, and the customized flag is true

#### Scenario: Unknown prompt name

- **WHEN** a client requests a prompt name other than the three defined templates
- **THEN** the API returns a not-found error naming the valid prompt names

### Requirement: Prompt templates can be updated

The API SHALL provide `PUT /1.0/prompts/{name}` accepting a new value for the named prompt and
writing it through to the slot's current selection: a new version on the active variant when one
is active, the versioned override for `source_rules`, and — when the built-in default is active
on a generation slot — the creation and activation of a variant named `custom` holding the
value. Empty or whitespace-only values SHALL be rejected — reset is an explicit operation, not a
side effect of clearing the value. A submitted value identical to the built-in default SHALL,
when the slot's selection is the `custom` variant or the `source_rules` override, return the
slot to the built-in default instead of storing a copy, so the customized flag reflects a real
divergence from the default.

#### Scenario: Storing a customization from the default state

- **WHEN** a client PUTs a non-empty value differing from the built-in default to a generation slot whose default is active
- **THEN** a `custom` variant is created with that value and activated
- **AND** subsequent reads return that value as the effective prompt with the customized flag set

#### Scenario: Write-through to the active variant

- **WHEN** a generation slot has a named variant active and a client PUTs a new value to the slot
- **THEN** the value is appended as a new version of that variant and becomes the effective prompt

#### Scenario: Empty value rejected

- **WHEN** a client PUTs an empty or whitespace-only value
- **THEN** the API rejects the request with a validation error and the stored prompt is
  unchanged

#### Scenario: Value equal to the default clears the legacy customization

- **WHEN** the `custom` variant is active on a slot and a client PUTs a value byte-identical to the built-in default
- **THEN** the slot returns to the built-in default and subsequent reads report the customized
  flag as false

### Requirement: Prompt templates can be reset to their defaults

The API SHALL provide `DELETE /1.0/prompts/{name}` returning the slot's effective value to the
built-in default: for a generation slot the active pointer is set to the default and stored
variants are preserved; for `source_rules` the override is cleared (its version history
preserved). Resetting a prompt already at its default SHALL succeed as a no-op.

#### Scenario: Resetting a slot with an active variant

- **WHEN** a client DELETEs a generation slot whose active pointer names a variant
- **THEN** subsequent reads return the built-in default as the effective value with the
  customized flag false
- **AND** the variant and its version history remain stored and listed

#### Scenario: Reset is idempotent

- **WHEN** a client DELETEs a prompt that is not customized
- **THEN** the request succeeds and the prompt remains at its default

### Requirement: Prompt customizations persist across daemon restarts

Variants, their version histories, the per-slot active pointers, and the `source_rules` override
SHALL be persisted by the daemon under its `$SNAP_COMMON` state directory so they survive daemon
restarts and snap refreshes. Each variant SHALL be persisted as its own record so a corrupt
record loses only that variant, never the store; the built-in default SHALL never be persisted —
a slot with no active variant always resolves to the built-in default of the running release. A
persisted record that cannot be parsed SHALL NOT fail prompt resolution; the daemon SHALL
degrade that record to absent with a logged warning.

#### Scenario: Variants survive a restart

- **WHEN** variants exist and an active pointer is set, and the daemon restarts
- **THEN** reads after the restart return the same variants, histories, and effective values

#### Scenario: Defaults track the installed release

- **WHEN** a slot has no active variant and a snap refresh changes that slot's built-in default
- **THEN** the effective value after the refresh is the new release's default

#### Scenario: Corrupt variant record degrades alone

- **WHEN** one persisted variant record cannot be parsed
- **THEN** that variant is treated as absent with a logged warning
- **AND** other variants and the slot's resolution keep working
