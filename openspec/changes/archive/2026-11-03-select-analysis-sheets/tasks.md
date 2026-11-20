## 1. Scope model and resolution

- [x] 1.1 Add `analysis.SheetScope`, `ScopedSheet` and `SheetInfo` types, and `ResolveScope` with every rule from the spec: all-`answer` default, unnamed-means-excluded once any sheet is named, at least one `answer`, case-insensitive trimmed names, 1-based positions, and an unknown sheet failing with an error that lists the sheets.
- [x] 1.2 Add a parser for the CLI list syntax (names, positions, `a-b` ranges, comma-separated). It rejects a sheet named in both `--sheets` and `--context-sheets`.
- [x] 1.3 Unit-test resolution and parsing. Cover the telco workbook's tab layout, including a name truncated at 31 characters and an `&` decoded from `&amp;`.

## 2. Conveyance

- [x] 2.1 Keep `[]rfp.SheetTable` on a converted spreadsheet `Document`, and render `Text` from the sheets that are not excluded, with the context-only heading marker.
- [x] 2.2 Thread the scope through `Converter.Convey`/`ConveyAll` for the primary document only. A scope on a CSV, PDF or DOCX primary document is rejected with the spec's message.
- [x] 2.3 Add `Converter.Inventory(path)`, sharing the Tika conversion with `conveyTabular`.
- [x] 2.4 Extend `DocumentReport` with the per-sheet list and the scoped `Detail` text. Size checks measure the scoped text.
- [x] 2.5 Tests: an excluded sheet is absent from the conveyed text, every row and column of kept sheets is present, and an unscoped conveyance is byte-identical to before.

## 3. Model calls and provenance

- [x] 3.1 When a context-only sheet is present, add the context-only sentence to the triage and extraction instructions, and the "may inform the prompt" sentence to the generation call.
- [x] 3.2 Make `attributeSections` report questions whose `source` names a context-only sheet as their own line.
- [x] 3.3 Add the resolved scope to `Provenance`, and write header lines only for non-default scopes.
- [x] 3.4 Tests for 3.1–3.3, including the absence of the extra sentences and header lines when unscoped.

## 4. CLI

- [x] 4.1 Add `--sheets` and `--context-sheets` to `answer` in `cmd/cli/basic/answer.go`, valid only with `--build <document> --analyze`. They are rejected otherwise, as `--instructions` is.
- [x] 4.2 Resolve the scope before any model call, and print the per-sheet scope in the conveyance report.
- [x] 4.3 Update the `answer` command's `--help` text, `docs/usage.md`, and `apps/completion.bash` if it lists `answer` flags.

## 5. REST API

- [x] 5.1 Add `POST /1.0/answer/analyze/sheets` (synchronous, no model call, no key needed, same size bound) in `internal/api/handlers_answer_analysis.go`, and register it in `server.go`.
- [x] 5.2 Accept the `sheets` JSON field on `POST /1.0/answer/analyze`, validate it with 400s before conveyance, stage the resolved scope with the token, and return it in the triage metadata.
- [x] 5.3 Make generation against a token reuse the staged scope.
- [x] 5.4 Handler tests for inventory (xlsx, csv, pdf, unsupported type), invalid scope, scoped triage metadata, and generation reuse.
- [x] 5.5 Update `rest-api.yaml` and `docs/rest-api.md`.

## 6. UI

- [x] 6.1 Add the inventory call and the `sheets` field to `ui/lib/api` and `ui/lib/types.ts`.
- [x] 6.2 Build the sheet-scope table with a per-sheet three-way control on the analysis upload step, for spreadsheet RFPs only. Block start when nothing is set to Answer, and handle the inventory loading and error states, falling back to unscoped.
- [x] 6.3 Show the active scope on the triage step beside the instructions.
- [x] 6.4 Component tests: scoping a workbook, nothing left to answer, non-spreadsheet RFP, inventory failure.
- [ ] 6.5 Verify compliance with the `ui-conventions` skill: both themes, a keyboard-only pass, `--vf-*` tokens only, and all four view states.
- [x] 6.6 Update `docs/local-ui.md`.

## 7. Verification

- [x] 7.1 Run `make all` and the UI test suite.
- [x] 7.2 Build and install the snap, then analyse the telco workbook with tabs 2–7 answered, "Read Me" as context and App B excluded, from both the CLI and the UI. Confirm the triage estimate and the extraction cover only tabs 2–7 and that the provenance records the scope.
