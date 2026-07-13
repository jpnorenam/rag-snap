# ui-app-shell Specification (delta)

## ADDED Requirements

### Requirement: Sidebar provides real multi-page navigation

The sidebar SHALL render each implemented section as a client-side navigation link
(`next/link`) in the order: Chat (`/`), Knowledge bases (`/knowledge/`), Search (`/search/`),
Answer RFPs (`/answer/`), Prompts (`/prompts/`), with Status (`/status/`) pinned to the bottom
of the rail above the dark-mode toggle as a utility item. Sections whose screens do not yet
exist SHALL render as non-focusable placeholder elements with a "Soon" badge and SHALL NOT be
links or buttons. Routes SHALL be static-export pages under `ui/app/<route>/page.tsx`,
honouring `basePath /ui` and `trailingSlash: true`.

#### Scenario: Navigating to an enabled section

- **WHEN** the user activates an enabled sidebar entry
- **THEN** the app navigates client-side to that section's route without a full document reload

#### Scenario: Unimplemented sections stay placeholders

- **WHEN** a section's screen has not been implemented yet
- **THEN** its sidebar entry renders as a non-focusable element with a "Soon" badge
- **AND** it is not reachable by keyboard focus and triggers no navigation

### Requirement: Active section is reflected in the sidebar and titles

The sidebar SHALL derive the active entry from the current pathname, marking it with
`aria-current="page"` and the existing active-state styling (3px orange left border). The
`<Header>` title SHALL reflect the active section and `document.title` SHALL be set to
`"<Section> — RAG"` on every route change.

#### Scenario: Active state follows navigation

- **WHEN** the user navigates to a section
- **THEN** that section's sidebar entry has `aria-current="page"` and the active styling
- **AND** no other entry is marked active

#### Scenario: Titles update on route change

- **WHEN** the route changes
- **THEN** the header shows the new section's title
- **AND** `document.title` becomes `"<Section> — RAG"`

### Requirement: Status icon joins the NavIcon set

The sidebar icon set SHALL remain inline line-SVG icons rendered through the `NavIcon`
component (stroke `currentColor`, 20×20, `viewBox 0 0 24 24`, `aria-hidden`), and SHALL gain a
`"status"` icon (pulse/heartbeat line) for the Status entry. No icon library SHALL be added.

#### Scenario: Status entry renders its icon

- **WHEN** the sidebar renders
- **THEN** the Status entry shows the pulse/heartbeat line icon via `NavIcon`
- **AND** the icon is `aria-hidden` decorative SVG

### Requirement: Collapsed rail stays usable

At viewport widths of 620px and below the sidebar SHALL collapse to the icon-only rail,
showing icons and the active indicator, with each enabled entry's label available as a
`title` tooltip. The page SHALL NOT scroll horizontally at this width.

#### Scenario: Icon rail at narrow width

- **WHEN** the viewport is 620px wide or narrower
- **THEN** the sidebar shows only icons plus the active indicator
- **AND** enabled entries expose their label via `title`
- **AND** the page has no horizontal scroll

### Requirement: Shared UI primitives live in components/common

The UI SHALL provide reusable primitives in `ui/components/common/` for later screens to
import rather than re-implement: an `EmptyState` component (muted icon, one-line headline,
one sentence of guidance including the CLI-equivalent command, primary action), a `Spinner`
component (spinner icon plus visible text label), and a generalized `.app-status-dot` style
(caution/positive/negative variants) derived from the existing `.chat__status-dot`.

#### Scenario: Empty state includes the CLI equivalent

- **WHEN** a screen renders the shared `EmptyState`
- **THEN** it shows a headline stating what is missing and guidance that includes the CLI command equivalent
- **AND** it offers the primary action for creating the missing thing

#### Scenario: Spinner is accessible

- **WHEN** a screen renders the shared `Spinner`
- **THEN** the spinner icon is `aria-hidden` and accompanied by visible loading text

### Requirement: Confirm modal primitive with type-to-confirm variant

The UI SHALL provide a `ConfirmModal` component in `ui/components/common/` implementing the
Vanilla `p-modal` pattern (`role="dialog"`, `aria-modal="true"`, `aria-labelledby`) with two
variants: a plain confirm (object named in the body) and a type-to-confirm variant whose
destructive `p-button--negative` action stays disabled until the user types the object's exact
name. The modal SHALL trap focus, move focus into the dialog on open, restore focus on close,
and close on Escape and overlay click. `window.confirm` SHALL NOT be used anywhere in the UI.

#### Scenario: Type-to-confirm gates the destructive action

- **WHEN** the type-to-confirm variant is open and the input does not exactly match the object name
- **THEN** the destructive button is disabled
- **AND** it enables only when the input matches exactly

#### Scenario: Modal focus management

- **WHEN** the modal opens
- **THEN** focus moves into the dialog and cannot tab out of it
- **AND** closing the modal (confirm, cancel, Escape, or overlay click) restores focus to the triggering element
