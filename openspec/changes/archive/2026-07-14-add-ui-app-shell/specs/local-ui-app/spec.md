# local-ui-app Delta Specification

Change: `add-ui-app-shell`

## ADDED Requirements

### Requirement: Multi-page navigation shell

The UI SHALL render a persistent application shell — sidebar navigation, top bar, and shared
application state — from the root layout so it survives client-side route changes. Each UI
section SHALL be a distinct route in the static export (`/`, `/knowledge/`, `/search/`,
`/answer/`, `/prompts/`, `/status/`). Sidebar entries for sections that have shipped SHALL be
links with an active state (`aria-current="page"`); entries for sections that have not shipped
SHALL be non-focusable placeholders (never links or buttons) marked as coming soon. The sidebar
SHALL include a Status entry pinned to the bottom of the rail above the dark-mode toggle. A route
change SHALL update both the top bar's section title and `document.title`.

#### Scenario: Navigating to a shipped section

- **WHEN** the user activates an enabled sidebar entry
- **THEN** the UI navigates client-side to that section's route without a full document reload
- **AND** the entry is marked active with `aria-current="page"`
- **AND** the top bar title and `document.title` reflect the section

#### Scenario: Unshipped sections are inert placeholders

- **WHEN** the sidebar renders an entry whose section has not shipped
- **THEN** the entry is a non-focusable placeholder labelled as coming soon
- **AND** it is not reachable by keyboard and triggers no navigation

#### Scenario: Shell state survives navigation

- **WHEN** the user navigates between sections
- **THEN** the sidebar, dark-mode state, and tracked operations are preserved without remounting

### Requirement: Global operations indicator

The UI SHALL show a global operations indicator in the top bar's status slot on every route,
coexisting with screen-specific status controls. The indicator SHALL be hidden until at least one
operation has been observed in the session, SHALL show the count of running operations with an
accessible label, and SHALL toggle an anchored operations panel listing the session's operations
newest first. Each panel row SHALL show a status dot distinguishing running, succeeded, failed,
and cancelled (derived from the operation's numeric `status_code`), the operation description, a
relative timestamp with the absolute time available, the operation's error message when it
failed, and a progress bar when the operation reports progress metadata. The panel SHALL be
keyboard-operable: the toggle exposes `aria-expanded`/`aria-controls`, the list announces updates
via `aria-live="polite"`, and the panel closes on Escape and on outside click.

#### Scenario: Indicator reflects running work

- **WHEN** a tracked operation is running
- **THEN** the indicator shows the count of running operations with an accessible label
- **AND** completion updates the indicator without a toast or dialog

#### Scenario: Panel lists session operations

- **WHEN** the user opens the operations panel
- **THEN** operations are listed newest first with status dot, description, and relative timestamp
- **AND** a failed operation's row shows its error message
- **AND** a cancelled operation renders distinctly from a failed one

#### Scenario: Panel dismissal

- **WHEN** the panel is open and the user presses Escape or clicks outside it
- **THEN** the panel closes and focus returns to the toggle when it was inside the panel

### Requirement: Operations state is live and reload-safe

The UI SHALL maintain operations state in a single shared provider available to every screen.
Screens SHALL register newly started operations with the provider after receiving an async
response. The provider SHALL seed its list from `GET /1.0/operations` on mount, SHALL subscribe
to the `GET /1.0/events` websocket filtered to operation events for live updates, SHALL
reconnect with backoff and re-fetch the operations list on every (re)connect, and SHALL fall back
to polling `GET /1.0/operations/{id}` for running operations while the websocket is unavailable.
Websocket unavailability SHALL degrade silently (no error surface) as long as the REST API is
reachable.

#### Scenario: Reload does not lose running operations

- **WHEN** the user reloads the UI while an operation is running
- **THEN** the operations panel lists that operation after the reload, seeded from `GET /1.0/operations`

#### Scenario: Live progress via events websocket

- **WHEN** the events websocket delivers an operation event for a tracked operation
- **THEN** the panel row and indicator update without any polling request

#### Scenario: Silent fallback to polling

- **WHEN** the events websocket is unavailable but the REST API is reachable
- **THEN** running operations continue to update via polling
- **AND** no error banner is shown for the websocket outage

### Requirement: Operations can be cancelled from the UI

The UI SHALL offer a Cancel action on a panel row only while the operation is running and its
`may_cancel` flag is true. Cancel SHALL require confirmation through the shared confirm modal
(never `window.confirm`) and then issue `DELETE /1.0/operations/{id}`. A failed cancellation
SHALL surface the API error message without removing the row.

#### Scenario: Cancelling a running operation

- **WHEN** the user activates Cancel on a running, cancellable operation and confirms
- **THEN** the UI issues `DELETE /1.0/operations/{id}`
- **AND** the row transitions to the cancelled state when the daemon reports it

#### Scenario: Non-cancellable operations offer no cancel

- **WHEN** a panel row shows an operation that is terminal or has `may_cancel` false
- **THEN** no Cancel action is rendered for that row

### Requirement: Shared UI primitives for subsequent changes

The UI SHALL provide shared primitives under `ui/components/common/` for later screens to import
rather than re-implement: an empty-state component (muted icon, one-line headline, guidance
including the CLI-equivalent command, optional primary action), a spinner component (spinner icon
plus visible text), a confirm modal component with plain and type-to-confirm variants, and a
generalized status-dot style. The confirm modal SHALL move focus into the dialog on open, trap
focus while open, restore focus on close, and close on Escape and overlay click; its
type-to-confirm variant SHALL keep the destructive button disabled until the typed text exactly
matches the required name.

#### Scenario: Type-to-confirm gates the destructive action

- **WHEN** the type-to-confirm modal is open and the input does not exactly match the required name
- **THEN** the destructive button is disabled
- **AND** it becomes enabled only when the input matches exactly

#### Scenario: Modal focus management

- **WHEN** a confirm modal opens
- **THEN** focus moves into the dialog and cannot Tab outside it
- **AND** closing the modal restores focus to the element that opened it

#### Scenario: Empty state includes the CLI equivalent

- **WHEN** a screen renders the shared empty-state component
- **THEN** the guidance text includes the equivalent CLI command
