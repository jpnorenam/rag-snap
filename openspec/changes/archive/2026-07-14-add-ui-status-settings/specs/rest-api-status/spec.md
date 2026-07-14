# rest-api-status Delta

## ADDED Requirements

### Requirement: Status endpoint reports per-service health

The daemon SHALL expose `GET /1.0/status`, gated by the same authentication as the rest of the
`/1.0` API, returning one entry per service in a fixed set — `opensearch`, `inference`, `tika`,
and `ragd` — where each external service entry reports a state of `running`, `unreachable`, or
`not configured`, plus the resolved endpoint URL when one is configured. Health SHALL be
determined by live HTTP-level probes performed at request time (not cached TCP dialability),
executed concurrently with a bounded per-probe timeout. Services SHALL degrade independently:
a failing or slow probe for one service MUST NOT fail the endpoint or affect other entries.

#### Scenario: All services reachable

- **WHEN** an authenticated client requests `GET /1.0/status` and all three external services answer their probes
- **THEN** the response reports `running` for `opensearch`, `inference`, and `tika`
- **AND** each entry includes its resolved endpoint URL

#### Scenario: One service is down

- **WHEN** OpenSearch does not answer its probe within the timeout but the inference server and Tika do
- **THEN** the `opensearch` entry reports `unreachable` with its resolved endpoint URL
- **AND** the `inference` and `tika` entries still report `running` with their details
- **AND** the request succeeds as a sync response

#### Scenario: A service has no configured endpoint

- **WHEN** a service's endpoint URL cannot be resolved from config
- **THEN** that entry reports `not configured` and no probe is attempted for it

#### Scenario: Unauthenticated caller

- **WHEN** a client without valid credentials requests `GET /1.0/status`
- **THEN** the daemon responds with the standard authentication error

### Requirement: OpenSearch entry reports configured and deployed models

The `opensearch` status entry SHALL include the configured embedding and rerank model IDs (from
the snapctl-backed config, when set) and, when OpenSearch is reachable, the live list of deployed
ML models obtained by searching `_plugins/_ml/models/_search` filtered to
`model_state: DEPLOYED`. Each deployed model SHALL report its OpenSearch document ID, `name`,
`algorithm`, `model_version`, and `model_group_id`. Each configured model ID SHALL carry a flag
stating whether it appears in the deployed list, so clients can flag configured-but-undeployed
models. When OpenSearch is unreachable, the configured model IDs SHALL still be reported and the
deployed list SHALL be absent.

#### Scenario: Deployed models are listed

- **WHEN** OpenSearch is reachable and has an embedding model and a rerank model in state `DEPLOYED`
- **THEN** the `opensearch` entry lists both deployed models with id, name, algorithm, model version, and model group id

#### Scenario: Configured model is not deployed

- **WHEN** the configured embedding model ID does not appear in the deployed-model search results
- **THEN** the configured embedding model entry is flagged as not deployed

#### Scenario: Models reported without OpenSearch

- **WHEN** OpenSearch is unreachable and an embedding model ID is configured
- **THEN** the `opensearch` entry still reports the configured model ID
- **AND** no deployed-model list is present

### Requirement: Inference and Tika entries report service details

When reachable, the `inference` entry SHALL include the LLM model name detected from the
inference server (the same detection the CLI `status` command performs), and the `tika` entry
SHALL include the Tika server version when the server reports one. A missing detail (probe
succeeded but detail unavailable) SHALL NOT change the service's `running` state.

#### Scenario: LLM model name detected

- **WHEN** the inference server is reachable and reports a model
- **THEN** the `inference` entry reports `running` and includes the detected LLM model name

#### Scenario: Tika version reported

- **WHEN** the Tika server answers its version endpoint
- **THEN** the `tika` entry reports `running` and includes the reported version string

### Requirement: Daemon reports its own listener state

The `ragd` entry SHALL report the daemon's API version and its enabled listeners: the unix
socket path, and — when the loopback listener is enabled — the loopback address. The status
payload MUST NOT include the localhost bearer token.

#### Scenario: Listener summary

- **WHEN** an authenticated client requests `GET /1.0/status` on a daemon with the loopback listener enabled
- **THEN** the `ragd` entry reports the API version, the unix socket path, and the loopback address
- **AND** the localhost token appears nowhere in the response

### Requirement: Status capability is feature-detectable

The daemon SHALL advertise the status resource by appending a `status` entry to
`api_extensions`.

#### Scenario: Extension advertised

- **WHEN** a client requests `GET /1.0`
- **THEN** `api_extensions` includes `status`
