# rest-api-chat Specification (delta)

## ADDED Requirements

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
