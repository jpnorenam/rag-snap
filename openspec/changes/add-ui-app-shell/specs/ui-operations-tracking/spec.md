# ui-operations-tracking Specification (delta)

## ADDED Requirements

### Requirement: Screens track long-running work through a shared operations context

The UI SHALL provide a single operations tracking context (`OperationsProvider` in
`ui/components/common/` with a `useOperations` hook in `ui/lib/`) that screens use to register
background operations after issuing an asynchronous request (`postAsync`). Screens SHALL NOT
implement their own operation polling loops. The provider SHALL keep the session's tracked
operations in memory, newest first, and SHALL seed its list from `GET /1.0/operations` on
mount so a page reload does not lose running operations.

#### Scenario: Tracking an operation after postAsync

- **WHEN** a screen starts a long-running action and calls `track(operation)` with the returned operation
- **THEN** the operation appears in the session's operation list with a running status

#### Scenario: Reload recovers running operations

- **WHEN** the page is reloaded while daemon operations are running
- **THEN** the provider fetches `GET /1.0/operations` on mount
- **AND** the running operations appear in the panel

### Requirement: Operation status updates arrive via the events websocket with polling fallback

The provider SHALL subscribe to the daemon's `GET /1.0/events` websocket filtered to operation
events, updating tracked operations from event metadata. If the socket drops, the provider
SHALL reconnect with backoff and, while disconnected, SHALL poll `GET /1.0/operations/{id}`
every few seconds for each tracked running operation. Degradation from websocket to polling
SHALL be silent — no error banner. When the daemon is unreachable entirely, the indicator
SHALL simply show nothing new; connection errors are surfaced by screens, not the indicator.

#### Scenario: Progress via events

- **WHEN** the events websocket is connected and a tracked operation progresses or completes
- **THEN** the tracked operation's status and metadata update from the received events

#### Scenario: Socket drops, polling takes over

- **WHEN** the events websocket becomes unreachable while the API still responds
- **THEN** tracked running operations continue to update via polling
- **AND** no error banner is shown for the socket loss

### Requirement: Header operations indicator

The header SHALL show a compact operations indicator button in its status slot, coexisting
with screen-specific status content. It SHALL be hidden until the first operation of the
session is tracked. While any operation runs it SHALL show a spinner icon variant and the
running count with `aria-label` announcing "N operations running". Completion feedback SHALL
be expressed in the indicator (dot/count changes) — the UI SHALL NOT introduce a toast system.

#### Scenario: Indicator hidden with no operations

- **WHEN** no operation has been tracked this session
- **THEN** the indicator is not rendered

#### Scenario: Indicator reflects running work

- **WHEN** at least one tracked operation is running
- **THEN** the indicator shows the spinner variant and the running count
- **AND** its accessible label states the number of operations running

### Requirement: Operations panel

Clicking the indicator SHALL toggle an anchored, right-aligned panel listing the session's
operations, newest first. Each row SHALL show a status dot (caution=running,
positive=succeeded, negative=failed, with cancelled rendered distinctly from failed), the
operation description, a relative timestamp (absolute time in `title`), and on the right a
Cancel action (only while the operation is running and cancellable) or a dismiss control.
Failed rows SHALL show the operation's error message beneath the row. Operations reporting
progress metadata SHALL render a thin progress bar under the row. The panel SHALL close on
Escape and outside click; the toggle SHALL carry `aria-expanded` and `aria-controls`; the
list SHALL be an `aria-live="polite"` region so completions are announced.

#### Scenario: Panel row for a failed operation

- **WHEN** a tracked operation fails
- **THEN** its row shows the negative status dot and the error message beneath the description

#### Scenario: Cancelled is distinct from failed

- **WHEN** a tracked operation is cancelled
- **THEN** its row renders a state visually and textually distinct from a failed operation

#### Scenario: Keyboard interaction

- **WHEN** the user toggles the panel from the keyboard and presses Escape
- **THEN** the panel opens and closes accordingly
- **AND** the toggle's `aria-expanded` reflects the panel state

#### Scenario: Completion announced

- **WHEN** a running operation completes while the panel is open
- **THEN** the update occurs inside the `aria-live="polite"` region and is announced by assistive technology

### Requirement: Cancelling an operation from the panel

The Cancel action SHALL be offered only for operations the daemon reports as cancellable and
SHALL flow through the shared confirm modal (plain variant, operation named in the body)
before issuing `DELETE /1.0/operations/{id}` via the shared API client. A confirmed
cancellation SHALL transition the row to the cancelled state.

#### Scenario: Cancel requires confirmation

- **WHEN** the user activates Cancel on a running cancellable operation
- **THEN** the confirm modal opens naming the operation
- **AND** the DELETE request is sent only after the user confirms

### Requirement: Typed operations API client module

The UI SHALL access operations exclusively through a typed module in `ui/lib/api/`
(`operations.ts`) built on the shared envelope client, exposing list, get, and cancel verbs
and an operation interface mirroring the daemon's operation view (including numeric
`status_code`, `may_cancel`, and `err`). Clients SHALL distinguish terminal states by
`status_code`, not by parsing the text status. A `deleteSync` verb SHALL be added to the
envelope client following the existing `request()` pattern.

#### Scenario: Status derived from status_code

- **WHEN** the provider evaluates whether an operation is running, succeeded, failed, or cancelled
- **THEN** it uses the numeric `status_code` from the operation view

#### Scenario: No direct fetch

- **WHEN** any UI code interacts with the operations API
- **THEN** it goes through `ui/lib/api/operations.ts` and the envelope client rather than calling `fetch` directly
