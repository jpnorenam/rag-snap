# Spec Delta

## ADDED Requirements

### Requirement: Saved assistant turns carry their cited sources
A saved chat's assistant turns SHALL store the sources the answer cited, each with its marker number,
citation key, label, link, locator, knowledge base, and label tag. User turns and uncited assistant
turns SHALL store no sources. Chats saved before this field existed SHALL load as turns without
sources.

#### Scenario: Saving a cited answer
- **WHEN** a session whose last answer cited `[1]` and `[2]` is saved
- **THEN** that assistant turn in the saved chat lists both sources with their marker numbers, labels, and links

#### Scenario: Loading an older saved chat
- **WHEN** a chat saved before sources were recorded is listed, fetched, or resumed
- **THEN** it loads successfully and its turns have no sources

### Requirement: Resuming restores sources and marker numbering
Resuming a saved chat SHALL display each restored assistant turn with its sources, and SHALL restore
the session's marker numbering so a previously cited document keeps its number and a newly retrieved
document receives a number greater than any number in the saved chat.

#### Scenario: Restored transcript shows sources
- **WHEN** a saved chat with cited assistant turns is resumed in the REPL or the UI
- **THEN** each restored assistant turn is shown with its sources

#### Scenario: Previously cited document re-retrieved
- **WHEN** a resumed chat's saved turns cited `guide.pdf` as `[2]` and the next turn retrieves `guide.pdf` again
- **THEN** `guide.pdf` is marked `[2]` in the new turn
