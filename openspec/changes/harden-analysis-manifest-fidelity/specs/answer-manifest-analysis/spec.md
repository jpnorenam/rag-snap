## MODIFIED Requirements

### Requirement: Every generated prompt carries the non-negotiable answer rules

The generated `prompt` SHALL carry all three of the following as instructions to the answering model,
regardless of the RFP, the interview answers, or the model's own judgement:

- That answering "no context available", or stating that a technology is NOT covered, is preferable to
  giving a false or fabricated answer. This SHALL be stated as a strong instruction, not a hedge or an
  aside.
- That answers are direct, and that filler language is to be avoided.
- That text attached to a question as its requirement domain describes what to address and where to
  look, and is not a source: a capability, figure, location, entity or commitment that appears only in
  that text and not in the retrieved passages SHALL be treated as unsubstantiated and reported as a gap.

These rules SHALL be appended by rag-cli after generation rather than requested of the model, and SHALL
be verifiable in the emitted manifest, so that a prompt missing any of them is detectable without
re-reading the RFP.

The generated prompt SHALL NOT contradict the guarantees the run already enforces around it — the
fixed no-context answer emitted when retrieval returns nothing, and the source rules appended to every
custom prompt. In particular the generated prompt SHALL NOT instruct the model to answer from its own
knowledge when context is absent.

#### Scenario: Rules present regardless of RFP

- **WHEN** any manifest is generated
- **THEN** its prompt instructs that "no context available" or "not covered" is preferable to a fabricated answer, that answers be direct and free of filler, and that requirement-domain text is not a source

#### Scenario: Generated prompt does not license ungrounded answers

- **WHEN** a manifest is generated
- **THEN** its prompt contains no instruction to answer from the model's own knowledge when no context is available

#### Scenario: Domain text is not evidence

- **WHEN** a generated manifest is run and a question's domain context names a capability its retrieved passages do not support
- **THEN** the prompt the answering model received instructs it to treat that capability as unsubstantiated rather than as established

### Requirement: A generated manifest is complete and structurally valid

A generated manifest SHALL load under the existing manifest reader without modification: it SHALL
carry a `version`, at least one question, no question with an empty question field, no two `domains`
entries with the same `match` pattern, and no `domains` entry carrying neither `context` nor
`keywords`.

Because a long RFP can drive the model's response against its output ceiling — a ceiling that on a
reasoning model covers the model's internal reasoning as well as the visible answer — an incomplete
response SHALL be an error and never a silently short manifest. Analysis SHALL treat the following as
failures and SHALL report each rather than writing what it received:

- A response the provider reports as having stopped at its output ceiling.
- A response that is **empty**, which SHALL NOT be interpreted as the model having found nothing. An
  empty response at the output ceiling is the signature of reasoning having consumed the budget.
- A generated question count that falls materially short of the triage estimate.

A response cut off at the output ceiling, and an empty response, SHALL NOT be acceptable by any means.

A question-count shortfall from a questions call that finished normally is different: two passes
disagreeing about how finely to split the document is as likely as a call that stopped early. Such a
shortfall SHALL be reported naming the expected count, the produced count, and that the call finished
normally, and SHALL NOT recommend changing reasoning effort as its remedy. The operator SHALL then be
able to accept the produced questions and continue to the rest of generation, without the questions
being generated again. Acceptance SHALL be an explicit operator action, never a default. When no
operator can be asked, the shortfall SHALL fail as an error.

Because the same pair of counts arises both from over-counting and from an extraction that left packed
cells unsplit, the report SHALL show, for every section triage estimated, that section's produced and
estimated counts, so the operator can see where extraction fell short rather than judging from a total.
As an alternative to accepting, the operator SHALL be able to re-run only the questions call at the
next higher reasoning effort, up to the highest, over the documents already conveyed.

Analysis SHALL request an explicit output ceiling rather than relying on a provider default, since a
default sized for ordinary replies truncates the answer without reducing the reasoning that displaced
it.

A response SHALL be validated against what the manifest requires rather than trusted because it
parsed, so a syntactically valid response missing a required field is a failure and not a partially
populated manifest.

A generated manifest SHALL be written only after it has been validated. When validation fails,
analysis SHALL surface the reason and SHALL NOT leave a partially written manifest in place of a
previously valid file.

The emitted manifest SHALL record that it was generated by analysis and the model that generated it,
so a manifest's provenance is readable from the file. A manifest generated after the operator accepted a
shortfall SHALL also record the acceptance with both counts.

#### Scenario: Generated manifest loads unmodified

- **WHEN** a generated manifest is loaded by the manifest reader
- **THEN** it loads without error and without hand-editing

#### Scenario: Truncated response

- **WHEN** the model's response is truncated against its output ceiling
- **THEN** analysis reports the truncation, does not write a manifest, and offers no way to accept it

#### Scenario: Empty response at the output ceiling

- **WHEN** the model returns no content and reports having stopped at its output ceiling
- **THEN** analysis reports it as a failure rather than as the model having found nothing, and does not write a manifest

#### Scenario: Response missing a required field

- **WHEN** the model returns a well-formed response that omits a field the manifest requires
- **THEN** analysis reports it as a failure rather than writing a manifest with that field absent

#### Scenario: Question count materially short of triage

- **WHEN** triage estimated roughly 150 questions and generation produced 40 from a call that finished normally
- **THEN** analysis reports the discrepancy naming both counts and that the call finished normally, recommends no change to reasoning effort, and does not present the 40-question manifest as complete without the operator's decision

#### Scenario: Operator accepts a shortfall

- **WHEN** the operator accepts a reported shortfall
- **THEN** generation continues with the questions already produced, without a second questions call, and the written manifest records the acceptance with both counts

#### Scenario: Shortfall shows where it falls

- **WHEN** a shortfall is reported for a document whose triage estimated two sections at about 95 and 115 questions
- **THEN** the report shows each section's produced count beside its estimate

#### Scenario: Re-running the extraction at higher effort

- **WHEN** the operator chooses to re-run after a shortfall from a questions call made at low reasoning effort
- **THEN** only the questions call is repeated, at medium effort, over the already-conveyed documents, and its result is checked against the estimate again

#### Scenario: Shortfall with no operator to ask

- **WHEN** a shortfall occurs and no operator can be asked to accept it
- **THEN** analysis fails as an error and writes no manifest

#### Scenario: Provenance is recorded

- **WHEN** a manifest is generated
- **THEN** the file records that it was produced by analysis and names the model that produced it

### Requirement: Generated domains route to real questions

The `domains` block a generated manifest carries SHALL be populated, not a commented-out stub, and its
patterns SHALL be written against the question ids the same manifest actually contains.

The generation step that writes domains SHALL be given each generated question's id **and text**, so
that a context written for a pattern concerns the questions that pattern actually matches. The document's
own clause numbering is not evidence of which generated question an id refers to.

Every generated `domains` entry SHALL carry a `context` naming the section or requirement domain it
covers. An entry MAY additionally carry `keywords`. A `context` SHALL describe what the matching
questions ask about, what to address and what counts as evidence for them. It SHALL NOT state the
proposed solution's capabilities, figures, locations, staffing, legal entities, certifications or
commitments as facts, since it reaches the answering model next to the retrieved passages and would be
repeated as though substantiated.

Domains exist primarily to attach context to a section of the RFP; attaching context to a single
question SHALL remain possible, expressed as a pattern matching that one question's id.

Generation SHALL verify each generated pattern against the generated question list, and SHALL report
any pattern matching no question. Such a pattern SHALL be reported rather than removed, since a
deliberately reserved pattern is legitimate, but it SHALL NOT pass unreported. For each entry matching
exactly one question, the generation summary SHALL show that question's text alongside the pattern.

#### Scenario: Domains block is populated

- **WHEN** a manifest is generated from an RFP with distinct sections
- **THEN** its `domains` block is present and active, with an entry per section carrying a context, rather than a commented-out stub

#### Scenario: Pattern matching no question

- **WHEN** a generated `domains` entry matches none of the generated questions
- **THEN** that pattern is reported to the operator and the manifest still records it

#### Scenario: Context attached to one question

- **WHEN** the RFP requires extra context for a single question
- **THEN** that is expressed as a `domains` entry whose pattern matches only that question's id, and whose context concerns that question's text

#### Scenario: Strategy is grounded in question text

- **WHEN** the domains are generated
- **THEN** the generation step received each question's id together with its text, not the ids alone

#### Scenario: Context directs rather than asserts

- **WHEN** a domain context is generated for a section about in-country support
- **THEN** it directs the answer to address in-country support and to substantiate any claim from the retrieved passages, rather than stating that the vendor provides in-country support

## ADDED Requirements

### Requirement: The worked example cannot stand in for the live catalogue

The worked example carried in each request SHALL name retrieval targets that are visibly placeholders
and cannot exist in a real catalogue, and each request SHALL state, next to the live catalogue, that only
names from that catalogue may be selected and that names in the example are not selectable.

When a generated selection names a target that is not in the live catalogue, the failure SHALL say so,
naming the target. When the name is one the worked example uses, the failure SHALL say that it was copied
from the example.

#### Scenario: Example names are placeholders

- **WHEN** the worked example is rendered into a request
- **THEN** every knowledge base and source group it names is a visible placeholder that no real catalogue could contain

#### Scenario: Name copied from the example

- **WHEN** the generated selection names a knowledge base taken from the worked example
- **THEN** analysis fails naming that knowledge base and saying it came from the example rather than the catalogue

### Requirement: Requirements are extracted from every column that states one

Triage and question extraction SHALL treat every part of a document that makes a demand of the
respondent as a source of questions. In a table that includes evidence, validation-proof, acceptance-
criteria or deliverable columns as well as the primary requirement column, a distinct demand in any of
them SHALL become a question. A column that only describes, labels, numbers or scores a row SHALL NOT.

The triage question-count estimate SHALL be made on the same basis as extraction, so that a demand
missed by extraction shows up as a shortfall against the estimate rather than being missed by both.

A question extracted from a secondary column SHALL carry, in its `source`, which row and column it came
from, so an operator reviewing the manifest can see why it exists.

#### Scenario: Evidence column carries a separate requirement

- **WHEN** a requirement row's evidence column asks the respondent to describe their image hardening process, a demand the requirement column does not make
- **THEN** that demand is extracted as its own question, with a source naming the row and the evidence column

#### Scenario: Descriptive column is not a question

- **WHEN** a column only labels a row's category or priority
- **THEN** no question is extracted from it

#### Scenario: Estimate and extraction agree on what counts

- **WHEN** triage estimates a question count for a table with requirement and evidence columns
- **THEN** the estimate includes the demands in the evidence column
