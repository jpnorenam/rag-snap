# rest-api-chat Specification

## Purpose

Expose the interactive, RAG-grounded chat experience over the API as a websocket-backed
session, so that a client can hold a multi-turn conversation, toggle the active knowledge
bases mid-session, and receive streamed tokens. The daemon owns the chat session state,
mirroring LXD's interactive operation model.

## Requirements

### Requirement: Chat sessions are interactive websocket operations

The API SHALL provide `POST /1.0/chat` to start a chat session. Starting a session SHALL return
an asynchronous, websocket-class operation whose metadata includes a websocket URL the client
connects to in order to exchange messages. The session SHALL persist across multiple prompts on
that connection and SHALL end when the connection closes or an idle timeout elapses.

#### Scenario: Starting a chat session

- **WHEN** a client sends `POST /1.0/chat`
- **THEN** the API returns a websocket-class operation referencing a websocket URL
- **AND** connecting to that URL establishes an interactive chat session

#### Scenario: Session spans multiple turns

- **WHEN** a client sends several prompts over one chat websocket connection
- **THEN** the daemon treats them as one continuing conversation with retained history

#### Scenario: Session ends on disconnect

- **WHEN** the chat websocket connection closes
- **THEN** the daemon ends the session and releases its state

### Requirement: Daemon owns chat session state

The daemon SHALL hold each chat session's state — the active knowledge-base set, the
conversation history, and the resolved model — for the lifetime of the session. The client SHALL
NOT be required to resend history on each prompt.

#### Scenario: History retained server-side

- **WHEN** a client sends a follow-up prompt referring to earlier turns without resending them
- **THEN** the daemon answers using the retained conversation history

### Requirement: Streamed token responses with reasoning blocks

When the daemon generates an answer, it SHALL stream the response to the client over the
websocket as it is produced, rather than returning it only when complete. Reasoning/`<think>`
content SHALL be distinguishable from final answer content, and a terminal message SHALL signal
the end of each answer.

#### Scenario: Tokens stream as generated

- **WHEN** the daemon generates an answer to a prompt
- **THEN** it sends answer content to the client incrementally as it is produced
- **AND** it sends a terminal message when the answer is complete

#### Scenario: Reasoning content is distinguishable

- **WHEN** the model emits reasoning/`<think>` content
- **THEN** the client can distinguish that content from the final answer content

### Requirement: Active knowledge bases are set via the session

The client SHALL be able to set or change the session's active knowledge bases through a control
message on the chat connection (the API equivalent of the in-REPL `/use-knowledge`). Retrieval
for subsequent prompts SHALL use the current active set.

#### Scenario: Selecting active knowledge bases

- **WHEN** a client sends a control message selecting one or more knowledge bases
- **THEN** subsequent prompts retrieve context from exactly those bases

#### Scenario: Changing the active set mid-session

- **WHEN** a client changes the active knowledge bases partway through a session
- **THEN** prompts after the change use the new active set

### Requirement: RAG grounding matches the existing chat loop

Answer generation over the API SHALL use the same retrieval-augmented pipeline as the existing
chat REPL — query rewriting into retrieval keywords, hybrid retrieval over the active bases,
prompt augmentation with the retrieved context, and the same grounding/provenance rules — so
that answers are equivalent to the CLI experience.

The prompt templates driving generation SHALL come from the daemon prompt store
(`rest-api-prompts`): the session's system prompt is the stored `chat_system_prompt`, resolving
to its built-in default when not customized. (The `source_rules` template governs batch answering
with a custom manifest prompt, not chat — chat's grounding rules live inside
`chat_system_prompt`, matching the chat REPL.) Prompts SHALL be resolved when the session starts;
changes to stored prompts SHALL apply to sessions started afterwards and SHALL NOT alter a
session already in progress.

The configured `chat_system_prompt` — the customization when one is stored, the built-in default
otherwise — SHALL be sent as the session's system prompt whether or not retrieval is available.
The daemon SHALL NOT substitute any other prompt: what the prompts API reports as the effective
value is exactly what a new session runs on.

#### Scenario: Grounded answer over the API

- **WHEN** a client asks a question with knowledge bases active
- **THEN** the daemon rewrites the query, retrieves context via the hybrid pipeline, augments the prompt, and streams a grounded answer
- **AND** the grounding and provenance behavior matches the existing chat REPL

#### Scenario: Chatting without active knowledge bases

- **WHEN** a client sends a prompt with no knowledge bases active
- **THEN** the daemon responds without retrieval augmentation

#### Scenario: Customized prompts drive new sessions

- **WHEN** the stored `chat_system_prompt` is customized and a client starts a chat session
- **THEN** the session's system prompt is the customized template instead of the built-in default

#### Scenario: Customized prompt honoured without retrieval

- **WHEN** the stored `chat_system_prompt` is customized and a client starts a chat session while
  retrieval is unavailable
- **THEN** the session's system prompt is the customized template

#### Scenario: Default prompt sent without retrieval

- **WHEN** the `chat_system_prompt` is not customized and a client starts a chat session while
  retrieval is unavailable
- **THEN** the session's system prompt is the built-in default — the same text the prompts API
  reports — with no substitute prompt swapped in

#### Scenario: Mid-session prompt edits do not affect the running session

- **WHEN** a stored prompt is updated while a chat session is in progress
- **THEN** the running session continues with the prompts it started with
- **AND** the next session started uses the updated prompts

### Requirement: Chat sessions can resume a saved chat

`POST /1.0/chat` SHALL accept an optional saved-chat id. When present, the daemon SHALL
seed the new session's conversation history and active knowledge-base set from the saved
chat before the websocket is connected, and SHALL associate the session with that chat id
so later saves update the same record. The response metadata SHALL include the restored
transcript (or enough for the client to render it) and the effective active bases. An
unknown chat id SHALL fail the request with a 404 error response rather than silently
starting a fresh session. Saved bases that no longer exist SHALL be dropped from the
active set and reported in the session metadata.

#### Scenario: Resume seeds history and bases

- **WHEN** a client sends `POST /1.0/chat` with a saved-chat id
- **THEN** the session starts with the saved transcript as conversation history and the saved bases active
- **AND** the first prompt on the websocket can reference earlier turns without resending them

#### Scenario: Resume with an unknown id fails

- **WHEN** a client sends `POST /1.0/chat` with an id that matches no saved chat
- **THEN** the API returns a 404 error response and no session is started

#### Scenario: Resume drops missing bases

- **WHEN** a resumed chat's saved bases include one that has been deleted
- **THEN** the session starts with the remaining bases and the response identifies the dropped base

### Requirement: A save control message persists the session

The chat websocket SHALL accept a `save` control message with an optional title. On
receipt the daemon SHALL persist the session's current conversation to the chat store —
creating a new record on first save, updating in place (same id) thereafter — and SHALL
reply with a server frame carrying the saved chat's id and title. Title defaulting and
the empty-session rejection SHALL follow the `chat-history` capability. A store failure
SHALL be reported as an error frame without terminating the session.

#### Scenario: Saving over the websocket

- **WHEN** a client sends a `save` control message on a session with completed turns
- **THEN** the daemon persists the conversation and replies with a frame containing the chat id and title

#### Scenario: Save failure keeps the session alive

- **WHEN** the chat store is unavailable and a client sends `save`
- **THEN** the daemon replies with an error frame and the websocket session continues
