## Why

In a telco RFP run (318 answers), the consistency pass returned a finding that was not a contradiction. It was a run-wide remark about customer names in the Reference fields, naming 284 rows. The comment and its 284-row list were pasted into 284 verdicts. Worse, `applyConsistency` capped every one of those rows at confidence 3. The run ended with 3 green rows, and nobody can tell which answers were really paste-ready. The prompt already forbids reporting rule violations, so this needs to be enforced in code, not asked for in the prompt.

## What Changes

- **Findings must carry evidence per row.** The consistency schema changes from `rows: [int]` to `rows: [{row, phrase}]`, quoting each row's conflicting phrase. A row is kept only if its phrase actually occurs in that row's answer. A finding left with fewer than two verified rows is discarded, because a contradiction needs two sides.
- **Broad findings never touch rows.** A finding with more than 8 verified rows is not attached to rows and caps no confidence. It is kept at run level, marked broad, for the reviewer to read once.
- **Findings are stored once, at run level.** The `judge` summary gains `consistency_findings` (every kept finding, with rows, phrases and comment) and `consistency_discarded` (the count dropped by verification). Each affected row gets a short note naming the *other* rows and the conflict, instead of the full finding and its row list.
- **The original score is preserved.** A row whose confidence the pass lowers records `pre_consistency_confidence`, so the effect of the pass can be seen and undone without re-judging.
- **Reporting.** The CLI's terminal summary prints the findings once, in their own section. The UI results review shows them once in the summary area, with links to the rows.
- The consistency prompt keeps its rule against reporting rule violations, and states that each side must be quoted.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `answer-judge`: the consistency-pass requirement (evidence per row, verification, the broad-finding threshold, the per-row note, the preserved confidence), and the verdict-recording requirement (the run-level findings and the discarded count in the `judge` summary and in the terminal summary). `answer-judge` is introduced by the in-flight `add-answer-judge` change, so archive this change after that one.
- `local-ui-app`: results review shows run-level consistency findings once.

## Impact

- **External services**: only the Gemini judge call changes, through its response schema and prompt. **OpenSearch**, the **chat inference server** and **Tika** are not touched.
- **Code**: `cmd/cli/basic/analysis/judge_schema.go`, `judge_prompt.go` and `judge.go` (schema, prompt, decoding); `cmd/cli/basic/chat/judge.go` (`ConsistencyFinding`, `applyConsistency`, `JudgeSummary`, `Judgment`, `FormatJudgeSummary`); `ui/lib/types.ts` and `ui/lib/results.ts`; and `ui/components/answer/ResultsReview.tsx`.
- **Config keys**: none. The broad-finding threshold is a constant.
- **Results file**: additive fields only (`judge.consistency_findings`, `judge.consistency_discarded`, `judgment.pre_consistency_confidence`). Older results files still render. `rest-api.yaml` documents the results shape and needs the new fields.
- **User-facing surfaces**: the terminal summary gains a consistency section, and the UI results review gains a findings list. Documentation to update: `docs/usage.md` (judge summary), `docs/local-ui.md`, `docs/rest-api.md` and `rest-api.yaml`.
- **Existing results** such as the earlier results file are not repaired by this change: their original confidences were never recorded. Re-run judging to get honest ratings.
