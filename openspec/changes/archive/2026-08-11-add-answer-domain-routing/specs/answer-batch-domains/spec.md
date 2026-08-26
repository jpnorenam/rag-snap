## ADDED Requirements

### Requirement: Manifest declares optional domain routing

The batch manifest SHALL accept an optional top-level `domains` list. Each entry SHALL carry a
`match` pattern, and at least one of `context` (prose describing the requirement domain) and
`keywords` (a list of retrieval terms). An entry MAY carry both. An entry carrying neither SHALL be
rejected as a validation error. Two entries SHALL NOT declare the same `match` pattern; a manifest
that does SHALL be rejected, so that the domain recorded on a result identifies exactly one entry.

Validation of the whole list SHALL complete before the first question is answered, so a malformed
block costs no model calls.

A manifest with no `domains` list SHALL behave exactly as a manifest did before this capability
existed: the system prompt, the question turn, and the retrieval queries SHALL be unchanged. Domain
routing SHALL be manifest data only — it SHALL NOT introduce configuration keys and SHALL NOT read
or write the config layer.

Both the CLI's direct run path and the daemon path SHALL apply domain routing identically, since both
drive the same batch pipeline.

#### Scenario: Manifest without a domains list

- **WHEN** a batch manifest carrying no `domains` list is run
- **THEN** each question's system prompt, question turn, and retrieval queries are identical to those produced before this capability existed

#### Scenario: Entry with neither context nor keywords

- **WHEN** a manifest declares a `domains` entry with a `match` but no `context` and no `keywords`
- **THEN** the run is rejected with a validation error and no question is answered

#### Scenario: Duplicate match patterns

- **WHEN** a manifest declares two `domains` entries with the same `match` pattern
- **THEN** the run is rejected with a validation error and no question is answered

#### Scenario: Validation precedes the first answer

- **WHEN** a manifest's `domains` list is invalid
- **THEN** the failure is reported before any question is answered and no model call is made

#### Scenario: Keywords-only entry

- **WHEN** a `domains` entry carries `keywords` but no `context`
- **THEN** matching questions inherit its keywords for retrieval
- **AND** no domain preamble is added to those questions' turns

### Requirement: Domain resolution is deterministic and longest-match-wins

Each question SHALL be resolved against the `domains` list before it is answered. A pattern SHALL
match by glob, supporting `*` (any run of characters) and `?` (exactly one character), compared
case-insensitively.

Where more than one entry matches a question, the entry with the greatest number of literal
(non-wildcard) characters in its `match` SHALL win. Where two matching entries tie on literal
length, the earlier entry in document order SHALL win. A question matching no entry SHALL resolve to
no domain and SHALL be answered as it is today.

Resolution SHALL be performed in code rather than delegated to the language model, so that the
mapping from question to domain is exact and reproducible.

#### Scenario: More specific pattern wins over a broader one

- **WHEN** a manifest declares both `match: "J*"` and `match: "J1.*"`, and a question has id `J1.3`
- **THEN** the `J1.*` entry is applied
- **AND** the `J*` entry is not

#### Scenario: Catch-all pattern

- **WHEN** a manifest declares `match: "*"` and a question's id matches no other entry
- **THEN** the catch-all entry is applied

#### Scenario: Catch-all never outranks a specific pattern

- **WHEN** a manifest declares both `match: "*"` and `match: "C*"`, and a question has id `C4`
- **THEN** the `C*` entry is applied

#### Scenario: Case-insensitive matching

- **WHEN** a manifest declares `match: "gis*"` and a question has id `GIS3`
- **THEN** the entry is applied

#### Scenario: Tie broken by document order

- **WHEN** two matching entries have the same number of literal characters in their patterns
- **THEN** the entry declared earlier in the manifest is applied

#### Scenario: No matching entry

- **WHEN** a question's id matches no `domains` entry
- **THEN** no domain is applied and the question is answered without a domain preamble or inherited keywords

### Requirement: Question source round-trips and serves as a fallback match key

The batch manifest's `questions[].source` field SHALL be read and retained rather than discarded.
Where a question's `id` is a bare sequence number carrying no domain prefix, resolution SHALL fall
back to matching the question's `source` value. A question whose `id` matches an entry SHALL NOT be
re-matched against its `source`.

`source` SHALL round-trip unchanged through every surface that carries a manifest, so that a manifest
produced by document extraction and then submitted for a run retains the field.

#### Scenario: Bare numeric id falls back to source

- **WHEN** a question has id `17` and source `C`, and the manifest declares `match: "C*"`
- **THEN** the `C*` entry is applied by matching the source

#### Scenario: Id match takes precedence over source

- **WHEN** a question's id matches one entry and its source would match a different entry
- **THEN** the entry matched by id is applied

#### Scenario: Source survives a round trip

- **WHEN** a manifest carrying `questions[].source` is submitted for a run
- **THEN** the source value is preserved rather than dropped

### Requirement: Resolved domain is injected into the question turn

A resolved domain's `context` SHALL be supplied to the model in the per-question turn, together with
the question's `id`. It SHALL NOT be appended to or merged into the batch system prompt.

Consequently, the system prompt sent for every question in a batch SHALL be byte-identical, so that
it remains a stable cacheable prefix across the run. A question that resolves to no domain SHALL
receive a turn containing no domain preamble.

The fixed no-context answer behaviour SHALL be unchanged: a question that retrieves no grounding
context SHALL still receive the fixed "not enough information" response without a model call,
regardless of whether a domain resolved for it.

#### Scenario: Domain reaches the model

- **WHEN** a question resolves to a domain
- **THEN** the turn sent to the model states the domain's context and the question's id alongside the question text

#### Scenario: System prompt is stable across the batch

- **WHEN** a batch runs with several questions resolving to different domains
- **THEN** the system prompt sent for each question is identical

#### Scenario: Unresolved question gets no preamble

- **WHEN** a question resolves to no domain
- **THEN** its turn carries no domain preamble

#### Scenario: No retrieved context still short-circuits

- **WHEN** a question resolves to a domain but retrieves no grounding context
- **THEN** its answer is the fixed "not enough information" response and no model call is made

### Requirement: Domain keywords are inherited by retrieval

A resolved domain's `keywords` SHALL be merged into the lexical retrieval query for the question, and
into the query used to steer semantic retrieval, through the same merge the manifest's per-question
keywords already use.

Priority SHALL be per-question `keywords` first, then the resolved domain's `keywords`, then the
model-generated keywords. Duplicates SHALL be removed case-insensitively, preserving the first
occurrence.

#### Scenario: Domain keywords steer retrieval

- **WHEN** a question resolves to a domain carrying keywords
- **THEN** those keywords are part of the lexical retrieval query and of the query steering semantic retrieval

#### Scenario: Per-question keywords outrank domain keywords

- **WHEN** a question declares its own `keywords` and also resolves to a domain carrying keywords
- **THEN** the question's own keywords lead, followed by the domain's, followed by the generated keywords

#### Scenario: Duplicate keywords are collapsed

- **WHEN** a domain keyword duplicates a per-question keyword differing only in case
- **THEN** the term appears once, in the position of its first occurrence

### Requirement: Results record the applied domain

Batch results SHALL record, per question, which domain was applied — identified by the matched
pattern — alongside the existing per-question question and answer fields. A question that resolved
to no domain SHALL record no domain rather than a placeholder.

This provenance SHALL be present wherever batch results are made available, so an operator can
confirm from the output that routing fired as intended rather than inferring it from the answers.

#### Scenario: Applied domain appears in results

- **WHEN** a batch completes and a question resolved to a domain
- **THEN** that question's result records the matched pattern

#### Scenario: Unresolved question records no domain

- **WHEN** a batch completes and a question resolved to no domain
- **THEN** that question's result carries no domain value

### Requirement: Every manifest surface carries domain routing

Every surface that reads, forwards, or writes a batch manifest SHALL carry the `domains` list and
`questions[].source` through intact. No surface SHALL accept a manifest carrying `domains` and then
run it with the routing silently discarded, and no surface SHALL drop the block when writing a
manifest back out.

A manifest that runs through one surface SHALL route identically through every other, so an operator
gets the same answers whether a batch is started from the command line or from the web UI. A surface
that rejects a `domains` block SHALL do so only because the block is invalid, reporting the same
validation failures — an entry with neither `context` nor `keywords`, or a duplicate `match` — and
never because the surface cannot represent it.

#### Scenario: Routing survives every surface

- **WHEN** a manifest carrying a `domains` list is submitted through any surface that starts a batch
- **THEN** the routing reaches the run rather than being dropped in transit

#### Scenario: Identical routing across surfaces

- **WHEN** the same manifest is run from the command line and from the web UI
- **THEN** each question resolves to the same domain in both runs

#### Scenario: Writing a manifest preserves routing

- **WHEN** a surface writes a batch manifest that carries domain routing
- **THEN** the written manifest still carries the `domains` list and any `questions[].source` values

#### Scenario: Rejection only on invalid input

- **WHEN** a surface rejects a manifest carrying a `domains` list
- **THEN** the rejection names an invalid entry or a duplicate pattern, not an unsupported feature

### Requirement: Document extraction emits a domains stub

The document-to-manifest build flow SHALL emit a commented-out `domains:` stub in the manifest it
writes, derived from the distinct question-id prefixes and source values it observed during
extraction. The stub SHALL be inert until the operator uncomments and fills it in, so a freshly built
manifest runs unchanged. Every surface that writes a built manifest SHALL emit the stub, so a
manifest built from a document is equally ready to route however it was produced.

#### Scenario: Built manifest carries a stub

- **WHEN** questions are extracted from a document and a manifest is written
- **THEN** the manifest contains a commented `domains:` stub listing the observed id prefixes and sources

#### Scenario: Stub does not alter a run

- **WHEN** a freshly built manifest is run without editing the stub
- **THEN** no domain is applied to any question

#### Scenario: Stub is emitted regardless of which surface built the manifest

- **WHEN** a manifest is built from a document through any surface offering the build flow
- **THEN** the written manifest carries the commented stub
