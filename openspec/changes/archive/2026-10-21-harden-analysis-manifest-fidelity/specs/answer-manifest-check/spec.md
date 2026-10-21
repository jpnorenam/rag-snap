## MODIFIED Requirements

### Requirement: Prompt content assertions are reported

When a manifest carries an inline `prompt`, the check SHALL report whether that prompt still carries
the content a generated prompt is required to have: an instruction preferring "no context available"
or "not covered" over a fabricated answer; an instruction that requirement-domain text is not a source;
and a stated answer length together with whether it is a hard limit or a soft target.

A missing assertion SHALL be reported as a warning rather than a defect, since a hand-written prompt
may legitimately express these differently or omit a word budget entirely. The report SHALL name which
assertion was not found, so the operator can judge it.

A manifest carrying no inline `prompt`, or carrying a reference to a stored prompt variant instead,
SHALL have the prompt assertions reported as not applicable rather than as failing.

#### Scenario: Prompt carrying all assertions

- **WHEN** a manifest whose prompt states a 120-word hard limit, the no-context preference and the domain-text rule is checked
- **THEN** the report records all three assertions as found

#### Scenario: Prompt missing the no-context rule

- **WHEN** a checked manifest's prompt carries no instruction preferring "no context available" over a fabricated answer
- **THEN** the check reports that assertion as not found, as a warning

#### Scenario: Prompt missing the domain-text rule

- **WHEN** a checked manifest's prompt carries no instruction that requirement-domain text is not a source
- **THEN** the check reports that assertion as not found, as a warning

#### Scenario: Manifest with no inline prompt

- **WHEN** a manifest carrying no inline `prompt` is checked
- **THEN** the prompt assertions are reported as not applicable

## ADDED Requirements

### Requirement: Single-question domain entries show the question they attach to

For every `domains` entry whose pattern matches exactly one question, the check SHALL show that
question's id and the start of its text alongside the entry's pattern and the start of its context.
This SHALL be informational, never a finding, since the check cannot judge whether a context fits a
question. Its purpose is to make a context attached to the wrong question visible to a reader before the
manifest is run.

#### Scenario: Single-question entry is previewed

- **WHEN** a manifest whose `T4-iii` domain matches exactly one question is checked
- **THEN** the routing report shows the `T4-iii` question's text and the domain's context together

#### Scenario: Section entry is not previewed

- **WHEN** a domain entry matches several questions
- **THEN** the routing report shows its match count as before, without per-question text
