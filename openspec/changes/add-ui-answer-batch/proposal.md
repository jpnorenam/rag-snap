# Proposal: add-ui-answer-batch

## Why

The browser UI's **Answer RFPs** sidebar entry is still an inert "Soon" placeholder, yet the CLI's `answer batch` — including its `--build` document-to-manifest wizard — is a headline RFP workflow. The app shell (`add-ui-app-shell`) already ships everything a batch screen needs: async-operations tracking (`useOperations`/`track`), shared primitives (`EmptyState`, `Spinner`, `ConfirmModal`), and the reserved `QAItem`/`QAFile`/`ParsedQAFile` review contract in `ui/lib/types.ts`. This change delivers full browser parity with `answer batch` (run + review) **and** `answer batch --build` (extract questions from an RFP document, review/edit, then save or run), per `docs/ux/PLAN.md` Change 4 (stories 4.1–4.3) and `docs/ux/04-answer-batch.md`.

Story 4.3 requires the daemon to extract candidate questions from an uploaded document — which the current API deliberately does not do. `internal/api/handlers_answer.go` records the document-to-manifest "build" flow as **intentionally CLI-only** ("The interactive document-to-manifest build flow is CLI-only and not exposed here"), and the `rest-api-answer` spec states it "SHALL be added as a separate, explicit capability" if ever needed over the API. **This change explicitly reverses that decision** and adds the extraction endpoint, because the browser cannot run Tika or the LLM refinement pass itself — the interactive review moves to the browser, but the extraction must run server-side.

## What Changes

### REST API (`rest-api-answer`)

- **BREAKING to the prior scope decision, additive to the API:** add `POST /1.0/answer/build` — accepts an uploaded RFP/RFI document (PDF, DOCX, XLSX, CSV) as `multipart/form-data`, extracts candidate questions server-side, and returns them for interactive review. Runs as an **asynchronous operation** (Tika extraction can be slow), reporting progress and publishing the extracted questions on the operation metadata on completion.
- The endpoint reuses the existing extraction logic (`cmd/cli/basic/rfp` — format detection, CSV/XLSX/PDF/DOCX handling via Tika) and the optional LLM semantic-refinement pass (`chat.RefineQuestions`), gated by a request flag (parity with `--build` / `--no-refine`). Refinement is on by default.
- Reverse the "intentionally CLI-only" comment and docstring in `internal/api/handlers_answer.go`; update the `rest-api-answer` spec's "Manifest is supplied prepared, not built interactively" requirement to reflect that build is now exposed as its own endpoint (the *run* endpoint still only accepts a prepared manifest — extraction is a distinct operation, not folded into the run).
- **Tika is now an external-service dependency of the API.** `docs/ux/PLAN.md` flags Change 4 as the **first API path to touch Tika**. (The daemon already reaches Tika on the *ingest* path via `clients.tikaURL()`, so no new snap interface, plug, or bundled binary is required — but this is the first path in the `answer` capability to depend on Tika, and the proposal states that dependency explicitly.)

### Browser UI (`local-ui-app`)

- Turn `/answer/` from a disabled placeholder into a real page (`ui/app/answer/page.tsx`) driven by a client-side **step-state machine** (landing → wizard/run → running → review), not separate routes — the flows share one review surface. Flip the Sidebar `answer` entry to `enabled` and set `<Header>`/`document.title` per foundation §9.
- **Landing** (story 4.1/4.3 entry): three entry cards (`.answer-entry`) — **Run a manifest**, **Build from a document**, **Review results**.
- **Flow 1 — Run a manifest** (4.1/4.2): YAML dropzone → client-side parse + preview (manifest name, target-KB chips, numbered read-only question list; invalid YAML surfaces a field-level error and never reaches the API); temperature input (default `0.1`); **Run batch** → `postAsync POST /1.0/answer/batch` + `track()` → running view reflecting per-question `questions_total`/`questions_done` progress; cancellable from the operations panel; survives navigation; completion → review surface.
- **Flow 2 — Build from a document (the three-step wizard)** (4.3): **(1) Upload** a PDF/DOCX/XLSX/CSV with a "Refine questions with the model" checkbox (default on); submit runs extraction via `POST /1.0/answer/build` as a tracked operation ("Extracting questions from `<file>`…"). **(2) Review questions** — each candidate is a checkbox (checked by default) + editable `<textarea>`; deselect, edit, and add-a-question supported; sticky footer shows "N of M selected". **(3) Configure & run** — KB multi-select chips + temperature + manifest name, with **Download manifest** (writes YAML the CLI accepts, parity with `--build` without running) and **Run batch** (continues into Flow 1's run + review). Wizard state is in-memory; warn on navigation away with an unsaved built manifest.
- **Flow 3 — Review results** (4.2): open a previously exported results JSON from disk into the shared review surface without a run.
- **Review surface** (shared): reuses `QAItem`/`QAFile`/`ParsedQAFile` (tolerating both `result`/`results` keys); header with manifest name, timestamp, KBs used, and **Export JSON** (verbatim, matching the CLI); one `.qa-card` per Q&A (question title, pre-wrapped answer, **conditional** collapsible Sources); failed/empty answers rendered with a caution treatment; jump-list index when >10 questions.
- New `ui/lib/api/answer.ts` (`runBatch`, `buildFromDocument`), manifest/results parse helpers in `ui/lib/`, answer styles under a `// --- answer ---` group in `ui/app/globals.scss`.

**Sources provenance stays conditional.** The batch *result* shape (`chat.BatchResult` = `{id, question, answer}`) still carries no per-question provenance; that is a separate API gap. The review surface renders the Sources section only when a result carries provenance, exactly as before — this change does not close that gap.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `rest-api-answer`: ADD a document-extraction requirement — `POST /1.0/answer/build` accepts an uploaded RFP document, extracts candidate questions (Tika + optional LLM refinement) as an asynchronous, cancellable operation, and returns them for client-side review; the daemon does not persist a manifest or auto-run. MODIFY the existing "Manifest is supplied prepared, not built interactively" requirement to reflect that interactive *review* is a client concern while *extraction* is now an explicit API operation (reversing the prior CLI-only decision); the run endpoint still accepts only prepared manifests.
- `local-ui-app`: ADD the Answer batch screen — the `/answer/` step-state page and three landing entry cards; the client-side manifest preview/validation gate; running a prepared manifest as a tracked operation with per-question progress; the three-step build-from-document wizard (upload → review/edit → configure & run) driving `POST /1.0/answer/build` and manifest download/run; the shared results review surface wired onto the existing `QAItem`/`ParsedQAFile` contract with verbatim Export JSON; and opening a previously exported results file. MODIFY the reserved answer-review data-contract requirement from "present and unused" to consumed by a shipped screen.

## Impact

- **Code**:
  - Daemon (`internal/api/`): new `POST /1.0/answer/build` handler and route registration in `server.go`; reuse `cmd/cli/basic/rfp` (extraction) and `cmd/cli/basic/chat` (`RefineQuestions`); run via the existing `ops.runTask` async machinery; reverse the CLI-only comment/docstring in `handlers_answer.go`.
  - Client (`internal/apiclient/`): a build verb parallel to the existing batch verb, if the CLI is to consume it (optional; the CLI keeps its direct path).
  - UI (`ui/`): new `ui/app/answer/page.tsx`, components under `ui/components/answer/`, `ui/lib/api/answer.ts`, manifest/results helpers in `ui/lib/`, styles in `ui/app/globals.scss`; flip the `answer` sidebar entry; consume `QAItem`/`ParsedQAFile` from `ui/lib/types.ts`.
- **APIs consumed/added**: NEW `POST /1.0/answer/build` (async, multipart upload); existing `POST /1.0/answer/batch` and the operations endpoints (`GET /1.0/operations`, `GET /1.0/events`, `DELETE /1.0/operations/{id}`).
- **External services**: OpenSearch (batch run retrieval), the inference server (grounded generation + optional refinement), and **Tika** (question extraction) — the batch/build capability now depends on all three. Tika is a new dependency **for this capability**; the daemon already has a configured Tika client from the ingest path, so no new snap interface/plug/bundled binary/hook is required.
- **Config**: no new config keys (neither `package` nor `user` scope). The build endpoint reads the existing `tika.http.*` keys via the daemon's client cache.
- **Secrets**: none added. Refinement and generation use the daemon's already-configured `CHAT_API_KEY`; extraction needs no secret.
- **Snap**: no `snapcraft.yaml` changes — no new interfaces, plugs, slots, bundled binaries, or hooks. The Go embed of `ui/out` picks up the new static assets on the next snap build.
- **User-facing surface**: a new REST endpoint (`POST /1.0/answer/build`) and a new browser Answer screen with all three flows. No CLI command/flag/slash-command changes. Documentation to update: `docs/rest-api.md` and `rest-api.yaml` (new endpoint), `docs/local-ui.md` (Answer section), and `docs/usage.md` if it documents the API surface. `--help` and `apps/completion.bash` are unaffected (no CLI change).
- **UX guidelines**: governed by `docs/ux/00-foundation.md` and `docs/ux/04-answer-batch.md`; the design links them and tasks carry the foundation Definition of done checklist plus the Change-4 additions.
- **Dependencies**: none added on the Go side (reuses existing packages). On the UI side, a minimal YAML parser only if a hand-rolled manifest parser proves brittle; no component library.
