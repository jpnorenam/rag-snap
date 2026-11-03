## ADDED Requirements

### Requirement: Judge verdicts are shown in results review

The batch results review surface SHALL show, on each question card whose result carries a verdict:
- a rating badge (green, yellow or red);
- the confidence score;
- the verdict comment.

The badge SHALL be distinct from the answer text. The comment SHALL appear in the card without being expanded. A not-judged result SHALL show a neutral "Not judged" badge and its reason.

When the result set carries a `judge` summary, the surface SHALL show a summary strip with the count per rating. The surface SHALL also offer a filter that limits the cards to chosen ratings. Every card SHALL keep its spreadsheet row number (first answer = row 2) when the cards are filtered.

A results file with no verdicts SHALL render exactly as before, with no badges, no summary strip and no filter.

While a batch operation is in its judging phase, the running view SHALL show judging progress.

#### Scenario: Verdict on a card

- **WHEN** a result carries a red verdict with a comment
- **THEN** its card shows a red badge, the score and the comment

#### Scenario: Filtering to the rows that need attention

- **WHEN** the reviewer filters to red and yellow
- **THEN** only red and yellow cards are shown
- **AND** each shows its original row number

#### Scenario: Unjudged file

- **WHEN** a user opens a results file with no verdicts
- **THEN** the review surface renders without badges, summary strip or filter

#### Scenario: Judging progress

- **WHEN** a tracked batch operation reports phase `judging`
- **THEN** the running view shows judging progress, not answering progress
