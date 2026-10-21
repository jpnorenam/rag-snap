## Context

Analysis makes three Gemini calls per manifest (`cmd/cli/basic/analysis`):

| Call | What it receives |
|---|---|
| Triage | `Grounding() + cat.Prompt()` as the system instruction; `triageInstruction` plus the document parts as user content |
| Questions | `Grounding()` as the system instruction; `questionsInstruction` plus triage-derived hints plus the documents as user content |
| Strategy | `Grounding() + cat.Prompt()` as the system instruction; `strategyInstruction` plus the question digest plus the documents as user content |

`AssembleGeneratedPrompt(generated, iv, t)` then returns one string: the model's prompt, followed by
the blocks rag-cli appends. Those are ANSWER LENGTH, optionally OPENSTACK OFFERING and SEVERAL FIELDS
PER QUESTION, then the three mandatory rules. The UI receives only that assembled string, in
`manifest.prompt`, and renders it in a `<pre>`.

The daemon's triage handler reads the multipart fields `file` and `context`. Its staged session
(`stagedAnalysis`) holds the conveyance, triage and catalogue between the two phases. The CLI holds
the same values in local variables across one process.

Live evidence for operator steering: the same MASCOM PDF produced 57, 83, 205 and 269 questions
across runs; Lafika produced 172 where a person wrote 24; and MASCOM's narrative sections added 61
questions that had to be pruned by hand. Each would have been fixed by one sentence from the operator.

## Goals / Non-Goals

**Goals:**

- The operator can state granularity, scope and selection rules once, and every call honours them.
- Triage and extraction keep counting on the same basis, so the shortfall check stays meaningful when
  scope is narrowed.
- Instructions stay operator text, never mixed with untrusted documents, and can never displace the
  mandatory rules.
- A manifest records the instructions that shaped it.
- The UI can edit the model-written prompt without being able to touch the appended rules.

**Non-Goals:**

- Instructions to the *answering* model. Answering is shaped by the prompt, which this change makes
  editable. Steering analysis and steering answers stay separate surfaces.
- Saving instructions as reusable presets, or saving an edited prompt as a stored prompt variant.
- Editing the prompt in the CLI. The CLI writes a YAML file the operator edits directly, and `--check`
  already warns when a rule is missing.
- Exporting question ids from the external review tool (see the proposal).

## Decisions

### Instructions go in the system instruction, under their own heading

Each call's system instruction gains a final section when instructions are present:

```
# Operator instructions

These come from the person running this analysis, not from the documents. Follow them: they take
precedence over the default extraction and strategy guidance above. They do not override the
manifest contract's rules (routable ids, catalogue-only selections, the output format).

<instructions verbatim>
```

The system instruction is the trusted channel. The documents arrive as user content and are the
untrusted input this feature has defended against since the original design. So text in a document
that imitates an instruction cannot claim this heading's authority, and a reviewer of a request can
see at a glance which text the operator wrote.

The precedence sentence has to name what instructions may and may not override. "One question per
section" is meant to override the default "ONE QUESTION PER ENTRY" splitting. But "use the ids
FR-1, FR-2" must not license ids that break routing, and "pick knowledge base X" must not bypass the
catalogue check. The catalogue check and the count check run locally afterwards whatever the
instructions say.

*Alternative: a labelled user-content part before the documents.* Rejected: user content is where
the documents are, which is exactly the boundary the feature should keep.

### All three calls, including triage

The questions call is where granularity is decided, so it needs the instructions. Triage needs them
because its estimate is the yardstick for that same call: "only the Annexure A matrix" given to
extraction alone would make a correct 150-question extraction look short against a 205-question
whole-document estimate. The strategy call needs them for selection and framing ("technical
questions only" affects which domains are worth writing).

### Bounded at 4,000 characters, rejected rather than truncated

That's about a thousand tokens, far more than the one-to-three sentences the live cases needed, and
small next to the documents. Over the limit is an error before any call, naming the limit:
truncating an instruction would silently change what it says. The bound is checked in the analysis
package, so the CLI, daemon and UI share one rule and one message.

### Kept on the session, recorded in the provenance

`stagedAnalysis` gains `instructions string`. Generation reads it from the session, so the client
cannot accidentally send different instructions to the second phase than the first. The CLI simply
passes the same variable to both phases.

The provenance header gains a block, one `#` line per instruction line so multi-line text stays a
valid YAML comment:

```
# Operator instructions:
#   Split every matrix row into one question per requirement.
#   Only the Annexure A compliance matrix.
```

The UI download writes the same block through `headerComments`, and the shared text is pinned by a
test that reads the Go source, as the acceptance line already is.

### Split prompt assembly into two parts, joined by one function

`AssembleGeneratedPrompt` becomes a thin join over `assemblePromptParts(generated, iv, t)
(model, appended string)`:

- `model` is the model's prompt, whitespace-trimmed.
- `appended` is everything rag-cli adds, from ANSWER LENGTH through BE DIRECT, ending in one newline.
- `JoinPrompt(model, appended) = model + "\n\n" + appended` is the only way the two are combined, in Go
  and in the UI (where its two-line equivalent is pinned against the Go source).

Generation metadata adds `prompt_generated` and `prompt_appended`. A test asserts
`JoinPrompt(generated, appended) == manifest.prompt` for every interview shape (hard and soft
length, OpenStack in or out of scope, multi-field on or off). That equality is the contract the UI
relies on.

*Alternative: let the UI split `manifest.prompt` at "ANSWER LENGTH".* Rejected. A model-written
prompt can contain that heading itself, and the split would then hand part of the rules to the
editable area, or part of the prose to the locked one.

### UI: an editable area above a locked block

On the review step:

- **The editable area** is the model-written part, in a `<textarea>` inside `p-form-group` with the
  label "RFP prompt (generated — editable)".
- **The locked block** sits below it, in the existing `analysis-prompt` region, headed "Added by
  rag-cli — not editable here". It reads as part of the same prompt but is visibly separate.
- **An "Edited" badge** (`p-chip`, text, not colour alone) and a "Restore generated prompt" button
  (`p-button--base`) appear once the text differs from the generated version.
- **Download and run** both use `JoinPrompt(edited, appended)`.
- **Unsaved edits** go through the wizard's existing `useUnsavedGuard`.

No re-check is needed on edit. The check's prompt assertions are about the rules, and those cannot
change. If the editable-with-locked-suffix pair isn't in `ui-conventions`' sanctioned patterns, the
skill is extended in the same change.

The instructions field on the upload step is a `<textarea>` with a label, help text and an example,
a live character count, and the over-limit message at the field (`p-form-validation is-error`). It's
rendered only on the analysis path, and the triage step echoes the instructions under "Working to".

### CLI flags

`--instructions "<text>"` and `--instructions-file <path>`, mutually exclusive, and only valid with
`--build --analyze`. The file is read in full and the same length bound applies. Under strict
confinement the file must be under the user's home, as every other CLI input already must, and the
help text says so.

## Risks / Trade-offs

- [An instruction tells the model to do something that breaks the manifest, such as invented ids or
  skipped required sections] → The contract rules are named as not overridable. Selections are still
  validated against the catalogue, routing is still compiled, the count is still checked, and
  `--check` runs on every generated manifest.
- [An instruction contradicts a mandatory rule, such as "don't report gaps"] → Instructions reach only
  the generation calls. The rules are appended to the prompt afterwards and stay its final word, the
  same guarantee that already holds against a hostile RFP.
- [The instructions box is used for answer-style guidance ("answers must be in French")] → The help
  text says instructions steer how the manifest is generated, and that the prompt (now editable) is
  where answer guidance goes. A French-answers instruction would still reach the strategy call, which
  would likely write it into the prompt, so the outcome is acceptable either way.
- [Edited prompts diverge from what the model generated] → That's the point. The provenance header
  still names the model, and the review step shows an "Edited" badge until the manifest is
  downloaded or run.
- [The two-part contract drifts between Go and UI] → `JoinPrompt` is defined once in Go, and the UI's
  join is pinned against it by the test that reads the Go source.

## Migration Plan

Additive. Without instructions, every request is byte-identical to today's apart from the two new
metadata fields. Older clients ignore the new metadata fields; newer clients that receive no
`prompt_generated` (from an older daemon) fall back to the current read-only display. Rollback is a
snap revert.

## Open Questions

- **Should instructions be saved as named presets** ("compliance matrix, split per requirement")?
  Useful once the same few instructions recur. Deferred until there's evidence of which ones do.
- **Should an edited prompt be saveable as a stored `answer_system_prompt` variant** for reuse across
  manifests of the same bid? Deferred, as in the original analysis design.
