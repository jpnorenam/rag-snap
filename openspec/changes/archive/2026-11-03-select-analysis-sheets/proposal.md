## Why

RFP workbooks usually hold more tabs than the ones to answer: a read-me, a cover sheet, sizing appendices, bills of quantities, pricing. Today the only way to narrow analysis to some tabs is a free-text operator instruction, and the model does not follow it reliably. The instruction works against the base guidance to extract every question and against the triage-versus-extraction count check. Tab names and question ids also do not line up: in a telco RFP workbook, tab "6 Delivery capability and re" reuses `MIG-` ids, and Excel cuts tab names at 31 characters. Spreadsheets already arrive as separate, named sheet tables before anything is sent to Gemini, so the scope can be applied exactly, in code.

## What Changes

- The operator can scope each sheet of a spreadsheet RFP (the primary document) before analysis. Each sheet is set to **answer** (questions are extracted from it), **context only** (conveyed as labelled background with no questions taken from it), or **exclude** (not conveyed at all). With no scope given, every sheet is answered, as today.
- The scope is applied before conveyance, so triage and extraction see the same document. The question-count check therefore measures the selected scope, excluded sheets cost no tokens, and the scope is reproducible.
- The conveyance report lists every sheet with its data-row count and its scope. The scope is recorded in the generated manifest's provenance beside the operator instructions.
- **CLI**: `rag-cli.rag answer --build <xlsx> --analyze` gains `--sheets` (sheets to answer) and `--context-sheets` (sheets to convey as context only). Each takes a comma-separated list of sheet names or 1-based positions, with ranges such as `2-7`. Unlisted sheets are excluded once either flag is given. An unknown name fails before any model call and lists the sheets available.
- **REST API**: a new synchronous endpoint returns a spreadsheet's sheet inventory (names, positions, data-row counts) with no model call and no API key needed. The analysis triage endpoint accepts an optional `sheets` scope field.
- **UI**: when the RFP uploaded on the analysis path is a spreadsheet, the build wizard's upload step lists its sheets with a per-sheet scope selector before analysis starts. The triage step shows the scope the session works to.
- The free-text instructions field stays as it is. It remains the way to scope PDF and DOCX documents, which have no sheets.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `answer-manifest-analysis`: conveyance sends every row of every *selected* sheet, not of every sheet. A new requirement covers the sheet scope: its three settings, how it applies to triage and extraction, how it is reported and recorded, and the CLI flags.
- `rest-api-answer`: a new sheet-inventory endpoint, and a `sheets` scope field on the analysis triage request.
- `local-ui-app`: the build wizard offers a sheet scope for spreadsheet RFPs and shows the active scope on the triage step.

## Impact

- **External services**: **Tika** is touched. The inventory endpoint converts the workbook through Tika to list its sheets, the same conversion analysis already does. The Gemini analysis calls receive less, or differently labelled, content. **OpenSearch** and the **chat inference server** are not touched.
- **Code**: `cmd/cli/basic/analysis/convey.go` (scope-aware tabular conveyance, report), `cmd/cli/basic/analysis/triage.go` and `generate.go` (labelling of context-only sheets), the provenance writer, `cmd/cli/basic/answer.go` (flags), `internal/api/handlers_answer_analysis.go` and `server.go` (inventory endpoint, `sheets` field), `ui/components/answer/` build wizard, and `ui/lib/types.ts` and `ui/lib/api`.
- **Config keys**: none.
- **User-facing surfaces**: two new CLI flags, a new REST endpoint and request field, and a new wizard control. Documentation to update: `docs/usage.md`, `docs/rest-api.md`, `rest-api.yaml`, `docs/local-ui.md`, the `answer` command's `--help` text, and `apps/completion.bash` if it lists `answer` flags.
- **Compatibility**: requests and commands without a scope behave exactly as today.
