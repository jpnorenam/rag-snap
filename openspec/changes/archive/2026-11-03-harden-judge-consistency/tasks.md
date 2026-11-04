## 1. Schema, prompt and decoding

- [x] 1.1 Change `consistencySchema` in `cmd/cli/basic/analysis/judge_schema.go` to `rows: [{row, phrase}]`, both required. It must use only constructs Gemini's structured-output subset has accepted.
- [x] 1.2 Add the quote-per-row requirement to `consistencySystemPrompt`, keeping the contradictions-only and no-rule-violations text.
- [x] 1.3 Implement phrase normalisation and matching: case, whitespace, curly quotes, trimmed quotes and punctuation, ellipsis fragments in order.
- [x] 1.4 In `Judge.Consistency`, drop rows outside the run or with an unmatched phrase, discard findings with fewer than 2 rows or an empty comment, and return the discarded count.
- [x] 1.5 Unit-test matching and decoding, including the three telco-run findings: the two real ones kept, and the customer-names one kept as broad.

## 2. Applying findings

- [x] 2.1 Extend `chat.ConsistencyFinding` with `Rows []ConsistencyRow{Row, Phrase}` and `Broad`, and update the `AnswerJudge.Consistency` signature to return the discarded count.
- [x] 2.2 Rewrite `applyConsistency`: mark findings with more than 8 rows broad and leave rows untouched for them; otherwise add the "conflicts with row N" note naming only the other rows, cap at 3, and set `PreConsistencyConfidence` when the cap lowers the score.
- [x] 2.3 Add `ConsistencyFindings` and `ConsistencyDiscarded` to `JudgeSummary`, filled in `summariseWithConsistency`.
- [x] 2.4 Add the consistency section to `FormatJudgeSummary`.
- [x] 2.5 Unit tests in `cmd/cli/basic/chat/judge_test.go` for every scenario in the `answer-judge` delta spec.

## 3. API and UI

- [x] 3.1 Document the new results fields in `rest-api.yaml` and `docs/rest-api.md`.
- [x] 3.2 Add the fields to `ui/lib/types.ts` and any normalisation in `ui/lib/results.ts`.
- [x] 3.3 Render the run-level findings list in `ResultsReview.tsx`, with row buttons that clear a hiding filter and scroll to the card, the broad badge and the discarded count.
- [x] 3.4 Show "lowered from N by consistency" on cards with `pre_consistency_confidence`.
- [x] 3.5 Component tests in `ResultsReview.test.tsx` and `QACard.test.tsx` for the `local-ui-app` delta scenarios, including an older results file.
- [ ] 3.6 Verify compliance with the `ui-conventions` skill: both themes, a keyboard-only pass, `--vf-*` tokens only, and all four view states.

## 4. Docs and verification

- [x] 4.1 Update the judge-summary description in `docs/usage.md` and the results-review section in `docs/local-ui.md`.
- [x] 4.2 Run `make all` and the UI test suite.
- [x] 4.3 Build and install the snap, then re-run the telco manifest with judging through the UI. Confirm that no comment carries a run-wide row list, that the real contradictions are applied to their rows, and that the green/yellow/red split is no longer dominated by consistency caps.
- [ ] 4.4 Calibrate (carried over from `add-answer-judge` 8.4): re-run the hand-rated reference manifest with judging and compare it against the human ratings (136/32/24). Report judge-green rows that a human rated red, false reds, and the measured cost and wall-clock time, then record a default judge model recommendation. This needs human ratings of the new run; leave it open until they exist.
