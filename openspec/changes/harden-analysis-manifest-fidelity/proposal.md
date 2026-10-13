## Why

The first live runs of RFP analysis against Gemini (2026-10-08) showed that the wiring works, but that
a generated manifest can mislead in ways `--check` cannot see.

The most serious finding: on `MeitY_RFP.xlsx` the generated `T4-*` domain context stated "Canonical
offers localized 24x7 enterprise support within India". The answering model (Bedrock Mistral) repeated
it as fact in two answers. Nothing in the retrieved sources was shown to support it. The appended rule
"report gaps, never fill them" did not stop this. That rule refers to "the provided context", and a
domain context *is* provided in the answering turn, as operator-authored instruction sitting next to the
retrieved passages. A claim placed there therefore bypasses the protection meant to stop fabrication.

Three further defects showed up in the same runs:

- **A domain attached to the wrong question.** The `T4-iii` domain carried CIS-image context, but the
  question with id `T4-iii` asks about operating without foreign dependency. The strategy call is given
  question ids only, never their text, so it mapped the spreadsheet's own clause numbering onto ids the
  extraction pass had assigned differently.
- **Requirements in evidence columns were dropped.** MeitY's "Evidence / Validation proof" column carries
  separate requirements (CIS image hardening among them). Generation produced 12 questions where the
  hand-written manifest has 16. Triage also estimated 12, so the completeness check had nothing to catch.
- **A false shortfall blocked a whole RFI.** On `Lafika_RFI.pdf` the questions call finished normally
  (`STOP`, 7K of 65K tokens, no reasoning tokens) with 111 questions against a triage estimate of 168.
  The two calls disagreed about how finely to split comma-separated lists in prose. The error advised
  lowering reasoning effort, which was not the problem. There was no way to proceed short of abandoning
  analysis.

Later runs on `Dell_MASCOM.pdf` (2026-10-12) added two more:

- **An under-split that looks like a false shortfall.** At `low` reasoning effort the questions call
  produced 53 questions against a triage estimate of 220. The matrix has 36 packed rows, and the
  hand-written manifest has 164 questions. At `medium` the same call passed the count check, producing at
  least 140. So Lafika (111 vs 168, hand-written 24) and MASCOM (53 vs 220, hand-written 164) gave the
  same signal with opposite meanings. A total alone cannot tell them apart, and the error's "lower
  reasoning effort" was backwards for MASCOM.
- **A knowledge base copied from the worked example.** A generation that was otherwise complete failed
  because the strategy call selected `vendor-rfp-answers`, a name from the bundled
  `manifest-example.md`, not from the live catalogue. A plausible-looking example name is a standing
  invitation to copy it.

These belong to the analysis capability introduced by `add-gemini-rfp-analysis`, not to the provider
switch in `switch-analysis-to-gemini-api`, and they apply whichever model generates the manifest.

## What Changes

- **Domain contexts are guidance, never evidence.**
  - The strategy call and the grounding contract instruct that a domain context says what to address and
    what to look for, and does not state capabilities, figures, locations, entities or commitments.
  - rag-cli appends a third non-negotiable rule to every generated prompt: text attached as a
    requirement domain is not a source, and a claim that appears only there is unsubstantiated.
  - `--check` verifies the new rule's presence like the existing two.
- **The strategy call sees what each question says.** It receives every question's id and text, not ids
  alone. In `--check` and the generation summary, a domain entry that matches exactly one question
  shows that question's text, so a misattached context is visible to a human before the run.
- **Requirements are extracted from every column that states one.** Triage counts, and the questions
  call extracts, demands carried in evidence, validation, acceptance-criteria and similar columns, not
  only the primary requirement column. A column that merely describes or labels is still not a question.
- **A count mismatch on a clean finish becomes an operator decision.**
  - The shortfall error states whether the questions call finished normally, and stops prescribing a
    reasoning-budget remedy when it did.
  - The operator may accept the produced questions and continue to the strategy call, without a second
    questions call: interactively in the CLI, by a request flag on the REST generation endpoint, and by
    a "Continue with N questions" action in the UI.
  - An accepted manifest records the acceptance and both counts in its provenance header.
  - The decision shows each section's produced and estimated counts, and offers a third choice: re-run
    only the questions call at the next higher reasoning effort.
  - A truncated or empty response is still never acceptable.
- **The worked example cannot be copied into a selection.** Its knowledge-base and source-group names
  become visible placeholders that no real catalogue could contain, and the request states that only
  live-catalogue names are selectable. A failure caused by a copied name says so.
- **`analysis.reasoning.questions` defaults to `medium`,** on the MASCOM evidence. This changes a
  `package` key's seeded value; no new keys are added.

No new config keys; one seeded default changes (`analysis.reasoning.questions`, `low` → `medium`). No
change to any of the three external services. Analysis still sends documents only
to the Gemini API, and **the answering pipeline is not changed**. The fixes act on what analysis
generates and on what `--check` reports, so hand-written manifests answer exactly as before.

User-facing surfaces that change:
- the `answer batch --build --analyze` interaction gains a continue prompt on a count mismatch;
- `answer batch --check` output gains the new prompt assertion and single-question domain previews;
- the REST generation endpoint gains a request field;
- the UI build wizard gains a shortfall notice with a continue action.

Documentation that must change: `docs/usage.md` (analysis and check sections), `docs/rest-api.md`,
`rest-api.yaml` (via its generator), and `.claude/skills/ui-conventions/SKILL.md`, since a notification
with actions is a new visual pattern.

## Capabilities

### New Capabilities

_None._

### Modified Capabilities

- `answer-manifest-analysis`:
  - generated prompts carry a third mandatory rule;
  - domain contexts are constrained to guidance;
  - the strategy call receives question text;
  - extraction covers every requirement-bearing column;
  - a count mismatch on a clean finish can be accepted by the operator.
- `answer-manifest-check`: reports the new prompt rule, and previews the question behind each
  single-question domain entry.
- `rest-api-answer`: the generation endpoint can continue a session whose question count fell short,
  reusing the questions already produced.
- `local-ui-app`: the build wizard presents a shortfall with both counts and a continue action.

All four capabilities are introduced by the still-open `add-gemini-rfp-analysis`. That change must be
archived first, then `switch-analysis-to-gemini-api`, then this one.

## Impact

- **External services:** none of OpenSearch, the inference server or Tika changes. Gemini receives more
  input on the strategy call (question text in addition to ids).
- **Code:**
  - `cmd/cli/basic/analysis/`: `generate.go` (instructions, question digest, accept path), `triage.go`
    (counting instruction), `prompt.go` (third rule and its detector), `errors.go` (`ShortfallError`
    carries how the call ended), `assets/manifest-contract.md`, `write.go` (provenance line);
  - `cmd/cli/basic/answer_analyze.go`: continue prompt;
  - `cmd/cli/basic/chat/check.go` and the CLI check renderer;
  - `internal/api/handlers_answer_analysis.go` and `answer_analysis_store.go`: produced questions
    retained on the staged session;
  - `ui/components/answer/AnalysisSteps.tsx`, `CheckFindings.tsx` and `ui/lib/api/answer.ts`.
- **API:** additive. A new optional generation request field, and a structured shortfall error carrying
  both counts and how the call ended.
- **Cost:** the strategy call's input grows by the question list (on the order of 10K tokens for a
  200-question RFP). Accepting a shortfall saves a whole re-run.
