## MODIFIED Requirements

### Requirement: Saved chats persist the conversation and its context

A saved chat SHALL contain everything needed to resume the conversation: a stable unique
id, a title, created and last-updated timestamps, the model name the session ran on, the
active knowledge-base set at save time, the active kapa.ai source-group selection at save time,
and the ordered conversation transcript (user and assistant turns with their final content). A
saved chat SHALL additionally record the session's prompt provenance — the resolved
`chat_system_prompt` variant name and version number, or empty when the session ran on the
built-in default. Provenance is informational: resuming SHALL NOT pin the recorded version, and
records saved before the provenance or kapa.ai fields existed SHALL remain readable and
resumable, reporting no provenance and no kapa.ai selection. Saved chats SHALL be stored locally
on the machine and SHALL NOT be transmitted anywhere other than the loopback/unix-socket API.

#### Scenario: A saved chat round-trips its context

- **WHEN** a user saves a chat with two turns, knowledge bases `default` and `docs` active, and one kapa.ai source group selected
- **THEN** the stored chat contains both turns in order, the two base names, the kapa.ai source-group id, the model name, its title, and timestamps

#### Scenario: Prompt provenance is recorded

- **WHEN** a session running on variant `presales-call` at version 3 is saved
- **THEN** the stored chat records `presales-call@3` as its prompt provenance

#### Scenario: Pre-existing records stay readable

- **WHEN** a chat saved before the provenance or kapa.ai fields existed is listed and resumed
- **THEN** it loads and resumes normally, reporting no prompt provenance and no kapa.ai selection

#### Scenario: Reasoning content is not required to resume

- **WHEN** a session whose answers included `<think>` reasoning content is saved
- **THEN** the stored transcript contains the final answer content of each assistant turn
- **AND** resuming does not depend on the reasoning content being present

### Requirement: Resuming restores transcript and knowledge-base context

Resuming a saved chat SHALL restore the conversation history as generation context (the
next answer can refer to earlier turns) and SHALL restore the saved active knowledge-base
set and the saved kapa.ai source-group selection. The restored transcript SHALL be displayed
to the user so they can see where the conversation left off. A saved knowledge base that no
longer exists SHALL be dropped from the active set with a notice, not treated as a fatal error.
A saved kapa.ai source-group id SHALL be restored as-is without existence validation — kapa.ai
has no local record to check against, and a stale id simply returns no matches on the next
retrieval. When kapa.ai is unconfigured at resume time, the user SHALL be notified that the saved
kapa.ai selection could not be applied, rather than the selection being dropped silently. The
session's model SHALL be resolved the same way as for a fresh session; if it differs from the
saved model name the user SHALL be informed.

#### Scenario: Follow-up uses restored history

- **WHEN** the user resumes a chat that discussed a specific error message and asks "what was the fix again?"
- **THEN** the assistant answers from the restored conversation history without the user restating the error

#### Scenario: Missing knowledge base degrades gracefully

- **WHEN** a resumed chat's saved base list names a knowledge base that has since been deleted
- **THEN** the session starts with the remaining bases active and the user is notified of the dropped base

#### Scenario: Saved kapa.ai selection is restored

- **WHEN** a resumed chat's saved kapa.ai source-group selection is non-empty and kapa.ai is configured
- **THEN** the session starts with those kapa.ai source groups active, without a separate confirmation step

#### Scenario: Saved kapa.ai selection while unconfigured

- **WHEN** a resumed chat's saved kapa.ai source-group selection is non-empty and kapa.ai is unconfigured
- **THEN** the session resumes with its transcript and bases
- **AND** the user is notified that the saved kapa.ai selection could not be applied
