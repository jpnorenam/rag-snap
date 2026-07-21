# knowledge-engine-models Specification (delta)

## ADDED Requirements

### Requirement: Model inventory is visible

The system SHALL let an operator list the models registered in the engine's model group, reporting
for each one its identifier, name, version, deployment state, size, and the engine role it serves
(the embedding model, the rerank model, or none). A model that occupies memory on the ML nodes —
including a partially deployed or failed deployment — SHALL be distinguishable from one that is
merely registered, and the listing SHALL state how much memory the models nothing points at are
holding.

An engine that has never been initialized SHALL report an empty inventory rather than an error.

#### Scenario: Listing models

- **WHEN** an operator lists the engine's models
- **THEN** each registered model is reported with its state, size, and engine role

#### Scenario: Unused deployed models are called out

- **WHEN** the inventory contains deployed models that no configuration key points at
- **THEN** their number and combined memory footprint are reported, with the action that reclaims it

#### Scenario: Engine never initialized

- **WHEN** the model group does not exist
- **THEN** the inventory is empty and no error is reported

### Requirement: Unused models can be reclaimed

The system SHALL let an operator undeploy and delete models the engine does not use, freeing the
memory they hold on the ML nodes. A model the engine currently uses — one a configuration key
points at — SHALL NOT be removed by a bulk reclaim, and SHALL be refused on a targeted removal
unless the operator explicitly forces it, because removing it breaks ingest and search until the
engine is initialized again. A bulk reclaim SHALL state what it will remove and require
confirmation before removing anything.

Removal SHALL undeploy before deleting, so a model that is still resident is not refused, and a
model that was never deployed is still deleted.

#### Scenario: Pruning unused models

- **WHEN** an operator prunes and confirms
- **THEN** every model with no engine role is undeployed and deleted
- **AND** the models in use for embedding and reranking are left alone

#### Scenario: Removing a model the engine uses

- **WHEN** an operator removes a model that a configuration key points at, without forcing it
- **THEN** the removal is refused and names the role that model serves

#### Scenario: Nothing to reclaim

- **WHEN** a prune finds no unused models
- **THEN** it reports that and removes nothing
