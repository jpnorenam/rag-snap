## 1. Sequencing

- [x] 1.1 Archive `add-gemini-rfp-analysis`, then `switch-analysis-to-gemini-api`, before archiving this change; re-run `openspec validate harden-analysis-manifest-fidelity` after each

## 2. Domain text is guidance, not evidence

- [x] 2.1 Add the third block to `mandatoryRules` in `prompt.go` ("REQUIREMENT DOMAIN TEXT IS GUIDANCE, NOT A SOURCE"), and narrow the gap rule's "the provided context" to "the retrieved context"
- [x] 2.2 Add `PromptHasDomainTextRule` and report it through `InspectAssembledPrompt` / `chat.PromptAssertions` alongside the existing two
- [x] 2.3 Add a test asserting that `buildRAGPrompt` output still contains the literal `Requirement domain` label the rule names, so a rename fails CI
- [x] 2.4 Add the domain-context rule and the asserts/directs contrast table to the `domains[]` section of `assets/manifest-contract.md`; repeat it in one sentence in `strategyInstruction` item 4
- [x] 2.5 Tests: an assembled prompt carries all three rules; a prompt without the domain-text rule is reported missing by the inspector

## 3. Strategy call sees question text

- [x] 3.1 Change `questionIDDigest` to emit `- <id>: <text>` lines under each source heading, with text trimmed to 300 characters at a word boundary
- [x] 3.2 Replace the strategy instruction's "You are given the question ids…" sentence with the instruction to write patterns and contexts against the given questions, not the document's clause numbering
- [x] 3.3 Tests: the strategy request body carries each question's text; trimming respects the cap and word boundaries

## 4. Single-question previews in the check

- [x] 4.1 In `chat/check.go`, add `OnlyQuestion *struct{ID, Text}` (`only_question`) to `DomainMatchCount`, set when an entry matches exactly one question, with text trimmed to 120 characters
- [x] 4.2 Render it in the CLI check output under the pattern, together with the start of the entry's context; confirm exit status is unaffected
- [x] 4.3 Render it in `ui/components/answer/CheckFindings.tsx`'s domain table, and add the field to the check type in `ui/lib/api/answer.ts`
- [x] 4.4 Tests: single-match entries carry the preview, multi-match and zero-match entries do not; the REST check response carries the field

## 5. Extraction covers every demand-bearing column

- [x] 5.1 Add the "EVERY COLUMN THAT MAKES A DEMAND" rule to `questionsInstruction` in `generate.go`, including the `source` format naming row and column
- [x] 5.2 Add the same counting basis to `triageInstruction`'s count bullet in `triage.go`
- [x] 5.3 Tests: both instructions carry the rule (guarding against a later edit dropping it); the request for the questions call carries it
- [x] 5.4 Added after the live MASCOM re-run (2026-10-21) regressed: answer-field columns (compliance, product, licence, cross-reference, comment) are excluded from extraction and from the triage count, and ranged rows keep their range in the id; spec, contract and instructions updated, with tests

## 6. Shortfall becomes an accept-or-abandon decision

- [x] 6.1 Extend `ShortfallError` with `FinishReason`, `AnswerTokens` and an unrendered `Questions`; have `complete` expose the finish reason and answer-token count to `generateQuestions`; rewrite the message so it names both counts and the token usage and recommends no reasoning change
- [x] 6.2 Add a `GenerateOptions{AcceptedQuestions}` argument to `Generate`: when set, skip the questions call and the shortfall comparison and run the strategy call over the given questions
- [x] 6.3 Add the acceptance provenance line to `write.go` when an acceptance is passed
- [x] 6.4 CLI (`answer_analyze.go`): on a shortfall with a terminal, print the error and ask `common.ConfirmationPrompt("Continue with N questions?")`; on yes, regenerate with `AcceptedQuestions`; on no, or without a terminal, exit with the error
- [x] 6.5 Daemon: add `shortfall` to `stagedAnalysis` (counted against the store's byte bound); on a shortfall call `op.UpdateMetadata({"shortfall": {...}})` before failing; accept `accept_shortfall` on the generation request, continuing from the retained questions and clearing them; reject it with 400 when the session holds no shortfall
- [x] 6.6 Tests:
  - message content, and no reasoning-effort remedy;
  - acceptance skips the questions call (the fake server sees only the strategy request);
  - provenance line written;
  - daemon metadata carries the counts;
  - `accept_shortfall` with and without a retained shortfall;
  - a truncated response still offers no acceptance in any surface.
- [x] 6.7 Regenerate `rest-api.yaml` with its generator for the new request field and failure metadata (never hand-edit it); run `make spec-check`

## 6b. Per-section breakdown, re-run, example placeholders, default effort

- [x] 6b.1 Add `Sections []SectionCount` to `ShortfallError`, attributing produced questions to triage sections by `id_prefix`, with an "unattributed" row and "unknown" where a section has no prefix; render it in the error text and the CLI decision
- [x] 6b.2 Add `GenerateOptions.QuestionsEffort` and a one-step effort ladder; the CLI decision becomes continue / re-run / abandon, with re-run omitted at `high`
- [x] 6b.3 Daemon: accept `retry_questions: true` (mutually exclusive with `accept_shortfall`), re-run only the questions call at the next effort over the staged documents, and reject it without a retained shortfall; put the per-section counts in the `shortfall` metadata
- [x] 6b.4 Rename the knowledge bases and source groups in `assets/manifest-example.md` to `example-`-prefixed placeholders; add the "select only from this list" sentence to `Catalogue.Prompt()`; make `ValidateSelections` report an `example-` name as copied from the worked example
- [x] 6b.5 Seed `analysis.reasoning.questions="medium"` in `snap/hooks/install` and `post-refresh`, and update the hook comment and the `docs/usage.md` defaults table
- [x] 6b.6 Tests:
  - the per-section attribution, including unattributed and unknown rows;
  - re-run repeats only the questions call at the next effort, and isn't offered at `high`;
  - `retry_questions` with and without a shortfall, and together with `accept_shortfall`;
  - every name in the rendered example carries the `example-` prefix;
  - a copied example name is reported as such.

## 7. UI shortfall decision

- [x] 7.1 In `AnalysisSteps.tsx`, render a generation failure whose metadata carries `shortfall` as a `p-notification--caution` with icon, title, both counts, the per-section table and `p-notification__actions` ("Continue with N questions" primary; "Re-run with more reasoning" and "Start again" secondary, the re-run omitted at `high`); keep other failures as `p-notification--negative` without a continue action
- [x] 7.2 Wire "Continue" through `postAsync` with `accept_shortfall: true`, and "Re-run with more reasoning" with `retry_questions: true`, both with the existing running state; wire "Start again" to the upload step
- [x] 7.3 Extend the Banners row of `.claude/skills/ui-conventions/SKILL.md` to sanction a notification with actions, citing `AnalysisSteps.tsx`
- [x] 7.4 Verify `ui-conventions` compliance: both themes, keyboard-only pass through the continue and start-again actions, `--vf-*` tokens only, all four view states, 620px width, and `npm test` plus `npm run build` under `output: 'export'` — confirmed by the operator 2026-10-21 (both themes, keyboard-only through the three shortfall actions, 620px); `npm test` 80/80 and `npm run build` pass

## 8. Documentation

- [x] 8.1 `docs/usage.md`:
  - the analysis section explains that domain contexts are guidance, describes the third appended rule, and describes the continue prompt on a shortfall, including when it is not offered;
  - the check section documents the new assertion and the single-question preview, with sample output.
- [x] 8.2 `docs/rest-api.md`: document `accept_shortfall`, the `shortfall` failure metadata, and the 400 for an acceptance without a shortfall

## 9. Verification

- [x] 9.1 Run `make all`; for lint, use `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run --new-from-rev=origin/main ./...` while the installed linter cannot run on Go 1.27, and report its findings as such — done 2026-10-13: tidy, fmt, vet, spec-check, test (12 packages) and build pass; lint via v2.14.0 `--new-from-rev=origin/main` reports 0 issues; UI `npm test` 80/80, `tsc --noEmit` clean, `npm run build` succeeds
- [x] 9.2 Build and install the snap, then re-run analysis on `MeitY_RFP.xlsx`. Confirm: — done 2026-10-20 (`test-rfp/meity-v2.yaml`): 30 questions (10-08: 12; hand-written: 16), including `T4-g` CIS image management sourced from the Evidence column; all five domains are section-level, so no single-question routing to misattach; contexts direct rather than assert (T4 now forbids inventing Indian entities or headcount). Observation: T1 and T5 contexts still instruct the answer to state a claim ("State that the architecture allows…", "…to meet the 20% open-source requirement"); whether the domain-text rule prevails is what 9.3 measures
  - a CIS image-hardening question is present, with a source naming the evidence column;
  - every single-question domain's context concerns its question, shown by the preview;
  - no generated context states an in-country entity, staffing or commitment as fact.
- [x] 9.3 Run the regenerated MeitY manifest through `answer batch` on Bedrock and compare it with `batch-results-20261008-121741.json`: whether the T4 answers still claim in-country support without retrieved backing, and how the gap count changed — done 2026-10-20 (`batch-results-20261020-175243.json`, 30 answers, Bedrock Mistral): in-country support is no longer asserted (T4-c: delivered globally, SCP establishes local structures); 22/30 answers scope hardware and supply-chain demands to the SCP partner (10-08: 1/12); T5-c states plainly that no native messaging platform is included. Residuals: T4-f hedges "access to India-based technical resources where applicable"; the T5-* context pushes the RFP's genuine 20% open-source requirement into T5-a/b/c, which don't ask it; package counts disagree (36,000 in T1-c vs 25,000 in T4-c/T4-e) and T2-b claims UK-only development — retrieval or model origin, not domain context, and unverifiable without per-answer sources
- [x] 9.4 Re-run analysis on `Lafika_RFI.pdf`: confirm a shortfall is reported with the new message, that accepting it at the prompt produces a manifest without a second questions call (verbose shows one questions call), and that the header records the acceptance — done 2026-10-21 on MASCOM rather than Lafika, which no longer falls short (it now over-splits; see design open questions): a shortfall at medium was reported with the per-section breakdown and no reasoning-effort advice; a re-run at high cleared one run live, and accepting in the UI produced a manifest without a second questions call whose header records `Question count accepted by the operator below the triage estimate: 83 produced, about 150 estimated`
- [x] 9.4b Re-run analysis on `Dell_MASCOM.pdf` with the default effort. Confirm: — done 2026-10-21 over three runs: triage stayed within 16K every time (715–806 answer tokens); no `example-` knowledge base was selected (all catalogued); a shortfall at medium (77 vs 220; NFR 15 vs ~120 — fewer questions than its 22 rows) was diagnosed by the per-section breakdown and cleared by a live re-run at high; that re-run exposed the answer-field regression fixed in 5.4, after which `mascom-v3.yaml` keeps ranged ids (`FR_16-20-a`), splits by requirement only, and lands at FR 82 / NFR 62 against the hand-written 91 / 73, plus 61 questions from the narrative sections for the operator to prune
  - triage stays within its 16K ceiling;
  - the strategy call selects only catalogued knowledge bases;
  - if a shortfall occurs, the per-section breakdown points at the under-split section, and a re-run at higher effort clears it.
- [x] 9.5 Repeat the Lafika acceptance through the UI wizard against `ragd`, and confirm a truncated generation (forced by setting `analysis.reasoning.questions=high` on a large RFP, or simulated) shows no continue action — done 2026-10-21 through the UI wizard against ragd on MASCOM (questions effort lowered to force the shortfall): the caution notice offered continue / re-run / start again, Continue completed generation, and the downloaded manifest carries the provenance header including the acceptance; a truncation shows no continue action, covered by `parseShortfall` returning null without shortfall metadata (accepted by the operator as unit-test evidence)
