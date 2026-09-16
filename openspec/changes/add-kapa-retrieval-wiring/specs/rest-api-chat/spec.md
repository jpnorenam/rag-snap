## ADDED Requirements

### Requirement: Active kapa source groups are set via the session

The client SHALL be able to set or change the session's active kapa.ai source groups through a
control message on the chat connection, the API equivalent of the in-REPL `/use-kapa`. Retrieval for
subsequent prompts SHALL use the current selection, alongside the session's active knowledge bases.
The selection SHALL be independent of the active knowledge bases: selecting source groups SHALL NOT
change the active bases, and selecting bases SHALL NOT clear the source groups.

A session SHALL start with no source groups selected, so a session never begins by querying an entire
kapa project. When the integration is unconfigured, a session SHALL report that a selection cannot be
applied rather than accepting it silently, as defined by `kapa-retrieval`.

#### Scenario: Selecting active source groups

- **WHEN** a client sends a control message selecting one or more kapa source groups
- **THEN** subsequent prompts retrieve kapa context from exactly those groups

#### Scenario: Changing the selection mid-session

- **WHEN** a client changes the selected source groups partway through a session
- **THEN** prompts after the change use the new selection

#### Scenario: Selection is independent of knowledge bases

- **WHEN** a client changes the active knowledge bases while source groups are selected
- **THEN** the source-group selection is unchanged and both continue to feed retrieval

#### Scenario: Session starts with nothing selected

- **WHEN** a client starts a chat session without selecting source groups
- **THEN** no kapa retrieval occurs until a selection is made

#### Scenario: Selecting with an unconfigured integration

- **WHEN** a client selects source groups and the daemon has no kapa credentials
- **THEN** the session reports that the selection cannot be applied
- **AND** subsequent prompts continue to retrieve from the active knowledge bases
