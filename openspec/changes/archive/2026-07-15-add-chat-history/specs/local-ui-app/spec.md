# local-ui-app Specification (delta)

## ADDED Requirements

### Requirement: Chat composer supports slash commands

The chat screen's composer SHALL recognize input starting with `/` as a slash command
instead of a prompt, supporting `/save [title]` and `/history` with the same semantics as
the REPL. While the input starts with `/`, the composer SHALL show a filtering hint list
of matching commands (mirroring the REPL's slash hints) that can be navigated and
accepted by keyboard. An unknown slash command SHALL show the list of available commands
inline without sending anything to the daemon. `/save` SHALL send the `save` control
message over the open chat websocket and surface the returned title in a positive
notification; saving with no completed turns SHALL surface the rejection message.

#### Scenario: Composer hints while typing a slash command

- **WHEN** the user types `/` in the chat composer
- **THEN** a hint list shows the available slash commands, narrowing as the user types
- **AND** the highlighted command can be accepted from the keyboard

#### Scenario: Saving from the composer

- **WHEN** the user submits `/save release notes` during an active session
- **THEN** the UI sends the `save` control message with that title over the websocket
- **AND** shows a positive notification naming the saved title

#### Scenario: Unknown command stays local

- **WHEN** the user submits `/frobnicate`
- **THEN** the UI lists the available slash commands and sends nothing over the websocket

### Requirement: Chat history panel lists, searches, and resumes saved chats

Submitting `/history` (or activating an equivalent History control on the chat screen)
SHALL open a history panel listing saved chats from `GET /1.0/chats` newest-first, each
row showing title, relative last-updated time, model, and turn count. A search box SHALL
filter the list via the endpoint's `search` parameter. Selecting a chat SHALL resume it:
the UI starts a new session via `POST /1.0/chat` with the saved-chat id, replaces the
transcript view with the restored conversation, and applies the restored knowledge-base
selection to the chips. The panel SHALL be keyboard-operable and close on Escape without
side effects. When no chats exist the panel SHALL show the shared empty-state component
including the `/save` guidance.

#### Scenario: Resuming from the panel

- **WHEN** the user opens the history panel and selects a saved chat
- **THEN** the UI issues `POST /1.0/chat` with that chat's id, renders the restored transcript, and updates the active-base chips to the saved set
- **AND** the next prompt continues that conversation

#### Scenario: Searching the panel

- **WHEN** the user types "bedrock" in the panel's search box
- **THEN** the list refreshes via `GET /1.0/chats?search=bedrock` and shows only matching chats

#### Scenario: Dismissing the panel

- **WHEN** the panel is open and the user presses Escape
- **THEN** the panel closes and the current conversation is unchanged

#### Scenario: Empty history state

- **WHEN** no chats have been saved and the panel opens
- **THEN** the shared empty-state component explains saving with `/save`

### Requirement: Saved chats can be deleted from the history panel

Each history panel row SHALL offer a delete action routed through the shared confirm
modal (never `window.confirm`), naming the chat title in the confirmation body.
Confirming SHALL issue `DELETE /1.0/chats/{id}` and remove the row; a failed deletion
SHALL surface the API error without removing the row. Deleting a chat SHALL NOT
interrupt the live session, including when the live session was resumed from that chat.

#### Scenario: Deleting a saved chat

- **WHEN** the user activates delete on a history row and confirms in the modal
- **THEN** the UI issues `DELETE /1.0/chats/{id}` and the row disappears from the panel

#### Scenario: Failed deletion keeps the row

- **WHEN** the delete request fails
- **THEN** the row remains and the API error message is shown
