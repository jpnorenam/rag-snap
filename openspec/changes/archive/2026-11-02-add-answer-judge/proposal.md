## Why

An `answer batch` run produces 30–200 answers, and today a human reviews every one from scratch to find the few that are wrong. The defects recorded so far are specific and repeatable: unsupported claims, figures that contradict another row, other customers named from the knowledge base, gaps where the context actually held the answer, and rule violations such as markdown or length. A second model can check each answer against the evidence it was generated from and tell the reviewer which rows to trust, which to check, and what exactly to check. Then the reviewer reads the 20–30% that need attention instead of all of them.

## What Changes

- **New: an LLM-as-a-judge pass inside `answer batch`.** Sonnet (or whichever `chat` backend is configured) answers every question as it does today. Gemini then judges each answer and returns:
  - a **confidence score** on an anchored 1–5 scale, with a derived **rating** of green, yellow or red that matches the review buckets reviewers already use;
  - a **comment** that names the specific claim, gap or rule violation and what to check or correct. The comment is blank when the score is 5.
  - a small set of **issue categories** for filtering (unsupported claim, contradicts context, missed context, incomplete, off-question, other-customer reference, rule violation, gap).
- **Each answer is judged against its own evidence.** The judge receives the question, the answer, the exact retrieved context the answer was grounded on, the applied domain context, and the batch's system prompt. Judging runs in the same process as the batch, while that context is still in memory. Retrieved context is not written to the results file.
- **Judging is parallel and overlaps answering.** Each answer is queued for judging as soon as it is produced, under a bounded number of concurrent Gemini calls. When the last answer lands, only a few judgments remain.
- **One cross-answer consistency pass** runs after all answers are judged. It sees every answer (without the retrieved context) and flags contradictions and repeated problems between rows, and adds them to the comments of the rows involved.
- **No-context gap answers are rated red without a model call.** Markdown leakage is detected locally. Word and sentence counts are computed locally and given to the judge as facts, since models count poorly.
- **Judging never costs the answers.** The CLI writes the results file as soon as answering finishes and rewrites it with judgments afterwards. A failed judgment marks that row "not judged" and does not fail the run.
- **Results carry verdicts.** Each `BatchResult` gains an optional `judgment` object and `BatchOutput` gains a `judge` summary (model, counts per rating). Both are omitted when judging did not run, so older files and unjudged runs keep their current shape.
- **The terminal ends with a review summary.** It shows counts per rating, then the red and yellow rows listed by **spreadsheet row number** (the first answer is row 2), each with its comment.
- **CLI surface:** `answer batch` judges automatically when judging is enabled. New flag `--no-judge` skips judging for one run.
- **REST API surface:** `POST /1.0/answer/batch` accepts an optional `judge` boolean (default: judge when enabled). Operation metadata reports a judging phase and progress, and publishes verdicts on the results. The daemon advertises judge availability in its capability list, like analysis.
- **Browser UI surface:** the results review surface shows each card's rating badge, score and comment, and adds a rating filter plus a summary strip. Files without verdicts render unchanged.
- **New `package`-scoped config keys**, seeded by `install` and `post-refresh`. An operator overrides them with a `user` set:
  - `judge.enabled`, default `true`. Judging is on for every run unless an operator turns it off here, or skips one run with `--no-judge`.
  - `judge.model`, default empty, which falls back to `analysis.model`.
  - `judge.reasoning`, default `medium`.
  - `judge.concurrency`, default `4`.
- **No new secret.** The judge uses the existing `GEMINI_API_KEY` from the environment and the same pinned Gemini API endpoint as analysis.

## Capabilities

### New Capabilities

- `answer-judge`: covers the LLM-as-a-judge pass over an `answer batch` run:
  - gating and per-run opt-out;
  - the evidence the judge receives;
  - the verdict shape and scoring rubric;
  - parallel judging and the consistency pass;
  - local checks;
  - failure isolation;
  - how verdicts are recorded and summarised.

### Modified Capabilities

- `rest-api-answer`: ADD that a batch operation is judged when judging is enabled and the request does not opt out, that the operation reports a judging phase and publishes verdicts on its results, and that judge availability is advertised as a capability.
- `local-ui-app`: ADD that the results review surface shows each answer's rating, score and comment, offers a rating filter and summary, and renders verdict-free files unchanged.

## Impact

- **External services:**
  - **Inference server:** unchanged; it still generates every answer.
  - **OpenSearch:** unchanged; the judge reuses the context retrieval already fetched and adds no extra retrieval.
  - **Tika:** not touched.
  - **New outbound data:** retrieved knowledge-base chunks and answers go to Google's Gemini API, in addition to the RFP documents that analysis already sends. This is covered by the existing compliance agreement with Google for Gemini, confirmed on 2026-10-21, so judging is on by default.
- **Code:**
  - `cmd/cli/basic/chat/batch.go`: `RunBatch` keeps each answer's context in memory, feeds an optional judge, and gains answer-complete and judgment hooks. `BatchResult` and `BatchOutput` gain the verdict fields.
  - New judge files in `cmd/cli/basic/analysis/`: the prompt, schema, settings, per-answer and consistency calls, and local checks. They reuse its pinned client, effort mapping and error types.
  - `cmd/cli/basic/answer.go` and `answer_api.go`: the `--no-judge` flag, the two-stage file write and the terminal summary.
  - `internal/api/handlers_answer.go`: the request field, phase metadata and capability.
  - `snap/hooks/install` and `post-refresh`: seed the new keys.
  - `ui/components/answer/QACard.tsx`, `ResultsReview.tsx` and `ui/lib/types.ts`.
- **APIs:** `POST /1.0/answer/batch` gains an optional request field and additional result and metadata fields. All additions are additive and nothing breaks.
- **Dependencies:** none new. `google.golang.org/genai` is already a dependency.
- **Snap:** no new interfaces, plugs or bundled binaries. The daemon needs `GEMINI_API_KEY` in its service drop-in, the same one analysis already documents. The only hook changes are the four seeded keys.
- **Docs to update:** `docs/usage.md` (judging, `--no-judge`, the `judge.*` keys, the results fields), the `answer batch --help` long text, `apps/completion.bash` (`--no-judge`), `docs/rest-api.md` and `rest-api.yaml` (the request field, metadata, results shape and capability), `docs/local-ui.md` (the review surface), and the `snapcraft.yaml` comment on `GEMINI_API_KEY`.
- **Cost:** about one Gemini call per answered question, plus one consistency call per run. Input is dominated by the retrieved context, roughly 5–10K tokens per question. A 192-row run therefore sends on the order of 1–2M input tokens. The judge model is configurable so a cheaper tier can be used.
