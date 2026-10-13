# answer-manifest-check Specification

## Purpose

Validate an `answer batch` manifest without running it. A manifest is an expensive thing to run — a
long RFP is many model calls — and its most damaging defects are silent: a domain pattern that matches
no question routes nothing and produces answers with no context attached, a knowledge base named with
a typo simply contributes no hits, and a prompt that lost its no-context rule invites fabrication.
None of these fail a run; they degrade it invisibly.

The check reads a manifest, resolves its references against the running services, and reports what it
finds. It applies to any manifest whatever its origin, including the hand-written ones, and is the
closing step of the analysis flow: the place a manifest written by a model that has never seen rag-cli
is held against what rag-cli will actually do with it.

## ADDED Requirements

### Requirement: The check is read-only and makes no model calls

The check SHALL NOT answer any question, SHALL NOT call the inference backend or the analysis model,
and SHALL NOT modify the manifest it is given or any other file. It SHALL be safe to run repeatedly at
no cost beyond reading the manifest and querying the knowledge store for the names it must resolve.

The check SHALL report on a manifest it cannot fully resolve rather than refusing to run: when the
knowledge store is unreachable, the checks that do not depend on it SHALL still be performed and
reported, and the reference checks SHALL be reported as not performed rather than as passing.

#### Scenario: Checking a manifest costs no model calls

- **WHEN** a manifest is checked
- **THEN** no question is answered and no model is called

#### Scenario: Manifest is not modified

- **WHEN** a manifest is checked
- **THEN** the manifest file is byte-for-byte unchanged

#### Scenario: Knowledge store unreachable

- **WHEN** a manifest is checked while the knowledge store cannot be reached
- **THEN** the structural, routing and prompt checks are still reported, and the reference checks are reported as not performed rather than as passing

### Requirement: Structural defects are reported

The check SHALL report a manifest that the run would reject, so a defect is found before a run is
started rather than during it: a missing or empty question list, a question with an empty question
field, a `domains` entry with no `match`, a `domains` entry carrying neither `context` nor `keywords`,
and two `domains` entries declaring the same `match` pattern.

The check SHALL additionally report defects the run tolerates but which cost the operator results: two
questions sharing the same `id`, since results are keyed by id, and a question with no `id` at all.

The check SHALL report the manifest's question count and domain count, so the operator can compare them
against the source document.

#### Scenario: Duplicate question ids

- **WHEN** a checked manifest contains two questions with the same `id`
- **THEN** the check reports the duplicated id

#### Scenario: Counts are reported

- **WHEN** a manifest is checked
- **THEN** the report states how many questions and how many domains the manifest carries

#### Scenario: Defect the run would reject

- **WHEN** a checked manifest carries a `domains` entry with neither `context` nor `keywords`
- **THEN** the check reports it as a defect that would fail the run

### Requirement: Unreachable domain patterns are reported as warnings, not errors

For each `domains` entry, the check SHALL resolve its `match` pattern against the manifest's questions
using the same resolution the run applies — matched against a question's `id`, falling back to
`source` for a question whose `id` is a bare sequence number, with glob wildcards, most specific
pattern winning — and SHALL report how many questions each entry would route.

An entry matching no question SHALL be reported as a warning and SHALL NOT fail the check, because a
pattern deliberately reserved for a section that carries no questions is legitimate. The report SHALL
make clear that a zero-match pattern may be intentional.

The check SHALL also report a question that no `domains` entry matches, when the manifest declares
domains at all, since an unrouted question in an otherwise routed manifest is usually an oversight.

A manifest declaring no `domains` block SHALL NOT be reported as defective on that account.

#### Scenario: Pattern matching no question

- **WHEN** a checked manifest declares a `domains` entry whose pattern matches none of its questions
- **THEN** the check reports it as a warning, notes it may be intentional, and does not fail

#### Scenario: Per-entry match counts

- **WHEN** a manifest with several domains is checked
- **THEN** the report states, for each entry, how many questions it would route

#### Scenario: Question matched by no domain

- **WHEN** a checked manifest declares domains and one question matches none of them
- **THEN** the check reports that question as unrouted

#### Scenario: Manifest without domains

- **WHEN** a manifest declaring no `domains` block is checked
- **THEN** the absence of domains is not reported as a defect

### Requirement: Referenced retrieval targets are resolved against the running services

The check SHALL resolve every entry of `knowledge_bases` against the knowledge bases that exist, and
every entry of `kapa_source_groups` against the kapa source groups that exist, and SHALL report any
entry that does not resolve.

For an unresolved knowledge base whose name is close to one that exists, the report SHALL name the
close match, since the defect is characteristically a typo and the consequence — a silently empty
contribution to retrieval — gives the operator no other signal.

An unresolved reference SHALL be reported as a defect and not a warning, because unlike a reserved
domain pattern it has no legitimate use: a knowledge base that does not exist contributes nothing to
any question.

#### Scenario: Knowledge base that does not exist

- **WHEN** a checked manifest names a knowledge base that the knowledge store does not have
- **THEN** the check reports it as a defect

#### Scenario: Near-miss knowledge base name

- **WHEN** a checked manifest names a knowledge base one character different from an existing one
- **THEN** the report names the existing knowledge base as a likely intended match

#### Scenario: Kapa source group that does not exist

- **WHEN** a checked manifest names a kapa source group identifier that the kapa project does not have
- **THEN** the check reports it as a defect

### Requirement: Prompt content assertions are reported

When a manifest carries an inline `prompt`, the check SHALL report whether that prompt still carries
the content a generated prompt is required to have: an instruction preferring "no context available"
or "not covered" over a fabricated answer, and a stated answer length together with whether it is a
hard limit or a soft target.

A missing assertion SHALL be reported as a warning rather than a defect, since a hand-written prompt
may legitimately express these differently or omit a word budget entirely. The report SHALL name which
assertion was not found, so the operator can judge it.

A manifest carrying no inline `prompt`, or carrying a reference to a stored prompt variant instead,
SHALL have the prompt assertions reported as not applicable rather than as failing.

#### Scenario: Prompt carrying both assertions

- **WHEN** a manifest whose prompt states a 120-word hard limit and the no-context preference is checked
- **THEN** the report records both assertions as found

#### Scenario: Prompt missing the no-context rule

- **WHEN** a checked manifest's prompt carries no instruction preferring "no context available" over a fabricated answer
- **THEN** the check reports that assertion as not found, as a warning

#### Scenario: Manifest with no inline prompt

- **WHEN** a manifest carrying no inline `prompt` is checked
- **THEN** the prompt assertions are reported as not applicable

### Requirement: The check distinguishes defects from warnings in its result

The check SHALL report findings at two severities: a defect, meaning the manifest would fail to run or
carries a reference that cannot contribute, and a warning, meaning the manifest will run but something
in it is likely unintended.

A manifest with no findings SHALL be reported as clean. A manifest with warnings but no defects SHALL
be distinguishable from both a clean manifest and a defective one, so that the common case of a
deliberately reserved domain pattern does not read as a failure.

When run from the command line, the exit status SHALL distinguish a manifest with defects from one
without, so the check is usable in a script; warnings alone SHALL NOT produce a failing exit status.

#### Scenario: Clean manifest

- **WHEN** a manifest with no findings is checked
- **THEN** it is reported as clean and the exit status indicates success

#### Scenario: Warnings only

- **WHEN** a manifest whose only finding is a zero-match domain pattern is checked
- **THEN** the result is distinguishable from clean and from defective, and the exit status indicates success

#### Scenario: Defects present

- **WHEN** a manifest naming a non-existent knowledge base is checked
- **THEN** the result reports a defect and the exit status indicates failure

### Requirement: The check closes the analysis flow

A manifest produced by analysis SHALL be checked as part of producing it, and the findings SHALL be
presented to the operator alongside the generated manifest, before they run it.

The check SHALL be reachable independently of analysis, so a manifest written by hand or edited after
generation can be checked at any time without re-running analysis.

#### Scenario: Findings accompany a generated manifest

- **WHEN** analysis generates a manifest
- **THEN** the check findings for that manifest are presented with it before it is run

#### Scenario: Checking a hand-written manifest

- **WHEN** an operator checks a manifest that analysis did not produce
- **THEN** the check runs and reports normally
