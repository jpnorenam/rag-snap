# Plan: column selection for spreadsheet/CSV question extraction

**Status:** plan only — no code yet.
**Problem:** `/1.0/answer/build` extracts tabular questions (XLSX, CSV) from a single column the
daemon *guesses* via `questionColumn` (header must contain the substring "question", else falls
back to column 0). A wrong guess silently extracts garbage — or, when column 0 is an ID column
whose cells are under the 20-char floor, nothing — and the user finds out only after a full,
wasted batch run. The CLI avoids this by letting a human pick the column interactively; the daemon
path replaced that with a fragile heuristic.

## Guiding principle

**The real fix is exposing column choice in the wizard.** A silent guess that runs a whole batch
on the wrong column is worse than surfacing the choice, because a wrong guess costs a full run
(LLM + retrieval per question) before the error is visible. The improved heuristic is therefore
**only the default preselection** in that UI, never a standalone "trust the guess and run" path.
For tabular uploads the user always sees and can correct the chosen column before extraction runs.

## Scope

- **Tabular formats (XLSX, CSV):** gain a column-selection step. Both share `questionColumn` today
  and both have the bug.
- **Free-text formats (PDF, DOCX):** unchanged. They use `ExtractQuestionsFromText` (pattern/`?`
  detection), not a column, so they skip the new step entirely and keep today's one-pass flow.

## Two-pass flow (tabular only)

Extraction becomes two daemon calls for XLSX/CSV; PDF/DOCX stay one call.

```
Pass 1 — inspect:   POST /1.0/answer/build            (multipart: file, refine)
   tabular?  → async op returns { needs_column: true, tables: [...] }, extracts nothing
   free-text?→ async op returns { questions: [...] }  (today's behavior, unchanged)

   (wizard shows column-selection step, heuristic guess preselected)

Pass 2 — extract:   POST /1.0/answer/build/extract     (JSON: token, column, min_length, refine)
   → async op returns { questions: [...] } for the chosen column
```

### Why a second endpoint, not one endpoint with a mode flag

The two passes have different inputs (pass 1 = the uploaded binary; pass 2 = a column choice
against an already-parsed table) and pass 2 must not re-upload the file. A distinct
`POST /1.0/answer/build/extract` keeps each request's contract clean and lets the daemon hold the
parsed table between passes behind an opaque token (below). An alternative — one endpoint that
returns tables on first hit and accepts a column on a second multipart POST with the file
re-sent — is rejected: it re-uploads the document and muddles one route with two response shapes.

### Holding the parsed table between passes

Pass 1 parses the spreadsheet once (Tika for XLSX, direct read for CSV) into `SheetTable`s and must
not re-parse on pass 2. The daemon stages the parsed tables server-side keyed by an opaque
`build_token` (returned in pass 1, echoed in pass 2), with a short TTL and a cap on retained
builds. This mirrors the existing upload-staging pattern (temp file + cleanup) but stages the
*parsed rows* rather than the raw file. Token is single-use-ish: consumed on a successful extract,
expired by TTL otherwise. If pass 2 arrives with an unknown/expired token → `400` with a "re-upload
the document" message; the wizard drops back to its upload step.

## API contract changes (`rest-api-answer`)

### `POST /1.0/answer/build` (pass 1) — modified response

Still async, still multipart (`file`, `refine`). The completion metadata gains a discriminated shape:

- **Free-text (PDF/DOCX)** — unchanged:
  ```
  { "questions": [ {id, question, source?} ], "count": N, "refined": bool }
  ```
- **Tabular (XLSX/CSV)** — new:
  ```
  {
    "needs_column": true,
    "build_token": "<opaque>",
    "format": "xlsx" | "csv",
    "tables": [
      {
        "name": "Security",            // sheet name (xlsx) / "Table 1" (csv)
        "page_index": 0,
        "header": ["ID", "Requirement", "Notes"],   // row 0, or synthesized "Column N"
        "row_count": 42,
        "columns": [
          { "index": 0, "sample": ["1", "2", "3"], "avg_len": 2, "suggested": false },
          { "index": 1, "sample": ["Describe your…", "Do you…"], "avg_len": 68, "suggested": true },
          ...
        ]
      }
    ],
    "suggested": { "table_index": 0, "column_index": 1 }   // heuristic default
  }
  ```
  `sample` is the first few non-empty cells (capped, e.g. 3, truncated to a safe length) so the
  wizard can show a preview per column. `avg_len` feeds the UI's rationale ("longest text column").
  Extraction runs on **no** column in pass 1.

The response is a discriminated union on `needs_column`; free-text omits it. Advertise via a new
`api_extensions` entry (e.g. `answer_build_columns`) so a client can detect two-pass support.

### `POST /1.0/answer/build/extract` (pass 2) — new endpoint

Async. JSON body:
```
{ "build_token": "<from pass 1>", "table_index": 0, "column_index": 1,
  "id_column_index": -1, "min_length": 20, "refine": true }
```
Completion metadata is the **same** `{ questions, count, refined }` shape pass 1 returns for
free-text, so the wizard's review step consumes one shape regardless of path. Errors: unknown/
expired token → 400; `column_index` out of range → 400; zero questions after the min-length filter
→ operation fails with a message naming the column and suggesting a lower min-length or a different
column (so the user can adjust and retry pass 2 **without** re-running a batch).

### Spec deltas

- MODIFY the build requirement (added earlier) to describe the discriminated pass-1 response and
  that tabular formats defer extraction to a chosen column.
- ADD a requirement for `POST /1.0/answer/build/extract` (column-scoped extraction against a staged
  build token; no re-upload; same results shape).
- Note Tika dependency is unchanged (already recorded); no new external service.

## Improved heuristic (default preselection only)

Replaces `questionColumn`'s substring-or-zero logic; used to compute `suggested` in pass 1. It never
runs extraction on its own. Scoring per column, pick the best:

- **Header synonyms** (case-insensitive contains): question, requirement, ask, query, description,
  item, control, criteria — weighted, but not decisive.
- **Average cell length** — questions are prose; longest-text column is a strong signal.
- **Question-mark fraction** — share of cells ending in "?".
- **Penalize** columns that look like IDs/numbers (mostly short, numeric, or matching `^Q?\d`).

Ties / no clear winner → pick the highest `avg_len` column. The header row is still assumed to be
row 0 for tabular; if a sheet has no header the synthesized `Column N` labels + per-column samples
let the user choose correctly anyway. The heuristic's job is to be *right often enough that the
default is usually correct*, not to be trusted blindly.

## Operation flow (what changes operationally)

- Tabular uploads now produce **two** async operations (inspect, then extract). Only the extract
  operation carries the batch-relevant `questions`; the inspect operation is a short-lived parse.
- **Indicator/consumed-exited implications:** the inspect op and the extract op are both build-phase
  operations, not the batch run. As today, `BuildWizard` dismisses the build op from the indicator
  once its result is consumed into the wizard (`ops.dismiss` on completion). Extend that so the
  **inspect** op is dismissed when its tables land in the column step, and the **extract** op is
  dismissed when its questions land in the review step — neither should linger as
  "completed-unconsumed." `isBatchRun` still must **not** match either (they lack
  `questions_total`/`results`), so AnswerScreen never tries to resume a build op — unchanged and
  important.
- **Cancel run** during either build op cancels that op (existing `cancelExtraction` generalizes to
  "cancel the active build op"); returns to the relevant prior wizard step.
- Free-text flow: still one op, unchanged.

## Wizard state machine (`BuildWizard.tsx`)

Current: `upload → extracting → review → configure`.

Proposed:
```
upload
  → inspecting            (pass 1 running)
      → [tabular]  columns        (new step: choose table + column + min-length)
                     → extracting  (pass 2 running)
                         → review
      → [free-text] review        (skips columns + second extracting; same as today)
  review → configure → (run / download)
```

- Rename the current `extracting` to `inspecting` for pass 1; add `columns` and a second
  `extracting` for pass 2. Or keep one `extracting` label reused for both passes (user-facing copy
  differs: "Reading the spreadsheet…" vs "Extracting questions from column *Requirement*…").
- **New `columns` step (tabular only):** renders each table with its columns as selectable options,
  showing the header label + sample cells per column; the heuristic `suggested` column is
  preselected. A **min-length** number input defaults to 20 (matches the CLI floor) with helper
  text ("Skip cells shorter than this — lower it if short questions are missed"). A table selector
  when there is more than one sheet/table. **Continue** fires pass 2 with `{build_token, table_index,
  column_index, min_length, refine}`.
- Free-text path never enters `columns`; `needs_column` absent → straight to `review`.
- In-memory wizard state gains `buildToken`, `tables`, `selectedTable`, `selectedColumn`,
  `minLength`. The unsaved-manifest `beforeunload` guard is unaffected (still keyed on built
  questions).
- **Back semantics** inside the wizard (pre-run) stay non-destructive resets, consistent with the
  current wizard; the modal "Cancel run" only applies while an op is in flight (inspecting or
  extracting), matching the existing extraction-cancel treatment.

## UX (docs/ux/04-answer-batch.md) — Definition of done additions

Add to the change's DoD checklist:
- [ ] Spreadsheet/CSV upload shows a column-selection step before extraction; the heuristic's best
      guess is preselected and every column is choosable with a sample preview.
- [ ] Min-length control present (default 20), and lowering it re-runs extraction (pass 2) **without**
      starting a batch run.
- [ ] A wrong column is recoverable: the user can change column/min-length and re-extract; a batch
      run never starts from a guessed column.
- [ ] PDF/DOCX uploads skip the column step entirely (no regression to the one-pass flow).
- [ ] Multi-sheet workbooks let the user pick the table; single-table files skip the table selector.
- [ ] Column step is keyboard-navigable and correct in light/dark (foundation §7/§9), sample cells
      truncate without horizontal page scroll at 620px.
- [ ] `docs/rest-api.md` + `rest-api.yaml` document both `POST /1.0/answer/build` (discriminated
      response) and `POST /1.0/answer/build/extract`.

## Open decisions to confirm before implementing

1. **Token-staged parsed tables vs. re-parse on pass 2.** Plan assumes staging behind a `build_token`
   (no re-parse, no re-upload). Alternative: pass 2 re-uploads the file and re-parses — simpler
   server state (no TTL/cache) but re-sends the document and re-runs Tika. Lean **staging**.
2. **CSV in scope now, or XLSX only?** CSV shares the exact bug (`questionColumn` → column 0). Lean
   **both**, since the two-pass machinery is format-agnostic once built.
3. **ID column selection.** The CLI also lets the user pick an ID column. Plan carries
   `id_column_index` in pass 2 (default -1 = auto-number) but the wizard could omit the control
   initially and always auto-number. Lean **auto-number first**, add the control only if asked.
4. **One `extracting` label reused vs. distinct `inspecting`/`extracting` steps.** Cosmetic; lean
   distinct copy for clarity.
