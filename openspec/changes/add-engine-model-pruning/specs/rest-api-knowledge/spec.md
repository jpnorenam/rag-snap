# rest-api-knowledge Specification (delta)

## ADDED Requirements

### Requirement: Engine models can be listed and removed via the API

The API SHALL provide an endpoint listing the models registered in the engine's model group with
their deployment state, size, worker-node count, and engine role, and an endpoint that undeploys
and deletes a model. Both SHALL be synchronous: unlike model deployment, listing and removal are
fast.

Removal SHALL refuse a model the engine currently uses unless the request explicitly forces it, so
that a client cannot break ingest and search by accident. The daemon SHALL enforce this guard
itself rather than relying on its clients, since it is the shared entry point for the CLI and the
browser.

#### Scenario: Listing the engine's models

- **WHEN** a client requests the engine's models
- **THEN** the response synchronously lists each model with its state, size, and engine role

#### Scenario: Deleting an unused model

- **WHEN** a client deletes a model no configuration key points at
- **THEN** the model is undeployed and deleted

#### Scenario: Deleting a model the engine uses

- **WHEN** a client deletes a model that serves an engine role, without forcing it
- **THEN** the request is rejected and the response names the role that model serves
