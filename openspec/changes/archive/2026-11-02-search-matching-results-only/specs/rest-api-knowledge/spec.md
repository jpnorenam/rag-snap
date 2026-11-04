# Spec Delta

## ADDED Requirements

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

## MODIFIED Requirements

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
