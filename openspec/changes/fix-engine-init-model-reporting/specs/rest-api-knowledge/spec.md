# rest-api-knowledge Specification (delta)

## MODIFIED Requirements

### Requirement: Initialize the knowledge engine as an operation

The API SHALL provide an endpoint to initialize the knowledge engine — creating the model group,
registering and deploying the embedding and rerank models, the ingest and search pipelines, the
index template, and the default and metadata indexes. Because model deployment is slow, this
SHALL run as an asynchronous operation.

The operation SHALL report each resolved model identifier (taken from the initialization result,
not from a prior config read) as soon as initialization resolves it, rather than only when the
whole operation ends — an identifier that has been resolved SHALL remain reported even if a later
initialization step fails.

The daemon SHALL persist those identifiers to the `package`-scoped config keys the engine reads
(`knowledge.model.embedding`, `knowledge.model.rerank`), so that chat, rerank, and search function
after a daemon-driven initialization without a manual `config set`. Persistence SHALL be
best-effort: a failed config write SHALL NOT fail the initialization, and the operation SHALL
report, per identifier, whether it was persisted, so a client can tell the operator whether any
manual step remains.

#### Scenario: Initializing the engine

- **WHEN** a client triggers knowledge-engine initialization
- **THEN** the API returns an asynchronous operation that sets up the pipelines, models, and indexes
- **AND** the operation reports the embedding and rerank model identifiers
- **AND** those identifiers are persisted to config so the engine is usable without a manual step

#### Scenario: Initialization fails after the models are deployed

- **WHEN** initialization resolves the model identifiers and a later step fails
- **THEN** the operation reports the failure
- **AND** the already-resolved identifiers are still reported to the client

#### Scenario: The daemon cannot write the configuration

- **WHEN** persisting a resolved identifier to the `package` config layer fails
- **THEN** the initialization does not fail because of it
- **AND** the operation reports that identifier as not persisted, so the client can tell the
  operator to set it

#### Scenario: Search works immediately after a daemon-driven init

- **WHEN** the engine is initialized through the API and no manual `config set` is performed
- **THEN** a subsequent search or rerank succeeds using the persisted model identifiers
