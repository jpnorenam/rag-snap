# Spec Delta

## ADDED Requirements

### Requirement: Batch results carry per-answer sources
Each batch result SHALL carry a `sources` list of the documents its answer cited, each with its
citation key, label, link, locator, knowledge base, and label tag, in first-cited order. The same
field SHALL appear in the CLI's JSON results file and in the API's results. A no-answer result SHALL
carry an empty `sources` list.

#### Scenario: Answer citing two documents
- **WHEN** a question's answer cites two retrieved documents
- **THEN** its result's `sources` lists both documents with their labels and links

#### Scenario: No context retrieved
- **WHEN** retrieval returns nothing for a question
- **THEN** its result's answer is the no-answer response and its `sources` list is empty

### Requirement: Batch answers are free of citation markers
A batch result's `answer` SHALL NOT contain citation markers: markers SHALL be removed from the
answer text, with surrounding whitespace and punctuation normalized, once the answer's sources are
recorded. Unlike chat, a batch answer for which context was retrieved but which cites no retrieved
document SHALL be withheld: replaced by the no-answer response with empty sources.

#### Scenario: Markers stripped from prose
- **WHEN** the model answers `The solution provides FIPS 140-3 certified modules. [1][2]`
- **THEN** the result's `answer` is `The solution provides FIPS 140-3 certified modules.` and its `sources` lists the `[1]` and `[2]` documents

#### Scenario: Uncited batch answer
- **WHEN** context was retrieved for a question and the model's answer cites no retrieved document
- **THEN** the result's `answer` is the no-answer response and its `sources` list is empty
