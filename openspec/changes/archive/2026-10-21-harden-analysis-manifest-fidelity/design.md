## Context

The analysis capability (from `add-gemini-rfp-analysis`, provider-switched by
`switch-analysis-to-gemini-api`) generates a manifest in three model calls: triage, questions and
strategy. rag-cli then appends the mandatory prompt rules (`prompt.go`, `mandatoryRules` via
`AssembleGeneratedPrompt`), validates the result, and runs the check.

The first live runs exposed four defects. They are recorded in the proposal, with the evidence in
`test-rfp/meity-gemini.yaml` and `batch-results-20261008-121741.json`. What matters here is where each
comes from:

1. **Domain context is laundered into evidence.**
   - `chat/rag.go` `buildRAGPrompt` renders a turn as `Context:\n<passages>`, then
     `Requirement domain: <context> Answer within this domain.`, then the question.
   - The appended gap rule says "Where the provided context does not substantiate…". A domain context is
     provided in the turn, and reads as operator-authored instruction. So the model treats a claim
     placed there as substantiated.
   - The strategy instruction asks contexts to say "what counts as evidence". Nothing tells it not to
     state capabilities, and `manifest-contract.md` has no such rule either. The bundled examples happen
     to be phrased as directions ("Do not present the manufacturer as certified unless the retrieved
     context says so"), but that is example, not rule.
2. **The strategy call routes blind.** `questionIDDigest` gives the strategy call ids grouped by
   `source` and nothing else. On MeitY the model knew from the spreadsheet that clause (iii) of T4 was
   CIS images, and wrote a `T4-iii` entry. The extraction pass had numbered T4's clauses differently.
3. **Extraction reads one column.** Conveyance keeps every column (`rfp/markdown.go`), so the evidence
   column reached the model. The triage and questions instructions, though, speak of cells, rows and
   bullets packing several questions, never of a *second column* making its own demand. Both calls
   skipped the evidence column, consistently, so the counts agreed and the check passed.
4. **A shortfall cannot be overridden.** `generate.go` fails with `ShortfallError` when
   `produced < estimate × 2/3`. A cut-off response never reaches that check: `complete` already turned
   it into a `TruncatedError`. So every shortfall comes from a call that finished normally. Yet the
   error advises lowering reasoning effort, and there is no path forward.

The daemon runs generation as an operation (`internal/api/operations.go`). `Operation.UpdateMetadata`
may be called before the task function returns an error, and a failed operation's view still carries its
metadata. Structured failure detail therefore needs no change to the operations layer.

## Goals / Non-Goals

**Goals:**

- A claim that exists only in a generated domain context must not reach an answer as established fact.
- A domain context must concern the questions its pattern actually matches. When it might not, a human
  must be able to see that before the run.
- Every demand a document makes is extracted, whichever column it sits in.
- A count disagreement from a clean finish is the operator's call, made once, without paying for the
  questions call twice, and recorded in the manifest.

**Non-Goals:**

- Changing the answering pipeline (`buildRAGPrompt`, retrieval, `RunBatch`). See Open Questions.
- Detecting assertive domain contexts by heuristic in `--check` (see the decision below).
- Tuning prose-RFI granularity, i.e. how finely triage splits comma lists. Acceptance is the remedy for
  this change; granularity tuning needs more RFIs than one.
- Exporting retrieved sources in batch results. The MeitY run could not show which claims were grounded,
  and that is worth fixing, but it is the answering capability's.

## Decisions

### Enforce "domain text is not evidence" as a third appended rule

`mandatoryRules` gains a third block, appended by `AssembleGeneratedPrompt` like the other two and
detected by a new `PromptHasDomainTextRule`:

> REQUIREMENT DOMAIN TEXT IS GUIDANCE, NOT A SOURCE
> The "Requirement domain" text attached to a question tells you what the evaluator is looking for and
> where to look. It is not evidence. A capability, figure, location, legal entity, staffing level,
> certification or commitment that appears only in that text, and not in the retrieved context, is
> unsubstantiated: report it as a gap exactly as you would any other unsupported claim.

Two things make this the guarantee rather than the instruction to the strategy call.

First, it is applied after generation, so it holds even when the model ignores its instruction, and even
when an operator later hand-writes an assertive context into the generated manifest. That is the same
reasoning that made the first two rules appended rather than requested.

Second, it names the label the answering model actually sees ("Requirement domain"). The rule therefore
binds to the turn's structure, not to a paraphrase of it. A test pins the label against `buildRAGPrompt`'s
output, so a later rename of the label cannot silently orphan the rule.

The existing gap rule's phrase "the provided context" is narrowed to "the retrieved context". That
removes the ambiguity that let the domain text count as provided.

*Alternative: change the answer-time framing.* Rendering the domain as
`Requirement domain (guidance — not a source): …` in `buildRAGPrompt` would protect hand-written
manifests too, and is arguably the better long-term home. It is rejected for this change for two
reasons. It alters the answering turn for every manifest, which the analysis spec forbids analysis from
doing ("Analysis SHALL NOT alter … generation for any run"). And it belongs to `answer-batch-domains`.
It is recorded as an open question.

### Constrain contexts at the source: contract rule, instruction, and a worked contrast

`manifest-contract.md` gains a rule in its `domains[]` section: a context describes what the matching
questions ask, what to address and what evidence to look for, and never states the proposed solution's
capabilities, figures, locations, staffing, entities, certifications or commitments. It includes a
before/after pair taken from the live failure:

| Asserts (wrong) | Directs (right) |
|---|---|
| "Canonical offers localized 24x7 enterprise support within India." | "In-country enterprise support: address the support model, coverage hours and escalation path. Any claim about an in-country entity, office or staff must come from the retrieved context, or be reported as not covered." |

The strategy instruction's item 4 repeats the rule in one sentence and points at the contract section.

*Why no `--check` heuristic for assertive contexts.* A detector would have to recognise "vendor states
capability" in vendor-agnostic prose. Candidates such as present-tense capability verbs, or figures like
"24x7" and "99.9%", fire on legitimate directions ("address whether 24x7 support is offered") and miss
paraphrases. A noisy warning trains operators to ignore warnings, and this check's value rests on its
findings being worth reading. The appended rule is the enforcement; the instruction lowers how often it
is needed; the preview below helps the human review.

### Give the strategy call each question's text

`questionIDDigest` becomes a list of `- <id>: <text>` lines, still grouped under each `source` heading.
Each text is trimmed to 300 characters at a word boundary. Routing needs to know what a question is
about, not every word of it, and the cap bounds the added input at about 200 × 300 characters, roughly
15K tokens, for the largest manifest in the repository. The instruction's sentence changes from "You are
given the question ids…" to "You are given every question with its id. Write each pattern and context
against these questions, not against the document's own clause numbering, which the extraction pass may
have renumbered."

### Preview single-question entries in the check and the generation summary

The check's routing report already counts matches per entry (`chat/check.go`). For an entry matching
exactly one question, the report entry gains an `only_question` object with `id` and `text`
(trimmed to 120 characters). The CLI renders it on the line under the pattern; the UI's
`CheckFindings` renders it in the domain table. It is not a finding and never affects exit status.

The CLI generation summary is the same check output, so it gets this for free. On MeitY the output would
have shown:

```
T4-iii   1 question(s)
         ↳ T4-iii: Does the sovereign cloud provider have the ability to operate, maintain…
           context: CIS image management process specifically…
```

That is the mismatch, visible at a glance.

### Extract from every column that makes a demand

Both instructions gain one rule. In `questionsInstruction`, alongside "ONE QUESTION PER ENTRY":

> EVERY COLUMN THAT MAKES A DEMAND. A table row may carry its requirement in more than one column —
> evidence, validation proof, acceptance criteria, deliverables. A distinct demand in any of these is
> its own question, even when the primary requirement column says nothing of it. Put the row id and the
> column heading in "source" (e.g. "T4 — Evidence / Validation proof"). Columns that only label,
> number, categorise or score a row are not questions.

`triageInstruction`'s counting bullet gains the same sentence, so the estimate counts on the same
basis. If extraction then misses an evidence column, the shortfall check sees it, which is the point of
counting at all.

Ids for secondary-column questions follow the existing rule (document ids where they exist, a short
suffix otherwise), so a `T4-*` pattern still routes them.

### A clean-finish shortfall is an accept-or-abandon decision

`ShortfallError` gains `FinishReason` (always `STOP` in practice, recorded rather than assumed),
`AnswerTokens` and `Questions []chat.BatchQuestion`. `Questions` is never rendered; it exists so the
caller can continue. Its message changes to:

> generation produced 111 questions but triage estimated about 168. The questions call finished normally
> (7,216 of 65,536 output tokens), so this is a disagreement between the two passes about how finely to
> split the document, or a call that stopped early — not an exhausted budget. Review the produced
> questions; accept them to continue, or re-run.

`Generate` gains an options argument with `AcceptedQuestions []chat.BatchQuestion`. When set, the
questions call and the shortfall comparison are skipped, and generation runs the strategy call over the
given questions. The manifest writer receives an `Accepted *ShortfallAcceptance{Expected, Produced}`
and adds a provenance line:

```
# Question count accepted by the operator below the triage estimate: 111 produced, about 168 estimated
```

**CLI.** On `IsShortfall`, when stdout is a terminal (`common` already tests this for the spinner), the
command prints the error and asks `common.ConfirmationPrompt("Continue with 111 questions?")`. That is
the same helper as the existing "Generate a manifest from this document?" prompt; neither has a default
answer. On yes it calls `Generate` again with `AcceptedQuestions`; on no, or without a terminal, it exits
with the error. A truncation or empty response never reaches this branch: those are different error
types.

**Daemon.** `stagedAnalysis` gains `shortfall *stagedShortfall{questions, expected, produced}`, set when
a generation operation fails on a shortfall. Before returning the error, the operation calls
`op.UpdateMetadata({"shortfall": {"expected", "produced", "finish_reason"}})`. Clients read structure
from that, never from the error string. The generation request gains `accept_shortfall: bool`. When true
and the session holds a shortfall, generation runs with `AcceptedQuestions` from it, then clears it. When
true and the session holds none, the request is rejected with 400 "there is no shortfall to accept for
this session". The session's TTL is unchanged; an expired session is the existing expired-token error.
Retained questions are text, small next to the documents already staged, and are counted against the
store's byte bound like them.

*Why retain the questions rather than re-run with a flag.* Re-running would pay for the questions call a
second time and, being non-deterministic, might return a different list from the one the operator just
reviewed and accepted. Accepting what was seen is the only coherent meaning of "accept".

*Why the whole list, not per-question review.* Per-question editing is the manifest editor's job, and
the review step after generation already offers it. Acceptance only unblocks the strategy call.

### Show where a shortfall falls, and offer a re-run at higher effort

Triage already returns `sections[]` with `id_prefix` and `estimated_questions`. On a shortfall,
`ShortfallError` gains `Sections []SectionCount{Name, IDPrefix, Estimated, Produced}`. Produced is
counted by matching each produced question's id against the section's `id_prefix` (case-insensitive
prefix). Questions that match no prefix are reported as one "unattributed" row, so the totals always add
up. Where triage gave no prefix for a section, its produced count is shown as unknown rather than guessed.

MASCOM shows why the breakdown matters. "NFR: 22 produced, about 115 estimated" points at unsplit rows
in one section; "every section about two-thirds of its estimate" points at a granularity disagreement.
The totals alone cannot distinguish the two.

The re-run is `GenerateOptions{QuestionsEffort}`: it overrides the configured effort for that call only,
stepping one level up the `minimal` → `low` → `medium` → `high` ladder. At `high` the re-run action is not
offered, since there is nowhere higher to go. A re-run's result is compared against the estimate again,
and can itself produce a shortfall decision. In the CLI the confirm becomes a three-way choice
(continue / re-run / abandon); on REST it is a `retry_questions: true` field, mutually exclusive with
`accept_shortfall`; in the UI it is a third action.

### Make example names unmistakably placeholders

`manifest-example.md`'s knowledge bases (`vendor-rfp-answers`, `platform-docs`,
`subscription-and-support`) become `example-kb-past-answers`, `example-kb-platform-docs` and
`example-kb-support-terms`. The source groups get the same `example-` treatment. The `example-` prefix
is reserved: `ValidateSelections` reports a selected name with that prefix as "copied from the worked
example", which is a more useful diagnosis than "does not exist". The catalogue prompt (`cat.Prompt()`)
gains one sentence placed next to the list itself: "Select only names from this list. Names in the
worked example are illustrations and are not selectable."

*Why not drop the invented name and continue.* The current failure is deliberate, and stays. A copied
name usually stands in for a real base the model meant to select (here, most likely the catalogue's
past-RFP-answers base), so silently dropping it leaves a gap that nobody sees.

### Default the questions call to `medium` effort

On MASCOM, `low` produced 53 questions with the call finishing normally, and `medium` produced at least
140, for about 4K thought tokens. MeitY extracted fully at `low`, so `low` is not always wrong, but
under-splitting is the costlier error and the extra reasoning is cheap next to the documents conveyed.
The install hook seeds `medium`. The post-refresh `seed` only writes an absent key, so existing installs
keep whatever they have. That is deliberate (the hook must never clobber a maintainer's value), and the
change is documented in `docs/usage.md` rather than forced.

### UI: a caution notification with three actions

`AnalysisSteps.tsx` handles a generation failure whose metadata carries `shortfall` by rendering a
`p-notification--caution` with its icon and a title "Fewer questions than estimated". The body states
both counts and that extraction finished normally. `p-notification__actions` holds "Continue with 111
questions" (primary), "Re-run with more reasoning" (secondary; absent when the effort is already
`high`) and "Start again" (secondary, returns to upload). Above the actions, a compact table lists each
section's produced and estimated counts. Continue posts the same token and
interview answers with `accept_shortfall: true` through the existing `postAsync` operation path, with
its running state. A failure without `shortfall` keeps today's `p-notification--negative` with no
continue action. A notification carrying actions is not yet in `ui-conventions`' sanctioned patterns, so
the skill's Banners row is extended in the same change, as the skill requires.

## Risks / Trade-offs

- [The domain-text rule makes the answering model flag more gaps, including some the retrieved context
  did support] → This is the intended direction, given the rule's priority ("a flagged gap is routed to a
  human; a fabricated claim survives into an evaluation"). It is measured by re-running MeitY and
  comparing gap counts and the T4 answers against the 2026-10-08 run.
- [Instructions change behaviour probabilistically; unit tests cannot show the model complies] → Unit
  tests pin what rag-cli controls: the rule is appended and detected, the digest carries text, acceptance
  skips the questions call, retained questions are reused, provenance is written. Model behaviour is
  verified by live re-runs of MeitY (expect the CIS question present, and `T4-iii`'s context to concern
  `T4-iii`) and Lafika (expect a shortfall that can be accepted).
- [Accepting lets a genuinely lazy extraction through] → Acceptance is explicit, never a default, needs a
  terminal or a deliberate API flag, and is written into the manifest's header, where review and
  `--check` show it.
- [The strategy call's input grows] → Capped at 300 characters per question; about 15K tokens at the
  repository's largest manifest, small next to the conveyed documents.
- [Rule wording coupled to the `Requirement domain` label] → A test asserts the label still appears in
  `buildRAGPrompt` output, so a rename fails CI rather than orphaning the rule.

## Migration Plan

Additive, on an unreleased feature. Manifests generated before this change lack the third rule;
`--check` reports that as a warning, prompting a regeneration or a manual edit. The new REST field is
optional, and the shortfall metadata is new data on an existing failure. Rollback is a snap revert.

Archive order: `add-gemini-rfp-analysis`, then `switch-analysis-to-gemini-api`, then this change, since
each modifies requirements the previous one introduces or changes.

## Open Questions

- **Should the answering turn itself frame domain text as non-evidence?** A one-line change to
  `buildRAGPrompt` would protect hand-written manifests as well as generated ones. It changes answering
  for every manifest, so it needs its own change against `answer-batch-domains`, ideally with a
  before/after run on a hand-written manifest such as `Telekom_Malaysia_0_Full.yaml`.
- **Should batch results record which retrieved sources each answer used?** Without them, a run like
  MeitY's cannot show whether a claim was grounded or carried in from the domain context.
- **Should prose RFIs be extracted at the granularity a human would choose?** Lafika's hand-written
  manifest has 24 domain-level questions; the models produced 111–168. Acceptance unblocks the run but
  does not make 111 the right number.
