## Context

The consistency pass (`add-answer-judge`) sends every answer to Gemini once, gets back `findings: [{rows: [int], comment}]`, and `applyConsistency` (`cmd/cli/basic/chat/judge.go`) appends `Consistency (rows …): <comment>` to every named row and caps each row at confidence 3. The only local checks are that rows exist and the comment is non-empty.

In the telco RFP run the model returned three findings:

| Finding | Rows | Type |
|---|---|---|
| SMB 3.x, row 37 | 2 | real contradiction |
| S3 replication, rows 47 and 263 | 3 | real contradiction |
| "Repeated violation of the rules against naming other customers" | 284 | rule violation, which the prompt forbids |

The third added a 284-number row list to 284 comments and moved 284 rows to yellow or red. Nothing recorded the pre-pass confidences, so the damage cannot be undone from the file.

The pass is still valuable: the two real findings are exactly what a per-row judge cannot see. The goal is to keep that while making a non-contradiction structurally unable to touch rows.

## Goals / Non-Goals

**Goals:**
- A finding affects a row only if it quotes that row and the quote is really there.
- A finding spanning much of the run cannot rewrite or downgrade rows.
- Each finding is stored and shown once. Rows get a pointer, not a copy.
- The pass's effect on scores is recorded and reversible.

**Non-Goals:**
- Repairing existing results files, or a standalone "re-judge a results file" command. That could be a later change.
- Detecting customer-name leaks in the consistency pass. Per-row judging already reports `other_customer`, and scrubbing knowledge-base source titles is a separate concern.
- Making the broad-finding threshold configurable.

## Decisions

### Quote-per-row schema, verified locally

The schema's `rows` becomes an array of `{row: integer, phrase: string}`, both required. The model has to commit to a specific phrase per row, which a blanket remark cannot do honestly at scale. The decoder normalises before matching: it lowercases, collapses whitespace, maps curly quotes to straight, and trims surrounding quotes and punctuation. A phrase containing an ellipsis (`…` or `...`) is split into fragments, and every fragment must occur in order. This tolerates the model abbreviating a long quote.

Verification lives in `analysis` (the Gemini decoder), next to the existing row-existence filtering. `chat.ConsistencyFinding` then carries only verified rows plus their phrases, and the decoder returns the discarded count with the findings.

*Alternative considered:* rely on the prompt alone, which already forbids the behaviour. Rejected: the run shows the prompt is not enough.

*Alternative considered:* require an `issue_type` enum and drop non-contradictions. Rejected: a model that ignores "contradictions only" would label its finding a contradiction just as readily. A quote that must exist is checkable, and an enum is not.

### Broad threshold: more than 8 verified rows

Real contradictions in the reviewed runs involve 2–3 rows. A conflicting value repeated across a section, such as a support term stated two ways, could reach perhaps 6–8 rows. A fixed cap of 8 keeps those and refuses run-wide remarks on any run size. A percentage would be meaningless for small runs: 10% of 20 rows is 2. Broad findings are kept at run level rather than dropped, so a genuinely wide contradiction is still visible, just without touching scores.

### Store once, point from rows

`JudgeSummary` gains:

```go
ConsistencyFindings  []ConsistencyFinding `json:"consistency_findings,omitempty"`
ConsistencyDiscarded int                  `json:"consistency_discarded,omitempty"`
```

`ConsistencyFinding` gets `Rows []ConsistencyRow{Row, Phrase}`, `Comment` and `Broad bool`. The per-row note becomes `Consistency: conflicts with row 263 — <comment>`, naming only the other rows. The comment is the model's one-paragraph comment, so it stays short. `FormatJudgeSummary` prints a "Consistency" section after "Not judged".

### Record the pre-pass confidence

`Judgment` gains `PreConsistencyConfidence int` (`json:"pre_consistency_confidence,omitempty"`). It is set only when the cap lowers the score, so files without consistency effects are unchanged. The markdown cap runs earlier, in the local checks, so the recorded value is the per-answer verdict after local checks, which is what a reviewer would compare against.

### UI

`ResultsReview` renders the findings inside the existing judge summary strip: a list with the comment, row-number buttons and a "not applied to rows" badge for broad ones. A row button clears a filter that would hide the target card and scrolls it into view, reusing the card's row-number anchor. Cards with `pre_consistency_confidence` show "lowered from N by consistency" beside the score. Follow the `ui-conventions` skill: Vanilla patterns, `--vf-*` tokens, both themes, keyboard operable.

### Prompt

The consistency prompt keeps its "contradictions only / no rule violations" text and adds that each row in a finding must quote the phrase from that row's answer that conflicts. Output-budget sizing is unchanged: the quotes add only a few tokens per finding.

### Configuration, secrets, snap

No config keys, secrets, snap interfaces or hooks. `GEMINI_API_KEY` handling is unchanged.

## Risks / Trade-offs

- [The model quotes a trivially common phrase, such as "Supported.", for many rows] → Mitigation: verification passes, but the broad cap stops it touching rows. A short common phrase on 2–8 rows remains possible, and the reviewer sees the quoted phrases in the run-level list.
- [A real contradiction across more than 8 rows is not applied to rows] → Accepted: it is still listed once at run level and in the terminal summary.
- [The model paraphrases rather than quotes, so real findings get discarded] → Mitigation: tolerant normalisation and ellipsis fragments, plus the discarded count surfaced to the reviewer. If live runs show high discard rates, loosen the matching rather than the rule.
- [The change modifies a capability that is not archived yet] → Archive `add-answer-judge` first. Both changes ship from `feat/llm-judge`.

## Migration Plan

Additive JSON fields. The UI and CLI accept files without them. Results produced before this change keep their capped scores. Re-run the batch to get honest ratings.
