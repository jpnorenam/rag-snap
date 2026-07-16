# Tasks: add-answer-build-column-selection

> Sequencing: `add-ui-answer-batch` (Change 4) must be synced/archived before this change is
> archived, since these deltas modify requirements it introduces.

## 1. Daemon: pass 1 (inspect) — discriminated build response

- [x] 1.1 Split `handleAnswerBuild` (`internal/api/handlers_answer_build.go`) by document kind: PDF/DOCX keep the current one-pass extract; XLSX/CSV parse into tables and return a discriminated response instead of extracting.
- [x] 1.2 For XLSX, parse via Tika HTML → `rfp.ParseTikaHTMLSheets` (+ `rfp.XLSXSheetNames` for names) into `[]rfp.SheetTable`; for CSV, read rows into a single synthesized `SheetTable` (synthesize `Column N` headers when row 0 is not a header).
- [x] 1.3 Build the pass-1 metadata: `needs_column: true`, `build_token`, `format`, `tables` (each `{name, page_index, header, row_count, columns:[{index, sample, avg_len, suggested}]}`) and top-level `suggested {table_index, column_index}`. Cap `sample` to a few short cells.
- [x] 1.4 Do NOT extract questions or run a batch on pass 1 for tabular formats.

## 2. Daemon: heuristic (default column only)

- [x] 2.1 Replace `questionColumn` with a column-scoring function: header-synonym match (question/requirement/ask/query/description/item/control/criteria), average cell length, question-mark fraction; penalize identifier/numeric columns; tie-break to highest average length.
- [x] 2.2 Use it only to compute `suggested` in the pass-1 response — never to run extraction without an explicit column choice. Keep it available for CSV and XLSX alike.

## 3. Daemon: build-token staging

- [x] 3.1 Add an in-memory store keyed by opaque `build_token` holding the parsed `[]rfp.SheetTable` and `format`, with a TTL (start 10 min) and a retained-build cap (evict oldest). Generate the token in pass 1.
- [x] 3.2 Consume the token on a successful extract; expire by TTL otherwise. Guard against unbounded growth.

## 4. Daemon: pass 2 (extract) — `POST /1.0/answer/build/extract`

- [x] 4.1 Add `handleAnswerBuildExtract`: JSON body `{build_token, table_index, column_index, id_column_index (default -1), min_length (default 20), refine}`; run as an async operation.
- [x] 4.2 Look up the staged tables by token (unknown/expired → 400 "re-upload the document"); validate `table_index`/`column_index` in range (else 400).
- [x] 4.3 Extract via `rfp.ExtractFromTable(rows, column_index, id_column_index, min_length)`; auto-number IDs when `id_column_index` is -1; assign `source` from the table name.
- [x] 4.4 Apply optional LLM refinement (same path as free-text build) when `refine` is set and inference is configured; publish `{questions, count, refined}` — the same shape free-text build publishes.
- [x] 4.5 Zero questions after the min-length filter → fail the operation with a message naming the column (so the client retries a different column/min-length, no batch run).

## 5. Daemon: wiring

- [x] 5.1 Register `POST /1.0/answer/build/extract` in `registerAPI` (`internal/api/server.go`), grouped with the other answer routes; add `answer_build_columns` to `api_extensions`.
- [x] 5.2 Update swagger annotations for both `POST /1.0/answer/build` (discriminated response) and `POST /1.0/answer/build/extract`; regenerate `rest-api.yaml` (`make spec`) and confirm `make spec-check` is in sync.

## 6. UI: API client

- [x] 6.1 In `ui/lib/api/answer.ts`, type the discriminated pass-1 result (free-text `questions` vs. tabular `needs_column`/`build_token`/`tables`/`suggested`) and add a `buildExtract({build_token, table_index, column_index, min_length, refine})` verb over `postAsync("/1.0/answer/build/extract", …)`.

## 7. UI: wizard state machine + column step

- [x] 7.1 Extend `BuildWizard.tsx` steps to `upload → inspecting → [tabular] columns → extracting → review → configure`; free-text goes `upload → inspecting → review` (skips column + second extracting). Use distinct per-phase copy.
- [x] 7.2 On pass-1 completion: free-text → populate review; tabular (`needs_column`) → store `buildToken`/`tables`/`suggested` and enter the `columns` step.
- [x] 7.3 Build the `columns` step: table selector shown only when >1 table; per-column options showing sample cells with the suggested column preselected; a minimum-length number input defaulting to 20 with helper text. Continue runs `buildExtract` as a tracked op and enters the second `extracting` step.
- [x] 7.4 Make a wrong choice recoverable: from review (or an empty-column failure) the user can return to `columns`, pick a different column/min-length, and re-extract — without starting a batch run.
- [x] 7.5 Phase-appropriate cancel: while inspecting or extracting, the in-place cancel confirm SHALL NOT use the batch-run "progress will be lost" wording; use copy reflecting that nothing meaningful is lost. Generalize the existing cancel handler to the active build op.
- [x] 7.6 Dismiss the inspect op from the indicator when its tables are consumed into the column step, and the extract op when its questions land in review (neither should linger).

## 8. UI: styles

- [x] 8.1 Add column-step styles under the `// --- answer ---` group in `ui/app/globals.scss` (column option list, sample-cell preview, min-length control); sample cells truncate without horizontal page scroll at 620px; `--vf-*` tokens only.

## 9. Definition of done (UX)

- [x] 9.1 Spreadsheet/CSV upload shows a column-selection step before extraction; the heuristic's best guess is preselected and every column is choosable with a sample preview.
- [x] 9.2 Min-length control present (default 20); lowering it re-runs extraction (pass 2) without starting a batch run.
- [x] 9.3 A wrong column is recoverable (change column/min-length and re-extract); a batch run never starts from a guessed column.
- [x] 9.4 PDF/DOCX uploads skip the column step entirely (no regression to the one-pass flow).
- [x] 9.5 Multi-sheet workbooks let the user pick the table; single-table files skip the table selector.
- [x] 9.6 Column step is keyboard-navigable and correct in light and dark; no horizontal page scroll at 620px; all colors via `--vf-*` tokens.
- [x] 9.7 Cancelling an inspect/extract phase uses phase-appropriate copy (not the batch-run warning).

## 10. Docs and verification

- [x] 10.1 Document `POST /1.0/answer/build` (discriminated response) and `POST /1.0/answer/build/extract` in `docs/rest-api.md`; ensure `rest-api.yaml` regenerated.
- [x] 10.2 Update `docs/local-ui.md` Answer section with the spreadsheet column-selection step.
- [x] 10.3 Add daemon tests: XLSX pass-1 returns tables + suggested column (no extraction); pass-2 extracts from a chosen column; unknown/expired token → 400; empty-column extract fails with a column-naming message. Extend `ui/lib` tests if the API client gains parseable logic.
- [x] 10.4 `cd ui && npm run build` (static export) then `make all` at repo root (tidy fmt vet lint test build); zero new lint issues in touched Go files.
- [ ] 10.5 In-snap validation (deferred — needs snapcraft build + `snap install --dangerous` + running Tika/OpenSearch/inference): upload a real RFP XLSX to `/answer/`, confirm the column step appears with a sensible default, pick the column, review, download a manifest the CLI accepts, and run a batch; confirm PDF still skips the step. Partial coverage landed instead: `internal/api` tests drive the two-pass flow (CSV inspect→extract with the heuristic preferring the prose column over the ID column, unknown-token 400, empty-column failure); `tsc`/build/`make all` (minus the pre-existing lint baseline) pass with zero new lint issues in touched files.
