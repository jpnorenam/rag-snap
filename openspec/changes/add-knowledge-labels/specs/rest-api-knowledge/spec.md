# rest-api-knowledge Specification (delta)

## ADDED Requirements

### Requirement: Base default label can be read and set via the API

The base detail payload SHALL include the base's effective default label and whether it is stored or convention-derived. The API SHALL allow setting a base's default label. When the set request includes an `apply_to_existing` flag, the API SHALL run the backfill (labeling only chunks and source records that lack a label) as an asynchronous operation, since it rewrites bulk data.

#### Scenario: Reading the default label

- **WHEN** a client requests `GET /1.0/knowledge/<name>`
- **THEN** the response includes the base's effective default label

#### Scenario: Setting the default label with backfill

- **WHEN** a client sets a base's default label with `apply_to_existing` enabled
- **THEN** the default is stored and an asynchronous operation backfills unlabeled chunks and source records

## MODIFIED Requirements

### Requirement: List and create knowledge bases

The API SHALL provide `GET /1.0/knowledge` to list knowledge bases and
`POST /1.0/knowledge` to create one. Listing and creation SHALL be synchronous. The list
response for each base SHALL include a `source_count` (the number of ingested sources)
alongside the index-level statistics, computed server-side in a single aggregation so clients
do not need a per-base fan-out.

The create request SHALL accept an optional `default_label` (validated against the
knowledge-labels format); when omitted, the base's effective default follows the legacy
convention (`upstream` when the name contains "upstream", otherwise `canonical`).

#### Scenario: Listing knowledge bases

- **WHEN** a client requests `GET /1.0/knowledge`
- **THEN** the response synchronously lists the existing knowledge bases
- **AND** each listed base includes its `source_count`

#### Scenario: Creating a knowledge base

- **WHEN** a client sends `POST /1.0/knowledge` with a base name
- **THEN** the base is created and the response is synchronous

#### Scenario: Creating a knowledge base with a default label

- **WHEN** a client sends `POST /1.0/knowledge` with a name and `default_label: partner`
- **THEN** the base is created with `partner` as its default label

#### Scenario: Creating a base with an invalid default label

- **WHEN** a client sends a `default_label` that fails validation
- **THEN** the API returns a `400` error naming the allowed format and no base is created

#### Scenario: Creating a duplicate knowledge base

- **WHEN** a client creates a base whose name already exists
- **THEN** the API returns a conflict error

### Requirement: List, inspect, and forget sources

The API SHALL provide `GET /1.0/knowledge/<name>/sources` to list ingested sources,
`GET /1.0/knowledge/<name>/sources/<id>` to return a source's metadata, and
`DELETE /1.0/knowledge/<name>/sources/<id>` to forget a source (remove its chunks and
metadata). These SHALL be synchronous. Source payloads SHALL include the source's label
(the stored label, or the effective fallback for sources ingested before labels existed).

#### Scenario: Listing sources

- **WHEN** a client requests `GET /1.0/knowledge/<name>/sources`
- **THEN** the response lists the source documents ingested into the base
- **AND** each source includes its label

#### Scenario: Inspecting source metadata

- **WHEN** a client requests `GET /1.0/knowledge/<name>/sources/<id>`
- **THEN** the response returns that source's metadata, including its ingestion status and label

#### Scenario: Forgetting a source

- **WHEN** a client sends `DELETE /1.0/knowledge/<name>/sources/<id>`
- **THEN** the source's chunks and metadata are removed and the response is synchronous

### Requirement: Ingest sources as an operation

The API SHALL provide `POST /1.0/knowledge/<name>/sources` to ingest content into a base. The
request SHALL accept a file upload or a URL to crawl, and a batch mode covering multiple
sources. The batch mode SHALL accept `url`, `github`, and `gitea` source entries; `github` and
`gitea` entries SHALL be fetched server-side using the daemon's `GITHUB_TOKEN` / `GITEA_TOKEN`
environment variables. Ingestion SHALL run as an asynchronous operation that downloads, extracts
text via Tika, chunks, embeds, and indexes the content, updating the source's status as it
progresses.

The request SHALL accept an optional `label` per source (and per batch entry), validated against
the knowledge-labels format; when omitted, the base's default label applies. The resolved label
SHALL be stored on the source's metadata and on every indexed chunk.

The request SHALL accept a `force` flag. When a source with the same identifier already exists
and is completed and `force` is not set, ingestion SHALL skip that source without re-indexing.
When `force` is set, ingestion SHALL first remove the existing source's chunks and then re-index,
so that a forced re-ingest **replaces** the source rather than appending duplicate chunks. The
daemon and CLI ingest paths SHALL share one implementation so their re-ingest semantics do not
diverge.

The operation's metadata SHALL convey ingestion progress, and the operation SHALL be cancellable.

#### Scenario: Ingesting a single document

- **WHEN** a client posts a document to `POST /1.0/knowledge/<name>/sources`
- **THEN** the API returns an asynchronous operation
- **AND** the operation extracts, chunks, embeds, and indexes the document
- **AND** the source's metadata status reflects processing then completion

#### Scenario: Ingesting with an explicit label

- **WHEN** a client posts a document with `label: internal`
- **THEN** the source's metadata and all its indexed chunks carry `internal`

#### Scenario: Ingesting without a label

- **WHEN** a client posts a document without a `label`
- **THEN** the base's effective default label is stored on the source and its chunks

#### Scenario: Ingesting from a URL

- **WHEN** a client posts a URL to ingest
- **THEN** the operation crawls the URL and ingests the retrieved content

#### Scenario: Batch ingestion

- **WHEN** a client posts a batch describing multiple `url`, `github`, or `gitea` sources
- **THEN** a single operation ingests each source and reports overall progress

#### Scenario: Batch entry requires a missing token

- **WHEN** a batch contains a `github` or `gitea` entry but the corresponding token env var is not set
- **THEN** that entry fails with an error naming the required env var (`GITHUB_TOKEN` or `GITEA_TOKEN`)
- **AND** the remaining entries in the batch still proceed

#### Scenario: Re-ingesting an existing source without force

- **WHEN** a client ingests a source whose identifier already exists and is completed, without `force`
- **THEN** the source is skipped and no duplicate chunks are added

#### Scenario: Re-ingesting an existing source with force

- **WHEN** a client ingests an existing source identifier with `force` set
- **THEN** the source's prior chunks are removed before re-indexing
- **AND** the base contains only the chunks from the new ingestion, with no orphaned duplicates

#### Scenario: Cancelling an ingest

- **WHEN** a client cancels a running ingest operation
- **THEN** ingestion stops cooperatively and the operation reports cancellation

### Requirement: Hybrid search

The API SHALL provide `POST /1.0/search` (or an equivalent search endpoint) that runs the
existing hybrid retrieval pipeline (BM25 + neural + rerank) over one or more named knowledge
bases and returns scored hits synchronously. The request SHALL accept the query, the target
bases, and an optional result count. Each hit SHALL include its score, originating base, source
identifier, creation date, resolved label, and content. The `label` field replaces the former
derived `provenance` field and SHALL be resolved by the shared knowledge-labels resolver
(stored chunk label, with index-name fallback for unlabeled chunks).

Search SHALL require the embedding model to be available; when it is not, the API SHALL return
an error stating retrieval is unavailable.

#### Scenario: Searching across bases

- **WHEN** a client posts a query and a set of target bases
- **THEN** the response synchronously returns scored hits ordered by relevance
- **AND** each hit includes its score, base, source id, creation date, label, and content

#### Scenario: Hits carry stored labels

- **WHEN** a hit's chunk carries a stored label
- **THEN** the hit's `label` field is that stored value, not one derived from the index name

#### Scenario: Limiting result count

- **WHEN** a client includes a result count in the search request
- **THEN** the response returns at most that many hits

#### Scenario: Embedding model unavailable

- **WHEN** a search is requested but the embedding model is not available
- **THEN** the API returns an error stating retrieval is unavailable
