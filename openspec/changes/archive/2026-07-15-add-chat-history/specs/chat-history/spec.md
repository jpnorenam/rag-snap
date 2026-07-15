# chat-history Specification (delta)

## ADDED Requirements

### Requirement: Saved chats persist the conversation and its context

A saved chat SHALL contain everything needed to resume the conversation: a stable unique
id, a title, created and last-updated timestamps, the model name the session ran on, the
active knowledge-base set at save time, and the ordered conversation transcript (user and
assistant turns with their final content). Saved chats SHALL be stored locally on the
machine and SHALL NOT be transmitted anywhere other than the loopback/unix-socket API.

#### Scenario: A saved chat round-trips its context

- **WHEN** a user saves a chat with two turns and knowledge bases `default` and `docs` active
- **THEN** the stored chat contains both turns in order, the two base names, the model name, its title, and timestamps

#### Scenario: Reasoning content is not required to resume

- **WHEN** a session whose answers included `<think>` reasoning content is saved
- **THEN** the stored transcript contains the final answer content of each assistant turn
- **AND** resuming does not depend on the reasoning content being present

### Requirement: /save persists the current conversation

The chat surfaces (CLI REPL in both direct and remote modes, and the UI chat screen)
SHALL provide a `/save [title]` slash command. Invoking it SHALL persist the current
session's conversation to the chat store and confirm to the user with the saved chat's
title. When a title argument is given it SHALL be used verbatim; otherwise the title
SHALL be derived from the first user prompt (truncated to a display-friendly length).
Saving an empty session (no completed turns) SHALL be rejected with a message rather
than creating an empty record.

#### Scenario: Save with an explicit title

- **WHEN** the user enters `/save release planning notes` in an active chat
- **THEN** the conversation is persisted with the title "release planning notes"
- **AND** the user sees a confirmation naming the title

#### Scenario: Save derives a title from the first prompt

- **WHEN** the user enters `/save` with no argument in a chat whose first prompt was "How do I rotate the OpenSearch admin password?"
- **THEN** the chat is saved with a title derived from that prompt

#### Scenario: Saving an empty session is rejected

- **WHEN** the user enters `/save` before any completed turn
- **THEN** no chat is stored and the user is told there is nothing to save

### Requirement: Re-saving updates the same chat

Within one session, invoking `/save` again after further turns SHALL update the
previously saved chat in place (same id, updated transcript and timestamp) rather than
creating a duplicate. A session resumed from a saved chat SHALL inherit that chat's id,
so saving after resuming also updates the original record. Providing a new title on a
re-save SHALL rename the chat.

#### Scenario: Second save updates in place

- **WHEN** the user saves a chat, continues for two more turns, and saves again
- **THEN** the store contains one chat with all turns and an updated last-updated timestamp

#### Scenario: Save after resume updates the original

- **WHEN** the user resumes a saved chat, asks a follow-up, and enters `/save`
- **THEN** the original saved chat is updated with the new turn

### Requirement: /history searches saved chats and resumes one

The chat surfaces SHALL provide a `/history` slash command that lists saved chats newest
first and lets the user filter them by typing — matching case-insensitively against
title and transcript content — then select one to resume. Each listed entry SHALL show
at least the title and a relative last-updated time. Selecting a chat SHALL resume it;
cancelling the picker SHALL leave the current session untouched. When no saved chats
exist, the user SHALL see a message explaining how to save one instead of an empty
picker.

#### Scenario: Filtering the history

- **WHEN** the user enters `/history` and types "opensearch"
- **THEN** the list narrows to chats whose title or transcript matches "opensearch" case-insensitively

#### Scenario: Cancelling keeps the current session

- **WHEN** the user opens `/history` and dismisses the picker without selecting
- **THEN** the current conversation and active knowledge bases are unchanged

#### Scenario: Empty history

- **WHEN** the user enters `/history` and no chats have been saved
- **THEN** a message explains that `/save` stores the current chat, and no picker is shown

### Requirement: Resuming restores transcript and knowledge-base context

Resuming a saved chat SHALL restore the conversation history as generation context (the
next answer can refer to earlier turns) and SHALL restore the saved active knowledge-base
set. The restored transcript SHALL be displayed to the user so they can see where the
conversation left off. A saved knowledge base that no longer exists SHALL be dropped from
the active set with a notice, not treated as a fatal error. The session's model SHALL be
resolved the same way as for a fresh session; if it differs from the saved model name the
user SHALL be informed.

#### Scenario: Follow-up uses restored history

- **WHEN** the user resumes a chat that discussed a specific error message and asks "what was the fix again?"
- **THEN** the assistant answers from the restored conversation history without the user restating the error

#### Scenario: Missing knowledge base degrades gracefully

- **WHEN** a resumed chat's saved base list names a knowledge base that has since been deleted
- **THEN** the session starts with the remaining bases active and the user is notified of the dropped base

### Requirement: History commands are registered slash commands

`/save` and `/history` SHALL be registered in the REPL slash-command registry so they
participate in autocomplete, the as-you-type hint list, argument syntax ghost text
(`/save` shows `[title]`), and the unknown-command help listing — in both direct and
remote REPL modes.

#### Scenario: Slash hints include the new commands

- **WHEN** the user types `/` in the chat REPL
- **THEN** the hint list includes `/save` and `/history` alongside the existing commands

### Requirement: Store location follows the daemon/daemonless split

Sessions running through ragd (the UI and the CLI's remote mode) SHALL read and write
saved chats in the daemon-owned store so both surfaces see one shared history. The
direct (daemonless) CLI REPL SHALL fall back to a client-local store in the user's
config directory, mirroring the prompt-store fallback; its `/history` lists the local
store. Store unavailability (e.g. unwritable path) SHALL degrade with a clear error on
`/save` while leaving the chat itself functional.

#### Scenario: UI and remote CLI share history

- **WHEN** a user saves a chat from the UI and then runs `/history` in the CLI remote REPL
- **THEN** the chat saved from the UI appears in the list

#### Scenario: Daemonless REPL uses the local store

- **WHEN** the direct REPL runs without a reachable ragd and the user saves a chat
- **THEN** the chat is written to the client-local store and appears in the direct REPL's `/history`

#### Scenario: Store failure does not kill the session

- **WHEN** the store cannot be written and the user enters `/save`
- **THEN** an error message is shown and the conversation continues normally
