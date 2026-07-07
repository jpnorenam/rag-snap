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