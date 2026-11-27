# Spec Delta

## ADDED Requirements

### Requirement: Retrieved sources are sent before the answer streams
When a prompt's retrieval supplies context, the daemon SHALL send a `sources` frame before the first
answer token, listing every document in that turn's context with its marker number, citation key,
label, link, locator, knowledge base, and label tag. A turn without retrieval SHALL send no `sources`
frame.

#### Scenario: Grounded prompt
- **WHEN** a prompt retrieves chunks from two documents
- **THEN** the client receives a `sources` frame listing both documents with their marker numbers before any `token` frame

#### Scenario: No active knowledge base
- **WHEN** a prompt is answered with no active knowledge base
- **THEN** no `sources` frame is sent for that answer

### Requirement: The terminal frame reports the cited sources
The terminal frame of each answer SHALL carry the answer's cited sources (after invalid markers are
removed), in first-cited order. When a grounded answer cites nothing, the terminal frame SHALL flag it
as unsourced and carry no sources; the streamed text stands.

#### Scenario: Cited answer completes
- **WHEN** a grounded answer citing `[2]` finishes streaming
- **THEN** the terminal frame lists only the `[2]` source

#### Scenario: Unsourced answer
- **WHEN** a grounded answer finishes streaming without citing any retrieved document
- **THEN** the terminal frame is flagged `unsourced`, carries no replacement content, and lists no sources
- **AND** the session history records the streamed answer
