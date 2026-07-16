# Design: add-ui-answer-batch

## Context

The `add-ui-app-shell` change (merged, archived `2026-07-14-add-ui-app-shell`) delivered
multi-page navigation and the async-operations UX the whole UI parity plan depends on, and left a
deliberate seam for this change: the `answer` sidebar entry is a "Soon" placeholder, and
`ui/lib/types.ts` carries the `QAItem`/`QAFile`/`ParsedQAFile` contract "present and unused" for
exactly the review surface built here.

`docs/ux/PLAN.md` Change 4 (stories 4.1–4.3) and `docs/ux/04-answer-batch.md` scope this change to
full parity with `answer batch` **and** `answer batch --build`. Story 4.3 (build from a document)
forces a decision the earlier scoping deferred: the browser cannot run Tika or the LLM refinement
pass, so **question extraction must run server-side**. This change adds that endpoint and
explicitly reverses the prior "intentionally CLI-only" decision.

Backing API today:
- `POST /1.0/answer/batch` (`rest-api-answer`) runs a prepared manifest as an async operation,
  publishing `questions_total`/`questions_done` progress and a `results` array
  (`chat.BatchResult` = `{id, question, answer}`) plus `generated_at`/`model` on the operation.
  This is the same JSON the CLI writes.
- `internal/api/handlers_answer.go` records the build flow as CLI-only in a type comment and the
  `handleAnswerBatch` docstring; the `rest-api-answer` spec says build "SHALL be added as a
  separate, explicit capability."

Reusable server-side logic already exists:
- `cmd/cli/basic/rfp` — format detection (`DetectFormat`), CSV parsing, XLSX/PDF/DOCX extraction
  through Tika, TOC filtering, and question-pattern detection.
- `cmd/cli/basic/chat.RefineQuestions(baseURL, model, questions)` — the LLM semantic-refinement
  pass (`--no-refine` skips it in the CLI).
- The daemon already has a Tika client: `handlers_ingest.go` calls `s.clients.tikaURL()` and runs
  `download → Tika → chunk → index` inside `ops.runTask`. So Tika reachability, config
  (`tika.http.*`), and the async-operation harness are all in place.

UX follows the `ui-conventions` skill: Vanilla tokens only, feature-prefixed BEM in one flat
`globals.scss`, hand-written Vanilla class names, all API calls through `ui/lib/api/envelope.ts`,
long-running work tracked via `useOperations`.

## Goals / Non-Goals

**Goals:**
- Add `POST /1.0/answer/build`: upload a document, extract candidate questions (Tika + optional
  LLM refinement) as a tracked async operation, return them for client review. No manifest
  persistence, no auto-run.
- Reverse the CLI-only decision cleanly and visibly (comment/docstring + spec delta).
- Ship all three UI flows: run a manifest (4.1/4.2), build-from-document wizard (4.3), review a
  results file (4.2), sharing one review surface.
- Wire the reserved `QAItem`/`ParsedQAFile` contract into the review surface; Export JSON is the
  CLI's format verbatim.

**Non-Goals:**
- Server-side manifest persistence or a server-driven "build and run in one call" — build and run
  stay distinct steps (client reviews between them).
- Closing the per-question **provenance** gap: the batch result carries no sources; the review
  surface renders Sources only when present. That is a separate API gap, left as-is.
- Any new config key, secret, snap interface/plug, bundled binary, or hook.
- CLI command/flag changes — the CLI keeps its existing direct `--build` path.

## Decisions

### `POST /1.0/answer/build` as a multipart, async, non-persisting operation
The new handler accepts `multipart/form-data` with a `file` field (same staging pattern as
`collectUploadedItems` in `handlers_ingest.go`: `ParseMultipartForm`, stage to a temp file, clean
up) plus a `refine` form field (default true). It runs inside `s.ops.runTask` so it appears in the
operations UX and is cancellable, and because Tika + refinement can be slow. Extraction reuses
`rfp.DetectFormat` + the format-specific extractors against `s.clients.tikaURL()`; when `refine`
is set and the inference backend is configured, it calls `chat.RefineQuestions`. On completion it
publishes the questions on the operation metadata (e.g. `{ "questions": [{id, question}], ...}`)
for the client to read via the operations context. **Alternatives considered:** (a) a synchronous
endpoint — rejected, Tika parsing of a large PDF can exceed a reasonable sync budget and there is
no progress/cancel; (b) build-and-run in one call — rejected, it removes the interactive review
that is the whole point of story 4.3 and contradicts the "build does not run" requirement.

### Reverse the CLI-only decision explicitly, not silently
`internal/api/handlers_answer.go` gets its `batchManifestRequest` comment and `handleAnswerBatch`
docstring updated to point at the new build endpoint instead of asserting build is CLI-only, and
the `rest-api-answer` spec MODIFIES the "prepared, not built interactively" requirement so the run
endpoint still only accepts prepared manifests while extraction becomes its own operation. This
keeps the reversal auditable — a reader of either the code or the spec sees the decision changed
and why. **Alternative considered:** leave the comment and just add the endpoint — rejected, it
would leave contradictory in-code guidance.

### Tika is now a dependency of the `answer` capability — stated, but no new plumbing
`docs/ux/PLAN.md` flags Change 4 as the first API path to touch Tika. The proposal states Tika is
now an external-service dependency of this capability. In practice the daemon already constructs a
Tika client for ingest, so there is **no new snap interface, plug, bundled binary, or hook** — the
build handler reads the existing `tika.http.*` config through `s.clients.tikaURL()`. If Tika is
unconfigured/unreachable, the operation fails with a clear error (same posture as ingest). This is
the honest nuance behind "first API path to touch Tika": first in the `answer` capability, not the
first Tika client in the daemon.

### UI: single page + step-state machine, running state derived from the tracker
`/answer/` is one `ui/app/answer/page.tsx` with a `useState` step
(`"landing" | "run" | "wizard" | "running" | "review"`), not sub-routes — the flows converge on
the review surface and in-memory state (parsed manifest, wizard questions, running operation)
would be lost across route changes under `trailingSlash` static export. Both the batch-run and the
build operation are registered with `track()` and read back out of `useOperations()` by id, so
returning to `/answer/` mid-operation rehydrates from the shared tracker (which seeds from
`GET /1.0/operations` and updates over `/1.0/events`). No screen-local polling (foundation §5).

### UI: client-side parse/validate helpers, wizard state in memory
`ui/lib/manifest.ts` parses/validates a YAML manifest into the `POST /1.0/answer/batch` body
(`batchManifestJSON` shape); `ui/lib/results.ts` normalizes a `QAFile` (`result`/`results`) into
`ParsedQAFile`. Both throw typed errors rendered as field-level messages and never reach the
network on failure. The build wizard holds its extracted/edited questions in component state; step
three serializes them to the run body (Run batch) or to YAML for **Download manifest** (a
CLI-accepted manifest — round-trip verified in tasks). A `beforeunload` + in-app confirm guards an
unsaved built manifest.

### UI: YAML dependency — prefer hand-rolled
The manifest is a small flat YAML subset. Prefer a hand-rolled parser for the run-manifest preview
and the download serialization; add a minimal `yaml` dependency only if hand-rolling proves
brittle (record which was used). Client-bundled, build-time only — no snap impact. **Alternative:**
parse YAML server-side — rejected, no endpoint accepts raw YAML and the build endpoint takes the
binary document, not YAML.

### Components and styles
New components under `ui/components/answer/` (`AnswerScreen`, `ManifestRunner`, `BuildWizard`
with `UploadStep`/`ReviewStep`/`ConfigureStep`, `RunningBatch`, `ResultsReview`, `QACard`),
`"use client"`, typed `Props`, reusing app-shell `EmptyState`/`Spinner`/operations context. New
API verbs in `ui/lib/api/answer.ts`: `runBatch(manifest)` (`postAsync`) and
`buildFromDocument(file, {refine})` (`postAsync` with a `FormData` body). Styles under a
`// --- answer ---` group in `ui/app/globals.scss`, sanctioned Vanilla patterns only: entry cards
on `--vf-color-background-alt`, `p-chip` KB chips, `p-button--positive` for the single primary per
view, `p-form-validation` for errors, growing `<textarea>` rows in the review step, native
`<details>`/`<summary>` for conditional sources, caution border token for failed answers. The
step indicator uses text emphasis only — no brand orange in content (foundation §1).

## Risks / Trade-offs

- **Tika/refinement latency and failure** → build runs can be slow or fail (bad file, Tika down, LLM unconfigured). **Mitigation:** async operation with progress + cancel; clear terminal error surfaced in the operations panel and the wizard; refinement is best-effort (fall back to raw extracted questions if the LLM step errors, matching the CLI's `rfpMaybeRefineQuestions` behavior).
- **Reversing a recorded decision** → risk of leaving contradictory guidance. **Mitigation:** update code comment + docstring + spec delta together; the spec MODIFIED requirement documents the new boundary (run = prepared only; build = separate operation).
- **Batch results carry no per-question sources** → the UX doc's Sources section cannot be fully honored. **Mitigation:** render Sources only when a result carries provenance; spec's review requirement is written conditionally; note the API gap in tasks for a future change. This is unchanged by this proposal.
- **Upload size / temp-file cleanup** → large documents. **Mitigation:** reuse the ingest upload limit (`processing.MaxIngestFileSize`) and temp-staging/cleanup pattern already proven in `handlers_ingest.go`.
- **`is-dark` regressions** → new surfaces must pass in both themes (Definition of done); all colors via `--vf-*` tokens.
- **Config / secrets / snap** → none added. No snapctl keys, no new secrets (reuses `CHAT_API_KEY` + `tika.http.*`), no snap interface/plug/bundled-binary/hook changes.

## Migration Plan

Additive. Ship the daemon endpoint and the UI together; the sidebar entry flips from placeholder
to link. Rollback = revert `internal/api/` (drop the route + restore the CLI-only comment) and the
`ui/` changes (re-disable the `answer` sidebar entry). No data migration, no config migration; the
API version is unchanged (new endpoint under the existing `/1.0` root, advertised via
`api_extensions` if the codebase gates it — verify in tasks).

## Resolved Questions

- **CLI `--build` stays independent.** The CLI's existing direct `--build` path is NOT refactored to call the new endpoint in this change. Both share the same `rfp` (extraction) and `chat.RefineQuestions` (refinement) packages, so behavior stays aligned without coupling the CLI to the daemon. Task 1.7 (an optional `apiclient` build verb) does not rewire the CLI. Dogfooding the endpoint from the CLI can be a later change if wanted.
- **Extracted-questions metadata key is `questions`,** matching the existing `rfp` package convention rather than inventing a name. The build operation publishes `metadata.questions` as a list of `{id, question, source?}` items — exactly `rfp.Question`'s shape (yaml/json tags `id`, `question`, `source`), and consistent with how `rfp.Manifest`, `chat.BatchManifest`, and the API's `batchManifestRequest` all name the list `questions`. This does not collide with the run endpoint's metadata keys (`questions_total`, `questions_done`, `results`). The optional `source` field (e.g. an XLSX sheet name) is carried through and ignored by the run path, matching CLI semantics.
- **Provenance enrichment is a separate change, out of scope here (confirmed).** The batch result stays `{id, question, answer}` with no per-question sources; the review surface renders the Sources section only when provenance is present. Enriching the batch result with retrieval provenance will be proposed as its own change.
