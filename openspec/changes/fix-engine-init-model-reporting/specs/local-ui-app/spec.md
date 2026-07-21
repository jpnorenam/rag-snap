# local-ui-app Specification (delta)

## MODIFIED Requirements

### Requirement: Knowledge engine initialization gate

When the knowledge engine is uninitialized, the `/knowledge/` screen SHALL surface a caution
notification with an action to initialize the engine, without blocking the rest of the page. The
initialization SHALL run as a tracked asynchronous operation. The UI SHALL show the resulting
embedding and rerank model identifiers whenever the operation reports them — including when the
operation failed after resolving them — and SHALL present in a copyable form only those
identifiers the daemon could not persist itself, stating plainly when no manual step remains.

#### Scenario: Engine uninitialized

- **WHEN** the engine is reported uninitialized on load
- **THEN** a caution notification with an "Initialize engine" action is shown
- **AND** the rest of the page remains usable

#### Scenario: Initializing the engine from the UI

- **WHEN** the user triggers "Initialize engine"
- **THEN** the work runs as a tracked operation
- **AND** on success the UI confirms the models are ready and configured

#### Scenario: Identifiers the daemon could not persist

- **WHEN** the operation reports an identifier that was not persisted
- **THEN** that identifier is shown in a copyable snippet as the remaining manual step

#### Scenario: Initialization fails after resolving models

- **WHEN** the operation fails but reported model identifiers
- **THEN** the failure notification still shows the unpersisted identifiers
