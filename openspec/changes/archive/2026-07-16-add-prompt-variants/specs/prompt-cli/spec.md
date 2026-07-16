# prompt-cli Delta Specification

## ADDED Requirements

### Requirement: Prompt variants can be managed from the CLI

The CLI SHALL provide scriptable subcommands under `prompt` for managing named prompt variants
via the daemon API: `prompt list` (slots with their variants and active markers), `prompt save
<slot> <name>` reading the value from `--file` or stdin (creating the variant or appending a
version), `prompt use <slot> <name>` and `prompt use <slot> --default` (activation), `prompt
history <slot> <name>` (version list), `prompt restore <slot> <name> <version>`, and `prompt
delete <slot> <name>`. Management subcommands SHALL name the slot explicitly. These subcommands
SHALL require the daemon; without one they SHALL fail with an error that suggests starting the
daemon rather than falling back to the client-local prompts file.

#### Scenario: Saving a named variant

- **WHEN** a user runs `prompt save chat_system_prompt presales-call --file p.txt` with the daemon running
- **THEN** the variant is created (or a new version appended) via the daemon API
- **AND** `prompt list` shows `presales-call` under `chat_system_prompt`

#### Scenario: Activating a variant

- **WHEN** a user runs `prompt use chat_system_prompt presales-call`
- **THEN** new chat sessions started without an explicit selection use that variant's head version

#### Scenario: Returning to the built-in default

- **WHEN** a user runs `prompt use chat_system_prompt --default`
- **THEN** the slot's active pointer returns to the built-in default and the variant remains stored

#### Scenario: Management without a daemon fails clearly

- **WHEN** a user runs a variant management subcommand and no daemon is reachable
- **THEN** the command fails with an error suggesting the daemon, and no local file is modified

### Requirement: prompt init offers variant selection

The interactive `prompt init` flow SHALL, when a daemon is available, extend its existing
slot selection with a variant level: after choosing a generation slot the user SHALL be able to
choose an existing variant, create a new named variant, or select the built-in default, and then
edit, activate, or (for a customized selection) restore. The daemonless `prompt init` fallback
editing `~/.config/rag-cli/prompts.json` SHALL keep its current single-override behavior
unchanged.

#### Scenario: Creating a variant interactively

- **WHEN** a user runs `prompt init` with the daemon running and chooses a generation slot, then "new variant"
- **THEN** they are prompted for a name and an initial value, and the variant is saved to the daemon

#### Scenario: Daemonless flow is unchanged

- **WHEN** a user runs `prompt init` without a daemon
- **THEN** the existing single-override editor over the client-local prompts file runs, with no variant features

### Requirement: Chat can select a prompt variant at start

The `chat` command SHALL accept a `--prompt <name>` flag naming a variant of
`chat_system_prompt` (the slot is implied). The named variant's head version SHALL be the
session's system prompt for that session only, without changing the slot's active pointer.
An unknown variant name SHALL fail the command before the session starts.

#### Scenario: Per-session selection

- **WHEN** a user runs `chat --prompt presales-call` while a different variant is active
- **THEN** the session uses `presales-call`'s head version as its system prompt
- **AND** the slot's active pointer is unchanged for subsequent sessions

#### Scenario: Unknown variant is rejected

- **WHEN** a user runs `chat --prompt no-such-variant`
- **THEN** the command fails naming the unknown variant and no session is started

### Requirement: Batch manifests can reference a named variant

The batch manifest SHALL accept a `prompt_ref: <name>` key naming a variant of
`answer_system_prompt` (the slot is implied). `prompt_ref` and the existing inline `prompt` key
SHALL be mutually exclusive; a manifest with both SHALL be rejected before any question is
answered. When `prompt_ref` is set, the variant's head version SHALL be used as the batch system
prompt exactly as the slot's stored value would be (it does not take the inline-custom path that
appends `source_rules`).

#### Scenario: Manifest references a variant

- **WHEN** a batch manifest contains `prompt_ref: rfp-govco-2026` and no inline `prompt`
- **THEN** the run's system prompt is that variant's head version

#### Scenario: Both prompt keys rejected

- **WHEN** a manifest contains both `prompt` and `prompt_ref`
- **THEN** the batch is rejected with a validation error before answering starts
