## Why

RFP analysis generates a manifest from the documents alone, and the operator has no way to steer it.
The live runs show what that costs:

- **Granularity is unstable.** The same 40-page `Dell_MASCOM.pdf` produced 57, 83, 205 and 269
  questions on different runs. At 57, triage and extraction agreed on one question per packed row, so
  the count check had nothing to compare against.
- **Prose RFIs over-split.** `Lafika_RFI.pdf` came back as 172 questions where the hand-written
  manifest has 24 section-level ones. Triage even noted that answers would be a prose proposal.
- **Scope is all or nothing.** MASCOM's narrative sections produced 61 questions the operator then has
  to prune by hand, including a price schedule and contact details no retrieval should answer.

In each case the operator knew the right answer and could have said so in a sentence: "split every
row into one question per requirement", "one question per section", "only the Annexure A matrix",
"technical questions only".

Separately, the generated prompt is shown read-only in the UI. Correcting it means downloading the
manifest and editing the YAML by hand, which also makes it easy to delete the appended mandatory rules
by accident.

## What Changes

- **Operator instructions for analysis.** Free-text instructions can be given alongside the
  documents:
  - in the CLI with `--instructions "<text>"` or `--instructions-file <path>`, used with
    `--build --analyze`;
  - as an `instructions` field on the REST triage upload;
  - in a text area on the UI build wizard's upload step.
- **They steer all three model calls.** Triage, question extraction and strategy all receive them, so
  the triage estimate counts on the same basis as the extraction. A scope instruction like "only the
  matrix on page 32" then shrinks the estimate too, instead of triggering a false shortfall.
- **They are trusted and bounded.** They travel as operator text in the request, never mixed into the
  untrusted document content. They are length-bounded, and they cannot remove the mandatory prompt
  rules, which are still appended after generation.
- **They are recorded.** Instructions are kept on the analysis session for generation and written into
  the manifest's provenance header, so a manifest shows what it was asked to do.
- **The generated prompt is editable in the UI; the appended rules are not.** The review step shows
  the model-written part of the prompt in an editable text area. Below it, the rag-cli rules (answer
  length, OpenStack offering, several-fields guidance, gaps, domain text, directness) are shown
  read-only. Downloading or running uses the edited part followed by the unchanged rules.
- **The two halves travel separately.** Generation metadata carries the model-written part and the
  appended rules as separate fields, so the UI never has to split an assembled prompt by guessing at
  its headings.

No new config keys. No change to OpenSearch, the inference server or Tika. Answering is not changed:
an edited prompt is just a manifest's `prompt`, run exactly as before.

User-facing surfaces that change:
- the CLI's `answer batch` gets the `--instructions` and `--instructions-file` flags;
- the REST triage upload gets an `instructions` field, and generation metadata gets the two prompt fields;
- the UI build wizard gets an instructions field and an editable prompt.

Documentation that must change:
- `docs/usage.md`: the analysis section, plus the flag table and an example;
- `docs/rest-api.md`;
- `rest-api.yaml`, regenerated from the handler annotations;
- `answer batch --help`;
- `.claude/skills/ui-conventions/SKILL.md`, if an editable-text-with-locked-suffix pattern is new.

**Out of scope:**
- Exporting question ids with reviewed answers. rag-cli's results JSON already carries `id`; the CSV
  that drops it comes from a separate review tool.
- Saving an edited prompt as a stored `answer_system_prompt` variant. That's a natural follow-up
  (it's an open question in the original analysis design), but it widens this into `rest-api-prompts`.

## Capabilities

### New Capabilities

_None._

### Modified Capabilities

- `answer-manifest-analysis`: analysis accepts operator instructions that reach every model call,
  are kept separate from document content, cannot displace the mandatory rules, and are recorded in
  the manifest's provenance. Generation exposes the model-written prompt separately from the
  appended rules.
- `rest-api-answer`: the triage upload accepts `instructions`, and generation metadata carries the
  generated prompt and the appended rules as separate fields.
- `local-ui-app`: the build wizard collects instructions on the upload step, and the review step lets
  the model-written prompt be edited while the appended rules stay read-only.

## Impact

- **External services:** none of OpenSearch, the inference server or Tika. Gemini receives the
  instructions as part of each analysis request.
- **Code:**
  - `cmd/cli/basic/analysis/`: `triage.go` and `generate.go` take instructions and render them into
    each call; `prompt.go` splits assembly into the generated part and the appended rules; `write.go`
    writes an instructions block in the provenance header.
  - `cmd/cli/basic/answer.go` and `answer_analyze.go`: the two flags.
  - `internal/api/handlers_answer_analysis.go` and `answer_analysis_store.go`: the `instructions`
    form field, held on the staged session, and the new generation metadata fields.
  - `ui/components/answer/BuildWizard.tsx`, `AnalysisSteps.tsx`, `ui/lib/api/answer.ts` and
    `ui/lib/manifest.ts` (provenance header).
- **API:** additive. One optional form field and two metadata fields; existing clients are unaffected.
- **Cost:** a few hundred extra input tokens per call. Instructions that fix granularity up front
  should reduce re-runs.
