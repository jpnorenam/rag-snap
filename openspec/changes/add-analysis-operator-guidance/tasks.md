## 1. Instructions in the analysis package

- [x] 1.1 Add `MaxInstructionsLength = 4000` and `ValidateInstructions(s string) error` (trimmed; over-limit names the limit) in `cmd/cli/basic/analysis`
- [x] 1.2 Add an `operatorSection(instructions string) string` rendering the "# Operator instructions" heading, the precedence sentence and the verbatim text; empty input renders nothing
- [x] 1.3 Thread instructions through `Triage`, `generateQuestions` and `generateStrategy` (via `Triage(..., instructions)` and `GenerateOptions.Instructions`), appending the section to each call's system instruction
- [x] 1.4 Add `Provenance.Instructions` and write the `# Operator instructions:` block, one `#   ` line per instruction line, after the model line
- [x] 1.5 Tests:
  - every call's `systemInstruction` carries the section when given, and is unchanged when not;
  - the instructions never appear inside a document part;
  - over-limit is rejected before any request reaches the fake server;
  - an instruction to omit gaps still yields a prompt ending in every mandatory rule;
  - the provenance block is written for multi-line text and still loads as YAML.
- [x] 1.6 Added after the first live runs (2026-10-21):
  - triage sections now require `id_prefix` and `estimated_questions`, after a MASCOM triage omitted both and left the shortfall breakdown empty. A `maxItems: 60` cap was tried and removed: the live schema test (`go test -tags live -run Live`) showed Gemini rejects it with a bare 400. The unused per-section `summary` was removed too, after that test reproduced a whitespace runaway in it;
  - questions are attributed to sections by `source` when no prefix matches, and a section that cannot be attributed is reported unknown, not 0;
  - the shortfall message says when triage gave no per-section estimates;
  - the operator section asks the model not to restate the instructions, after a Lafika triage looped echoing them (Gemini's schema subset has no `maxLength`, so string fields cannot be capped).

## 2. Prompt parts

- [x] 2.1 Split `AssembleGeneratedPrompt` into `assemblePromptParts(generated, iv, t) (model, appended string)` and `JoinPrompt(model, appended string) string`; keep `AssembleGeneratedPrompt` as their composition
- [x] 2.2 Tests: `JoinPrompt(parts...) == AssembleGeneratedPrompt(...)` for hard and soft length, OpenStack in and out of scope, and multi-field on and off; a model prompt that itself contains "ANSWER LENGTH" still splits correctly

## 3. CLI

- [x] 3.1 Add `--instructions` and `--instructions-file` to `answer batch` (mutually exclusive, only with `--build --analyze`, file read in full, same bound), and pass them to both phases and to the provenance
- [x] 3.2 Print the instructions under the conveyance report ("Working to: …") so the operator sees what triage was given
- [x] 3.3 Update `answer batch --help` and the long description for the new flags, including the home-directory note for the file

## 4. Daemon

- [x] 4.1 Read the optional `instructions` multipart field on `POST /1.0/answer/analyze`, validate it before starting the operation (400 naming the limit), keep it on `stagedAnalysis`, and echo it in the triage metadata
- [x] 4.2 Apply the session's instructions in the generation handler, including the accept and re-run paths
- [x] 4.3 Add `prompt_generated` and `prompt_appended` to the generation metadata
- [x] 4.4 Update the swagger annotations for both endpoints and regenerate `rest-api.yaml` with `make spec`; confirm `make spec-check`
- [x] 4.5 Tests: over-limit field gives a 400 with no operation started; instructions are retained on the session; metadata field names and the `prompt_generated` + `prompt_appended` join, at the wire level

## 5. UI

- [x] 5.1 `ui/lib/api/answer.ts`: send `instructions` with `analyzeDocuments`, parse the echoed value from the triage metadata and the prompt parts from the generation metadata (absent → fall back to the read-only display), and add `joinPrompt` mirroring `JoinPrompt`
- [x] 5.2 Upload step (analysis path only): labelled instructions `<textarea>` with help text, an example, a live character count and an at-field over-limit error that blocks starting
- [x] 5.3 Triage step: show the instructions under "Working to"
- [x] 5.4 Review step: editable textarea for the model-written prompt, the appended rules read-only below in the `analysis-prompt` region, an "Edited" chip and a "Restore generated prompt" button; download and run use `joinPrompt(edited, appended)`; edits go through the existing unsaved-progress guard
- [x] 5.5 Provenance: the downloaded manifest's header carries the instructions block, with its text pinned against the Go source by a test
- [x] 5.6 Extend `.claude/skills/ui-conventions/SKILL.md` if the editable-text-with-locked-suffix pattern is new
- [x] 5.7 Tests (`npm test`): `joinPrompt` matches the Go join pinned from source; instructions are sent and echoed; the prompt-parts fallback works; edits download as edited part plus unchanged rules
- [x] 5.8 Verify `ui-conventions` compliance: both themes, keyboard-only through the instructions field and the prompt editor, `--vf-*` tokens only, all four view states, 620px, and `npm run build` under `output: 'export'` — confirmed by the operator 2026-10-21 (both themes, keyboard-only, 620px); the restore action was changed to a bordered `p-button` on review; `npm test` 85/85 and `npm run build` pass

## 6. Documentation

- [x] 6.1 `docs/usage.md`:
  - an "Instructions" subsection in the analysis section, with examples taken from the live cases (per-requirement splitting, section-level granularity, matrix-only scope), what instructions can and cannot override, and where they are recorded;
  - the two flags in the flag table.
- [x] 6.2 `docs/rest-api.md`: the `instructions` field and its limit, its echo in the triage metadata, and `prompt_generated` / `prompt_appended` with the join contract

## 7. Verification

- [x] 7.1 Run `make all` (lint via `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run --new-from-rev=origin/main ./...` while the installed linter cannot run on Go 1.27) — done: tidy, fmt, vet, spec-check, test (12 packages) and build pass; lint via v2.14.0 `--new-from-rev=origin/main` reports 0 issues; UI `npm test` 85/85, `tsc --noEmit` clean, `npm run build` succeeds
- [x] 7.1b Run the live schema test (`GEMINI_API_KEY=… go test -tags live -run Live -v ./cmd/cli/basic/analysis/`) and confirm Gemini accepts the triage, questions and strategy schemas as shipped — done 2026-10-21: all three schemas accepted as shipped, after the variants test isolated `maxItems` as the rejected constraint
- [x] 7.2 Build and install the snap. Run `Dell_MASCOM.pdf` twice with "Split every matrix row into one question per requirement. Only the Annexure A compliance matrix." Confirm both runs land near the hand-written 164 questions with no narrative-section questions, and that the header records the instructions — done 2026-10-21 over three instructed runs: granularity followed each instruction ("one question per requirement" 236, "…list as a single question" 36, "one question per distinct obligation, grouping related items" 140 against the hand-written 164 — FR 73/91, NFR 67/73); narrative sections excluded every time; headers record the instructions; `mascom-i3.yaml` was accepted through the live CLI shortfall decision and its header records both the instructions and the acceptance; `--check` clean. Observation: with a grouping instruction triage over-counted (~220), so the shortfall screen fired and the per-section table correctly showed a counting difference
- [x] 7.3 Run `Lafika_RFI.pdf` with "One question per section of the RFI; do not split comma-separated lists." and confirm the count lands near the hand-written 24 — done 2026-10-21 (`test-rfp/lafika-i1.yaml`): 27 questions against the hand-written 24 (172 without instructions), triage estimate 27, no runaway; all 21 domain contexts direct rather than assert
- [x] 7.4 In the UI, run an analysis with instructions, edit the generated prompt, restore it, edit it again, download and run. Confirm the downloaded prompt is the edited part followed by the unchanged rules, and that `--check` on the download finds every prompt assertion — done 2026-10-21 through the UI on Lafika with instructions: the triage step showed them under "Working to"; edit, restore and re-edit behaved as specified; the downloaded prompt is exactly the edit + "\n\n" + a rules block byte-identical to the unedited download, everything else in the manifest is identical, the header records the instructions, and `--check` on the edited download finds no issues
