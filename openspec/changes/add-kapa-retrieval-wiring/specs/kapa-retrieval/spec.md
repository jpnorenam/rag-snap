## ADDED Requirements

### Requirement: Kapa credentials and project identity resolve from environment and config

The kapa.ai client SHALL be constructed only when the integration is enabled and both a project
identifier and an API key are available. `kapa.enabled` SHALL be a `package`-scoped config key
defaulting to enabled when unset, so an unconfigured installation is not treated as explicitly
disabled. The project identifier SHALL come from the `kapa.project.id` config key, overridable by
the `KAPA_PROJECT_ID` environment variable. The API key SHALL come exclusively from the
`KAPA_API_KEY` environment variable, never from config, matching how `CHAT_API_KEY` and the
OpenSearch credentials are supplied.

When the integration is disabled, or either the project identifier or the API key is absent, no
client SHALL be constructed and retrieval SHALL proceed over local knowledge bases alone.

#### Scenario: Enabled with both values present

- **WHEN** `kapa.enabled` is unset or true, `kapa.project.id` holds a project identifier, and `KAPA_API_KEY` is set in the environment
- **THEN** a kapa client is constructed and available for retrieval

#### Scenario: API key absent

- **WHEN** `kapa.project.id` is set but `KAPA_API_KEY` is not present in the environment
- **THEN** no kapa client is constructed and retrieval uses local knowledge bases only

#### Scenario: Environment overrides the configured project

- **WHEN** `kapa.project.id` holds one value and `KAPA_PROJECT_ID` holds another
- **THEN** the environment value is used

#### Scenario: Explicitly disabled

- **WHEN** `kapa.enabled` is set to false and both credentials are present
- **THEN** no kapa client is constructed

### Requirement: Kapa configuration keys are registered on install and on refresh

The `kapa.enabled` and `kapa.project.id` keys SHALL be registered as `package`-scoped keys by both
the `install` and `post-refresh` snap hooks. Because a `user` set rejects a key that does not
already exist at the `package` layer, seeding on install alone leaves every installation that
reached its current revision by `snap refresh` unable to configure the integration at all.
Registration SHALL be idempotent and SHALL NOT overwrite a value an operator has already set.

#### Scenario: Refreshed installation can be configured

- **WHEN** an installation predating these keys is refreshed to a revision containing them
- **THEN** `kapa.enabled` and `kapa.project.id` exist at the `package` layer and can be set

#### Scenario: Refresh preserves an operator's value

- **WHEN** an operator has set `kapa.project.id` and the snap is refreshed
- **THEN** the configured value survives the refresh

### Requirement: Selected source groups gate kapa retrieval

Kapa retrieval SHALL occur only when a client is available **and** at least one source group is
selected. Source-group selection SHALL have no default: an absent or empty selection SHALL mean no
kapa retrieval, so a configured installation never silently widens a query across an entire kapa
project. In batch answering the selection SHALL come from the manifest's `kapa_source_groups` field;
in an interactive session it SHALL come from the session's current selection.

#### Scenario: No groups selected

- **WHEN** a kapa client is available but no source groups are selected
- **THEN** no kapa request is made and retrieval uses local knowledge bases only

#### Scenario: Groups selected

- **WHEN** a kapa client is available and the manifest or session selects one or more source groups
- **THEN** kapa retrieval runs against exactly those groups

### Requirement: Kapa and local retrieval run in parallel and merge deterministically

When both local knowledge bases and kapa source groups are active, the two retrievals SHALL be
issued concurrently rather than in sequence, and the merged result SHALL place local hits before
kapa hits. Kapa hits SHALL carry the implicit `kapa-canonical` label defined by
`knowledge-labels`; this change SHALL NOT alter labeling or any prompt-level priority attached to
labels.

#### Scenario: Both sources active

- **WHEN** a query runs with local knowledge bases and kapa source groups both active
- **THEN** both retrievals are issued concurrently and the merged context lists local hits before kapa hits

#### Scenario: Kapa hits are labeled

- **WHEN** the merged context includes kapa hits
- **THEN** those hits carry the `kapa-canonical` label and are tagged accordingly in the RAG context

### Requirement: An unusable kapa selection is reported, not silently ignored

The system SHALL report that kapa grounding was requested and could not be applied whenever source
groups are selected but no client can be constructed — because the integration is disabled, or the
project identifier or API key is missing. It SHALL NOT answer as though no kapa source had been
selected.
A kapa retrieval that fails at request time SHALL likewise be reported and SHALL NOT fail the
surrounding turn or batch question: the answer proceeds on whatever local context was retrieved.

#### Scenario: Groups selected but credentials missing

- **WHEN** a batch manifest selects kapa source groups and no API key is present in the environment
- **THEN** the run reports that kapa grounding was requested but is unconfigured
- **AND** the batch still runs against the local knowledge bases

#### Scenario: Kapa request fails mid-run

- **WHEN** a kapa retrieval request fails for a question
- **THEN** the failure is reported and that question is answered from local context alone

#### Scenario: Failure does not abort the batch

- **WHEN** a kapa retrieval request fails for one question in a batch
- **THEN** the remaining questions continue to be answered

### Requirement: Direct-CLI and daemon paths behave identically

Kapa retrieval SHALL behave the same whether a run executes in the CLI's direct mode or through the
`ragd` daemon. Because `answer batch` prefers the daemon whenever it is running, a manifest field or
credential that reaches only the direct path is unreachable in normal operation. Every input that
governs kapa retrieval — the source-group selection and the resolved credentials — SHALL be
available on both paths, and no path SHALL substitute a null client for a configured integration.

#### Scenario: Same manifest, same grounding

- **WHEN** the same manifest selecting kapa source groups is run in direct mode and through the daemon, both configured
- **THEN** both runs retrieve from kapa and produce equivalently grounded answers

#### Scenario: Daemon does not disable kapa

- **WHEN** a batch runs through the daemon with kapa configured and source groups selected
- **THEN** the daemon performs kapa retrieval rather than running without it

## REMOVED Requirements

### Requirement: Kapa API key is readable from config

**Reason**: Storing the kapa.ai API key in `kapa.api.key` contradicts the project rule that secrets
travel by environment variable and never through config — the rule already followed by
`OPENSEARCH_USERNAME`, `OPENSEARCH_PASSWORD`, and `CHAT_API_KEY`. A config-borne key is readable
through the config API and the `rag get` surface, and is carried in snapd state, none of which is
appropriate for a credential. The key was never documented as the recommended path.

**Migration**: Move the value to the `KAPA_API_KEY` environment variable. For the `rag` CLI, export
it in the shell. For the `ragd` daemon, supply it through a root-only systemd drop-in under
`/etc/systemd/system/snap.rag-cli.ragd.service.d/`, the same mechanism already documented for
`CHAT_API_KEY` — not through a `snapcraft.yaml` `environment:` stanza, which would be applied after
systemd and would override the drop-in. The `kapa.api.key` key is no longer registered by the
install hook and any value previously set at either layer is ignored.
