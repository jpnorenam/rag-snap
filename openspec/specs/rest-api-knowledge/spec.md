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
bases, and an optional result count. Each hit SHALL include its score, originating base, source
identifier, creation date, provenance tag, and content.

Search SHALL require the embedding model to be available; when it is not, the API SHALL return
an error stating retrieval is unavailable.

#### Scenario: Searching across bases

- **WHEN** a client posts a query and a set of target bases
- **THEN** the response synchronously returns scored hits ordered by relevance
- **AND** each hit includes its score, base, source id, creation date, provenance tag, and content

#### Scenario: Limiting result count

- **WHEN** a client includes a result count in the search request
- **THEN** the response returns at most that many hits

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

### Requirement: Search hits include their citation
Each hit returned by `POST /1.0/search` SHALL include its resolved citation — citation key, label,
link (absent when none), and locator — in addition to the fields it already carries.

#### Scenario: Hit with a recorded link
- **WHEN** a search returns a chunk whose source was ingested from `https://ubuntu.com/pro`
- **THEN** the hit's citation carries a label and the link `https://ubuntu.com/pro`

#### Scenario: Hit without a link
- **WHEN** a search returns a chunk whose source has no recorded URL
- **THEN** the hit's citation carries a label and no link

### Requirement: Ingest accepts an optional source URI
`POST /1.0/knowledge/<name>/sources` SHALL accept an optional source URI for file uploads, recorded
as the source's `source_uri`. URL ingests SHALL record their URL as `source_uri`, and `github` and
`gitea` batch entries SHALL record each file's full web URL. Source metadata returned by the sources
endpoints SHALL include `source_uri` when recorded.

#### Scenario: File upload with a source URI
- **WHEN** a client uploads a file supplying source URI `https://example.com/spec.pdf`
- **THEN** the source's metadata includes `source_uri` `https://example.com/spec.pdf`

#### Scenario: Invalid source URI
- **WHEN** a client uploads a file supplying a source URI that is not an absolute http(s) URL
- **THEN** the API rejects the request with a client error before starting an operation

#### Scenario: Repo batch entry
- **WHEN** a `github` batch entry ingests `docs/install.md` from `canonical/k8s-snap`
- **THEN** that source's metadata includes a `source_uri` linking to the file on github.com at the fetched ref
