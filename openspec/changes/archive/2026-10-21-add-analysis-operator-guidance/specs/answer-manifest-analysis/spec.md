## ADDED Requirements

### Requirement: Operator instructions steer analysis

Analysis SHALL accept optional free-text instructions from the operator alongside the documents, and
SHALL apply them to every model call the analysis makes: triage, question extraction and strategy.
Applying them to triage is what keeps the question-count estimate on the same basis as the extraction,
so an instruction that narrows scope narrows the estimate too rather than producing a shortfall.

Instructions SHALL be conveyed as operator-authored text, separate from and labelled distinctly from
the document content, which remains untrusted input. They SHALL be bounded in length, and an
over-long set SHALL be rejected before any model call, naming the limit.

Instructions SHALL NOT remove, weaken or replace the mandatory prompt rules. The rules SHALL continue
to be appended after generation, and SHALL remain the final word in the generated prompt whatever the
instructions say.

The instructions given SHALL be kept with the analysis session, so that generation uses the same
instructions triage did. They SHALL be recorded in the generated manifest's provenance, so that a
manifest shows what its generation was asked to do.

#### Scenario: Granularity instruction reaches extraction

- **WHEN** analysis is run with the instruction "split every matrix row into one question per requirement"
- **THEN** the question-extraction request carries that instruction

#### Scenario: Scope instruction narrows the estimate

- **WHEN** analysis is run with an instruction limiting it to one matrix in the document
- **THEN** the triage request carries the instruction, so its estimate counts only that matrix

#### Scenario: Instructions are separate from document content

- **WHEN** an analysis request is prepared with instructions
- **THEN** the instructions appear as labelled operator text, not inside any document's content

#### Scenario: Instructions cannot remove the mandatory rules

- **WHEN** analysis is run with an instruction to omit gap reporting from the prompt
- **THEN** the generated prompt still ends with every mandatory rule

#### Scenario: Over-long instructions

- **WHEN** instructions longer than the limit are given
- **THEN** analysis fails before any model call, naming the limit

#### Scenario: Instructions are recorded

- **WHEN** a manifest is generated with instructions
- **THEN** its provenance records the instructions that were given

#### Scenario: No instructions

- **WHEN** analysis is run without instructions
- **THEN** it behaves exactly as before, and the provenance records no instructions

### Requirement: The generated prompt is exposed separately from the appended rules

Generation SHALL make available, alongside the assembled prompt, the model-written part of the prompt
and the block rag-cli appends to it, such that the assembled prompt is exactly the model-written part
followed by the appended block. A client offering prompt edits SHALL be able to replace the
model-written part and re-assemble the prompt without parsing the assembled text.

#### Scenario: Parts re-assemble to the prompt

- **WHEN** a manifest is generated
- **THEN** joining the model-written part and the appended block reproduces the manifest's prompt exactly
