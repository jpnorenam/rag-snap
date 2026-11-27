## Context

`answer batch` answers each manifest question sequentially through `chat.RunBatch`:
1. keyword rewrite;
2. hybrid retrieval, which returns about 15 chunks formatted by `formatContext`;
3. one grounded generation on the configured `chat` backend, today Claude Sonnet 5.5 on Bedrock's Anthropic route.

The same function serves the CLI's direct path (`ProcessBatchChat`) and the daemon (`handlers_answer.go`). The daemon publishes results incrementally on the operation metadata.

The retrieved context lives only in a local variable of the loop. It is never returned and never persisted.

Gemini is already integrated in `cmd/cli/basic/analysis`, for manifest generation:
- a `Client` pinned to `https://generativelanguage.googleapis.com/`, with every SDK environment default overridden;
- the key read only from `GEMINI_API_KEY`;
- `complete()`, which performs a schema-constrained call and turns truncation, empty, blocked and malformed responses into typed errors;
- reasoning-effort mapping;
- `QuotaError` for HTTP 429.

Compliance requires that confidential content go straight to Google and nowhere else, with no gateway and no `CHAT_API_KEY` fallback.

How reviewers work today: they paste answers into a spreadsheet and sort them into three buckets:
- 🟢 green: paste as is;
- 🟡 yellow: needs an edit;
- 🔴 red: wrong, invented, a gap, or a decision.

They refer to rows by spreadsheet number, with the first answer on row 2.

The defects found by hand on real runs are the judge's target list:
- unsupported figures;
- contradictions across rows, such as 36,000 vs 25,000 packages;
- other customers named from the `canonical-rfp` KB, such as other operators;
- source-document errors, such as a DL385 paired with an Intel Xeon;
- volunteered limitations;
- length overruns;
- markdown leaks;
- gaps the context could have filled.

## Goals / Non-Goals

**Goals:**
- Give every answer a confidence score, a green/yellow/red rating and an actionable comment, so a reviewer can open only the rows that need attention and know what to check.
- Judge each answer against the evidence it was actually generated from, not against Gemini's own beliefs.
- Add little wall-clock time to a run that already takes tens of minutes.
- Never lose or delay the answers because of the judge.
- Use the same Gemini compliance posture as analysis (pinned endpoint, env-only key), with judging on by default and an off switch.

**Non-Goals:**
- **Re-judging an existing results file.** The evidence is not persisted (see D2). Re-retrieving it later would judge an answer against context it never saw. That needs persisted provenance, which belongs to its own change.
- **Auto-correcting answers.** The judge advises; it never rewrites the answer text.
- **Running the judge on the chat backend.** Sonnet judging itself misses its own blind spots. An independent model family is the point.
- **Calibrated probabilities.** The score is an anchored ordinal rubric, not a probability of correctness.
- **Judging `chat` REPL turns.**

## Decisions

### D1. Judge per answer, in parallel, plus one cross-answer consistency pass, instead of a single all-at-once call

This is the question raised in the request. The evidence decides it.

The judge must read each answer's own retrieved context, or it can only judge plausibility, which is the mode in which Gemini earlier asserted unverified facts. That context is about 5–10K tokens per question. For a 192-row run that is 1–2M tokens, which does not fit one request.

Even without context, one call would put 192 verdicts and all of Gemini's reasoning under a single output ceiling. Gemini counts thinking tokens against that ceiling, and at high effort reasoning expands to fill the budget. A truncated response then loses every verdict at once. Long lists also invite skipped items, mismatched ids, and severity drift between the first and the last row.

Judging per answer instead gives:
- **Failure isolation:** one bad call costs one row.
- **A fresh reasoning budget per row.**
- **Parallelism.**

Its cost is that the rubric and system prompt are repeated in every call. They form an identical leading prefix, which is the shape Gemini's implicit prefix caching rewards, and they are small next to the evidence.

What per-answer judging cannot see is the relationship between rows. A single **consistency call** over all answers covers that, with the evidence omitted: about 130 tokens × 192 ≈ 25K input. It catches contradictions between rows, and nothing else. Rule violations are already reported on each row. On the first full run, re-reporting them here ("repeated markdown violation" on 10 rows) buried the contradictions and capped otherwise-clean rows. This is the only part of the design where seeing everything at once is an advantage.

Alternatives considered:
- **All at once:** rejected for the reasons above.
- **Micro-batches of about 10 answers per call:** halves the prefix overhead but still couples the rows' failures, and does not fit the evidence of 10 rows comfortably. It also complicates the mapping of verdicts back to rows.
- **Per answer only, with no consistency pass:** misses the cross-row contradictions that have already been found by hand.

### D2. Judge inside `RunBatch`, while the evidence is in memory, not as a later pass over the results file

`RunBatch` keeps each answer's `ragContext` and domain context only until that answer has been judged. The evidence never touches disk.

Running the judge inside `RunBatch` makes the CLI direct path and the daemon judge identically, since both already call `RunBatch`. It needs no change to the results file's size or sensitivity: 15 chunks × 192 rows of internal documents would otherwise land in a file that people pass around.

The user's flow ("Sonnet generates all the answers, then Gemini evaluates them") is preserved in what the reviewer sees. Internally, judging overlaps answering (D3).

Alternatives considered:
- **A standalone `answer judge results.json` command that re-retrieves the evidence:** judges against different evidence, because retrieval is not deterministic across KB edits and query rewrites.
- **Persisting the evidence in a sidecar file:** viable, and it would enable re-judging and provenance (the UI already reserves a Sources slot). It is deferred to a provenance change rather than smuggled in here.

### D3. Judging is pipelined with answering through a bounded worker pool

`RunBatch` keeps answering sequentially and unchanged. After each answer it enqueues a job for a pool of at most `judge.concurrency` workers. Each job carries the position, question, answer, evidence, domain context and local facts. Workers write verdicts into a slice indexed by position, so the results stay in manifest order.

When answering ends, `RunBatch` waits for the pool and then runs the consistency call.

Sonnet takes several seconds per answer, and a Gemini judgment takes a comparable time. With 4 workers the pool keeps up, so the judging phase after the last answer lasts roughly one or two judgments, not N.

Retries: a `QuotaError` or a 5xx response is retried up to 3 times, with exponential backoff and jitter starting at 2s. Truncated, blocked and malformed responses are not retried; their row is marked not judged with the typed error's message. Cancelling the context drains the pool.

Default concurrency is 4, which is conservative for paid-tier preview-model rate limits. It is configuration, because the right value depends on the operator's quota tier.

### D4. Package placement: the Gemini judge lives in `analysis`; `chat` defines the seam

`analysis` is the project's only Gemini integration, and it holds the compliance-critical pinning and error typing. A second Gemini client would duplicate exactly the code that must not diverge.

The judge therefore lives in `analysis`:
- `judge.go`: settings, the `Judge` type, `Judge` and `Consistency`;
- `judge_prompt.go`: the rubric and the turn rendering;
- `judge_schema.go`.

The package doc is widened to "Gemini-backed manifest analysis and answer judging".

`Client` gets a second constructor, `NewJudgeClient(JudgeSettings, verbose)`. It shares `newClient`'s SDK configuration and validates the judge keys instead of the analysis keys.

`analysis` already imports `chat`, for the manifest types and `CompileDomains`, so the dependency can only run that way:
- `chat` declares a small `AnswerJudge` interface: `Judge(ctx, JudgeInput) (Judgment, error)` and `Consistency(ctx, []ConsistencyInput) ([]ConsistencyFinding, error)`;
- `chat` declares the plain types those methods use;
- `*analysis.Judge` implements the interface directly, so no adapter is needed;
- the local checks (gap, markdown, counts) live in `chat/judge_checks.go`, because `RunBatch` applies them.

`RunBatch` takes the interface, and nil means no judging. Tests use a fake. `RunBatch`'s callers (the CLI and the daemon) already import `analysis`, so building the judge there adds no new edges.

Alternative considered: extract a shared `gemini` package used by both. That is cleaner long-term, but it means refactoring a package with about 2,000 lines of tests in a feature change. It is left as a follow-up if a third Gemini use appears.

### D5. Rubric: anchored 1–5 score, a derived rating, and a comment written before the score

LLM judges are poorly calibrated on 0–100 scales and cluster scores. An anchored 5-point scale with concrete descriptors is more stable, and it maps cleanly onto the reviewers' three buckets: 5 → green, 4–3 → yellow, 2–1 → red. The anchors are in the `answer-judge` spec.

The rating is computed locally from the score, never requested from the model, so the two cannot disagree.

The red/yellow line follows the reviewers' own buckets: red is wrong, invented, leaking another customer, or no answer; yellow is anything an edit or a check fixes. The prompt states that a problem fixed by deleting or rewording a phrase is never below 3, except naming another customer. The first probe needed that rule, because the judge put five rows in red for a single deletable sentence or a preamble.

"Never correct from your own knowledge" is paired with "do ask for checks". A figure, version, channel name or command example that looks malformed or implausible is flagged for verification at 3, even when the context states it. Without that pairing, the probe's judge flagged no claim at all, and passed both a malformed channel example and a support duration that a human had flagged.

The schema is built from a Go map, so its properties reach Gemini in alphabetical order: `comment`, then `confidence`, then `issues`. The model therefore writes what is wrong before it chooses a number. The thinking budget does most of this work already, and the ordering is cheap insurance.

The prompt states the rules:
- Judge only against the provided context and the system prompt's rules.
- Quote the offending phrase in the comment.
- Say what to do: remove, verify against `<source>`, or answer from `<source>`.
- Never supply a corrected fact from outside the context; ask the reviewer to verify instead.
- Treat naming another customer, bid, RFI or proposal as `other_customer`, even when the context contains it. The manifest prompt bans only inventing customer names, so grounded leaks pass it.
- Treat volunteered limitations or caveats as `rule_violation`.
- Enforce [CANONICAL]/[KAPA-CANONICAL]/[UPSTREAM] precedence as the source rules define it.

Schema caution: Gemini's structured-output subset has rejected `maxItems` before and has no `maxLength`. The schema therefore uses only the constructs that analysis already proved live: `type`, `enum` on strings, `properties`, `required`, and `minimum`/`maximum` on the integer, which the live test verifies. Everything is validated again locally. `confidence` outside 1–5, an issue that is not in the enum, or an empty comment with a confidence below 5 → the row is not judged.

### D6. Local checks before and around the model

The following are decided in code, because they are mechanical and models do them badly:
- **No-context gaps** are matched against `noContextAnswer`. When retrieval came back empty they get red with `gap` and no call, which saves cost and is exact. When the answering model wrote that string itself, context *was* retrieved: the answer is still judged so `missed_context` can be found, and the verdict is then forced red. The first live probe showed why: the judge rated two correct refusals 5/5, which is right about the answer and wrong for the reviewer, who has an empty cell to fill.
- **Markdown** is matched as `**`, `__`, backticks, a leading `#`, a leading `- ` or `* ` list marker, or a numbered list at line start. A match adds a `rule_violation` line to the comment and caps the confidence at 4.
- **Word and sentence counts** are passed to the judge as stated facts, and the judge compares them against whatever length rule the prompt states, which keeps the judge prompt manifest-agnostic. A leading declaration of up to three words ("Supported.") is reported as an opener and is not counted. On the first live probe, counting it caused 9 of 11 length flags, because the same prompt both requires the opener and caps the sentence count.

### D7. Two-stage write; the judge never blocks or loses the answers

The CLI direct path gets a new `BatchHooks.OnAnswered(out *BatchOutput)` hook, which fires once the answering loop ends. The CLI writes `batch-results-<ts>.json` there.

After judging, it rewrites the same file in place: it writes to a temporary file in the same directory and renames it over the original. A crash during judging therefore leaves the answers-only file, never a half-written one.

The daemon publishes results as today. During the judging phase it re-publishes `results` with the verdicts attached, and sets `phase`, `judged_done` and `judged_total`.

The remote CLI path (`runBatchRemote`) writes the file from the final operation metadata, as today, and gains the same summary printing.

### D8. Results shape

```json
{
  "generated_at": "…", "model": "global.anthropic.claude-sonnet-5-5", "prompt": "…",
  "judge": { "model": "gemini-3.1-pro-preview", "green": 136, "yellow": 32, "red": 24,
             "not_judged": 0, "consistency": true },
  "results": [
    { "id": "N.32", "question": "…", "answer": "…", "domain": "N*",
      "judgment": { "confidence": 2, "rating": "red",
                    "issues": ["other_customer"],
                    "comment": "Names \"the Customer A RFI\" and \"Customer B OpenStack proposal\"; remove both references." } },
    { "id": "N.33", "question": "…", "answer": "…",
      "judgment": { "error": "judge call failed: quota exceeded after 3 retries" } }
  ]
}
```

Both keys are `omitempty`. A reader of an older file, or a run with judging off, sees no change. Every result in a judged run gets a `judgment`, and a not-judged row gets an `error` instead.

The JSON stays nested rather than flattened into `confidence`/`comment` columns. The UI and any spreadsheet export can flatten it, and nesting keeps "not judged" unambiguous.

### D9. Configuration and gating

All keys are `package`-scoped and seeded by `install`. `post-refresh` also seeds them through its existing set-if-absent helper, so snaps installed before this change accept a `user` set.

| key | default | meaning |
|---|---|---|
| `judge.enabled` | `true` | Turns judging off for every run when false |
| `judge.model` | `""` | Gemini model id; empty → `analysis.model` |
| `judge.reasoning` | `medium` | Effort level, same parser as `analysis.reasoning.*` (`none` rejected) |
| `judge.concurrency` | `4` | Max in-flight judge calls; a positive integer |

Judging is **on by default**. The compliance agreement with Google for Gemini covers sending answers and knowledge-base chunks, which was confirmed on 2026-10-21, so the judge does not need an opt-in gate the way analysis did. `judge.enabled=false` turns it off for every run, and `--no-judge` or `judge: false` skips a single run.

`judge.enabled` is still a separate key from `analysis.enabled`. The two features are used independently, and an operator may want one without the other.

Being on by default has one consequence: an install with no `GEMINI_API_KEY`, or with no model configured, would hit the missing prerequisites on every run. That is reported as a single notice line naming what is missing and how to silence it (`judge.enabled=false`). The batch then runs unjudged and never fails because of it.

A flag-to-enable design, like `--analyze`, was considered and rejected, because the judge is meant to be the normal path.

Config is read through snapctl like every other key, so the feature is testable end to end only inside the installed snap. Unit tests use the existing fake `storage.Config`.

### D10. Secrets and snap

No new secret: `GEMINI_API_KEY`, from the environment only.

In direct mode the CLI's environment supplies it. In daemon mode it must be in ragd's service drop-in, as `snapcraft.yaml` already documents for analysis; that comment is extended to mention judging.

The snap needs no new plugs, interfaces, bundled binaries or hook logic beyond the four seeded keys.

### D11. Terminal summary

After a judged run, the CLI prints:

```
Judge (gemini-3.1-pro-preview): 136 green · 32 yellow · 24 red · 0 not judged
Rows are spreadsheet rows; row 2 is the first answer.

Red:
  row 34 [N.32]  Names "the Customer A RFI" …; remove both references.
  …
Yellow:
  row 12 [G6]    "12 years" of Ubuntu Pro coverage is not in the retrieved context; verify.
```

The summary uses spreadsheet row numbers, with the id kept for cross-checking. The same rows feed the UI filter.

## Risks / Trade-offs

- **[Risk] An install without a Gemini key or model sees a judge notice on every run.** → The notice is one line, names the fix, and points at `judge.enabled=false` for installs that will never judge.
- **[Risk] The judge is confidently wrong, especially rating a fabrication green, which is the costly direction.** → The anchored rubric, a separate model family, and evidence-based judging all push against it. Before trusting a green, validate on the hand-rated reference run (`batch-results-20261021-194634.json`, 136/32/24) and measure how many judge-greens were human-red. The task list includes this calibration step. If that miss rate is above a few percent, the reviewer keeps reading the greens until the rubric is fixed.
- **[Risk] The judge rejects grounded answers, because it is told to use only the context and reads paraphrase as invention.** → The prompt allows faithful paraphrase and synonym expansion and asks for the source chunk when it flags a claim. Calibration measures false reds as well.
- **[Risk] Rate limits on preview models stretch a run.** → Bounded concurrency, backoff and per-row isolation. The concurrency is configurable, and the cost of a slow judge is a later summary, never later answers.
- **[Trade-off] Cost.** About one Pro-tier call per answer with 5–10K input tokens, plus reasoning output. A full 192-row run is on the order of 1–2M input tokens. → `judge.model` can point at a cheaper tier, gaps are free, and the prefix is cacheable. The cost per run should be measured on the calibration run before a default model is recommended.
- **[Trade-off] Evidence is not persisted, so verdicts cannot be reproduced later.** → Accepted for now (D2). A provenance change can persist it, and it would unlock both re-judging and the UI's Sources slot.
- **[Risk] Schema constructs rejected live.** → `go test -tags live -run Live ./cmd/cli/basic/analysis/` gains judge-schema cases, which are run before the change ships, as was done for analysis.
- **[Risk] Changing `RunBatch`'s signature touches every caller** (the CLI, the daemon and the tests). → It is one new optional parameter, nil meaning off, and the hooks struct is additive.

## Migration Plan

1. Ship with `judge.enabled=true`. Runs on installs that have `GEMINI_API_KEY` and a model configured are judged from the first run after refresh. Other installs print the one-line notice and are otherwise unchanged.
2. For daemon runs, add `GEMINI_API_KEY` to the ragd drop-in, as for analysis. Optionally set `judge.model`.
3. Calibrate on the hand-rated reference run before reviewers rely on greens.
4. Rollback: set `judge.enabled=false`. Nothing is migrated, and files with verdicts stay readable by older UIs, which ignore unknown keys.

## Open Questions

1. ~~Compliance~~ — resolved 2026-10-21: the Gemini compliance agreement covers answers and KB chunks.
2. **Default judge model:** Pro or Flash tier? Decide from the calibration run's miss rate and cost.
3. **Should a rubric line live in the manifest?** RFP-specific rules, such as "never mention pricing", are already in the manifest prompt, which the judge reads. A separate `judge_instructions` manifest field is left out until a run shows the prompt is not enough.
