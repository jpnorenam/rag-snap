# knowledge-labels Specification (delta)

## ADDED Requirements

### Requirement: Sources carry exactly one user-defined label

Every ingested source SHALL carry exactly one label, resolved at ingest time with this precedence: an explicit label given for the ingest (CLI `k ingest --label <label>`, a `label:` field on a batch manifest job, or a `label` field on an API ingest request) wins; otherwise the target base's default label applies. The resolved label SHALL be stored in the source's metadata record and SHALL be denormalized onto every chunk document indexed for that source (a `label` keyword field), so search hits carry the label without a metadata lookup and export/import moves labels with the data.

Label values SHALL match `^[a-z0-9][a-z0-9-]{0,31}$`; an invalid label SHALL be rejected before any ingestion starts.

#### Scenario: Explicit label at ingest

- **WHEN** a user runs `k ingest mybase doc-1 --file f.pdf --label internal`
- **THEN** the source's metadata records label `internal`
- **AND** every chunk indexed for `doc-1` carries `label: internal`

#### Scenario: Base default applies when no label is given

- **WHEN** a user ingests into a base whose default label is `partner` without passing `--label`
- **THEN** the source and its chunks are labeled `partner`

#### Scenario: Batch manifest jobs can set labels

- **WHEN** a batch manifest job includes `label: upstream`
- **THEN** that job's source and chunks are labeled `upstream` regardless of the base default

#### Scenario: Invalid label is rejected

- **WHEN** a user passes `--label "Not Valid!"`
- **THEN** the command fails with a validation error naming the allowed format and no ingestion is performed

### Requirement: Knowledge bases have a default label

A knowledge base SHALL have a default label stored in its index mapping `_meta` so it travels with `k export`/`k import`. `k create <name> --label <label>` SHALL set it at creation. When unset, the effective default SHALL follow the legacy convention: `upstream` when the base name contains "upstream", otherwise `canonical`.

A `k label <base>` command SHALL show the base's effective default label and whether it is stored or convention-derived; `k label <base> <label>` SHALL set it. Changing the default SHALL only affect future ingests unless backfill is requested.

#### Scenario: Creating a base with a default label

- **WHEN** a user runs `k create partner-docs --label partner`
- **THEN** the base's default label is `partner` and subsequent ingests without `--label` use it

#### Scenario: Legacy convention supplies the default

- **WHEN** a user inspects an existing base named `kubernetes-upstream` that has no stored default label
- **THEN** `k label kubernetes-upstream` reports the effective default `upstream` as convention-derived

#### Scenario: Default label survives export/import

- **WHEN** a base with stored default label `partner` is exported and imported on another machine
- **THEN** the imported base's default label is `partner`

### Requirement: Retrieval resolves labels from stored data with a legacy fallback

A single label resolver SHALL be used by every consumer (chat REPL retrieval and `/search`, `k search`, the daemon search endpoint, and remote clients). For each hit it SHALL return the chunk's stored `label` when present; for chunks without one it SHALL fall back to index-name inference (`upstream` when the index name contains "upstream", otherwise `canonical`). Kapa.ai hits, which are fetched live and never indexed, SHALL carry the fixed implicit label `kapa-canonical`.

#### Scenario: Stored label wins

- **WHEN** a search hit's chunk carries `label: internal` in an index named `docs-upstream`
- **THEN** the resolved label is `internal`, not `upstream`

#### Scenario: Unlabeled legacy chunks keep today's behavior

- **WHEN** a hit comes from a chunk with no `label` field in an index whose name contains "upstream"
- **THEN** the resolved label is `upstream`

#### Scenario: Kapa hits are implicitly labeled

- **WHEN** retrieval includes results from the kapa.ai integration
- **THEN** those hits carry the label `kapa-canonical`

### Requirement: RAG context tags chunks with the resolved label

When building the RAG context for chat and batch answer, each chunk SHALL be prefixed with its resolved label rendered as an uppercase bracketed tag (e.g. label `internal` → `[INTERNAL]`). The system SHALL NOT attach any built-in meaning or priority to labels beyond this tagging: label semantics are expressed in the user's system prompts via the existing prompt-variant mechanism. The compiled-in default prompts SHALL continue to reference the default label set (`[CANONICAL]`, `[KAPA-CANONICAL]`, `[UPSTREAM]`) so stock behavior is unchanged.

#### Scenario: Custom labels appear in the LLM context

- **WHEN** a chat turn retrieves chunks labeled `internal` and `upstream`
- **THEN** the injected context tags them `[INTERNAL]` and `[UPSTREAM]` respectively

#### Scenario: Custom prompts can rank custom labels

- **WHEN** a user activates a prompt variant stating `[INTERNAL]` overrides `[UPSTREAM]` and both tags appear in context
- **THEN** the model receives both the tags and the user's priority rules, with no system-injected label semantics

#### Scenario: Stock setup is unchanged

- **WHEN** a user with default prompts and unlabeled bases runs a chat turn
- **THEN** the context tags and prompt rules are identical to the pre-labels behavior

### Requirement: Existing chunks can be backfilled with the base default

`k label <base> <label> --apply-to-existing` SHALL, after storing the new default: ensure the index mapping has the `label` keyword field, then set the label on existing chunks **that lack one** and on the base's source metadata records that lack one. Chunks and sources that already carry a label SHALL NOT be overwritten. The equivalent API action SHALL run asynchronously as an operation.

#### Scenario: Backfilling unlabeled chunks

- **WHEN** a user runs `k label mybase internal --apply-to-existing` on a base with pre-labels chunks
- **THEN** all chunks and source records without a label are set to `internal`
- **AND** searches over that base now resolve `internal` from stored data

#### Scenario: Explicit per-source labels survive backfill

- **WHEN** a source was ingested with `--label restricted` and the base is later backfilled with `internal`
- **THEN** that source's chunks and metadata still carry `restricted`

### Requirement: Labels are visible in source listings

`k sources <base>` and source metadata inspection SHALL display each source's label. Sources ingested before this capability (no stored label) SHALL display the effective fallback label.

#### Scenario: Listing sources shows labels

- **WHEN** a user runs `k sources mybase`
- **THEN** each source row shows its label

#### Scenario: Legacy sources show the fallback

- **WHEN** a listed source has no stored label in a base named `ceph-upstream`
- **THEN** its displayed label is `upstream`
