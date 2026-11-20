## MODIFIED Requirements

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

Every row and column of a converted table SHALL be conveyed, for every sheet the operator's sheet scope
does not exclude. The conversion SHALL NOT select a single question column, drop columns judged
uninteresting, or truncate rows, since the discarded columns are the material the RFP-level prompt and
the domain contexts are built from. Leaving out a whole sheet SHALL happen only by the operator's
explicit sheet scope, never by conversion's own judgement.

The number of documents conveyed, and per-document the format detected and the conveyance path taken,
SHALL be reported to the operator, so a document silently converted rather than read natively is
visible before the manifest is generated.

#### Scenario: Spreadsheet with context columns beside the requirement

- **WHEN** a spreadsheet whose sheets carry a requirement column plus description, current-environment and evaluator-guidance columns is analysed
- **THEN** the conveyed representation retains every column with its heading, retains the sheet name for each table, and no column is dropped

#### Scenario: Excluded sheet is not conveyed but kept sheets are whole

- **WHEN** a spreadsheet is analysed with one sheet excluded by the operator's sheet scope
- **THEN** no row of the excluded sheet is conveyed
- **AND** every row and column of every other sheet is conveyed

#### Scenario: PDF conveyance

- **WHEN** a PDF is analysed
- **THEN** the original bytes are conveyed rather than an extracted text transcription

#### Scenario: Format without native document understanding

- **WHEN** a word-processor document is analysed
- **THEN** it is converted to a table-preserving textual representation rather than uploaded as original bytes, and the operator is told the document was converted

#### Scenario: Document exceeding the model's document limits

- **WHEN** a document exceeds the model's documented size or page limit for its conveyance path
- **THEN** analysis fails before any model call with a message naming the document, the limit, and the measured value

## ADDED Requirements

### Requirement: Spreadsheet sheets can be scoped before analysis

Analysis SHALL accept an optional sheet scope for a spreadsheet RFP, setting each sheet of the primary
document to one of three values: `answer`, `context`, or `exclude`.

- `answer`: the sheet SHALL be conveyed in full, and questions SHALL be extracted and counted from it.
- `context`: the sheet SHALL be conveyed in full and labelled in the conveyed text as background with no
  questions to extract. The triage and extraction requests SHALL say that context-only sheets are not
  counted or extracted. A context-only sheet MAY inform the generated prompt and domain contexts.
- `exclude`: the sheet SHALL NOT be conveyed in any request.

With no scope given, every sheet SHALL be `answer`, which is the behaviour without this requirement.
Once a scope names any sheet, every sheet it does not name SHALL be `exclude`. A scope that leaves no
sheet as `answer` SHALL be rejected before any model call.

The scope SHALL be applied before conveyance, so that triage and generation receive the same scoped
document and the question-count comparison measures the scoped document only. The scope SHALL be kept
with the analysis session, so generation uses the scope triage did.

Sheets SHALL be identified by name or by 1-based position in workbook order. Names SHALL match
ignoring case and surrounding whitespace. A name or position that matches no sheet SHALL be rejected
before any model call, with an error that lists the available sheets with their positions.

The scope SHALL apply only to a spreadsheet primary document. A scope given for a CSV, PDF or DOCX
primary document SHALL be rejected with an error saying that sheet scope applies to spreadsheets, and
pointing to the operator instructions for scoping other formats. Context documents SHALL be conveyed
whole.

The conveyance report for a scoped spreadsheet SHALL list every sheet with its position, name,
data-row count and scope. The scope SHALL be recorded in the generated manifest's provenance beside
the operator instructions.

The CLI SHALL take the scope as `--sheets` (sheets to answer) and `--context-sheets` (sheets to convey
as context only). Each SHALL accept a comma-separated list of names, positions and position ranges
such as `2-7`, and SHALL apply only with `--build <document> --analyze`. A sheet named in both flags
SHALL be rejected.

#### Scenario: Answering a subset of tabs

- **WHEN** a workbook with tabs "Read Me", "1 Hardware" … "6 Delivery capability and re" and "App B Sizing & BoQ" is analysed with `--sheets 2-7`
- **THEN** triage and extraction receive only tabs 2 to 7
- **AND** the triage estimate and the extracted questions come only from those tabs

#### Scenario: Context-only sheet

- **WHEN** the same workbook is analysed with `--sheets 2-7 --context-sheets "Read Me,8"`
- **THEN** "Read Me" and "App B Sizing & BoQ" are conveyed, labelled as context only
- **AND** no question is extracted from them, and neither adds to the triage estimate

#### Scenario: Unknown sheet

- **WHEN** analysis is run with `--sheets "7 Pricing"` and the workbook has no such sheet
- **THEN** analysis fails before any model call, listing every sheet with its position

#### Scenario: Scope with nothing to answer

- **WHEN** analysis is run with `--context-sheets 1` and no `--sheets`
- **THEN** analysis fails before any model call, saying that at least one sheet must be answered

#### Scenario: Scope on a PDF

- **WHEN** a PDF is analysed with `--sheets 2`
- **THEN** analysis fails, saying that sheet scope applies to spreadsheets and that instructions scope other formats

#### Scenario: No scope keeps today's behaviour

- **WHEN** a workbook is analysed with no sheet scope
- **THEN** every sheet is conveyed and answered, as before

#### Scenario: Scope is recorded

- **WHEN** a manifest is generated from a scoped workbook
- **THEN** its provenance header records each sheet's scope beside the operator instructions
