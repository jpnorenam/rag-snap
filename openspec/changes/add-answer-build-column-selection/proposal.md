# Proposal: add-answer-build-column-selection

## Why

`POST /1.0/answer/build` extracts questions from spreadsheet/CSV uploads by *guessing* which column
holds the questions (`questionColumn`: the header must contain the substring "question", otherwise
it falls back to column 0). Real RFP spreadsheets rarely satisfy that — the column is headed
"Requirement", "Ask", "Description", or the sheet has no header row — so the guess picks the wrong
column or column 0 (typically an ID column whose cells fall under the 20-char floor), and extraction
returns "no questions could be extracted." The CLI's `answer batch --build` never hits this because a
human picks the column interactively; the daemon path replaced that selection with a fragile
heuristic.

A silent wrong guess is worse than failing: it can extract garbage and only reveal the mistake after
a full, wasted batch run (LLM generation + retrieval per question). The fix is to **expose the column
choice in the build wizard**, with an improved heuristic serving only as the preselected default —
never a "trust the guess and run" path. See `docs/plans/answer-build-column-selection.md` for the
full design rationale this proposal is drawn from.

## What Changes

- **BREAKING (response shape) for spreadsheet/CSV builds:** `POST /1.0/answer/build` becomes the
  first pass of a two-pass flow for tabular formats. Instead of extracting immediately, a tabular
  upload's operation completes with a discriminated response — `needs_column: true`, an opaque
  `build_token`, and the parsed `tables` (each with its header row and, per column, sample cells and
  an average length) plus a heuristic `suggested` table/column. It extracts no questions on this
  pass. Free-text formats (PDF, DOCX) are unchanged: they still complete with `{ questions, … }` in
  one pass.
- **New endpoint `POST /1.0/answer/build/extract`** (async): accepts `{ build_token, table_index,
  column_index, id_column_index, min_length, refine }` and extracts questions from the chosen column
  of the staged, already-parsed table — no re-upload, no re-parse. Its completion metadata is the
  same `{ questions, count, refined }` shape the free-text build returns, so the client consumes one
  results shape regardless of path.
- **Server-side staging of parsed tables** behind the `build_token` (short TTL, small retained cap,
  consumed on a successful extract), mirroring the existing upload temp-file staging. An
  unknown/expired token yields a 400 telling the client to re-upload.
- **Improved default heuristic** (replaces `questionColumn`): scores each column by header synonyms
  (question / requirement / ask / query / description / item / control / criteria), average cell
  length, and question-mark fraction, penalizing ID/numeric columns; ties break to the longest-text
  column. Used only to compute `suggested` — it never runs extraction on its own. CSV is in scope
  (it shares the same bug and the same two-pass machinery); ID columns are auto-numbered by default.
- **Build wizard (`ui/`) gains a column-selection step** for tabular uploads: table selector
  (multi-sheet only), per-column sample preview with the suggested column preselected, and a
  min-length control defaulting to 20. Continue runs pass 2. A wrong column or min-length is
  recoverable by re-running pass 2 alone — a batch run never starts from a guessed column. PDF/DOCX
  skip the step entirely.
- **Phase-appropriate cancel copy:** cancelling an inspect or extract build operation uses wording
  that reflects that nothing meaningful is lost — not the "Progress will be lost" batch-run warning.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `rest-api-answer`: MODIFY the document-extraction requirement so `POST /1.0/answer/build` returns a
  discriminated response — free-text formats extract in one pass (unchanged); spreadsheet/CSV formats
  return the parsed tables (headers, per-column samples, suggested column) and a staged `build_token`
  without extracting. ADD a requirement for `POST /1.0/answer/build/extract` that extracts questions
  from a chosen column against a staged token and returns the same results shape.
- `local-ui-app`: MODIFY the build-from-document wizard requirement to insert a column-selection step
  for tabular uploads (table + column + min-length, heuristic default preselected, wrong choice
  recoverable without a batch run) that free-text uploads skip, and to make in-flight build
  cancellation copy phase-appropriate.

## Impact

- **Code**:
  - Daemon (`internal/api/`): rework `handleAnswerBuild` into pass 1 (discriminated response +
    table staging) and add `handleAnswerBuildExtract` (pass 2); replace `questionColumn` with the
    scoring heuristic; add a build-token stage (parsed `rfp.SheetTable`s + TTL/cleanup). Reuses the
    existing `rfp` extraction primitives (`ParseTikaHTMLSheets`, `ExtractFromTable`,
    `XLSXSheetNames`, `ExtractFromCSV`).
  - UI (`ui/`): `BuildWizard.tsx` state machine gains `inspecting` → `columns` → `extracting`
    (tabular) vs. straight-to-`review` (free-text); new `ui/lib/api/answer.ts` verb for the extract
    call; phase-aware cancel-confirm copy.
- **APIs**: `POST /1.0/answer/build` response shape changes for tabular; new `POST
  /1.0/answer/build/extract`. Advertise a new `api_extensions` entry (e.g. `answer_build_columns`).
- **External services**: OpenSearch and the inference server via the eventual batch run; **Tika**
  for XLSX parsing (already a dependency of this capability — unchanged). CSV needs no Tika.
- **Config**: no new config keys (`package` or `user`). The build path reads the existing
  `tika.http.*` keys via the client cache.
- **Secrets**: none added. Refinement uses the daemon's configured `CHAT_API_KEY`; extraction needs
  no secret.
- **Snap**: no `snapcraft.yaml` changes — no new interfaces, plugs, bundled binaries, or hooks.
- **User-facing surface**: a changed build response, a new REST endpoint, and a new wizard step. No
  CLI command/flag/slash-command changes (the CLI keeps its own interactive `--build`). Documentation
  to update: `docs/rest-api.md` + `rest-api.yaml` (both endpoints), `docs/local-ui.md` (Answer
  wizard column step).
- **UX guidelines**: governed by `docs/ux/00-foundation.md` and `docs/ux/04-answer-batch.md`; the
  design links them and tasks carry the foundation Definition of done plus the column-step additions.
- **Dependencies**: none added.
