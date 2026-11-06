# rest-api-knowledge Specification

## Purpose

Expose knowledge-base and source management plus hybrid search over the REST API, covering the
operations currently available through the `rag-cli.rag knowledge` (`k`) subcommands. Read and
quick-write actions are synchronous; long-running actions (model deploy, ingest, export, import)
are asynchronous operations.

## Requirements

### Requirement: List and create knowledge bases

The API SHALL provide `GET /1.0/knowledge` to list knowledge bases and
`POST /1.0/knowledge` to create one. Listing and creation SHALL be synchronous. The list
response for each base SHALL include a `source_count` (the number of ingested sources)
alongside the index-level statistics, computed server-side in a single aggregation so clients
do not need a per-base fan-out.

#### Scenario: Listing knowledge bases

- **WHEN** a client requests `GET /1.0/knowledge`
- **THEN** the response synchronously lists the existing knowledge bases
- **AND** each listed base includes its `source_count`

#### Scenario: Creating a knowledge base

- **WHEN** a client sends `POST /1.0/knowledge` with a base name
- **THEN** the base is created and the response is synchronous

#### Scenario: Creating a duplicate knowledge base

- **WHEN** a client creates a base whose name already exists
- **THEN** the API returns a conflict error

### Requirement: Inspect and delete a knowledge base

The API SHALL provide `GET /1.0/knowledge/<name>` to return a base's detail and
`DELETE /1.0/knowledge/<name>` to delete a base and its source metadata. Both SHALL be
synchronous. Deletion SHALL NOT require an interactive confirmation at the API layer (the
confirmation is a CLI-client concern).

#### Scenario: Inspecting a knowledge base

- **WHEN** a client requests `GET /1.0/knowledge/<name>` for an existing base
- **THEN** the response returns its detail synchronously

#### Scenario: Deleting a knowledge base

- **WHEN** a client sends `DELETE /1.0/knowledge/<name>` for an existing base
- **THEN** the base and its source metadata are removed and the response is synchronous

#### Scenario: Operating on a missing knowledge base

- **WHEN** a client targets a base name that does not exist
- **THEN** the API returns a `404` error

### Requirement: List, inspect, and forget sources

The API SHALL provide `GET /1.0/knowledge/<name>/sources` to list ingested sources,
`GET /1.0/knowledge/<name>/sources/<id>` to return a source's metadata, and
`DELETE /1.0/knowledge/<name>/sources/<id>` to forget a source (remove its chunks and
metadata). These SHALL be synchronous.

#### Scenario: Listing sources

- **WHEN** a client requests `GET /1.0/knowledge/<name>/sources`
- **THEN** the response lists the source documents ingested into the base

#### Scenario: Inspecting source metadata

- **WHEN** a client requests `GET /1.0/knowledge/<name>/sources/<id>`
- **THEN** the response returns that source's metadata, including its ingestion status

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
bases, and an optional result count, which SHALL cap the total number of hits across all target
bases. Each hit SHALL include its score, originating base, source identifier, creation date,
provenance tag, content, and a `matched` boolean that is true exactly when the hit's relevance
score is strictly greater than 0.

Search SHALL require the embedding model to be available; when it is not, the API SHALL return
an error stating retrieval is unavailable.

#### Scenario: Searching across bases

- **WHEN** a client posts a query and a set of target bases
- **THEN** the response synchronously returns scored hits ordered by relevance
- **AND** each hit includes its score, base, source id, creation date, provenance tag, content, and `matched` flag

#### Scenario: Limiting result count

- **WHEN** a client includes a result count of 10 in a search over two bases that each hold more than 10 relevant chunks
- **THEN** the response returns at most 10 hits in total, the highest-scoring across both bases

#### Scenario: Match flag reflects the relevance score

- **WHEN** a search returns hits scoring 3.12, 0, and -4.70
- **THEN** the hit scoring 3.12 has `matched: true`
- **AND** the hits scoring 0 and -4.70 have `matched: false`

#### Scenario: Response shape is unchanged for existing clients

- **WHEN** a client that predates the `matched` field posts a search request
- **THEN** the response is a list of hits with all the fields it previously contained, ordered by relevance

#### Scenario: Embedding model unavailable

- **WHEN** a search is requested but the embedding model is not available
- **THEN** the API returns an error stating retrieval is unavailable

### Requirement: Initialize the knowledge engine as an operation

The API SHALL provide an endpoint to initialize the knowledge engine — creating the model group,
registering and deploying the embedding and rerank models, the ingest and search pipelines, the
index template, and the default and metadata indexes. Because model deployment is slow, this
SHALL run as an asynchronous operation. On success the operation SHALL report the resolved
embedding and rerank model identifiers directly (taken from the initialization result, not from a
prior config read), AND the daemon SHALL persist those identifiers to the `package`-scoped config
keys the engine reads (`knowledge.model.embedding`, `knowledge.model.rerank`), so that chat,
rerank, and search function after a daemon-driven initialization without a manual `config set`.

#### Scenario: Initializing the engine

- **WHEN** a client triggers knowledge-engine initialization
- **THEN** the API returns an asynchronous operation that sets up the pipelines, models, and indexes
- **AND** on success the operation reports the embedding and rerank model identifiers
- **AND** those identifiers are persisted to config so the engine is usable without a manual step

#### Scenario: Search works immediately after a daemon-driven init

- **WHEN** the engine is initialized through the API and no manual `config set` is performed
- **THEN** a subsequent search or rerank succeeds using the persisted model identifiers

### Requirement: Export and import as operations

The API SHALL provide endpoints to export a knowledge base and to import a knowledge base. Both
SHALL run as asynchronous operations because they move bulk data. Export SHALL produce a portable
compressed archive and SHALL make that archive retrievable by an HTTP client (for example a
browser) via an authenticated download keyed to the completed operation — a client need not have
access to the daemon's filesystem. Import SHALL accept a previously exported archive as an
uploaded file (`multipart/form-data`) in addition to the existing daemon-local directory form;
an uploaded archive SHALL be staged and unpacked server-side before import. The interactive
Google Drive authentication flow is a CLI-client concern and is NOT part of this API capability.

#### Scenario: Exporting a knowledge base

- **WHEN** a client requests an export of a base
- **THEN** the API returns an asynchronous operation that produces a portable compressed archive

#### Scenario: Downloading an exported archive

- **WHEN** a client requests the archive of a completed export operation
- **THEN** the API streams the archive as a file download without requiring filesystem access

#### Scenario: Importing an uploaded archive

- **WHEN** a client uploads a previously exported archive to the import endpoint
- **THEN** the API returns an asynchronous operation that stages, unpacks, and restores the base without re-embedding

### Requirement: Search match threshold

A hit's `score` SHALL be the cross-encoder reranker's relevance score for the query and chunk. This score is a logit: positive when the reranker judges the chunk relevant, negative when it does not, so 0 is the reranker's own decision boundary. The match threshold SHALL be 0, exclusive, and SHALL be owned by the server. The API documentation SHALL state the threshold, what the score means, and that clients rely on `matched` rather than comparing scores.

#### Scenario: Threshold boundary

- **WHEN** a search returns hits scoring 0.01, 0, and -0.01
- **THEN** only the hit scoring 0.01 has `matched: true`

#### Scenario: Threshold change stays on the server

- **WHEN** the reranker or the threshold changes in a future release
- **THEN** only the server's `matched` computation changes
- **AND** clients that read `matched` show the new split without changes of their own

#### Scenario: Threshold is documented

- **WHEN** a client developer reads the REST API documentation for `POST /1.0/search`
- **THEN** it states that `matched` is true when the reranker score is greater than 0, explains that the score is a relevance logit, and tells clients to use `matched` instead of comparing scores
