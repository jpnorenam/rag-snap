## ADDED Requirements

### Requirement: Consistency findings are shown once in results review

When the result set's `judge` summary carries consistency findings, the results review surface SHALL list them once, in the summary area, and not repeat them on every card.

Each listed finding SHALL show its comment and its rows by spreadsheet row number (first answer = row 2). Each row number SHALL be a control that brings that row's card into view, clearing a rating filter that would hide it. A broad finding SHALL be marked as not applied to rows. When findings were discarded by verification, the surface SHALL say how many.

A card whose confidence the consistency pass lowered SHALL indicate that, showing the confidence from before the pass beside the current one.

A results file without these fields SHALL render as before.

#### Scenario: Findings listed once

- **WHEN** a user opens a results file with one applied finding over rows 47 and 263 and one broad finding
- **THEN** the summary area lists both findings once, the broad one marked as not applied to rows
- **AND** no card repeats the broad finding

#### Scenario: Jumping to a row

- **WHEN** the reviewer has filtered to red and activates row 263 in a finding, and row 263 is yellow
- **THEN** the filter is cleared and row 263's card is brought into view

#### Scenario: Lowered confidence

- **WHEN** a card's verdict records a confidence from before the pass of 5 and a current confidence of 3
- **THEN** the card shows that the consistency pass lowered it from 5

#### Scenario: Older results file

- **WHEN** a user opens a judged results file with no `consistency_findings`
- **THEN** the review surface renders as before
