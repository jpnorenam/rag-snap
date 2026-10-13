## 1. Sequencing

- [ ] 1.1 Archive `add-gemini-rfp-analysis`, then `switch-analysis-to-gemini-api`, before archiving this change; re-run `openspec validate harden-analysis-manifest-fidelity` after each

## 2. Domain text is guidance, not evidence

- [ ] 2.1 Add the third block to `mandatoryRules` in `prompt.go` ("REQUIREMENT DOMAIN TEXT IS GUIDANCE, NOT A SOURCE"), and narrow the gap rule's "the provided context" to "the retrieved context"
- [ ] 2.2 Add `PromptHasDomainTextRule` and report it through `InspectAssembledPrompt` / `chat.PromptAssertions` alongside the existing two
- [ ] 2.3 Add a test asserting that `buildRAGPrompt` output still contains the literal `Requirement domain` label the rule names, so a rename fails CI
- [ ] 2.4 Add the domain-context rule and the asserts/directs contrast table to the `domains[]` section of `assets/manifest-contract.md`; repeat it in one sentence in `strategyInstruction` item 4
- [ ] 2.5 Tests: an assembled prompt carries all three rules; a prompt without the domain-text rule is reported missing by the inspector

## 3. Strategy call sees question text

- [ ] 3.1 Change `questionIDDigest` to emit `- <id>: <text>` lines under each source heading, with text trimmed to 300 characters at a word boundary
- [ ] 3.2 Replace the strategy instruction's "You are given the question ids…" sentence with the instruction to write patterns and contexts against the given questions, not the document's clause numbering
- [ ] 3.3 Tests: the strategy request body carries each question's text; trimming respects the cap and word boundaries

## 4. Single-question previews in the check

- [ ] 4.1 In `chat/check.go`, add `OnlyQuestion *struct{ID, Text}` (`only_question`) to `DomainMatchCount`, set when an entry matches exactly one question, with text trimmed to 120 characters
- [ ] 4.2 Render it in the CLI check output under the pattern, together with the start of the entry's context; confirm exit status is unaffected
- [ ] 4.3 Render it in `ui/components/answer/CheckFindings.tsx`'s domain table, and add the field to the check type in `ui/lib/api/answer.ts`
- [ ] 4.4 Tests: single-match entries carry the preview, multi-match and zero-match entries do not; the REST check response carries the field

## 5. Extraction covers every demand-bearing column

- [ ] 5.1 Add the "EVERY COLUMN THAT MAKES A DEMAND" rule to `questionsInstruction` in `generate.go`, including the `source` format naming row and column
- [ ] 5.2 Add the same counting basis to `triageInstruction`'s count bullet in `triage.go`
- [ ] 5.3 Tests: both instructions carry the rule (guarding against a later edit dropping it); the request for the questions call carries it

## 6. Shortfall becomes an accept-or-abandon decision

- [ ] 6.1 Extend `ShortfallError` with `FinishReason`, `AnswerTokens` and an unrendered `Questions`; have `complete` expose the finish reason and answer-token count to `generateQuestions`; rewrite the message so it names both counts and the token usage and recommends no reasoning change
- [ ] 6.2 Add a `GenerateOptions{AcceptedQuestions}` argument to `Generate`: when set, skip the questions call and the shortfall comparison and run the strategy call over the given questions
- [ ] 6.3 Add the acceptance provenance line to `write.go` when an acceptance is passed
- [ ] 6.4 CLI (`answer_analyze.go`): on a shortfall with a terminal, print the error and ask `common.ConfirmationPrompt("Continue with N questions?")`; on yes, regenerate with `AcceptedQuestions`; on no, or without a terminal, exit with the error
- [ ] 6.5 Daemon: add `shortfall` to `stagedAnalysis` (counted against the store's byte bound); on a shortfall call `op.UpdateMetadata({"shortfall": {...}})` before failing; accept `accept_shortfall` on the generation request, continuing from the retained questions and clearing them; reject it with 400 when the session holds no shortfall
- [ ] 6.6 Tests:
  - message content, and no reasoning-effort remedy;
  - acceptance skips the questions call (the fake server sees only the strategy request);
  - provenance line written;
  - daemon metadata carries the counts;
  - `accept_shortfall` with and without a retained shortfall;
  - a truncated response still offers no acceptance in any surface.
- [ ] 6.7 Regenerate `rest-api.yaml` with its generator for the new request field and failure metadata (never hand-edit it); run `make spec-check`

## 6b. Per-section breakdown, re-run, example placeholders, default effort

- [ ] 6b.1 Add `Sections []SectionCount` to `ShortfallError`, attributing produced questions to triage sections by `id_prefix`, with an "unattributed" row and "unknown" where a section has no prefix; render it in the error text and the CLI decision
- [ ] 6b.2 Add `GenerateOptions.QuestionsEffort` and a one-step effort ladder; the CLI decision becomes continue / re-run / abandon, with re-run omitted at `high`
- [ ] 6b.3 Daemon: accept `retry_questions: true` (mutually exclusive with `accept_shortfall`), re-run only the questions call at the next effort over the staged documents, and reject it without a retained shortfall; put the per-section counts in the `shortfall` metadata
- [ ] 6b.4 Rename the knowledge bases and source groups in `assets/manifest-example.md` to `example-`-prefixed placeholders; add the "select only from this list" sentence to `Catalogue.Prompt()`; make `ValidateSelections` report an `example-` name as copied from the worked example
- [ ] 6b.5 Seed `analysis.reasoning.questions="medium"` in `snap/hooks/install` and `post-refresh`, and update the hook comment and the `docs/usage.md` defaults table
- [ ] 6b.6 Tests:
  - the per-section attribution, including unattributed and unknown rows;
  - re-run repeats only the questions call at the next effort, and isn't offered at `high`;
  - `retry_questions` with and without a shortfall, and together with `accept_shortfall`;
  - every name in the rendered example carries the `example-` prefix;
  - a copied example name is reported as such.

## 7. UI shortfall decision

- [ ] 7.1 In `AnalysisSteps.tsx`, render a generation failure whose metadata carries `shortfall` as a `p-notification--caution` with icon, title, both counts, the per-section table and `p-notification__actions` ("Continue with N questions" primary; "Re-run with more reasoning" and "Start again" secondary, the re-run omitted at `high`); keep other failures as `p-notification--negative` without a continue action
- [ ] 7.2 Wire "Continue" through `postAsync` with `accept_shortfall: true`, and "Re-run with more reasoning" with `retry_questions: true`, both with the existing running state; wire "Start again" to the upload step
- [ ] 7.3 Extend the Banners row of `.claude/skills/ui-conventions/SKILL.md` to sanction a notification with actions, citing `AnalysisSteps.tsx`
- [ ] 7.4 Verify `ui-conventions` compliance: both themes, keyboard-only pass through the continue and start-again actions, `--vf-*` tokens only, all four view states, 620px width, and `npm test` plus `npm run build` under `output: 'export'`

## 8. Documentation

- [ ] 8.1 `docs/usage.md`:
  - the analysis section explains that domain contexts are guidance, describes the third appended rule, and describes the continue prompt on a shortfall, including when it is not offered;
  - the check section documents the new assertion and the single-question preview, with sample output.
- [ ] 8.2 `docs/rest-api.md`: document `accept_shortfall`, the `shortfall` failure metadata, and the 400 for an acceptance without a shortfall

## 9. Verification

- [ ] 9.1 Run `make all`; for lint, use `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run --new-from-rev=origin/main ./...` while the installed linter cannot run on Go 1.27, and report its findings as such
- [ ] 9.2 Build and install the snap, then re-run analysis on `MeitY_RFP.xlsx`. Confirm:
  - a CIS image-hardening question is present, with a source naming the evidence column;
  - every single-question domain's context concerns its question, shown by the preview;
  - no generated context states an in-country entity, staffing or commitment as fact.
- [ ] 9.3 Run the regenerated MeitY manifest through `answer batch` on Bedrock and compare it with `batch-results-20261008-121741.json`: whether the T4 answers still claim in-country support without retrieved backing, and how the gap count changed
- [ ] 9.4 Re-run analysis on `Lafika_RFI.pdf`: confirm a shortfall is reported with the new message, that accepting it at the prompt produces a manifest without a second questions call (verbose shows one questions call), and that the header records the acceptance
- [ ] 9.4b Re-run analysis on `Dell_MASCOM.pdf` with the default effort. Confirm:
  - triage stays within its 16K ceiling;
  - the strategy call selects only catalogued knowledge bases;
  - if a shortfall occurs, the per-section breakdown points at the under-split section, and a re-run at higher effort clears it.
- [ ] 9.5 Repeat the Lafika acceptance through the UI wizard against `ragd`, and confirm a truncated generation (forced by setting `analysis.reasoning.questions=high` on a large RFP, or simulated) shows no continue action
