# chat-history Delta Specification

## MODIFIED Requirements

### Requirement: Saved chats persist the conversation and its context

A saved chat SHALL contain everything needed to resume the conversation: a stable unique
id, a title, created and last-updated timestamps, the model name the session ran on, the
active knowledge-base set at save time, and the ordered conversation transcript (user and
assistant turns with their final content). A saved chat SHALL additionally record the session's
prompt provenance — the resolved `chat_system_prompt` variant name and version number, or empty
when the session ran on the built-in default. Provenance is informational: resuming SHALL NOT
pin the recorded version, and records saved before the field existed SHALL remain readable and
resumable, reporting no provenance. Saved chats SHALL be stored locally on the
machine and SHALL NOT be transmitted anywhere other than the loopback/unix-socket API.

#### Scenario: A saved chat round-trips its context

- **WHEN** a user saves a chat with two turns and knowledge bases `default` and `docs` active
- **THEN** the stored chat contains both turns in order, the two base names, the model name, its title, and timestamps

#### Scenario: Prompt provenance is recorded

- **WHEN** a session running on variant `presales-call` at version 3 is saved
- **THEN** the stored chat records `presales-call@3` as its prompt provenance

#### Scenario: Pre-existing records stay readable

- **WHEN** a chat saved before the provenance field existed is listed and resumed
- **THEN** it loads and resumes normally, reporting no prompt provenance

#### Scenario: Reasoning content is not required to resume

- **WHEN** a session whose answers included `<think>` reasoning content is saved
- **THEN** the stored transcript contains the final answer content of each assistant turn
- **AND** resuming does not depend on the reasoning content being present
