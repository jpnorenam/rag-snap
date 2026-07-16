# rest-api-chat Delta Specification

## ADDED Requirements

### Requirement: Chat sessions can select a prompt variant

`POST /1.0/chat` SHALL accept an optional prompt variant name. When present, the named variant
of `chat_system_prompt` SHALL be resolved at session start and its head version used as the
session's system prompt, without changing the slot's active pointer. An unknown variant name
SHALL fail the request with a not-found error and no session SHALL be started. The session SHALL
record the resolved prompt reference — variant name and version number, or empty for the
built-in default — as provenance available to the save path. Provenance is a recording, not a
pin: resuming a saved chat SHALL re-resolve the prompt fresh, exactly as an unselected session
does.

#### Scenario: Explicit selection overrides the active pointer

- **WHEN** a client starts a session naming variant `presales-call` while a different variant is active
- **THEN** the session's system prompt is `presales-call`'s head version
- **AND** the slot's active pointer is unchanged

#### Scenario: Unknown variant fails the request

- **WHEN** a client starts a session naming a variant that does not exist
- **THEN** the API returns a not-found error and no session is started

#### Scenario: Resume re-resolves rather than pinning

- **WHEN** a chat saved with prompt provenance `presales-call@2` is resumed after the variant gained a version 3
- **THEN** the resumed session's system prompt is the current resolution (version 3), not the recorded version

## MODIFIED Requirements

### Requirement: RAG grounding matches the existing chat loop

Answer generation over the API SHALL use the same retrieval-augmented pipeline as the existing
chat REPL — query rewriting into retrieval keywords, hybrid retrieval over the active bases,
prompt augmentation with the retrieved context, and the same grounding/provenance rules — so
that answers are equivalent to the CLI experience.

The prompt templates driving generation SHALL come from the daemon prompt store
(`rest-api-prompts`): the session's system prompt is the resolved `chat_system_prompt` — the
variant named in the session request when one is given, otherwise the slot's active variant,
otherwise the built-in default. (The `source_rules` template governs batch answering with a
custom manifest prompt, not chat — chat's grounding rules live inside `chat_system_prompt`,
matching the chat REPL.) Prompts SHALL be resolved when the session starts; changes to stored
prompts or the active pointer SHALL apply to sessions started afterwards and SHALL NOT alter a
session already in progress.

The resolved `chat_system_prompt` — the selected or active variant's head when one applies, the
built-in default otherwise — SHALL be sent as the session's system prompt whether or not
retrieval is available. The daemon SHALL NOT substitute any other prompt: what the prompts API
reports as the slot's effective value is exactly what a new unselected session runs on.

#### Scenario: Grounded answer over the API

- **WHEN** a client asks a question with knowledge bases active
- **THEN** the daemon rewrites the query, retrieves context via the hybrid pipeline, augments the prompt, and streams a grounded answer
- **AND** the grounding and provenance behavior matches the existing chat REPL

#### Scenario: Chatting without active knowledge bases

- **WHEN** a client sends a prompt with no knowledge bases active
- **THEN** the daemon responds without retrieval augmentation

#### Scenario: Active variant drives new sessions

- **WHEN** a variant is active on `chat_system_prompt` and a client starts a chat session with no explicit selection
- **THEN** the session's system prompt is that variant's head version instead of the built-in default

#### Scenario: Customized prompt honoured without retrieval

- **WHEN** the resolved `chat_system_prompt` is a variant and the session starts while retrieval
  is unavailable
- **THEN** the session's system prompt is that variant's head version

#### Scenario: Default prompt sent without retrieval

- **WHEN** `chat_system_prompt` has no active variant, no selection is made, and a client starts
  a session while retrieval is unavailable
- **THEN** the session's system prompt is the built-in default — the same text the prompts API
  reports — with no substitute prompt swapped in

#### Scenario: Mid-session prompt edits do not affect the running session

- **WHEN** a stored prompt or active pointer is updated while a chat session is in progress
- **THEN** the running session continues with the prompt it started with
- **AND** the next session started uses the updated resolution
