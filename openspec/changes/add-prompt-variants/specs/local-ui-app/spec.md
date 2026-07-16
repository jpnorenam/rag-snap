# local-ui-app Delta Specification

## ADDED Requirements

### Requirement: Prompt variants can be managed from the Prompts page

Each generation-slot card on the Prompts page SHALL offer variant management: a radio group
(fieldset with legend) listing the stored variants (with head-version tags) and the built-in
default, where selecting a radio activates that choice immediately; per-variant row actions to
open a version history, edit that variant (appending a version without changing the active
selection), and delete it; and a new-variant editor pre-filled with the current effective prompt
(fork semantics) whose name field is validated client-side against the API's naming rule.
Activation, save, create, and restore SHALL use the variants API and on success show a
notification, rendered inside the card acted on, stating that new chats and batch runs will use
the change (or, for a non-active variant edit, that the active prompt is unchanged). Deleting
the active variant SHALL be disabled with the reason exposed in the control's accessible name.
The version-history view SHALL show each version with a content preview, the full text on
demand, and a restore action. The `source_rules` card SHALL keep the single-override edit and
reset controls only — no variant selector, save-as, or version history (that slot exposes no
variants over the API). All controls SHALL be keyboard-reachable and labelled; state SHALL NOT
be conveyed by color alone.

#### Scenario: Activating a variant from the card

- **WHEN** the user selects a stored variant's radio on a generation-slot card
- **THEN** the variant is activated, the card's chip and preview follow it, and a notification
  inside that card states that new chats and batch runs will use it

#### Scenario: Editing a non-active variant

- **WHEN** the user chooses Edit on a variant row that is not active and saves a change
- **THEN** a new version is appended to that variant while the active selection is unchanged

#### Scenario: Creating a new variant forks the current prompt

- **WHEN** the user chooses New variant on a generation-slot card
- **THEN** the editor opens pre-filled with the current effective prompt and a name field
- **AND** on create with a valid name the variant appears in the card's radio group

#### Scenario: Restoring an old version

- **WHEN** the user opens a variant's history and restores an earlier version
- **THEN** the variant's effective content becomes that version's text via a newly appended head version

#### Scenario: Guardrail card has no variants

- **WHEN** the user views the `source_rules` card
- **THEN** it offers the single-override edit and reset controls only, with no variant selector,
  save-as, or version history

## MODIFIED Requirements

### Requirement: Prompts page shows the three prompt templates with their state

The UI SHALL provide a Prompts page at `/prompts/` (with the sidebar entry becoming a live
route) rendering the three prompt templates as three stacked cards in the fixed order
`chat_system_prompt`, `answer_system_prompt`, `source_rules`, sourced from `GET /1.0/prompts`.
Each card SHALL show a title, a state chip with a text label reading Default or Customized (not
conveyed by color alone), and a read-only preview of the first lines of the *effective* prompt.
Generation-slot cards SHALL additionally name the active variant when one is active. Cards SHALL
be `<section>` elements labelled by their headings. While the prompts are loading, the page
SHALL render three fixed-height skeleton cards without layout shift; when loading fails, the
page SHALL show the standard error state and block editing. There is no empty state — defaults
always exist.

#### Scenario: Cards render with state

- **WHEN** the Prompts page loads successfully
- **THEN** three cards render in the fixed order, each with its title, a Default or Customized
  chip, and a preview of the effective prompt text
- **AND** a generation-slot card with an active variant names that variant

#### Scenario: Load failure blocks editing

- **WHEN** the prompts cannot be fetched
- **THEN** the page shows the standard error notification with a retry action
- **AND** no card can enter edit mode
