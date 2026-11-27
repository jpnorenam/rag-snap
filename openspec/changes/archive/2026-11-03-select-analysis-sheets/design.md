## Context

Analysis conveys a spreadsheet by converting it through Tika into `[]rfp.SheetTable` and rendering every table as Markdown under a `## Sheet: <name>` heading (`conveyTabular` in `cmd/cli/basic/analysis/convey.go`). The rendered text is the only thing kept: `Document.Text` holds it, and the API's analysis store stages that text with the triage under a token.

Today scope can only be narrowed with operator instructions (`operatorSection`), which reach triage and extraction as system-instruction text. On a telco RFP workbook (eight tabs: Read Me, 1–6, App B), an instruction to take tabs 1–6 was not followed. Why it is fragile:

- The base guidance says to extract every question, and the shortfall check treats a smaller extraction as an early stop.
- Tab names and id prefixes diverge. Tab 6 holds `MIG-16`…`MIG-23`, while tab 4 holds `MIG-01`…`MIG-14`.
- Tab names are truncated at 31 characters.

The sheets are already separate, named tables in memory before any model call. Selecting among them is a filter, not a judgement.

## Goals / Non-Goals

**Goals:**
- An operator states the sheets to answer, the sheets that are background only, and the sheets to drop. The two analysis calls then see exactly that.
- Triage and extraction see the same scoped document, so the shortfall check stays meaningful.
- The scope is visible in the conveyance report, the triage step and the manifest provenance.
- Today's behaviour is unchanged when no scope is given.

**Non-Goals:**
- Model-suggested scopes: pre-selecting sheets from triage or name heuristics such as "Read Me" or "Appendix". This could be a later addition on top of the same control.
- Sheet scope for context documents. The operator can trim those beforehand, and a three-state control per context file adds UI weight for little gain.
- Row- or column-level scope inside a sheet.
- The non-analysis extraction path (`answer --build` without `--analyze`). It could reuse the same filter later, but its column-detection flow is different.
- Scoping PDF and DOCX sections. Instructions remain the mechanism for those.

## Decisions

### Filter the sheet tables at conveyance, before rendering

The scope is known when the documents are conveyed: it arrives with the upload, or with the CLI flags. So `conveyTabular` resolves it against the sheet inventory, drops the tables of excluded sheets, relabels the headings of context-only sheets (`## Sheet: Read Me (context only — background, no questions to extract)`), and only then renders the Markdown. `Document.Text` stays the single staged form, and the API store needs no change: the generation phase reuses the staged, already-scoped conveyance. Size checks and `TotalBytes` measure the scoped text, so excluding a large sheet can bring an over-limit workbook under the bound.

Sheets map to workbook tabs through `rfp.XLSXSheetNames`. One tab can yield several Tika tables, so tables are grouped by name, and positions follow the tab list, so a tab Tika emitted nothing for keeps its number. When the names cannot be read or do not account for every table, each table is its own sheet, in conversion order.

*Alternative considered:* keep the tables on `Document` and render at send time. This was rejected because no call needs a different scope from the one conveyed, so keeping the tables would only add state to the store.

*Alternative considered:* filter the rendered Markdown by splitting on `## Sheet:` headings. This was rejected because it is fragile when a cell contains that string.

### Resolve the scope in one place, shared by CLI and daemon

A new `analysis.SheetScope` type holds the requested scope (name or position → value), and `ResolveScope(sheets []rfp.SheetTable, req SheetScope) ([]ScopedSheet, error)` applies it. Every validation rule in the spec lives there:

- default all `answer`;
- named-means-others-excluded;
- at least one `answer`;
- case-insensitive name matching;
- 1-based positions;
- unknown sheet → error listing the sheets.

The CLI parses `--sheets`/`--context-sheets`, including ranges, into a `SheetScope`. The API parses the `sheets` JSON field into the same type. Both surfaces therefore give identical errors, which follows the precedent of `ValidateInstructions`.

Positions follow workbook order as Tika emits the sheets (`SheetTable.PageIndex`), which matches tab order in Excel. Empty sheets keep their position, so the numbers the operator sees match the tabs.

### The conversion produces the inventory; one conversion per analysis

`Converter.Convey` gains a scope parameter (the zero value means none). A new `Converter.Inventory(path)` runs the same Tika conversion and returns `[]SheetInfo{Position, Name, DataRows}` for the inventory endpoint; the CLI's unknown-sheet error lists the same inventory from inside conveyance.

**API flow.** The UI calls `POST /1.0/answer/analyze/sheets` when the RFP file is chosen, then uploads again to `POST /1.0/answer/analyze` with the `sheets` field. The workbook is converted twice, once for the inventory and once for triage.

*Alternative considered:* an inventory call that stages the converted tables under a token, which triage then references instead of a re-upload. It saves one Tika pass, but it adds a second token lifecycle, expiry and store accounting for a conversion that takes about a second on a typical RFP workbook. The upload is loopback-local. This alternative was rejected; revisit it if large workbooks make the double conversion noticeable.

### Tell the model what context-only means, and keep counting honest

The triage and extraction instructions gain one sentence each, added only when a context-only sheet is present: sheets marked context only are background, and no questions are counted or extracted from them. The generation (prompt/domains) call is told those sheets may inform the prompt and contexts.

This sentence reaches the model as instruction text beside the documents, not as operator text. The heading marker is in the document content, but rag-cli wrote it, so the document-is-untrusted rule is unaffected: the marker only narrows what is extracted.

### Report and provenance

The `DocumentReport` for a scoped spreadsheet gains `sheets: [{position, name, data_rows, scope}]`. Its `Detail` names the counts, for example "converted to Markdown tables: 6 of 8 sheet(s) answered, 1 context only, 1 excluded; 147 data row(s)". `Provenance` gains the resolved scope and renders one header line per non-default sheet next to the instruction lines. A scope that answers every sheet is written as no line, so unscoped manifests are byte-identical to today.

### UI control

The upload step, on the analysis path with a spreadsheet RFP, renders a table: position, sheet name, data rows, and a three-option segmented control (Answer / Context only / Exclude). It follows the `ui-conventions` skill: Vanilla `p-table` and form patterns, `--vf-*` tokens, both themes, keyboard operable, and loading, empty and error states for the inventory fetch. The control lives with the existing instructions field, and the triage step echoes the scope beside the instructions.

### Configuration, secrets, snap

No new config keys, secrets, snap interfaces or hook changes. The inventory endpoint uses the existing `tika.http.*` keys through the snapctl-backed config, as analysis conversion already does. It needs no `GEMINI_API_KEY`.

## Risks / Trade-offs

- [The model extracts a question from a context-only sheet anyway] → Mitigation: the heading marker plus the instruction sentence. Questions whose `source` names a context-only sheet are counted on their own "(from a context-only sheet)" line in the per-section attribution. That breakdown is shown only on a question-count shortfall, so a slip without a shortfall shows up in manifest review, not as a warning.
- [Tika's sheet names differ from Excel's tab labels, for example when an HTML entity is decoded differently] → Mitigation: names are matched case-insensitively after trimming, positions are always accepted, and the unknown-sheet error lists exactly the names Tika produced.
- [Excluding a sheet hides a question the operator did not know was there] → Mitigation: the report lists every sheet with its row count, and the exclusion is recorded in provenance. This is an explicit operator choice, which the spec requires.
- [Double Tika conversion in the UI flow] → Accepted: about a second on typical workbooks. See the alternative above.

## Migration Plan

Additive only. Clients and commands that send no scope behave as before. Rollback means reverting the change, and there is no stored state to migrate.
