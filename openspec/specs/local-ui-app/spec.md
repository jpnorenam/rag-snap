# local-ui-app Specification

## Purpose

Define the browser UI that drives the local `ragd` REST API, replicating the framework
and visual style of the existing `rag-snap-ui` (Next.js + React + TypeScript + Canonical
Vanilla Framework) while being a real client of the local API rather than a Firebase-backed
JSON reviewer. The first delivered experience is interactive chat.

## Requirements

### Requirement: UI is a static single-page application

The UI SHALL be built as a client-rendered single-page application that exports to static
assets (HTML/JS/CSS) with no server-side runtime of its own, so it can be embedded into the
`ragd` binary and served as files. It SHALL use Next.js (App Router) with `output: 'export'`,
React, and TypeScript, matching the existing `rag-snap-ui` stack.

#### Scenario: Static export produces servable assets

- **WHEN** the UI is built for production
- **THEN** the build emits a directory of static files including an `index.html` entry point
- **AND** the assets require no Node.js runtime to be served

#### Scenario: Client-side routing within the SPA

- **WHEN** the user navigates between views in the UI
- **THEN** navigation is handled client-side without a full document reload

### Requirement: UI replicates the Vanilla Framework style

The UI SHALL use Canonical's Vanilla Framework (compiled via Sass) as its design system,
reproducing the look and feel of `rag-snap-ui`, including a dark-mode toggle persisted in
the browser. It SHALL NOT introduce a different component framework (e.g. Tailwind,
Material UI).

#### Scenario: Vanilla Framework styling applied

- **WHEN** the UI renders any screen
- **THEN** layout and components use Vanilla Framework classes and CSS custom properties

#### Scenario: Dark mode persists

- **WHEN** the user enables dark mode and reloads the UI
- **THEN** dark mode remains active

### Requirement: API client targets the same origin

The UI SHALL call the API using paths rooted at `${ROOT_PATH}/1.0/...`, where `ROOT_PATH`
defaults to empty so requests are relative to the origin that served the page. The client
SHALL be organized as a small set of resource-oriented modules (e.g. a server/info module,
a chat module, and a knowledge module) using the browser `fetch` API. The client SHALL
interpret the daemon's response envelope: reading `metadata` from `sync` responses, the
operation reference from `async` responses, and surfacing `error` responses as typed errors.

#### Scenario: Same-origin requests

- **WHEN** the UI issues an API request
- **THEN** the request URL is relative to the serving origin (no hardcoded host or port)

#### Scenario: Error envelope surfaced

- **WHEN** the API returns an `error` response
- **THEN** the client raises a typed error carrying the status code and message for display

### Requirement: API client attaches the localhost token at runtime

The API client SHALL include the localhost bearer token with each `/1.0/...` request over
the loopback listener (via an Authorization header or the loopback-scoped credential set by
the `/ui/login` handoff), because those calls require it. The token SHALL be sourced at
runtime and SHALL NOT be baked into the embedded/static assets at build time.

#### Scenario: Token accompanies API calls

- **WHEN** the UI issues a `/1.0/...` request over the loopback listener
- **THEN** the request carries the localhost token obtained at runtime

#### Scenario: Token is not embedded in build artifacts

- **WHEN** the UI is built
- **THEN** no localhost token value is present in the static assets

### Requirement: Interactive chat screen

The UI SHALL provide a chat screen that starts a chat session via `POST /1.0/chat`,
connects to the websocket referenced by the returned operation, and holds a multi-turn
conversation on that connection. It SHALL render streamed assistant tokens incrementally as
they arrive, visually distinguish `<think>` content from the answer, and let the user submit
new prompts without re-establishing the session.

#### Scenario: Start a chat session

- **WHEN** the user opens the chat screen and the session starts
- **THEN** the UI sends `POST /1.0/chat` and connects to the websocket from the operation metadata

#### Scenario: Streamed response rendering

- **WHEN** the daemon streams `token` and `think` frames over the chat websocket
- **THEN** the UI appends tokens to the current answer as they arrive
- **AND** renders `think` content distinctly from the final answer

#### Scenario: Multi-turn on one connection

- **WHEN** the user submits a second prompt in the same chat screen
- **THEN** the UI reuses the open websocket without starting a new session

### Requirement: Active knowledge bases can be selected in chat

The chat screen SHALL let the user choose which knowledge bases are active for the session.
It SHALL list available bases (via `GET /1.0/knowledge`) and apply changes mid-session by
sending the active-knowledge-base control message over the chat websocket, without restarting
the session.

#### Scenario: List available knowledge bases

- **WHEN** the chat screen loads
- **THEN** the UI fetches the list of knowledge bases from `GET /1.0/knowledge` for selection

#### Scenario: Switch active bases mid-session

- **WHEN** the user changes the active knowledge-base selection during a session
- **THEN** the UI sends the active-knowledge-base control message over the open websocket
- **AND** the session continues without reconnecting

### Requirement: Preserve the answer-review data contract

The UI codebase SHALL retain the `QAItem` and `ParsedQAFile` type contract from
`rag-snap-ui` (tolerating both `result` and `results` keys in batch output) so the later
migration of the `answer batch` review experience can reuse it. This contract SHALL NOT be
wired into a UI screen in this change.

#### Scenario: Type contract present and unused

- **WHEN** the UI is built in this change
- **THEN** the `QAItem`/`ParsedQAFile` types exist in the codebase
- **AND** no shipped screen depends on them yet

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

### Requirement: Prompts page shows the three prompt templates with their state

The UI SHALL provide a Prompts page at `/prompts/` (with the sidebar entry becoming a live
route) rendering the three prompt templates as three stacked cards in the fixed order
`chat_system_prompt`, `answer_system_prompt`, `source_rules`, sourced from `GET /1.0/prompts`.
Each card SHALL show a title, a state chip with a text label reading Default or Customized (not
conveyed by color alone), and a read-only preview of the first lines of the *effective* prompt.
Cards SHALL be `<section>` elements labelled by their headings. While the prompts are loading,
the page SHALL render three fixed-height skeleton cards without layout shift; when loading
fails, the page SHALL show the standard error state and block editing. There is no empty state
— defaults always exist.

#### Scenario: Cards render with state

- **WHEN** the Prompts page loads successfully
- **THEN** three cards render in the fixed order, each with its title, a Default or Customized
  chip, and a preview of the effective prompt text

#### Scenario: Load failure blocks editing

- **WHEN** the prompts cannot be fetched
- **THEN** the page shows the standard error notification with a retry action
- **AND** no card can enter edit mode

### Requirement: Prompts can be edited and saved from the UI

Activating Edit on a card SHALL expand it into edit mode: a monospace textarea (wired to a
label) pre-filled with the effective prompt, with the built-in default viewable and copyable in
a read-only disclosure under the textarea while editing. Only one card SHALL be in edit mode at
a time. Save SHALL be disabled until the content differs from the stored value, SHALL persist
via `PUT /1.0/prompts/{name}`, and on success SHALL show a positive notification stating that
new chats and batch runs will use the saved prompt. A failed save SHALL keep the textarea
content and show a negative notification with retry. Escape in edit mode SHALL act as Cancel.

#### Scenario: Editing with the default visible

- **WHEN** the user enters edit mode on a card
- **THEN** the textarea holds the effective prompt
- **AND** the built-in default is viewable and copyable without leaving edit mode

#### Scenario: Save persists and states when it applies

- **WHEN** the user saves a modified prompt successfully
- **THEN** the UI issues `PUT /1.0/prompts/{name}` and shows a positive notification saying new
  chats and batch runs will use it
- **AND** the card's chip reflects the customized state

#### Scenario: Failed save preserves input

- **WHEN** saving a prompt fails
- **THEN** the textarea keeps the user's content
- **AND** a negative notification with a retry action is shown

#### Scenario: Save disabled without changes

- **WHEN** the textarea content equals the stored value
- **THEN** the Save action is disabled

### Requirement: Prompts can be reset to their defaults from the UI

A card whose prompt is customized SHALL offer a reset-to-default action, available in edit mode,
routed through the shared confirm modal (never `window.confirm`) whose body states that the
customized prompt will be replaced with the built-in default. Confirming SHALL issue
`DELETE /1.0/prompts/{name}` and the card SHALL then show exactly the default text that was
displayed to the user. Non-customized prompts SHALL NOT offer reset.

#### Scenario: Reset flows through confirmation

- **WHEN** the user activates reset on a customized prompt and confirms
- **THEN** the UI issues `DELETE /1.0/prompts/{name}`
- **AND** the card shows the same default text the confirm flow displayed, with the chip back to
  Default

#### Scenario: No reset on default prompts

- **WHEN** a card's prompt is not customized
- **THEN** no reset action is rendered for that card

### Requirement: Unsaved prompt edits are guarded

The UI SHALL track dirty state per editing card. Entering edit mode on another card, navigating
away in-app, or closing/reloading the page with unsaved changes SHALL require confirmation
(in-app via the shared confirm modal; page unload via a `beforeunload` guard). Cancelling an
edit with unsaved changes SHALL also confirm before discarding.

#### Scenario: Switching cards with unsaved changes

- **WHEN** a card has unsaved edits and the user activates Edit on another card
- **THEN** a confirm dialog is shown before the unsaved edits are discarded

#### Scenario: Leaving the page with unsaved changes

- **WHEN** a card has unsaved edits and the user navigates away or unloads the page
- **THEN** the user is asked to confirm before the edits are lost

### Requirement: Knowledge bases list screen

The UI SHALL provide a `/knowledge/` screen that lists the user's knowledge bases in a semantic
table showing each base's name and source count, with per-row actions to open, export, and delete
the base. The screen SHALL implement all four standard view states (loading, empty, loaded, error)
per the UX foundation, and its empty state SHALL include the CLI-equivalent command. The base name
SHALL be a real link into the detail screen; the whole row SHALL NOT be a click target.

#### Scenario: Listing knowledge bases

- **WHEN** the user opens `/knowledge/` and bases exist
- **THEN** each base is listed with its name and source count and row actions

#### Scenario: No knowledge bases yet

- **WHEN** the user opens `/knowledge/` and no bases exist
- **THEN** an empty state is shown with a create action and the `rag-cli.rag k create <name>` hint

#### Scenario: Opening a base

- **WHEN** the user activates a base's name link
- **THEN** the detail screen for that base is shown

### Requirement: Knowledge engine initialization gate

When the knowledge engine is uninitialized, the `/knowledge/` screen SHALL surface a caution
notification with an action to initialize the engine, without blocking the rest of the page. The
initialization SHALL run as a tracked asynchronous operation; on success the UI SHALL show the
resulting embedding and rerank model identifiers in a copyable form.

#### Scenario: Engine uninitialized

- **WHEN** the engine is reported uninitialized on load
- **THEN** a caution notification with an "Initialize engine" action is shown
- **AND** the rest of the page remains usable

#### Scenario: Initializing the engine from the UI

- **WHEN** the user triggers "Initialize engine"
- **THEN** the work runs as a tracked operation
- **AND** on success the embedding and rerank model identifiers are shown in a copyable snippet

### Requirement: Create and delete a knowledge base from the UI

The UI SHALL let the user create a knowledge base via a modal with a validated name field, and
delete a base via a type-to-confirm modal whose body states the source count and that the action
cannot be undone. Deletion SHALL match CLI semantics (§8 of the UX foundation). Validation errors
on create SHALL keep the modal open with a field-level message and preserve the user's input.

#### Scenario: Creating a base

- **WHEN** the user submits a valid name in the create modal
- **THEN** the base is created, the list refreshes, and a success notification is shown

#### Scenario: Create validation error

- **WHEN** creation fails validation or conflicts with an existing name
- **THEN** the modal stays open with a field-level error and the entered name is preserved

#### Scenario: Deleting a base

- **WHEN** the user opens delete for a base
- **THEN** a type-to-confirm modal states the source count and requires typing the base name
- **AND** the destructive action stays disabled until the typed name matches exactly

### Requirement: Knowledge base detail with sources

The UI SHALL provide a `/knowledge/?kb=<name>` detail view (query-param routing, read via
`useSearchParams()`) rendered by the same page as the list, with a back link to the list. The
detail view SHALL list the base's ingested sources in a table (source id, title/filename, type,
ingested time as relative-with-absolute-title), with per-source actions to view metadata and to
forget the source. Forgetting SHALL use a plain confirm modal naming the source and base. The
metadata view SHALL render the stored metadata and expose the raw JSON in a copyable block.

#### Scenario: Viewing sources

- **WHEN** the user opens a base's detail view
- **THEN** its ingested sources are listed with id, title, type, and ingested time

#### Scenario: No sources ingested

- **WHEN** a base has no sources
- **THEN** an empty state is shown with an ingest action and the CLI ingest hint

#### Scenario: Inspecting source metadata

- **WHEN** the user opens a source's metadata
- **THEN** the stored metadata is shown with the raw JSON available in a copyable block

#### Scenario: Forgetting a source

- **WHEN** the user confirms forget for a source
- **THEN** the source's chunks and metadata are removed and the list refreshes

### Requirement: Ingest a document from the UI

The UI SHALL let the user ingest a single source into a base via a modal offering an upload-file
or from-URL choice, a source-identifier field prefilled from the filename, and a force-re-ingest
option. On submit the UI SHALL start a tracked operation and close the modal immediately, letting
the row appear when the operation reports success. A duplicate-identifier error without force SHALL
keep the modal open with a field-level message and preserve the user's input.

#### Scenario: Ingesting by upload

- **WHEN** the user chooses a file, sets or accepts a source id, and submits
- **THEN** a tracked ingest operation starts and the modal closes immediately

#### Scenario: Ingesting from a URL

- **WHEN** the user enters a valid URL and submits
- **THEN** a tracked ingest operation starts for that URL

#### Scenario: Duplicate source id without force

- **WHEN** ingestion is rejected because the source id already exists and force is off
- **THEN** the modal stays open with a message telling the user to enable force re-ingest, and input is preserved

#### Scenario: In-progress hint on the detail view

- **WHEN** an ingest operation for the open base is running
- **THEN** the sources table shows a live-updating in-progress hint above it

### Requirement: Batch ingest from the UI

The UI SHALL let the user batch-ingest by uploading the YAML manifest the CLI accepts, parse it
client-side, and preview the entries (with a type indicator per entry) before starting. Each entry
SHALL join a tracked operation. Entries requiring credentials the daemon lacks SHALL fail with the
exact env-var hint (`GITHUB_TOKEN` / `GITEA_TOKEN`).

#### Scenario: Previewing a manifest

- **WHEN** the user uploads a valid manifest
- **THEN** its entries are previewed with type indicators before the batch starts

#### Scenario: Running a batch

- **WHEN** the user starts the batch
- **THEN** the entries are ingested as tracked operations with progress

#### Scenario: Missing token for a repo entry

- **WHEN** a github or gitea entry lacks its token on the daemon
- **THEN** that entry fails with the exact env-var hint and the rest proceed

### Requirement: Export and import knowledge bases from the UI

The UI SHALL let the user export a base as a tracked operation and, on success, download the
resulting archive through the browser. The UI SHALL let the user import a base via a modal whose
source is chosen between **From file** and **From Google Drive**. For **From file**, the user
uploads an archive with an optional target name and a force-overwrite option that warns inline.
Import SHALL run as a tracked operation and refresh the list on success. The import modal SHALL NOT
tell the user to drop to the CLI for Google Drive; the Drive source is provided in-modal.

#### Scenario: Exporting and downloading

- **WHEN** the user exports a base and the operation completes
- **THEN** the UI offers a browser download of the archive

#### Scenario: Importing an archive

- **WHEN** the user uploads an archive in the import modal and submits
- **THEN** a tracked import operation runs and the list refreshes on success

#### Scenario: Round-trip

- **WHEN** a base is exported and its downloaded archive is re-imported
- **THEN** the base is restored without re-embedding

#### Scenario: Choosing a source

- **WHEN** the user opens the import modal
- **THEN** the UI offers a From file / From Google Drive source chooser and shows no CLI-only Drive hint

### Requirement: Connect and disconnect Google Drive from the import modal

The UI SHALL let the user connect a Google account from the Drive source of the import modal. When
the daemon reports Drive is not configured, the UI SHALL render an information state naming the
`gdrive.client.id`/`gdrive.client.secret` package config keys and linking the Status page, instead of
a connect action. When configured but not connected, the UI SHALL offer a connect action that opens
the daemon-provided consent URL in a **new tab** (never navigating the app away) and shows a waiting
state with a focusable cancel while it polls for completion. On denial or timeout the UI SHALL show a
recoverable error with retry. Once connected, the UI SHALL show the connected account when available
and a disconnect action guarded by a confirm that deletes the stored token. The UI SHALL never render
the token or raw OAuth URLs containing secrets.

#### Scenario: Drive not configured

- **WHEN** the daemon reports Drive is not configured
- **THEN** the UI shows an information state pointing at the config keys and the Status page, with no connect button

#### Scenario: Connecting in a new tab

- **WHEN** the user starts the connect flow
- **THEN** the UI opens the consent URL in a new tab, keeps app state, and shows a waiting state with cancel

#### Scenario: Consent completes

- **WHEN** consent completes in the other tab
- **THEN** the modal advances automatically to locating archives

#### Scenario: Consent denied or times out

- **WHEN** consent is denied or times out
- **THEN** the modal shows a recoverable error with retry, distinct from the timeout case

#### Scenario: Disconnecting

- **WHEN** the user disconnects behind the confirm
- **THEN** the stored token is deleted and the connect state returns

### Requirement: Locate and pick Google Drive archives

The UI SHALL let the connected user enter a Drive folder or file URL, validate its shape client-side,
and resolve it via the daemon. Resolution errors SHALL be shown as specific, actionable messages
(not found / no access / not a rag-cli archive). For a single-file URL the UI SHALL skip picking and
go to confirm. For a folder the UI SHALL present the discovered archives as a real checkbox group
(fieldset + legend) showing name, size, and modified time, with a tri-state **Select all** checkbox
that is equivalent to the CLI `--all`, a selected count, and a **Force** checkbox with the same
overwrite semantics as local import.

#### Scenario: Single-file URL skips picking

- **WHEN** the resolved URL is a single archive file
- **THEN** the UI skips the picker and proceeds to confirm

#### Scenario: Folder select-all

- **WHEN** the resolved URL is a folder and the user checks Select all
- **THEN** all discovered archives are selected, matching the CLI `--all`

#### Scenario: Resolution error

- **WHEN** resolution fails because the account cannot access the URL
- **THEN** the UI shows a specific no-access message with an actionable next step

### Requirement: Import Google Drive archives via the operations panel

The UI SHALL start a Drive import of the selected archives, closing the modal immediately, with each
archive tracked as an operation surfaced in the global operations panel. The knowledge-base list SHALL
refresh as imports land. Partial failure SHALL be reported per archive in the operations panel rather
than as all-or-nothing messaging.

#### Scenario: Importing selected archives

- **WHEN** the user imports the selected archives
- **THEN** the modal closes and each archive appears as a tracked operation; the list refreshes as each lands

#### Scenario: Partial failure

- **WHEN** some archives import successfully and others fail
- **THEN** each archive's success or failure is visible individually in the operations panel
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

### Requirement: Search page runs retrieval-only queries

The UI SHALL provide a `/search/` page that runs hybrid retrieval (via `POST /1.0/search`)
over selected knowledge bases and displays the matching chunks, without any LLM generation —
parity with `k search` and the in-chat `/search` slash command. The page SHALL be a single
column under the app shell: a Vanilla `p-search-box` query bar (input + submit,
`aria-label="Search knowledge bases"`, Enter submits), a scope row, and the results list.
The sidebar's Search entry SHALL become a live route to this page, marked active with
`aria-current="page"` when current.

#### Scenario: Submitting a query returns ranked chunks

- **WHEN** the user enters a query with at least one knowledge base selected and submits
- **THEN** the UI issues `POST /1.0/search` with the verbatim query, the selected bases, and the chosen top-k count
- **AND** the results render in ranked order without contacting the inference server

#### Scenario: Search entry is a live route

- **WHEN** the user activates the sidebar's Search entry
- **THEN** the UI navigates to `/search/` and the entry carries `aria-current="page"`

### Requirement: Search scope is selectable via KB chips and a top-k select

The scope row SHALL offer a knowledge-base multi-select rendered as toggle chips
(`p-chip`/`p-chip--positive`, the exact pattern from the chat screen) and a compact
`<select>` labeled "Results" with options 5 / 10 / 15 / 25, defaulting to **10** (parity
with `k search --top`). Default base selection: all bases selected when exactly one exists;
otherwise the base named `default` when it exists, else all bases. Submitting with no base
selected SHALL be prevented client-side rather than surfacing the daemon's 400 error. Chips
and the select SHALL sit in tab order between the query input and the results.

#### Scenario: Default scope with multiple bases

- **WHEN** the page loads and the knowledge bases include one named `default`
- **THEN** only the `default` base chip starts selected and the Results select shows 10

#### Scenario: Toggling scope chips

- **WHEN** the user toggles a base chip off so that no base remains selected
- **THEN** submission is prevented and the UI indicates at least one base is required

### Requirement: Search query and scope round-trip through the URL

The query, selected bases, and top-k SHALL persist in the URL (`/search/?q=…`) so a search
is shareable and reloadable. Loading a URL containing a query SHALL restore the scope and
re-run the search automatically.

#### Scenario: Reload reproduces the search

- **WHEN** the user reloads a URL of the form `/search/?q=<query>&…` produced by a previous search
- **THEN** the query bar, base chips, and Results select restore the encoded state
- **AND** the same search runs and renders results without further input

### Requirement: Search results render full chunks with score and provenance

Each hit SHALL render as one card in ranked order showing: a header with the rank number,
the source ID, the knowledge base name as a non-interactive chip, and the relevance score
right-aligned to 3 decimals; the chunk's full content preserving paragraph breaks and
without truncation; and a footer with provenance details in small text. The results region
SHALL be announced via `aria-live="polite"` as "N results", be preceded by an off-screen
"Results" heading, and focus SHALL remain in the query input after submit. The source ID
SHALL render as plain text until a knowledge-detail route exists to link to.

#### Scenario: Result card contents

- **WHEN** a search returns hits
- **THEN** each card shows rank, source ID, KB name chip, and the score to 3 decimals
- **AND** the complete chunk content renders untruncated with paragraph breaks preserved
- **AND** the output matches what `k search` prints for the same query (chunks, scores, provenance)

#### Scenario: Results announced to assistive tech

- **WHEN** a search completes with N hits
- **THEN** a polite live region announces "N results" and focus is still in the query input

### Requirement: Search page distinguishes initial, loading, no-hits, no-KBs, and error states

The page SHALL implement distinct states: **initial** (no query yet) — an empty state
explaining hybrid semantic + lexical retrieval with reranking, no LLM, including the CLI
hint `rag-cli.rag k search "<query>"`; **loading** — a spinner replaces the results area and
the submit control is disabled to prevent double-submit; **no hits** — a message naming the
searched bases and suggesting widening the base selection or raising top-k; **no knowledge
bases exist** — a caution notification linking to create/ingest a knowledge base first;
**error** — the standard error notification, with the standard daemon-unreachable message
for connection failures. No-hits, no-KBs, and error SHALL be visually and semantically
distinct states.

#### Scenario: Initial state

- **WHEN** the page loads without a query in the URL
- **THEN** an empty state explains retrieval-only search and shows the CLI equivalent command

#### Scenario: No hits vs error are distinct

- **WHEN** a search succeeds with zero hits
- **THEN** the UI shows the no-hits message naming the searched bases — not an error notification
- **AND** a failed request instead shows a negative notification with the daemon error message

#### Scenario: No knowledge bases exist

- **WHEN** the page loads and `GET /1.0/knowledge` returns no bases
- **THEN** a caution notification explains a knowledge base must be created and ingested first

### Requirement: Chat composer supports slash commands

The chat screen's composer SHALL recognize input starting with `/` as a slash command
instead of a prompt, supporting `/save [title]` and `/history` with the same semantics as
the REPL. While the input starts with `/`, the composer SHALL show a filtering hint list
of matching commands (mirroring the REPL's slash hints) that can be navigated and
accepted by keyboard. An unknown slash command SHALL show the list of available commands
inline without sending anything to the daemon. `/save` SHALL send the `save` control
message over the open chat websocket and surface the returned title in a positive
notification; saving with no completed turns SHALL surface the rejection message.

#### Scenario: Composer hints while typing a slash command

- **WHEN** the user types `/` in the chat composer
- **THEN** a hint list shows the available slash commands, narrowing as the user types
- **AND** the highlighted command can be accepted from the keyboard

#### Scenario: Saving from the composer

- **WHEN** the user submits `/save release notes` during an active session
- **THEN** the UI sends the `save` control message with that title over the websocket
- **AND** shows a positive notification naming the saved title

#### Scenario: Unknown command stays local

- **WHEN** the user submits `/frobnicate`
- **THEN** the UI lists the available slash commands and sends nothing over the websocket

### Requirement: Chat history panel lists, searches, and resumes saved chats

Submitting `/history` (or activating an equivalent History control on the chat screen) SHALL
open a history panel listing saved chats from `GET /1.0/chats` newest-first, each
row showing title, relative last-updated time, model, and turn count. A search box SHALL
filter the list via the endpoint's `search` parameter. Selecting a chat SHALL resume it:
the UI starts a new session via `POST /1.0/chat` with the saved-chat id, replaces the
transcript view with the restored conversation, and applies the restored knowledge-base
selection to the chips. The panel SHALL be keyboard-operable and close on Escape without
side effects. When no chats exist the panel SHALL show the shared empty-state component
including the `/save` guidance.

#### Scenario: Resuming from the panel

- **WHEN** the user opens the history panel and selects a saved chat
- **THEN** the UI issues `POST /1.0/chat` with that chat's id, renders the restored transcript, and updates the active-base chips to the saved set
- **AND** the next prompt continues that conversation

#### Scenario: Searching the panel

- **WHEN** the user types "bedrock" in the panel's search box
- **THEN** the list refreshes via `GET /1.0/chats?search=bedrock` and shows only matching chats

#### Scenario: Dismissing the panel

- **WHEN** the panel is open and the user presses Escape
- **THEN** the panel closes and the current conversation is unchanged

#### Scenario: Empty history state

- **WHEN** no chats have been saved and the panel opens
- **THEN** the shared empty-state component explains saving with `/save`

### Requirement: Saved chats can be deleted from the history panel

Each history panel row SHALL offer a delete action routed through the shared confirm
modal (never `window.confirm`), naming the chat title in the confirmation body.
Confirming SHALL issue `DELETE /1.0/chats/{id}` and remove the row; a failed deletion
SHALL surface the API error without removing the row. Deleting a chat SHALL NOT
interrupt the live session, including when the live session was resumed from that chat.

#### Scenario: Deleting a saved chat

- **WHEN** the user activates delete on a history row and confirms in the modal
- **THEN** the UI issues `DELETE /1.0/chats/{id}` and the row disappears from the panel

#### Scenario: Failed deletion keeps the row

- **WHEN** the delete request fails
- **THEN** the row remains and the API error message is shown
