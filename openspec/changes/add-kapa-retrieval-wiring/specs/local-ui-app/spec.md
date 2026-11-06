## ADDED Requirements

### Requirement: Kapa source groups are selectable on the answer-batch screen

The answer-batch screen SHALL let a user select kapa.ai source groups for a run, listing the
project's groups by display name from the API rather than requiring the user to type group
identifiers. The selection SHALL be carried on the manifest the UI submits and SHALL be visible in
the pre-run preview alongside the run's other scope, so a user can confirm what a run will ground
against before starting it.

The screen SHALL distinguish the API's unconfigured, empty, loading, and error conditions rather than
rendering an empty picker for all of them: an unconfigured integration SHALL show a hint that
credentials are required, and a failure SHALL be surfaced as an error. A manifest that already
selects source groups SHALL keep them when the integration cannot be listed, so an unreachable API
never silently strips a selection the user made earlier.

#### Scenario: Selecting source groups for a run

- **WHEN** a user opens the answer-batch screen with kapa configured
- **THEN** the project's source groups are listed by display name and can be selected
- **AND** the selection appears in the pre-run preview

#### Scenario: Integration not configured

- **WHEN** the API reports the integration as unconfigured
- **THEN** the screen shows a hint that credentials are required instead of an empty picker

#### Scenario: Listing fails

- **WHEN** the source-group listing request fails
- **THEN** the screen surfaces an error rather than an empty selection

#### Scenario: Existing selection survives a failed listing

- **WHEN** a loaded manifest already selects source groups and the listing cannot be retrieved
- **THEN** the manifest's selection is preserved and submitted with the run

## MODIFIED Requirements

### Requirement: Manifests written by the UI carry routing and a stub

A manifest the UI serialises SHALL carry any `domains` list, `questions[].source` values, and
`kapa_source_groups` selection it holds, using YAML forms the CLI reads back with the same meaning —
including a block scalar where a value spans multiple lines.

A manifest the UI's document build flow writes SHALL carry the same commented `domains:` stub the CLI
emits, so a manifest built in the UI is as ready to route as one built on the command line.

#### Scenario: Serialised manifest round-trips routing

- **WHEN** the UI serialises a manifest carrying domain routing
- **THEN** re-reading the output yields the same routing

#### Scenario: Serialised manifest round-trips kapa source groups

- **WHEN** the UI serialises a manifest carrying a `kapa_source_groups` selection
- **THEN** re-reading the output yields the same selection

#### Scenario: Multi-line values survive serialisation

- **WHEN** the UI serialises a manifest whose prompt or domain context spans multiple lines
- **THEN** the output preserves the line structure and re-reads to the same value

#### Scenario: UI-built manifest carries the stub

- **WHEN** a user builds a manifest from a document in the UI
- **THEN** the written manifest carries the commented `domains:` stub
