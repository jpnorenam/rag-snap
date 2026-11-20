## MODIFIED Requirements

### Requirement: A consistency pass compares answers across the run

After every answer has been judged, the run SHALL make one consistency call. That call SHALL receive every answered result's position, id, question and answer, plus the system prompt, and no retrieved context.

The call SHALL report contradictions only, and SHALL NOT re-report rule violations such as markdown, length or customer naming, which are already reported on each answer. Each finding SHALL name the rows involved and, for each row, quote the phrase in that row's answer that takes part in the contradiction, together with a comment saying what conflicts and what the reviewer should reconcile.

Each finding SHALL be verified locally before it is used:
- a row outside the run SHALL be dropped;
- a row whose quoted phrase does not occur in that row's answer, compared ignoring case and runs of whitespace, SHALL be dropped;
- a finding left with fewer than two rows, or with an empty comment, SHALL be discarded.

The number of findings discarded SHALL be recorded.

A kept finding with more than 8 rows is broad. A broad finding SHALL NOT be attached to any row and SHALL NOT change any row's confidence. It SHALL be kept only at run level.

Every other kept finding SHALL add to each row it names a short note. The note SHALL name the other rows in the finding and carry the finding's comment. It SHALL NOT repeat the row's own number. The confidence of each of those rows SHALL then be capped at 3, which makes the row at most yellow. When the cap lowers a row's confidence, the verdict SHALL record the confidence it had before the pass.

If the consistency call fails, the per-answer verdicts SHALL be kept unchanged, and the failure SHALL be reported.

#### Scenario: Contradicting figures

- **WHEN** one answer states 36,000 packages and another states 25,000 for the same repository, and the finding quotes each figure from its row
- **THEN** both rows carry a note that names the other row and the conflict
- **AND** neither row is rated green
- **AND** a row that was green before the pass records its earlier confidence

#### Scenario: Quoted phrase is not in the answer

- **WHEN** a finding names rows 37 and 52 but the phrase quoted for row 52 does not occur in row 52's answer
- **THEN** row 52 is dropped, the finding is left with one row and is discarded, and the discarded count is incremented
- **AND** neither row's verdict changes

#### Scenario: Run-wide remark returned as a finding

- **WHEN** the consistency call returns one finding about customer names that names 284 rows, each with a phrase found in its answer
- **THEN** the finding is kept at run level and marked broad
- **AND** no row's comment or confidence changes

#### Scenario: Consistency call fails

- **WHEN** the consistency call errors
- **THEN** every per-answer verdict is retained
- **AND** the run reports that the consistency pass did not complete

### Requirement: Verdicts are recorded on the results and summarised for the reviewer

Each judged `BatchResult` SHALL carry its verdict under a `judgment` key. The `BatchOutput` SHALL carry a `judge` summary holding:
- the judge model;
- the count of green, yellow, red and not-judged rows;
- whether the consistency pass completed;
- every kept consistency finding, with its rows, each row's quoted phrase, its comment, and whether it is broad;
- the number of consistency findings discarded by verification.

Both keys SHALL be omitted when judging did not run. A verdict whose confidence the consistency pass lowered SHALL carry the confidence from before the pass.

When a judged run completes, the CLI SHALL print the counts. It SHALL then list every red row and every yellow row, identified by spreadsheet row number, with the id in brackets and the comment. The first result is row 2. The summary SHALL state that row 2 is the first answer. When there are kept consistency findings, the summary SHALL list each one once, in its own section, with its rows and comment, marking broad findings as not applied to rows. It SHALL state how many findings were discarded when any were.

#### Scenario: Results carry verdicts

- **WHEN** a judged batch completes
- **THEN** every result in the file has a `judgment`
- **AND** the file has a `judge` summary whose counts sum to the number of results

#### Scenario: Terminal summary by row

- **WHEN** the third result is rated red
- **THEN** the summary lists it as row 4 with its id and comment

#### Scenario: Findings printed once

- **WHEN** a judged run has one applied finding over rows 47 and 263 and one broad finding
- **THEN** the terminal summary has a consistency section listing each finding once
- **AND** the broad finding is marked as not applied to rows

#### Scenario: Unjudged results keep their shape

- **WHEN** a batch runs with judging disabled
- **THEN** its results file has no `judgment` or `judge` keys
