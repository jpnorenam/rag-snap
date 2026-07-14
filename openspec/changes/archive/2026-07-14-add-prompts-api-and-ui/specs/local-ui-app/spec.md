# local-ui-app Specification (delta)

## ADDED Requirements

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
