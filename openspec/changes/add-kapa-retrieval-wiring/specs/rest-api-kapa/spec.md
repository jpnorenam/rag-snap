## ADDED Requirements

### Requirement: Source groups are listable over the API

The API SHALL provide a read endpoint that lists the kapa.ai project's source groups, each with the
identifier a client sends back as a selection and a human-readable name to display. The listing
SHALL follow the upstream API's pagination to completion, so a project whose sources span multiple
pages yields the full set rather than the first page. Source groups SHALL be reported de-duplicated:
the upstream shape nests groups under sources, so the same group can appear on many sources.

The endpoint SHALL be synchronous — it is a read against a remote API, not a long-running action —
and SHALL be subject to the same localhost authentication as every other API endpoint, as defined by
`rest-api-localhost-auth`.

#### Scenario: Listing configured source groups

- **WHEN** a client requests the source-group listing and kapa is configured
- **THEN** the API returns each source group's identifier and display name

#### Scenario: A group attached to several sources appears once

- **WHEN** the same source group is attached to more than one source in the project
- **THEN** the listing reports that group exactly once

#### Scenario: Paginated projects list in full

- **WHEN** the project's sources span more than one upstream page
- **THEN** the listing includes source groups from every page

#### Scenario: Unauthenticated request is rejected

- **WHEN** a request omits or presents an invalid localhost token
- **THEN** the API rejects it exactly as it rejects any other unauthenticated API request

### Requirement: An unconfigured integration is distinguishable from an empty project

The endpoint SHALL report "kapa is not configured" as a condition distinct from "kapa is configured
and has no source groups". A client SHALL be able to tell the two apart in order to render a
configuration hint rather than an empty picker. An upstream failure — an authentication rejection, an
unreachable host, or an unexpected status — SHALL likewise be reported as a failure and SHALL NOT be
flattened into an empty list.

#### Scenario: Not configured

- **WHEN** a client requests the listing and the integration is disabled or its credentials are absent
- **THEN** the response identifies the integration as unconfigured rather than returning an empty list

#### Scenario: Configured but empty

- **WHEN** a client requests the listing, kapa is configured, and the project has no source groups
- **THEN** the response is a successful empty listing

#### Scenario: Upstream rejects the credentials

- **WHEN** the upstream API rejects the configured API key
- **THEN** the response reports the failure rather than an empty listing

#### Scenario: Upstream unreachable

- **WHEN** the upstream API cannot be reached
- **THEN** the response reports the failure rather than an empty listing
