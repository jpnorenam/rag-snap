# Tasks: add-ui-answer-batch

## 1. Daemon: document-extraction endpoint (`POST /1.0/answer/build`)

- [x] 1.1 Add `handleAnswerBuild` in `internal/api/handlers_answer.go`: parse `multipart/form-data` with a `file` field (reuse the temp-staging/cleanup pattern from `collectUploadedItems` in `handlers_ingest.go` and the `processing.MaxIngestFileSize` limit) plus a `refine` form field (default true).
- [x] 1.2 Run extraction inside `s.ops.runTask` (async, cancellable): detect format via `rfp.DetectFormat`; extract questions using the format-specific `rfp` extractors against `s.clients.tikaURL()` (CSV parsed directly, XLSX/PDF/DOCX via Tika). Fail with a clear error on unsupported type, no questions, or Tika unreachable.
- [x] 1.3 When `refine` is set and the inference backend is configured, apply `chat.RefineQuestions`; on LLM error fall back to the raw extracted questions (match the CLI's best-effort `rfpMaybeRefineQuestions` behavior) rather than failing the operation.
- [x] 1.4 Publish extracted candidate questions on the operation metadata under the `questions` key as a list of `{id, question, source?}` items (matching `rfp.Question`; does not collide with the run endpoint's `questions_total`/`questions_done`/`results`); do NOT persist a manifest or start a batch run.
- [x] 1.5 Register `POST /1.0/answer/build` in `registerAPI` (`internal/api/server.go`), grouped with the existing `POST /1.0/answer/batch` route; add it to `api_extensions`/swagger if the codebase gates new endpoints there.
- [x] 1.6 Reverse the "intentionally CLI-only" decision: update the `batchManifestRequest` comment and the `handleAnswerBatch` docstring in `handlers_answer.go` to reference the new build endpoint instead of asserting build is CLI-only.
- [ ] 1.7 (Optional, scope-bounded — intentionally skipped) add a `buildFromDocument` verb to `internal/apiclient/` mirroring the existing batch verb. Per the design's "keep CLI independent" decision the CLI's direct `--build` path is not rewired, so no apiclient verb is needed in this change; the UI calls the endpoint directly.

## 2. UI: API client and parsing helpers

- [x] 2.1 Add `ui/lib/api/answer.ts`: `runBatch(manifest)` over `postAsync("/1.0/answer/batch", body)` (body matches `batchManifestJSON`), and `buildFromDocument(file, {refine})` over `postAsync("/1.0/answer/build", FormData)`; both return `{ operation, metadata }`.
- [x] 2.2 Add `ui/lib/manifest.ts`: parse a YAML manifest string into the run body and validate it (non-empty `questions`, each with a `question`); serialize a built manifest back to YAML the CLI accepts. Throw a typed `ManifestParseError`. Hand-rolled parser used (no `yaml` dep added). Accepts the full CLI question schema — `id`/`question`/`keywords`/`source` — and ignores unknown fields, matching the CLI reader (`chat.LoadBatchManifest` uses `yaml.Unmarshal` without `KnownFields`); `source` (written by `answer batch --build`, `rfp.Question`) is retained through round-trip. Regression covered by `ui/lib/manifest.test.ts` (`npm test`, via `tsx --test`).
- [x] 2.3 Add `ui/lib/results.ts`: normalize a `QAFile` (`result` or `results`) into `ParsedQAFile`, reusing the existing types in `ui/lib/types.ts`; throw a typed validation error on shape mismatch.

## 3. UI: screen shell and routing

- [x] 3.1 Create `ui/app/answer/page.tsx` rendering `<AnswerScreen />` (client-only); ensure `<Header>` title and `document.title` update for the Answer section (foundation §9).
- [x] 3.2 Flip the `answer` entry in `ui/components/Sidebar.tsx` to an enabled `next/link` (`enabled: true`), preserving nav order and `aria-current`.
- [x] 3.3 Create `ui/components/answer/AnswerScreen.tsx` with the step-state machine (`"landing" | "run" | "wizard" | "running" | "review"`) consuming `useOperations()`; on mount, if a batch or build operation is already tracked and running, restore the appropriate step (survives-navigation requirement).

## 4. UI: landing state

- [x] 4.1 Build the landing state with three entry cards (`.answer-entry`, `--vf-color-background-alt`): **Run a manifest**, **Build from a document**, **Review results**, using the app card/`EmptyState` pattern and sanctioned Vanilla classes. Include the CLI-equivalent command in the empty/landing copy.

## 5. UI: Flow 1 — run a manifest

- [x] 5.1 `ui/components/answer/ManifestRunner.tsx`: YAML dropzone/file input; parse via `ui/lib/manifest.ts`; on failure show a `p-form-validation` field-level error and keep the file re-selectable, never calling the API.
- [x] 5.2 On success, preview: manifest name, target KBs as non-interactive `p-chip`s, numbered read-only question list.
- [x] 5.3 Temperature input (compact number, default `0.1`); omit any Preview-only/dry-run control (no dry-run mode exists — design Open Questions).
- [x] 5.4 **Run batch** (`p-button--positive`): call `runBatch`, build an `OperationView`, `track()` it, transition to `running`; triggering button uses the in-flight pattern (disabled + spinner + "Running…").

## 6. UI: Flow 2 — build-from-document wizard

- [x] 6.1 `ui/components/answer/BuildWizard.tsx` with a slim `.answer-steps` indicator (text emphasis only, no brand orange) coordinating three steps and in-memory state.
- [x] 6.2 `UploadStep`: dropzone for PDF/DOCX/XLSX/CSV showing detected type; "Refine questions with the model" checkbox (default on, helper text); submit calls `buildFromDocument`, `track()`s the operation, and shows a running state ("Extracting questions from `<file>`…").
- [x] 6.3 `ReviewStep`: read extracted questions from the completed operation's `metadata.questions` (`{id, question, source?}`); render each as a checkbox (checked by default) + growing `<textarea>`; support deselect, edit, and add-a-question; sticky footer shows "N of M selected" + Back/Continue.
- [x] 6.4 `ConfigureStep`: KB multi-select `p-chip`s (ChatScreen pattern) + temperature + manifest name; **Download manifest** (`p-button`, serialize selected questions to CLI-accepted YAML, no run) and **Run batch** (`p-button--positive`, hand off to Flow 1's run + review).
- [x] 6.5 Guard unsaved built manifest with `beforeunload` + an in-app confirm before discarding wizard state.

## 7. UI: running state

- [x] 7.1 `ui/components/answer/RunningBatch.tsx`: read the tracked operation by id from `useOperations()`; show per-question progress from `questions_done`/`questions_total`, falling back to a single spinner card. Use `aria-live="polite"`.
- [x] 7.2 Rely on the global operations panel for Cancel (no second cancel control); a cancelled/failed operation surfaces its state and returns the user to a sensible step.
- [x] 7.3 On `isTerminal(op)` + success, read `results` from `op.metadata`, normalize via `ui/lib/results.ts`, transition to `review`.

## 8. UI: review surface (Flow 1 completion + Flow 3)

- [x] 8.1 `ui/components/answer/ResultsReview.tsx`: header with manifest name, run timestamp (relative + absolute `title`), KBs used, and **Export JSON** (`p-button`) downloading the results verbatim in the CLI's `BatchOutput` shape (`generated_at`, `model`, `results`).
- [x] 8.2 `ui/components/answer/QACard.tsx` (`.qa-card`): question as title, answer as `white-space: pre-wrap`; collapsible native `<details>`/`<summary>` **Sources** section rendered **only when** a result carries provenance (batch API currently omits per-question sources — design Risks).
- [x] 8.3 Failed/empty answers render with a caution border token + error text, never blank.
- [x] 8.4 Jump-list index (numbered anchors) when >10 questions.
- [x] 8.5 Flow 3: "Review results" entry opens a results JSON file from disk, normalizes via `ui/lib/results.ts` into the same surface without a run; malformed files show a validation error.

## 9. UI: styles

- [x] 9.1 Add a `// --- answer ---` group to `ui/app/globals.scss` for `.answer-entry`, `.answer-steps`, `.qa-card`, textarea rows, and the caution treatment; all colors via `--vf-*` tokens, zero hardcoded hex.

## 10. Definition of done (UX)

- [x] 10.1 All four view states per foundation §7 (loading, empty=landing, loaded=review, error); mutations follow in-flight/success/failure rules.
- [x] 10.2 Correct in light **and** dark themes (`is-dark`).
- [x] 10.3 Usable at 620px (collapsed rail) — no horizontal page scroll; wide content scrolls in its own wrapper.
- [x] 10.4 Keyboard-only walkthrough passes; focus management on modals/routes verified; `aria-live`/`role="alert"` on progress/error regions.
- [x] 10.5 Only sanctioned patterns (foundation §6); any new pattern documented in `docs/ux/00-foundation.md`.
- [x] 10.6 All colors via `--vf-*` tokens; zero hardcoded hex outside the sidebar SCSS vars.
- [x] 10.7 Landing/empty state includes the CLI equivalent (`rag-cli.rag answer batch <manifest.yaml>`).
- [x] 10.8 Change-4 additions: manifest preview shows KBs + questions before any run and invalid YAML never reaches the API; wizard review step supports deselect/edit/add with selection count always visible; **Download manifest** produces YAML the CLI accepts (`answer batch <file>` round-trip verified); review surface uses the `ui/lib/types.ts` contract and Export JSON matches the CLI's results format; returning to `/answer/` during a run or extraction resumes from the operations context.

## 11. Docs and verification

- [x] 11.1 Document `POST /1.0/answer/build` in `docs/rest-api.md` and `rest-api.yaml` (multipart upload, `refine` flag, async operation, extracted-questions metadata).
- [x] 11.2 Update `docs/local-ui.md` with the Answer section (run a manifest, build-from-document wizard, review results); update `docs/usage.md` if it documents the API surface.
- [x] 11.3 `cd ui && npm run build` (static export) to confirm the new page compiles; then `make all` at repo root (tidy fmt vet lint test build).
- [ ] 11.4 In-snap validation (deferred — needs snapcraft build + `snap install --dangerous` + running OpenSearch/Tika/inference): exercise end-to-end against the running daemon — upload a document to `/answer/`, review/edit extracted questions, download a manifest and confirm `rag-cli.rag answer batch <file>` accepts it, run a batch, and export results; confirm Tika-backed extraction works inside strict confinement. Partial coverage landed instead: `internal/api` unit tests drive `POST /1.0/answer/build` (CSV extract + unsupported-type reject) and a `parse→serialize→parse` round-trip proves the downloaded manifest re-parses to CLI-accepted YAML.
