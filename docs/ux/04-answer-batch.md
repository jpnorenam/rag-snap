# 04 — UX guidelines: `add-ui-answer-batch`

`/answer/` — batch Q&A over knowledge bases. Parity with `answer batch` (run + review) and `answer batch --build` (RFP extraction wizard). Read `00-foundation.md` and `01-app-shell.md` first.

## Information architecture

The section landing page has three entry points, presented as three cards (`.answer-entry`, `--vf-color-background-alt` surfaces) when nothing is in progress:

1. **Run a manifest** — upload an existing YAML manifest (parity: `answer batch manifest.yaml`)
2. **Build from a document** — extract questions from an RFP/RFI file (parity: `--build`)
3. **Review results** — open a previous results JSON (parity: reviewing `batch-results-<ts>.json`)

Keep one page (`ui/app/answer/page.tsx`) with a step-state machine, not separate routes — the flows share the review surface.

## Flow 1 — Run a manifest

1. Dropzone accepts the YAML manifest; parse client-side and show a **preview**: manifest name, target KBs (chips, non-interactive), and the questions as a numbered read-only list. Bad YAML → field-level validation error, keep the file selectable again.
2. Options row: temperature (compact number input, default **0.1** per CLI), and a **Preview only** checkbox mapping to `--preview` semantics if the API supports a dry run — otherwise omit, don't fake it.
3. **Run batch** (`p-button--positive`) → `postAsync POST /1.0/answer/batch`, `track()` it, and switch the page into a **running** view: question list with per-question status dots (pending → running → done/failed) if the operation exposes progress metadata; otherwise a single spinner card "Answering N questions…". Cancel via the operations panel.
4. On completion, transition straight into the review surface.

## Flow 2 — Build from a document (the wizard)

Three explicit steps with a slim step indicator (custom `.answer-steps`, current step bold + orange-free — use text emphasis, not brand color):

1. **Upload** — dropzone for PDF/DOCX/XLSX/CSV. Show detected type. Option checkbox: **Refine questions with the model** (default on, maps to the LLM-refinement pass; helper text: "Uses the chat model to clean up extracted questions"). Submit runs extraction as a tracked operation (Tika parse can be slow — the running state says "Extracting questions from `<file>`…").
2. **Review questions** — the parity moment with the CLI's `huh` multi-select, done better in a browser:
   - A list of candidate questions, each row = checkbox (checked by default) + editable text (`<textarea>` grows with content). Users can **deselect**, **edit**, **add a question** (button appends an empty editable row), and **reorder is not required**.
   - Sticky footer summary: "N of M questions selected" + **Back** / **Continue**.
3. **Configure & run** — KB multi-select chips (ChatScreen pattern) + temperature + manifest name. Two actions: **Download manifest** (`p-button` — saves the YAML, parity with `--build` writing a manifest without running) and **Run batch** (`p-button--positive`, continues into Flow 1 step 3).

Wizard state is in-memory only; warn on navigation away while a built manifest is unsaved (`beforeunload` + in-app confirm).

## Review surface (both flows + Flow 3 upload)

Wire up the **existing type contract** `QAItem`/`QAFile`/`ParsedQAFile` in `ui/lib/types.ts` (it already handles the `results` vs `result` key variants) — do not invent a new shape.

- Header: manifest name, run timestamp, KBs used, **Export JSON** (`p-button`, downloads the results file verbatim).
- One card per Q&A (`.qa-card`): the **question** as the card title; the **answer** as rendered text (`white-space: pre-wrap`); a collapsible **Sources** section (`<details>`/`<summary>` — native, keyboard-free-lunch) listing provenance chunks with KB + source ID + score, each linking to the Change-2 metadata view when available.
- Failed/empty answers render with a caution border token and the error text — never silently blank.
- A compact index (jump list) at the top when there are >10 questions: numbered links to card anchors.

## States
Foundation §7 throughout. Notably: the landing cards are the "empty" state; a running batch survives navigation (it's a tracked operation — returning to `/answer/` while one runs shows the running view, seeded from the operations context).

## Definition of done (UX)
Foundation checklist, plus:
- [ ] Manifest preview shows KBs + questions before any run; invalid YAML never reaches the API
- [ ] Wizard review step supports deselect/edit/add; selection count always visible
- [ ] "Download manifest" produces YAML the CLI accepts (`answer batch <file>` round-trip verified)
- [ ] Review surface uses `ui/lib/types.ts` contract; Export JSON matches the CLI's results format
- [ ] Returning to `/answer/` during a run resumes the running view from the operations context
