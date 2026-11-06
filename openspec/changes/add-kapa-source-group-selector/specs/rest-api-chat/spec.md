## MODIFIED Requirements

<!-- Builds on `add-kapa-retrieval-wiring`, which ADDs this requirement; this change extends it
with start-time selection and the acknowledgement frame. Archive that change first. -->

### Requirement: Active kapa source groups are set via the session

The client SHALL be able to set or change the session's active kapa.ai source groups through a
control message on the chat connection, the API equivalent of the in-REPL `/use-kapa`. Retrieval for
subsequent prompts SHALL use the current selection, alongside the session's active knowledge bases.
The selection SHALL be independent of the active knowledge bases: selecting source groups SHALL NOT
change the active bases, and selecting bases SHALL NOT clear the source groups. The daemon SHALL
acknowledge each change with the effective selection, as it already does for the active knowledge
bases.

A session SHALL start with no source groups selected, so a session never begins by querying an entire
kapa project. A client MAY name an initial selection in `POST /1.0/chat`, the same way it names the
initial knowledge bases; only the groups it names are active. When the integration is unconfigured, a
session SHALL report that a selection cannot be applied rather than accepting it silently, as defined
by `kapa-retrieval` — whether the selection arrives as a control message or in `POST /1.0/chat`.

#### Scenario: Selecting active source groups

- **WHEN** a client sends a control message selecting one or more kapa source groups
- **THEN** subsequent prompts retrieve kapa context from exactly those groups
- **AND** the daemon acknowledges the change with the effective selection

#### Scenario: Changing the selection mid-session

- **WHEN** a client changes the selected source groups partway through a session
- **THEN** prompts after the change use the new selection

#### Scenario: Selection is independent of knowledge bases

- **WHEN** a client changes the active knowledge bases while source groups are selected
- **THEN** the source-group selection is unchanged and both continue to feed retrieval

#### Scenario: Session starts with nothing selected

- **WHEN** a client starts a chat session without selecting source groups
- **THEN** no kapa retrieval occurs until a selection is made

#### Scenario: Initial selection at session start

- **WHEN** a client sends `POST /1.0/chat` naming one or more kapa source groups
- **THEN** the session starts with exactly those groups active

#### Scenario: Selecting with an unconfigured integration

- **WHEN** a client selects source groups and the daemon has no kapa credentials
- **THEN** the session reports that the selection cannot be applied
- **AND** subsequent prompts continue to retrieve from the active knowledge bases

### Requirement: RAG grounding matches the existing chat loop

Answer generation over the API SHALL use the same retrieval-augmented pipeline as the existing
chat REPL — query rewriting into retrieval keywords, hybrid retrieval over the active bases and
the active kapa.ai source groups, prompt augmentation with the retrieved context, and the same
grounding/provenance rules — so that answers are equivalent to the CLI experience.

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
retrieval (local, kapa.ai, or both) is available. The daemon SHALL NOT substitute any other
prompt: what the prompts API reports as the slot's effective value is exactly what a new
unselected session runs on.

#### Scenario: Grounded answer over the API

- **WHEN** a client asks a question with knowledge bases active
- **THEN** the daemon rewrites the query, retrieves context via the hybrid pipeline, augments the prompt, and streams a grounded answer
- **AND** the grounding and provenance behavior matches the existing chat REPL

#### Scenario: Grounded answer combining local and kapa.ai context

- **WHEN** a client asks a question with both local knowledge bases and kapa.ai source groups active
- **THEN** the daemon retrieves from both sources as defined by `kapa-retrieval` and streams a grounded answer
- **AND** the grounding and provenance behavior matches the chat REPL with the same sources active

#### Scenario: Chatting without active knowledge bases

- **WHEN** a client sends a prompt with no knowledge bases and no kapa.ai source groups active
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

### Requirement: Chat sessions can resume a saved chat

`POST /1.0/chat` SHALL accept an optional saved-chat id. When present, the daemon SHALL
seed the new session's conversation history, active knowledge-base set, and active kapa.ai
source-group selection from the saved chat before the websocket is connected, and SHALL
associate the session with that chat id so later saves update the same record. The response
metadata SHALL include the restored transcript (or enough for the client to render it), the
effective active bases, and the effective kapa.ai source-group selection. An unknown chat id
SHALL fail the request with a 404 error response rather than silently starting a fresh session.
Saved bases that no longer exist SHALL be dropped from the active set and reported in the
session metadata. Saved kapa.ai source-group ids SHALL be restored as-is: kapa.ai has no local
record to validate them against, and a stale id matches nothing on the next retrieval. When the
saved chat has a kapa.ai selection but the integration is unconfigured, the selection SHALL be
reported as not applied, as for any other selection made while unconfigured.

#### Scenario: Resume seeds history and bases

- **WHEN** a client sends `POST /1.0/chat` with a saved-chat id
- **THEN** the session starts with the saved transcript as conversation history and the saved bases active
- **AND** the first prompt on the websocket can reference earlier turns without resending them

#### Scenario: Resume seeds the kapa.ai selection

- **WHEN** a client sends `POST /1.0/chat` with a saved-chat id whose saved kapa.ai source groups are non-empty and kapa.ai is configured
- **THEN** the session starts with those kapa.ai source groups active
- **AND** the response metadata reports them as the effective selection

#### Scenario: Resume with an unknown id fails

- **WHEN** a client sends `POST /1.0/chat` with an id that matches no saved chat
- **THEN** the API returns a 404 error response and no session is started

#### Scenario: Resume drops missing bases

- **WHEN** a resumed chat's saved bases include one that has been deleted
- **THEN** the session starts with the remaining bases and the response identifies the dropped base

#### Scenario: Resume with a kapa.ai selection while unconfigured

- **WHEN** a resumed chat's saved kapa.ai source groups are non-empty and the daemon has no kapa credentials
- **THEN** the session starts with the saved transcript and bases
- **AND** the response metadata reports that the saved kapa.ai selection could not be applied
