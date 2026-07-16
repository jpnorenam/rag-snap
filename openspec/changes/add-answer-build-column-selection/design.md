# Design: add-answer-build-column-selection

## Context

`add-ui-answer-batch` (Change 4, merged to `main` but **not yet archived** into the specs) added
`POST /1.0/answer/build`: upload a document, extract candidate questions server-side, review them in
a wizard. For **tabular** uploads (XLSX, CSV) the daemon guesses the question column via
`questionColumn` — the header must contain the substring "question", else it falls back to column 0.
Real RFP spreadsheets rarely match, so the guess picks the wrong column or column 0 (usually an ID
column whose cells are under the 20-char floor), and `ExtractFromTable` returns nothing → the user
sees "no questions could be extracted." The CLI's `answer batch --build` avoids this by letting a
human pick the column (`huh` selects for table, column, ID column, min-length); the daemon path
replaced all of that with one substring heuristic.

The full rationale and the surveyed alternatives live in
`docs/plans/answer-build-column-selection.md`; this design is its implementation shape. UX is
governed by `docs/ux/00-foundation.md` and `docs/ux/04-answer-batch.md`.

## Goals / Non-Goals

**Goals:**
- Make spreadsheet/CSV extraction correct by letting the user choose the question column, with a
  good heuristic default — never a silent guess that runs a whole batch on the wrong column.
- Keep free-text (PDF/DOCX) extraction exactly as it is today (one pass).
- Reuse the existing `rfp` primitives; add no new external dependency.

**Non-Goals:**
- Changing the CLI `answer batch --build` flow (it stays interactive and independent).
- Free-text column selection (there is no column; PDF/DOCX use text detection).
- Reordering questions, or any change to the batch *run* or review surface beyond what feeds them.
- Persisting builds across daemon restarts (staging is in-memory with a TTL).

## Decisions

### Two-pass flow, two endpoints (not one endpoint with a mode)
Pass 1 `POST /1.0/answer/build` inspects; pass 2 `POST /1.0/answer/build/extract` extracts from a
chosen column. The passes have different inputs (binary upload vs. a column choice against an
already-parsed table) and pass 2 must not re-upload the file. A distinct endpoint keeps each
request/response clean. **Alternative considered:** one endpoint that returns tables first, then
accepts a column on a second multipart POST with the file re-sent — rejected: re-uploads the
document, re-runs Tika, and overloads one route with two response shapes.

### Discriminated pass-1 response, keyed on document kind
Free-text → `{ questions, count, refined }` (unchanged). Tabular → `{ needs_column: true,
build_token, format, tables: [{ name, page_index, header, row_count, columns: [{ index, sample,
avg_len, suggested }] }], suggested: { table_index, column_index } }`, extracting nothing. Clients
switch on `needs_column`. Advertise a new `api_extensions` entry (`answer_build_columns`) so a client
can detect two-pass support before relying on it.

### Server-side staging of parsed tables behind an opaque token
Pass 1 parses once (Tika HTML → `ParseTikaHTMLSheets` for XLSX; `ExtractFromCSV` source rows for
CSV) and stages the parsed `rfp.SheetTable`s in an in-memory map keyed by a random `build_token`,
with a short TTL (e.g. 10 min) and a small cap on retained builds (evict oldest). Pass 2 looks up by
token — no re-parse, no re-upload. Consumed on a successful extract; expired by TTL otherwise; an
unknown/expired token → 400 "re-upload the document." **Alternative considered:** re-parse on pass 2
from a re-sent file — simpler server state but re-uploads and re-runs Tika; rejected per the
no-re-upload goal (confirmed).

### Heuristic is default-only, replacing `questionColumn`
Score each column: header-synonym match (question/requirement/ask/query/description/item/control/
criteria), average cell length, question-mark fraction; penalize identifier/numeric columns (mostly
short, numeric, or `^Q?\d`). Best score → `suggested`; ties break to highest `avg_len`. It only
computes the preselected default in the pass-1 response — extraction never runs off it without an
explicit `column_index` in pass 2. This is the crux of the "wrong silent guess is worse than
failing" principle.

### CSV uses the same two-pass machinery
CSV shares the exact bug (also routes through `questionColumn`). `ExtractFromCSV` already reads a
chosen column; pass 1 parses the CSV into a single-table shape (synthesizing `Column N` headers when
row 0 isn't a header) and stages it like a one-sheet workbook. No Tika for CSV.

### IDs auto-numbered by default
Pass 2 carries `id_column_index` (default -1 = auto-number). The wizard does not expose an ID-column
control initially (confirmed); the field exists in the API for future use and CLI parity.

### Wizard state machine and phase-appropriate cancel
`BuildWizard` becomes: `upload → inspecting → [tabular] columns → extracting → review → configure`;
free-text goes `upload → inspecting → review`. Distinct user-facing copy per phase ("Reading the
spreadsheet…" vs "Extracting questions from column *Requirement*…"). New in-memory state:
`buildToken`, `tables`, `selectedTable`, `selectedColumn`, `minLength` (default 20). The cancel
confirmation while a build op is in flight (inspect or extract) SHALL NOT reuse the batch-run
"progress will be lost" copy — nothing meaningful is lost — e.g. "Stop extracting questions? You can
upload again." This generalizes the existing `cancelExtraction` handler to name the active build op
and use phase-appropriate wording.

### Operations lifecycle
Tabular builds now produce two short-lived build operations (inspect, extract) before any batch run.
Neither is a batch run: `isBatchRun` must continue to NOT match them (they lack
`questions_total`/`results`), so the Answer screen never tries to resume a build op. The wizard
dismisses the inspect op from the indicator when its tables land in the column step, and the extract
op when its questions land in review — neither lingers as "completed-unconsumed."

## Risks / Trade-offs

- **[Delta-on-a-delta ordering]** This change MODIFIES requirements that Change 4 (`add-ui-answer-batch`)
  ADDs and that are not yet in the main spec. → **Mitigation:** Change 4 must be synced/archived
  before this change archives; the spec deltas carry a header comment stating the dependency, and the
  MODIFIED headers match Change 4's exact text so the base resolves.
- **[Stale build tokens / memory]** Staged tables held in memory could accumulate. → **Mitigation:**
  short TTL + retained-build cap with oldest-eviction; consumed on successful extract; parsed rows
  are small relative to the uploaded file.
- **[Heuristic still wrong sometimes]** The default column may be wrong. → **Mitigation:** by design
  the user always sees and can change it before extraction, and can re-extract without a batch run;
  being wrong is cheap now.
- **[Headerless sheets]** Row 0 may be data, not a header. → **Mitigation:** synthesize `Column N`
  labels and show per-column sample cells so the user can choose correctly regardless; document the
  row-0-as-header assumption in the column step copy.
- **Config / secrets / snap:** none added. No snapctl keys, no new secrets (reuses `CHAT_API_KEY` +
  `tika.http.*`), no snap interface/plug/bundled-binary/hook changes.

## Migration Plan

Additive on the API (new endpoint) plus a response-shape change for tabular builds gated behind the
`answer_build_columns` extension. Ship the daemon and wizard together. Rollback = revert
`internal/api` (drop `/extract`, restore single-pass `handleAnswerBuild` + `questionColumn`) and the
`BuildWizard` changes. No data/config migration. Because Change 4 is still pre-archive, land this on
top of it; sequence the archives Change 4 → this change so the specs compose.

## Open Questions

- TTL and retained-build cap values (start 10 min / small cap; tune if needed).
- Whether to surface the ID-column control later (out of scope now; API carries the field).
