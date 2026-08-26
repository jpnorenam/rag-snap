## ADDED Requirements

### Requirement: Batch manifests are read with a full YAML reader

The UI SHALL parse batch manifests with a reader that handles the same YAML the CLI accepts, rather
than a subset. It SHALL correctly read nested block sequences of mappings, block scalar values
(`|` and `>`), and both inline and block sequence forms — so a manifest authored for the CLI is read
by the UI with the same meaning.

In particular, a top-level `prompt` written as a block scalar SHALL be read with its full body. A
manifest SHALL NOT be accepted with a silently truncated or substituted value for any field the UI
recognises; a manifest the reader cannot interpret SHALL surface a field-level validation error and
SHALL NOT be sent to the API.

Unknown fields SHALL continue to be tolerated rather than rejected, matching the CLI's lenient
decoding, but a recognised field SHALL never be read into a wrong value.

#### Scenario: Block scalar prompt is read in full

- **WHEN** a user supplies a manifest whose `prompt` is written as a block scalar
- **THEN** the preview and the submitted run carry the prompt's full body

#### Scenario: Nested mappings are read

- **WHEN** a user supplies a manifest containing a nested block sequence of mappings
- **THEN** the reader interprets its entries rather than skipping them

#### Scenario: Unknown fields still tolerated

- **WHEN** a manifest carries a field the UI does not recognise
- **THEN** the manifest is still accepted and the unknown field does not cause a validation error

### Requirement: Domain routing is carried and previewed before a run

The UI SHALL read a manifest's `domains` list and `questions[].source` values, include them in the
manifest it posts to the batch API, and show the routing in the pre-run preview alongside the
knowledge bases and question list.

The preview SHALL show, per domain entry, its `match` pattern, its context, and how many of the
manifest's questions that entry would apply to — so an operator can see that routing covers the rows
they expect before spending a run. A question the routing would not reach SHALL be identifiable in the
preview.

The preview's resolution SHALL be advisory: the run's applied domain, as recorded in the results, is
authoritative. Where the UI's own validation of the `domains` list fails — an entry with neither
`context` nor `keywords`, or a duplicate `match` — it SHALL surface a field-level validation error and
SHALL NOT call the batch API.

#### Scenario: Routing appears in the preview

- **WHEN** a user supplies a manifest carrying a `domains` list
- **THEN** the preview shows each entry's pattern, its context, and the number of questions it would apply to

#### Scenario: Unrouted questions are visible

- **WHEN** a manifest's routing leaves some questions matching no entry
- **THEN** the preview identifies those questions

#### Scenario: Routing is posted with the run

- **WHEN** a user runs a previewed manifest carrying domain routing
- **THEN** the posted manifest includes the `domains` list and any `questions[].source` values

#### Scenario: Invalid routing is rejected client-side

- **WHEN** a manifest's `domains` list contains an entry with neither context nor keywords, or a duplicate pattern
- **THEN** the UI shows a validation error and does not call the batch API

### Requirement: Applied domain is shown in results review

The batch results review surface SHALL show the domain applied to each question when the results
record one, and SHALL render a question that resolved to no domain without a placeholder. The domain
SHALL be presented as part of the question's card, distinct from the answer text and from the
collapsible provenance section.

Results files that carry no domain values — those produced before domain routing existed — SHALL
render unchanged, so the surface stays backward compatible with previously exported files.

#### Scenario: Domain shown on a card

- **WHEN** a result set records an applied domain for a question
- **THEN** that question's card shows the domain

#### Scenario: Unrouted question card

- **WHEN** a result set records no domain for a question
- **THEN** that question's card shows no domain and no placeholder

#### Scenario: Older results file still renders

- **WHEN** a user opens a previously exported results file that carries no domain values
- **THEN** the review surface renders it without error

### Requirement: Manifests written by the UI carry routing and a stub

A manifest the UI serialises SHALL carry any `domains` list and `questions[].source` values it holds,
using YAML forms the CLI reads back with the same meaning — including a block scalar where a value
spans multiple lines.

A manifest the UI's document build flow writes SHALL carry the same commented `domains:` stub the CLI
emits, so a manifest built in the UI is as ready to route as one built on the command line.

#### Scenario: Serialised manifest round-trips routing

- **WHEN** the UI serialises a manifest carrying domain routing
- **THEN** re-reading the output yields the same routing

#### Scenario: Multi-line values survive serialisation

- **WHEN** the UI serialises a manifest whose prompt or domain context spans multiple lines
- **THEN** the output preserves the line structure and re-reads to the same value

#### Scenario: UI-built manifest carries the stub

- **WHEN** a user builds a manifest from a document in the UI
- **THEN** the written manifest carries the commented `domains:` stub
