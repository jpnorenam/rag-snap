# rest-api-knowledge Specification

## Purpose

Expose knowledge-base and source management plus hybrid search over the REST API, covering the
operations currently available through the `rag-cli.rag knowledge` (`k`) subcommands. Read and
quick-write actions are synchronous; long-running actions (model deploy, ingest, export, import)
are asynchronous operations.

## ADDED Requirements

### Requirement: List and create knowledge bases

The API SHALL provide `GET /1.0/knowledge` to list knowledge bases and
`POST /1.0/knowledge` to create one. Listing and creation SHALL be synchronous.

#### Scenario: Listing knowledge bases

- **WHEN** a client requests `GET /1.0/knowledge`
- **THEN** the response synchronously lists the existing knowledge bases

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
sources. Ingestion SHALL run as an asynchronous operation that downloads, extracts text via
Tika, chunks, embeds, and indexes the content, updating the source's status as it progresses.

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

- **WHEN** a client posts a batch describing multiple sources
- **THEN** a single operation ingests each source and reports overall progress

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
SHALL run as an asynchronous operation. The resulting model identifiers SHALL be reported so the
operator can persist them in config.

#### Scenario: Initializing the engine

- **WHEN** a client triggers knowledge-engine initialization
- **THEN** the API returns an asynchronous operation that sets up the pipelines, models, and indexes
- **AND** on success the operation reports the embedding and rerank model identifiers

### Requirement: Export and import as operations

The API SHALL provide endpoints to export a knowledge base and to import a knowledge base. Both
SHALL run as asynchronous operations because they move bulk data. Export SHALL produce a portable
artifact; import SHALL accept a previously exported artifact. The interactive Google Drive
authentication flow is a CLI-client concern and is NOT part of this API capability.

#### Scenario: Exporting a knowledge base

- **WHEN** a client requests an export of a base
- **THEN** the API returns an asynchronous operation that produces a portable export artifact

#### Scenario: Importing a knowledge base

- **WHEN** a client requests an import from a previously exported artifact
- **THEN** the API returns an asynchronous operation that restores the base without re-embedding
