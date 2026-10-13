# answer-manifest-analysis Specification

## Purpose

Generate a complete `answer batch` manifest from RFP/RFI documents using a document-capable LLM, so
the operator no longer hand-writes the semantic half of a manifest. Where the existing build path
extracts questions mechanically and leaves the RFP-level prompt, knowledge-base selection and domain
routing to the operator, analysis produces all of them together: `questions`, `knowledge_bases`,
`kapa_source_groups`, a single RFP-level `prompt`, and a populated `domains` block.

This capability covers how documents reach the model without losing content, how the model is
grounded in a manifest contract it has no prior knowledge of, the two-phase triage-then-generate flow
and the operator interview between them, the rules baked into every generated prompt, and the
guarantees on the emitted manifest. It is an additional mode of the build path shared by the CLI and
the daemon; the existing extraction behaviour is unchanged and remains the default.

## ADDED Requirements

### Requirement: Analysis is an opt-in mode of the document build path

Manifest analysis SHALL be an explicitly requested mode of the existing document build path, not a
change to its default behaviour. When analysis is not requested, the build path SHALL behave exactly
as it did before this capability existed: Tika extraction, pattern-based question detection, the
optional refinement pass, and a manifest carrying `questions` plus `knowledge_bases` with a
commented-out `domains` stub.

The mode SHALL accept one primary RFP document and zero or more additional context documents. Every
supplied document SHALL be conveyed to the model; a context document SHALL NOT be silently dropped.

Analysis SHALL NOT be the only way to obtain a manifest — an operator SHALL retain the existing
extraction path as a fallback when analysis is unavailable, fails, or produces a result they reject.

#### Scenario: Build without requesting analysis

- **WHEN** a document is built without requesting analysis
- **THEN** the extracted questions, the emitted manifest fields, and the commented-out domains stub are identical to those produced before this capability existed

#### Scenario: Analysis with additional context documents

- **WHEN** analysis is requested with a primary RFP document and two additional context documents
- **THEN** all three documents are conveyed to the model and the response is generated with all of them in scope

#### Scenario: Analysis unavailable

- **WHEN** analysis is requested but cannot run
- **THEN** the failure names the reason and states that the existing extraction path remains available, and no partial manifest is written

### Requirement: Documents are conveyed without losing tabular or column content

Conveyance SHALL preserve the document content that naive text extraction destroys: the description,
current-environment and evaluator-guidance columns beside a requirement, and the sheet or section a
requirement belongs to. The value of a generated manifest depends on exactly that content.

A PDF SHALL be conveyed as the original bytes, so the model applies its own document understanding to
layout, tables and figures rather than receiving a flattened transcription.

A format for which the model offers no native document understanding — spreadsheet and word-processor
formats among them — SHALL NOT be conveyed by uploading the original bytes and SHALL NOT be conveyed
as unstructured plain text. It SHALL instead be converted to a textual representation that preserves
table structure, in which every cell of every row remains associated with its column heading, and each
table is labelled with the sheet or section name it came from. Conversion SHALL NOT rely on markup
that the model strips before reading.

Every row and column of a converted table SHALL be conveyed. The conversion SHALL NOT select a single
question column, drop columns judged uninteresting, or truncate rows, since the discarded columns are
the material the RFP-level prompt and the domain contexts are built from.

The number of documents conveyed, and per-document the format detected and the conveyance path taken,
SHALL be reported to the operator, so a document silently converted rather than read natively is
visible before the manifest is generated.

#### Scenario: Spreadsheet with context columns beside the requirement

- **WHEN** a spreadsheet whose sheets carry a requirement column plus description, current-environment and evaluator-guidance columns is analysed
- **THEN** the conveyed representation retains every column with its heading, retains the sheet name for each table, and no column is dropped

#### Scenario: PDF conveyance

- **WHEN** a PDF is analysed
- **THEN** the original bytes are conveyed rather than an extracted text transcription

#### Scenario: Format without native document understanding

- **WHEN** a word-processor document is analysed
- **THEN** it is converted to a table-preserving textual representation rather than uploaded as original bytes, and the operator is told the document was converted

#### Scenario: Document exceeding the model's document limits

- **WHEN** a document exceeds the model's documented size or page limit for its conveyance path
- **THEN** analysis fails before any model call with a message naming the document, the limit, and the measured value

### Requirement: The model is grounded in the manifest contract and live catalogues

Each analysis request SHALL carry all of the following as part of the request itself rather than as
operator documentation, because the model has no prior knowledge of rag-cli, the manifest format, or
the deployment it is generating for:

- The manifest contract: every field the manifest may carry, which are required, and what each means.
- The domain resolution rules the runtime actually applies — that a pattern is matched against a
  question's `id`, that a question whose `id` is a bare sequence number is matched against its
  `source` instead, that patterns may use glob wildcards, and that the most specific matching pattern
  wins — so that generated patterns route as the model intends them to.
- At least one worked example of a complete, well-formed manifest, so the contract is shown and not
  only described.

Each request SHALL additionally carry the **live** catalogue of selectable retrieval targets, read
from the running services at request time: the knowledge bases that exist, and the kapa source groups
that exist with both their names and their identifiers. The model SHALL be instructed to select only
from that catalogue. Kapa source group identifiers are opaque and unguessable, so a manifest SHALL
NOT be generated from a remembered or invented catalogue.

When the live catalogue cannot be read, analysis SHALL fail with a message naming the unreachable
service rather than proceeding with an empty or assumed catalogue.

#### Scenario: Catalogue is read at request time

- **WHEN** an analysis request is prepared
- **THEN** the knowledge bases and kapa source groups offered to the model are those the running services report at that moment

#### Scenario: Generated manifest selects only catalogued targets

- **WHEN** a manifest is generated
- **THEN** every entry in its `knowledge_bases` and `kapa_source_groups` appears in the catalogue that was supplied with the request

#### Scenario: Catalogue unreachable

- **WHEN** the knowledge store cannot be reached while preparing an analysis request
- **THEN** analysis fails naming that service and no model call is made

### Requirement: Analysis runs as two phases over one document upload

Analysis SHALL run as two phases. The first phase SHALL convey the documents and return a triage of
what was found, without generating a manifest: the RFP's identity as stated in the document, its
section or sheet structure, an estimate of how many questions it contains, whether the RFP requires
OpenStack to be addressed, whether questions carry more than one field to be answered each, and the
knowledge bases the triage suggests. The second phase SHALL generate the manifest.

Documents SHALL be conveyed once and reused by both phases; the second phase SHALL NOT require the
operator to supply or re-upload them. The reference to the conveyed documents SHALL be staged under a
token returned by the first phase, and SHALL expire.

The first phase exists so that the interview can be conditioned on the document's actual content —
in particular so the OpenStack question is asked only when the RFP requires OpenStack. An
implementation SHALL NOT collapse the phases in a way that requires the operator to answer questions
about content that has not yet been read.

When the second phase is invoked with an expired or unknown token, it SHALL fail with a message saying
the analysis session expired and that the documents must be re-conveyed, rather than proceeding
without them.

#### Scenario: Triage precedes the interview

- **WHEN** the first phase completes
- **THEN** it reports the RFP identity, structure, question-count estimate, whether OpenStack is in scope, whether questions carry multiple answer fields, and suggested knowledge bases, and no manifest has been generated

#### Scenario: Second phase reuses the conveyed documents

- **WHEN** the second phase is invoked with the token from a completed first phase
- **THEN** the manifest is generated from the already-conveyed documents without the operator supplying them again

#### Scenario: Expired analysis session

- **WHEN** the second phase is invoked with an expired or unknown token
- **THEN** it fails with a message stating the session expired and that the documents must be re-conveyed

### Requirement: An operator interview conditions the generated manifest

Between the phases the operator SHALL be asked, and their answers SHALL materially shape the
generated manifest:

- **Answer length.** A target answer length in words, defaulting to 100.
- **Length enforcement.** Whether that length is a hard limit that must never be exceeded, or a soft
  target that may be slightly exceeded.
- **OpenStack flavour.** When and only when triage reported that the RFP requires OpenStack to be
  addressed, whether answers SHALL present Sunbeam or Charmed OpenStack. When triage reported
  OpenStack is not in scope, this question SHALL NOT be asked.

The chosen length and its enforcement mode SHALL both appear in the generated prompt, stated as a
number of words and explicitly as either a hard limit or a soft target — a generated prompt SHALL NOT
state a length without stating which of the two it is.

When an OpenStack flavour is chosen, the generated prompt SHALL name that flavour as the offering to
describe, and SHALL NOT present the other flavour as the proposed solution.

When triage found that questions carry more than one field to be answered each, the generated prompt
SHALL state how the several fields of a question are to be answered.

#### Scenario: Default answer length

- **WHEN** the operator accepts the default at the answer-length question
- **THEN** the interview records 100 words

#### Scenario: Hard limit is stated as a hard limit

- **WHEN** the operator sets 120 words as a hard limit
- **THEN** the generated prompt states 120 words and states that the limit must never be exceeded

#### Scenario: Soft target is stated as a soft target

- **WHEN** the operator sets 150 words as a soft target
- **THEN** the generated prompt states 150 words and states that it may be slightly exceeded

#### Scenario: OpenStack in scope

- **WHEN** triage reports that the RFP requires OpenStack and the operator chooses Sunbeam
- **THEN** the OpenStack question was asked, and the generated prompt names Sunbeam as the offering and does not present Charmed OpenStack as the proposed solution

#### Scenario: OpenStack not in scope

- **WHEN** triage reports that the RFP does not require OpenStack
- **THEN** the operator is not asked to choose a flavour

#### Scenario: Questions with several answer fields

- **WHEN** triage reports that each question carries more than one field to be answered
- **THEN** the generated prompt states how the several fields of a question are to be answered

### Requirement: Every generated prompt carries the non-negotiable answer rules

The generated `prompt` SHALL carry both of the following as instructions to the answering model,
regardless of the RFP, the interview answers, or the model's own judgement:

- That answering "no context available", or stating that a technology is NOT covered, is preferable to
  giving a false or fabricated answer. This SHALL be stated as a strong instruction, not a hedge or an
  aside.
- That answers are direct, and that filler language is to be avoided.

These rules SHALL be verifiable in the emitted manifest, so that a prompt missing either is detectable
without re-reading the RFP.

The generated prompt SHALL NOT contradict the guarantees the run already enforces around it — the
fixed no-context answer emitted when retrieval returns nothing, and the source rules appended to every
custom prompt. In particular the generated prompt SHALL NOT instruct the model to answer from its own
knowledge when context is absent.

#### Scenario: Rules present regardless of RFP

- **WHEN** any manifest is generated
- **THEN** its prompt instructs that "no context available" or "not covered" is preferable to a fabricated answer, and instructs that answers be direct and free of filler

#### Scenario: Generated prompt does not license ungrounded answers

- **WHEN** a manifest is generated
- **THEN** its prompt contains no instruction to answer from the model's own knowledge when no context is available

### Requirement: A generated manifest is complete and structurally valid

A generated manifest SHALL load under the existing manifest reader without modification: it SHALL
carry a `version`, at least one question, no question with an empty question field, no two `domains`
entries with the same `match` pattern, and no `domains` entry carrying neither `context` nor
`keywords`.

Because a long RFP can drive the model's response against its output ceiling — a ceiling that on a
reasoning model covers the model's internal reasoning as well as the visible answer — an incomplete
response SHALL be an error and never a silently short manifest. Analysis SHALL treat all three of the
following as failures and SHALL report each rather than writing what it received:

- A response the provider reports as having stopped at its output ceiling.
- A response that is **empty**, which SHALL NOT be interpreted as the model having found nothing. An
  empty response at the output ceiling is the signature of reasoning having consumed the budget.
- A generated question count that falls materially short of the triage estimate, reported naming both
  the expected and the produced count.

Analysis SHALL request an explicit output ceiling rather than relying on a provider default, since a
default sized for ordinary replies truncates the answer without reducing the reasoning that displaced
it.

A response SHALL be validated against what the manifest requires rather than trusted because it
parsed, so a syntactically valid response missing a required field is a failure and not a partially
populated manifest.

A generated manifest SHALL be written only after it has been validated. When validation fails,
analysis SHALL surface the reason and SHALL NOT leave a partially written manifest in place of a
previously valid file.

The emitted manifest SHALL record that it was generated by analysis, and the model that generated it,
so a manifest's provenance is readable from the file.

#### Scenario: Generated manifest loads unmodified

- **WHEN** a generated manifest is loaded by the manifest reader
- **THEN** it loads without error and without hand-editing

#### Scenario: Truncated response

- **WHEN** the model's response is truncated against its output ceiling
- **THEN** analysis reports the truncation and does not write a manifest

#### Scenario: Empty response at the output ceiling

- **WHEN** the model returns no content and reports having stopped at its output ceiling
- **THEN** analysis reports it as a failure rather than as the model having found nothing, and does not write a manifest

#### Scenario: Response missing a required field

- **WHEN** the model returns a well-formed response that omits a field the manifest requires
- **THEN** analysis reports it as a failure rather than writing a manifest with that field absent

#### Scenario: Question count materially short of triage

- **WHEN** triage estimated roughly 150 questions and generation produced 40
- **THEN** analysis reports the discrepancy naming both counts rather than presenting the 40-question manifest as complete

#### Scenario: Provenance is recorded

- **WHEN** a manifest is generated
- **THEN** the file records that it was produced by analysis and names the model that produced it

### Requirement: Generated domains route to real questions

The `domains` block a generated manifest carries SHALL be populated, not a commented-out stub, and its
patterns SHALL be written against the question ids the same manifest actually contains.

Every generated `domains` entry SHALL carry a `context` naming the section or requirement domain it
covers. An entry MAY additionally carry `keywords`.

Domains exist primarily to attach context to a section of the RFP; attaching context to a single
question SHALL remain possible, expressed as a pattern matching that one question's id.

Generation SHALL verify each generated pattern against the generated question list, and SHALL report
any pattern matching no question. Such a pattern SHALL be reported rather than removed, since a
deliberately reserved pattern is legitimate, but it SHALL NOT pass unreported.

#### Scenario: Domains block is populated

- **WHEN** a manifest is generated from an RFP with distinct sections
- **THEN** its `domains` block is present and active, with an entry per section carrying a context, rather than a commented-out stub

#### Scenario: Pattern matching no question

- **WHEN** a generated `domains` entry matches none of the generated questions
- **THEN** that pattern is reported to the operator and the manifest still records it

#### Scenario: Context attached to one question

- **WHEN** the RFP requires extra context for a single question
- **THEN** that is expressed as a `domains` entry whose pattern matches only that question's id

### Requirement: Analysis is configured by config keys and authenticated by environment

The analysis API key SHALL be supplied by environment variable and SHALL NOT be stored in, read from,
or written to the config layer. It SHALL be read from an analysis-specific variable when set, falling
back to the variable the chat backend already uses, in that order and with no reliance on any client
library's own environment auto-detection. It SHALL NOT appear in log or verbose output, in an error
message, or in the emitted manifest.

Whether analysis is available, the model id, the analysis endpoint, and the reasoning effort used for
each model call SHALL be configuration, seeded as `package` keys so that a `user` set can override each
of them. The model id SHALL be configurable rather than compiled in, because the published identifier of
a document-capable model is versioned and changes.

The analysis endpoint SHALL default to the chat backend when its own configuration is unset, so that an
operator whose chat backend is already document-capable configures nothing further, while one whose chat
backend cannot read documents can direct analysis elsewhere.

Reasoning effort SHALL be configurable separately for the mechanical extraction call and the synthesis
call, because the appropriate floor is provider-dependent and some models do not permit reasoning to be
disabled at all.

The interview defaults — the answer length in words and whether length is enforced as a hard limit or
a soft target — SHALL also be `package` configuration, so a team can change the starting point of the
interview without changing code. The default answer length SHALL be 100 words.

When analysis is requested but no API key is present in the environment, the request SHALL fail
naming the environment variables consulted, and SHALL NOT proceed without one.

When the configured analysis backend rejects the conveyed document because it cannot accept documents at
all, the failure SHALL name the backend and say that it is not document-capable, rather than surfacing
the provider's raw rejection — this is the expected outcome of falling back to a chat backend that is a
local inference server, and it SHALL be diagnosable without reading provider documentation.

Analysis SHALL NOT silently degrade when it cannot run: it SHALL NOT fall back to answering the
questions, and it SHALL NOT quietly substitute the pattern-based extraction path for the operator's
request.

#### Scenario: Missing API key

- **WHEN** analysis is requested with no API key in the environment
- **THEN** it fails naming the environment variables consulted, and no model call is attempted

#### Scenario: Key is never disclosed

- **WHEN** analysis runs with verbose output enabled, and when analysis fails
- **THEN** the API key appears in neither the verbose output, nor the error message, nor the emitted manifest

#### Scenario: Endpoint defaults to the chat backend

- **WHEN** analysis runs with no analysis endpoint configured
- **THEN** it uses the chat backend's endpoint

#### Scenario: Backend that cannot accept documents

- **WHEN** the configured analysis backend rejects the conveyed document because it accepts no documents
- **THEN** the failure names the backend and states that it is not document-capable

#### Scenario: Model id is overridden on the user layer

- **WHEN** an operator sets the model id on the `user` layer
- **THEN** analysis calls that model instead of the packaged default

### Requirement: Analysis leaves the retrieval and answering pipeline untouched

Analysis SHALL affect only manifest production. It SHALL NOT alter retrieval, reranking, prompt
resolution, or generation for any run, and a manifest produced by analysis SHALL be run by exactly the
same pipeline as a hand-written one, with no field or behaviour available only to generated manifests.

Analysis SHALL NOT be routed through the RAG inference backend, and its use SHALL NOT change which
model answers questions during a batch run.

#### Scenario: Generated and hand-written manifests run identically

- **WHEN** a generated manifest and a hand-written manifest carrying the same fields are run
- **THEN** the runs are identical, with no behaviour conditioned on how the manifest was produced

#### Scenario: Answering model is unaffected

- **WHEN** a batch is run from a generated manifest
- **THEN** the model answering the questions is the one the run resolves as usual, not the analysis model
