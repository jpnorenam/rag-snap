## 1. Configuration and gating

- [x] 1.1 Add `JudgeSettings` and `LoadJudgeSettings` to `cmd/cli/basic/analysis/judge.go`, reading `judge.enabled`, `judge.model` (falling back to `analysis.model`), `judge.reasoning` (via `loadEffort`, so `none` is rejected) and `judge.concurrency` (a positive integer, default 4); export the `ConfJudge*` key names
- [x] 1.2 Add `ValidateJudge(JudgeSettings)`, which collects every missing prerequisite (gate off, no model, no `GEMINI_API_KEY`) into one `NotConfiguredError`, and `NewJudgeClient`, which shares `newClient`'s pinned SDK configuration
- [x] 1.3 Seed `judge.enabled=true`, `judge.model=""`, `judge.reasoning=medium` and `judge.concurrency=4` as package keys in `snap/hooks/install`, and through the set-if-absent helper in `snap/hooks/post-refresh`
- [x] 1.4 Unit-test settings loading (defaults, fallback model, bad concurrency, `none` rejected) and validation messages against a fake `storage.Config`

## 2. Judge prompt, schema and local checks (analysis package)

- [x] 2.1 Write the judge system prompt in `judge_prompt.go`. It holds the anchored 1–5 rubric and these rules: context-only judging; quote the offending phrase; state the action; never supply an outside fact as a correction; `other_customer` even when the name is grounded; volunteered limitations count as `rule_violation`; the source-tag precedence; paraphrase is allowed
- [x] 2.2 Render the per-answer turn: the batch system prompt (as "rules the answer had to follow"), domain context, question id and text, the answer, the local facts (word and sentence counts) and the retrieved context, with the stable rubric prefix first so it is cacheable
- [x] 2.3 Define the verdict schema in `judge_schema.go`, with `comment`, `confidence` (integer, min 1, max 5) and `issues` (string enum), in that alphabetical order, and the consistency schema (findings, each with an integer `positions` array and a `comment`), using only the constructs proven live
- [x] 2.4 Implement `JudgeAnswer` and `CheckConsistency` on `*Client` via `complete()`, with a judge-sized output ceiling. Validate locally: confidence range, known issues, a non-empty comment below 5, and discard positions outside the run
- [x] 2.5 Implement local checks in `chat/judge_checks.go`: markdown detection (bold, italic, backticks, headings, list markers) and word and sentence counts
- [x] 2.6 Unit-test prompt rendering, response validation (valid, out of range, unknown issue, empty comment below 5) and the markdown and count checks
- [x] 2.7 Add judge-schema cases to `schema_live_test.go` (build tag `live`) and run `GEMINI_API_KEY=… go test -tags live -run Live -v ./cmd/cli/basic/analysis/` to confirm Gemini accepts both schemas
- [x] 2.8 Widen the `analysis` package doc to cover answer judging

## 3. Batch integration (chat package)

- [x] 3.1 Add a `Judgment` type (confidence, rating, issues, comment, error) to `BatchResult` and a `JudgeSummary` type (model, green, yellow, red, not_judged, consistency) to `BatchOutput`, both `omitempty`, plus a rating-from-confidence helper
- [x] 3.2 Define the `AnswerJudge` interface and its `JudgeInput`, `ConsistencyInput` and `ConsistencyFinding` types in `chat/judge.go`, implemented directly by `*analysis.Judge`, since `analysis` already imports `chat`
- [x] 3.3 Give `RunBatch` an optional `AnswerJudge` (nil means off) and a concurrency value. Each answered question enqueues a job carrying its position, evidence and domain context, and the evidence is released once judged. Gap answers get the local red verdict with no job. Markdown caps confidence at 4 and appends to the comment
- [x] 3.4 Implement the bounded worker pool: index-addressed verdicts, retry of `QuotaError` and 5xx up to 3 times with jittered exponential backoff from 2s, no retry for truncated, blocked or malformed responses, and cooperative cancellation
- [x] 3.5 After the pool drains, run the consistency call over the answered results. Append each finding to every named row's comment, cap those rows at confidence 3, and on failure keep the verdicts and set `consistency: false`
- [x] 3.6 Add `BatchHooks.OnAnswered(out)`, which fires when answering ends and before judging completes, and `OnJudged(i, total, result)` for progress
- [x] 3.7 Compute the `JudgeSummary` counts, so the counts sum to the number of results
- [x] 3.8 Unit-test with a fake judge: verdict order is independent of completion order, the concurrency bound is never exceeded, gaps make no call, a failed judgment marks the row not judged without failing the run, consistency merge and capping, and cancellation during judging

## 4. CLI

- [x] 4.1 In `answer.go`, build the judge for direct runs: load and validate the settings; when enabled but invalid, print every reason once and run unjudged; `--no-judge` skips it. Pass it to `RunBatch` through `ProcessBatchChat`
- [x] 4.2 Write the results file in `OnAnswered`, then rewrite it atomically (a temporary file in the same directory, then a rename) with the verdicts once judging completes
- [x] 4.3 Print judging progress, then the review summary: the counts per rating, the "row 2 is the first answer" line, and the red then yellow rows as `row N [id]  comment`. Share the summary code between the direct path and `runBatchRemote`
- [x] 4.4 Add `Judge *bool` to `batchManifestJSON`, set from `--no-judge` in `runBatchRemote`, and have the remote spinner show "Judging answers n/N" during the judging phase
- [x] 4.5 Unit-test the summary row numbering (first result → row 2) and the not-judged rendering

## 5. Daemon REST API

- [x] 5.1 Accept optional `judge` on `batchManifestRequest`, build the judge from the daemon's settings when it is absent or true and judging is available, and pass it to `RunBatch`
- [x] 5.2 Publish `phase` (`answering` or `judging`), `judged_done` and `judged_total` on the operation metadata. Re-publish `results` with verdicts as judgments land, and add the `judge` summary on completion
- [x] 5.3 Advertise answer judging in the capability list with an available flag and reason, mirroring `analysisAvailable`
- [x] 5.4 Handler tests: judged by default, `judge: false` opts out, the judging phase is visible, and a failing judge still yields a successful operation

## 6. Browser UI

- [x] 6.1 Extend `ui/lib/types.ts` with the optional `judgment` on a result and the `judge` summary on a results file, and have the results parser tolerate both being absent
- [x] 6.2 In `QACard.tsx`, render the rating badge, score and comment, or a neutral "Not judged" badge with its reason. Keep each card's spreadsheet row number
- [x] 6.3 In `ResultsReview.tsx`, add the summary strip and a rating filter, both shown only when the file carries a `judge` summary
- [x] 6.4 In `RunningBatch.tsx`, show judging progress when the operation reports phase `judging`
- [x] 6.5 Component tests: a card with a verdict, the not-judged card, the filter keeping row numbers, and a verdict-free file rendering as before
- [x] 6.6 Verify compliance with the `ui-conventions` skill: both themes, a keyboard-only pass over the filter, `--vf-*` tokens for the badge colours, and all four view states

## 7. Documentation

- [x] 7.1 `docs/usage.md`: how judging works, the rubric and ratings, `--no-judge`, the `judge.*` keys and their scope (on by default), the results fields, the row-numbered summary, and the note that answers and KB chunks are sent to Google under the Gemini agreement
- [x] 7.2 Update `answer batch` `--help` long text and the `--no-judge` flag help, and add `--no-judge` to `apps/completion.bash`
- [x] 7.3 `docs/rest-api.md` and `rest-api.yaml`: the `judge` request field, the phase and progress metadata, the `judgment` and `judge` result fields, and the capability entry
- [x] 7.4 `docs/local-ui.md`: verdict badges, summary strip and filter; and extend the `GEMINI_API_KEY` comment in `snap/snapcraft.yaml` to cover judging on the daemon

## 8. Validation

- [x] 8.1 Run `make all` (tidy fmt vet lint test build); lint per the known workaround (`go run …golangci-lint@v2.14.0 run --new-from-rev=origin/main`)
- [x] 8.2 Build and install the snap. Confirm that the four `judge.*` keys exist after a fresh install and after a refresh over an older revision, and that `rag-cli.rag set judge.enabled=true` is accepted as a user set
- [x] 8.3 In the installed snap, run a small manifest (a probe manifest) both direct and through ragd with judging enabled. Confirm the two-stage file write, the verdicts and the summary, then repeat with `--no-judge` and with the key unset
- [ ] 8.4 Calibrate: re-run the hand-rated reference manifest with judging and compare it against the human ratings (136/32/24). Report judge-green rows that a human rated red, false reds, and the measured cost and wall-clock time, then record a default judge model recommendation
