# rest-api-chats Specification (delta)

## ADDED Requirements

### Requirement: Saved chats are a listable REST resource

The API SHALL provide `GET /1.0/chats` returning saved chat summaries newest-first by
last-updated time. A summary SHALL include the chat's id, title, created and last-updated
timestamps, model name, saved knowledge-base set, and turn count — but NOT the full
transcript. An optional `search` query parameter SHALL filter results by case-insensitive
substring match against title and transcript content. The endpoints SHALL follow the
daemon's standard response envelope and require the same authentication as other `/1.0`
resources.

#### Scenario: Listing saved chats

- **WHEN** a client sends `GET /1.0/chats` with three chats saved
- **THEN** the response lists three summaries ordered newest-first, each with id, title, timestamps, model, bases, and turn count

#### Scenario: Server-side search filter

- **WHEN** a client sends `GET /1.0/chats?search=tika`
- **THEN** only chats whose title or transcript contains "tika" (case-insensitive) are returned

#### Scenario: Empty store lists cleanly

- **WHEN** no chats have been saved and a client sends `GET /1.0/chats`
- **THEN** the response is a successful sync response with an empty list

### Requirement: A saved chat can be fetched with its transcript

The API SHALL provide `GET /1.0/chats/{id}` returning the full saved chat, including the
ordered transcript. An unknown id SHALL return a 404 error response.

#### Scenario: Fetching a chat

- **WHEN** a client sends `GET /1.0/chats/{id}` for an existing chat
- **THEN** the response contains the summary fields plus the full ordered transcript

#### Scenario: Unknown id

- **WHEN** a client requests a chat id that does not exist
- **THEN** the API returns a 404 error response

### Requirement: Saved chats can be deleted

The API SHALL provide `DELETE /1.0/chats/{id}` removing the saved chat. Deleting an
unknown id SHALL return 404. Deletion SHALL NOT affect any live session that was resumed
from that chat; a later `/save` from such a session SHALL recreate the record under the
same id.

#### Scenario: Deleting a chat

- **WHEN** a client sends `DELETE /1.0/chats/{id}` for an existing chat
- **THEN** the chat no longer appears in `GET /1.0/chats`

#### Scenario: Deleting does not break a resumed session

- **WHEN** a session resumed from a chat is live and that chat is deleted
- **THEN** the session continues unaffected and a subsequent save recreates the stored chat

### Requirement: Chat store persists under $SNAP_COMMON

The daemon SHALL persist saved chats under `$SNAP_COMMON/ragd/` (alongside the prompt
store) so they survive snap refreshes and revision rollbacks. Each chat SHALL be stored
as its own file keyed by id, so one corrupt file cannot take down the whole store: a
chat that fails to parse SHALL be skipped from listings with a logged warning, never a
failed request. If the store path cannot be resolved or created, saving SHALL fail with
a clear error while all other chat functionality keeps working.

#### Scenario: Chats survive a snap refresh

- **WHEN** chats are saved and the snap is refreshed to a new revision
- **THEN** `GET /1.0/chats` still lists the saved chats

#### Scenario: One corrupt file does not break listing

- **WHEN** one stored chat file is corrupt and a client sends `GET /1.0/chats`
- **THEN** the remaining chats are listed successfully and the corrupt file is logged and skipped
