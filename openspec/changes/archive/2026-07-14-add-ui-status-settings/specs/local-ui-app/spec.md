# local-ui-app Delta

## ADDED Requirements

### Requirement: Status page shows per-service health cards

The UI SHALL provide a `/status/` page whose Status zone renders a list (semantic `<ul>`) of
one card per service in fixed order — OpenSearch (knowledge store), Inference server (chat
backend), Tika (text extraction), ragd (daemon) — sourced from `GET /1.0/status`. Each card
SHALL show a status dot plus the state word (**Running** / **Unreachable** / **Not
configured**) so color never carries meaning alone, and the resolved endpoint URL as copyable
muted small text. Per-card details: the OpenSearch card SHALL show the configured embedding and
rerank model IDs as copyable code snippets, the list of deployed OpenSearch ML models (name,
algorithm, version), and a caution note on any configured model ID that is not deployed; the
Inference card SHALL show the detected LLM model name; the Tika card SHALL show the reported
version; the ragd card SHALL show the API version and enabled listeners. An unreachable
service's card SHALL grow a one-line CLI diagnostic hint (e.g. `snap services rag-cli` or the
relevant config key). Cards SHALL degrade independently — one unreachable service MUST NOT
error the page. The sidebar's bottom-pinned Status entry SHALL become a live route to this
page.

#### Scenario: Healthy services render with details

- **WHEN** the status page loads and `GET /1.0/status` reports all services running
- **THEN** four cards render in the fixed order, each with a dot, the word "Running", and a copyable endpoint
- **AND** the OpenSearch card lists the configured model IDs as copyable snippets and the deployed models with name, algorithm, and version

#### Scenario: Configured model not deployed is flagged

- **WHEN** the status payload flags the configured embedding model ID as not deployed
- **THEN** the OpenSearch card shows a caution note on that model ID

#### Scenario: Unreachable service degrades alone

- **WHEN** the status payload reports Tika unreachable and the other services running
- **THEN** the Tika card shows "Unreachable" plus a CLI diagnostic hint
- **AND** the other cards render their normal details and the page shows no global error

#### Scenario: Status entry is a live route

- **WHEN** the user activates the sidebar's Status entry
- **THEN** the UI navigates to `/status/` and the entry is marked active with `aria-current="page"`

### Requirement: Status refreshes on demand, not by polling

The Status zone SHALL fetch on page mount and via an explicit Refresh button accompanied by a
relative last-checked timestamp. The page MUST NOT auto-poll. A completed refresh SHALL be
announced through a polite live region.

#### Scenario: Manual refresh

- **WHEN** the user activates Refresh
- **THEN** the UI re-requests `GET /1.0/status`, updates the cards and the last-checked timestamp
- **AND** a polite live region announces that the status was updated

### Requirement: Configuration table lists keys with layer provenance

The `/status/` page's Configuration zone SHALL render the entries from `GET /1.0/config` as a
semantic table — dot-namespaced Key in monospace, Value, and a Layer chip (`package` plain,
`user` positive) — with column header cells, filterable client-side through a search box.
Redacted values SHALL render as a mask and never as the secret. The zone's loading and error
states SHALL be independent of the Status zone, and the error state SHALL offer the CLI
fallback command.

#### Scenario: Filterable config table

- **WHEN** the user types `chat` into the configuration search box
- **THEN** only rows whose key matches remain visible

#### Scenario: Secrets render masked

- **WHEN** the config payload contains a redacted value
- **THEN** the row renders a mask (`••••`) and the secret value appears nowhere in the DOM

### Requirement: Config values are editable on the user layer only

Each config row SHALL offer inline editing via a pencil button (`aria-label="Edit <key>"`) that
swaps the value cell for an input with Save and Cancel, moving focus into the input and back to
the pencil on cancel or save. Saving SHALL issue `PUT /1.0/config/{key}` (a user-layer write);
daemon validation errors SHALL render as field-level messages on the row without losing the
input. The UI MUST NOT offer creation of new keys. A row whose layer is `user` SHALL offer
"Revert to package value" behind a confirm modal showing both values, issuing
`DELETE /1.0/config/{key}`. After a successful save of a key affecting a service connection,
the UI SHALL show a caution notification pointing at the Status zone. When `GET /1.0/config`
reports the caller may not write, the whole zone SHALL render read-only with an information
notification explaining the CLI alternative (`sudo rag-cli.rag set <key>=<value>`), with no
edit affordances.

#### Scenario: Inline edit writes the user layer

- **WHEN** the user edits `chat.http.port`, enters a new value, and saves
- **THEN** the UI issues `PUT /1.0/config/chat.http.port` and the row shows the new value with a `user` layer chip
- **AND** a caution notification advises checking the Status zone

#### Scenario: Validation error stays on the row

- **WHEN** the daemon rejects a save as a client error
- **THEN** the row shows a field-level validation message and the user's input is preserved

#### Scenario: Revert to package value

- **WHEN** the user chooses "Revert to package value" on a user-layer row and confirms in the modal showing both values
- **THEN** the UI issues `DELETE /1.0/config/{key}` and the row shows the package value with a `package` chip

#### Scenario: Read-only mode without write permission

- **WHEN** `GET /1.0/config` reports `writable` false
- **THEN** the Configuration zone renders without any edit or revert affordances
- **AND** an information notification explains how to edit via the CLI
