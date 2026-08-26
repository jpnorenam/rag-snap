# Design: add-answer-domain-routing

## Context

`chat.RunBatch` (`cmd/cli/basic/chat/batch.go:194-309`) is the single batch pipeline: the CLI's direct
path calls it via `ProcessBatchChat`, and the daemon calls it from
`internal/api/handlers_answer.go:183`. Per question it does four things — rewrite the question into
lexical keywords, merge in the manifest's per-question `keywords`, retrieve hybrid context, then
generate. The generation call builds exactly two messages: the batch system prompt and
`buildRAGPrompt(ragContext, q.Question)`, which formats `"Context:\n%s\n\nQuestion: %s"`
(`cmd/cli/basic/chat/rag.go:328-330`).

`q.ID` never enters either message. It exists only to stamp `BatchResult`. Operators writing ID→domain
routing tables into the manifest `prompt` are therefore writing instructions against data the model
cannot see. `questions[].source` is worse off: `rfp.Question` has the field and the extractor writes
it into every built manifest, but `chat.BatchQuestion` has no field to receive it, so it is dropped at
YAML decode. The `source` comment in `ui/lib/api/answer.ts:5-8` documents this drop as intended
behaviour.

Constraints shaping the design:

- **No config involvement.** Domain routing is manifest data. The snapctl-only backend and the
  `package` → `user` precedence model are untouched: no key is added, so nothing needs seeding in the
  install hook and nothing can hit the "user set rejects unknown keys" rule. Manifests are ordinary
  files read from the user's cwd, which strict confinement already permits for the existing
  `answer batch <manifest.yaml>` path.
- **No new secrets.** Nothing here is credential-bearing; `OPENSEARCH_USERNAME`,
  `OPENSEARCH_PASSWORD`, and `CHAT_API_KEY` remain the only secrets and remain environment-only.
- **No snap packaging change.** No new interface or plug, no bundled binary, no hook change — nothing
  in `snap/snapcraft.yaml` moves. Tika is not touched.
- **Four mirrors of the manifest type.** `chat.BatchManifest`, `batchManifestRequest`
  (`internal/api/handlers_answer.go:36-59`), `batchManifestJSON` (`cmd/cli/basic/answer_api.go:59`),
  and `ui/lib/api/answer.ts`. The `source` bug is what happens when one mirror is missed.
- **The UI parses manifests with a hand-rolled line parser.** `ui/lib/manifest.ts:18-123` handles
  top-level scalars plus a `questions:` block list. It ignores unknown top-level keys for
  forward-compatibility (`:79-81`) and skips indented lines outside `questions:` (`:97`), so a nested
  `domains:` block would be silently swallowed — the exact failure mode this change exists to remove.
  Feature parity between the CLI and the UI is a requirement here, so this parser has to change; see
  decision 8.
- **The UI screens this extends are in flight.** `add-ui-answer-batch` (the answer-batch screens) and
  `add-answer-build-column-selection` (the build wizard) are complete in the working tree but not
  archived. The UI work here is sequenced after them and its spec delta is additive only, so the two
  pending deltas are not disturbed.

## Goals / Non-Goals

**Goals:**

- Make the ID→domain mapping an exact, code-resolved lookup recorded in the output, replacing an
  inert prompt instruction.
- Let a domain contribute retrieval terms, so terse labels retrieve on more than their own two tokens.
- Stop dropping `questions[].source`, and give it a job: the fallback match key for bare numeric IDs.
- Keep the batch system prompt byte-identical across a run so it stays a cacheable prefix.
- Change nothing for a manifest without a `domains:` block.
- Reach full CLI/UI parity: the same manifest routes the same way whether the run starts on the command
  line or in the web UI, and the web UI shows which questions the routing reaches before the run and
  which domain applied after it.

**Non-Goals:**

- A `domains` *editor* in the web UI. The UI reads, previews, posts, and re-serialises routing — it does
  not offer form fields for composing entries. Authoring stays in the manifest file.
- Auto-inferring domains from the document at `--build` time. The stub lists observed prefixes; a human
  writes the prose.
- Per-domain knowledge-base selection, model overrides, or prompt overrides. Domains carry context and
  keywords only; widening them into a per-domain run configuration is a separate design.
- Reranking or retrieval-parameter changes beyond the keyword merge.

## Decisions

### 1. Resolve the mapping in Go, not in the model

Compile `domains` into a resolver and match each question before answering it.

*Alternative: pass the ID through and keep the routing table in the system prompt.* Rejected. It ships
a 15-row lookup table in every request, delegates a deterministic function to a sampler, and produces
no artefact proving which row was chosen — for a compliance matrix whose answers get audited, "the
model probably routed it right" is not a result. It also cannot help retrieval, which is where the
larger quality gain is.

*Alternative: one manifest per domain, each with its own `prompt`.* Rejected. It duplicates a ~50-line
prompt N times and guarantees drift on the first edit; it breaks a combined single-file run; and with
`prompt_ref` it multiplies the audited variant set by N, defeating the provenance the `prompt` result
field exists to provide.

### 2. Inject the domain into the question turn, not the system prompt

The resolved `context` and the question `id` go into the user message; the system prompt is built once
per run and reused verbatim.

Two reasons. **Cache economics:** the system prompt is the only prefix stable across a 200-row batch —
the RAG context differs per question, so nothing after it is cacheable. Folding a per-question domain
into the system prompt destroys the one cacheable segment on a metered backend such as Bedrock.
**Semantics:** a domain scopes one row; it is not a standing rule of the batch. The system prompt
holds rules (answer shape, grounding, gap handling); the turn holds this row's facts.

*Alternative: a second system message carrying the domain.* Rejected. OpenAI-compatible servers vary
in how they treat multiple system messages, and the `prompt_ref` provenance contract in
`rest-api-answer` reads as one resolved system prompt per run — keeping that literally true is worth
more than the marginal instruction-following gain.

Shape of the turn (domain resolved):

```
Context:
<retrieved chunks>

Requirement domain: <context>. Answer within this domain.

Question [C4]: SR-IOV
```

With no domain resolved, the middle paragraph is absent and the question line carries the id only when
one exists — so a manifest without ids or domains produces today's exact string.

### 3. Glob matching, longest-literal-count wins

`match` is a glob over `*` and `?`, compared case-insensitively (uppercase both sides, then
`path.Match`). Among matching entries, the winner has the most non-wildcard characters in its pattern;
ties break to document order; duplicate patterns are a validation error.

Literal count is the specificity metric because it gives the answers operators expect without
requiring them to order the list: `J1.*` (3) beats `J*` (1); `GIS*` (3) beats `G*` (1); `*` (0) always
loses. Ordering-dependent "first match wins" was rejected because it makes a correct manifest silently
depend on line order, and a later-added broad pattern can shadow a specific one.

*Alternative: plain string prefixes.* Rejected — expressive enough for `A*` but not for anything with
an interior wildcard, and it invites a second syntax later. *Alternative: regex.* Rejected — no
natural specificity ordering, and awkward to write and quote inside YAML.

`path.Match` is stdlib, so no dependency is added. Its `[...]` class support comes along for free; the
spec commits only to `*` and `?`.

### 4. `source` is the fallback key for bare numeric IDs

Match on `id` first. If a question's `id` is a bare sequence number (`^[0-9]+$`) and matched nothing,
retry against `source`. An `id` that matched is never re-matched.

This is not hypothetical: the extractor assigns `q.ID = fmt.Sprintf("%d", globalSeq)` when a row has no
identifier (`cmd/cli/basic/rfp.go:401-403`), and simultaneously records the sheet name in `source`.
Those manifests are exactly the ones where the ID carries no domain and the sheet name does.

### 5. Keyword precedence: question, then domain, then generated

`mergeKeywords` currently takes `(generated, manifestKWs)` and puts the manifest's first. It gains a
third tier rather than a second call, so dedup stays global and ordering stays in one place —
signature `mergeKeywords(generated string, leading ...[]string)`, called with the question's keywords
then the domain's.

Domain keywords also join the semantic query, because `batch.go:257-260` already derives the semantic
query from the merged lexical query. That is the intended existing behaviour ("user-provided keywords
like magnum influence embedding similarity, not just BM25 scoring") and inheritance rides it for free.

### 6. One compile choke point, validated before question one

`CompileDomains([]Domain) (*DomainSet, error)` is called at the top of `RunBatch`, so every caller —
CLI direct, daemon, future callers — validates identically and no path can skip it.
`LoadBatchManifest` calls it too, purely so the CLI reports a malformed block at load time instead of
after connecting to OpenSearch. The API handler surfaces the same error as a 400 before creating the
operation, matching how `prompt_ref` conflicts already fail fast.

A `nil`/empty list compiles to a `DomainSet` whose lookup always returns "no domain", so the
no-domains path costs one nil check and touches neither the prompt builder nor the merge.

### 7. Results identify the domain by its matched pattern

`BatchResult.Domain` holds the winning `match` string, omitted when nothing matched. A pattern is
self-describing in a way an index is not, needs no extra `name` field to maintain, and is unambiguous
because duplicate patterns are rejected. It sits beside the existing `BatchOutput.Prompt` provenance
field: together they answer "which prompt and which domain produced this cell".

### 8. The UI gets a real YAML reader, because parity demands it

Feature parity is a standing goal: a manifest must behave the same whether it is run from the command
line or the web UI. That makes the UI's manifest reader the enabling decision, because
`ui/lib/manifest.ts` is a hand-rolled line parser that cannot represent what a `domains:` block needs —
a nested block sequence of mappings, whose `context` will routinely be a block scalar and whose
`keywords` may be inline or a block list. Left alone it would swallow the block through its
unknown-top-level-key branch (`:79-81`) and skip the indented body (`:97`), reproducing the exact
silent-drop bug this change exists to remove.

**Decision: replace the parser with a YAML library** (`yaml`, eemeli/yaml — pure JS, browser-safe, no
native bindings), keeping the `ManifestParseError` type and message contract so the screens' existing
field-level error rendering (`ManifestParseError`, `ui/lib/manifest.ts:6`) is untouched. The hand-rolled parser's inline
validation (non-empty `question`, at least one question) moves into a typed validation layer over the
parsed document, which is where it belonged anyway; the API stays `parseManifest(text) → BatchManifest`
so callers do not change.

*Alternative: extend the hand-rolled parser.* Rejected. Nested mappings plus block scalars plus two
sequence forms is most of a YAML parser, hand-written, with no test corpus behind it — and it would
have to be extended again for the next manifest field. The parser's own header comment says it exists
to avoid a YAML dependency; that trade made sense for "top-level scalars plus a flat question list"
and stops making sense at nesting.

*Alternative: keep the parser and refuse `domains:` in the UI.* This was the previous plan and is
rejected under the parity requirement. It is honest rather than lossy, but it leaves the UI unable to
run a manifest the CLI runs, which is the thing parity forbids.

**Cost, stated plainly:** this adds the UI's sixth runtime dependency, to a project that has kept
them deliberately few (`next`, `react`, `react-dom`, `sass`, `vanilla-framework`). It is confined to
one route's client bundle. If keeping the count at five matters more than UI parity, the fallback is
the refusal design above — but then parity is not achieved and that should be an explicit choice.

**A defect this fixes as a side effect.** The line parser mishandles block scalars: for `prompt: |`,
`unquote("|")` yields the literal string `"|"` and the indented body is skipped as an unknown indented
region, so a manifest whose custom prompt is a block scalar currently uploads to the web UI with its
prompt replaced by one pipe character. Every prompt-bearing manifest in this repo's working tree uses
that form, and no test covers it (`ui/lib/manifest.test.ts` exercises only inline values). Under the
previous refuse-only plan this was out of scope; a real reader fixes it, so the parity work should be
credited with closing it — and a regression test for it belongs in the same phase.

### 9. The preview shows per-pattern match counts, and the server stays authoritative

The pre-run preview shows each domain entry's pattern, context, and **how many questions it would
apply to**, plus which questions match nothing. The counts are the point: an operator reads
"`C*` → 12 questions, `J2.*` → 0 questions" and catches a typo before spending a run, which is the
one thing a preview can do that the results file cannot do earlier.

This means resolution exists twice — Go for the run, TypeScript for the preview — which is a real
divergence risk. Mitigations, in order of importance: the preview is **advisory** and labelled as
such, the run's recorded `domain` is the authority; and both implementations are tested against **one
shared fixture** of `(patterns, id, source) → expected match` cases, so a change to the algorithm that
updates only one side fails the other's tests. A preview that merely listed the block without counts
would avoid the duplication, but it would also avoid being useful.

### 10. Stub emission is comment text, not schema — from both writers

`rfp.WriteManifest` already writes a header comment before handing the file to `yaml.Encoder`
(`cmd/cli/basic/rfp/extractor.go:511-521`). The stub is more comment lines in that gap, listing the
distinct id prefixes and `source` values observed. Nothing is added to `rfp.Manifest`, so a built
manifest still round-trips through the encoder unchanged and an unedited stub cannot affect a run.

The UI's `serializeManifest` emits the same stub from its build wizard. Under the refuse design this
would have been a trap — a surface emitting a block it then rejects — but with the UI reading
`domains` the asymmetry disappears and a manifest built in either place is equally ready to route.
`serializeManifest` also gains block-scalar output for multi-line values, since a domain `context` and
a custom `prompt` both want it and the current `yamlScalar` folds a multi-line value into a
double-quoted scalar with raw newlines.

## Risks / Trade-offs

**Replacing the UI's manifest parser is the riskiest part of this change, and it is not the part the
change is about.** The parser sits on the path of every batch run started in the UI, so a regression
there breaks manifests that have nothing to do with domains. → It lands as its own phase, after the Go
core is proven, with the existing `ui/lib/manifest.test.ts` cases kept as-is (they become the
regression suite for the swap) plus new cases for block scalars, nested sequences, both sequence forms,
and unknown-field tolerance. The `parseManifest` signature and `ManifestParseError` contract do not
change, so no caller or screen is touched by the swap itself.

**A sixth UI dependency in a five-dependency project.** → Confined to the manifest module; pure-JS and
browser-safe, so it adds no build machinery. The alternative — hand-extending a parser already shown to
be lossy — is worse. This is the one decision in the change worth vetoing outright, and vetoing it
means giving up UI parity rather than getting it more cheaply.

**Two resolvers can drift.** Go resolves for the run, TypeScript resolves for the preview; a change to
specificity or the `source` fallback could be applied to one and not the other, and a preview that
lies is worse than no preview. → The shared fixture in decision 9 is the guard, plus the preview being
labelled advisory and the result's `domain` being the record. If the fixture proves awkward to share
across languages, the fallback is to drop match counts from the preview rather than to keep an
untested second resolver.

**Four Go type mirrors mean a fifth surface could drop `domains` the way `source` was dropped.** →
Add a round-trip test that decodes a manifest carrying `domains` and `source`, marshals it to the API
request shape, converts back via `toManifest`, and asserts both survive. That test is what makes the
"no surface silently discards" requirement enforceable rather than aspirational.

**Longest-literal-count is a heuristic, and an operator could still write two patterns that tie.** →
Ties resolve to document order, which is deterministic and documented; identical patterns are rejected
outright. The `domain` field in the results makes a surprising resolution visible on the first run
rather than after the audit.

**Injecting the domain changes the model input for every question in a manifest that adopts the
block.** Answers will differ from prior runs — that is the point, but it invalidates comparison
against earlier result files. → Backward compatibility is at the *manifest* level, not the answer
level: existing manifests are untouched until someone adds a `domains:` block. Adopting the block
domain-by-domain and diffing answers is the migration path below.

**Domain keywords over-steer retrieval.** A broad domain contributing many terms could pull BM25
toward the domain vocabulary and away from a question's own specifics. → Question keywords lead the
merged list, and domain keywords are opt-in per entry: an entry can carry `context` alone. Guidance in
`docs/usage.md` should say keep domain keywords few and domain-defining.

**Prompt-cache benefit is unverified.** Decision 2 is partly justified by keeping the system prefix
cacheable; the actual saving depends on the backend honouring prefix caching for this shape. → The
decision stands on its semantic argument alone, so a cache that never materialises costs nothing.

## Migration Plan

1. Land the Go core (types, `CompileDomains`, resolution, prompt and keyword wiring, results field)
   with unit tests. No behaviour changes for existing manifests, so this is safe to ship on its own.
2. Extend the three transports plus `rest-api.yaml`, with the round-trip test.
3. Add the CLI `--build` stub emitter.
4. Update `docs/usage.md`: `domains` and its precedence in the YAML schema table, `questions[].source`,
   the stub in the `--build` section, and the `questions[].keywords` field the table omits today.
5. `make all`, then build and install the snap and run a real manifest — the batch path needs snapctl
   for config and live OpenSearch and inference endpoints, so this cannot be validated by `go test`
   alone. **The CLI is fully usable at this point**; the UI phases below are additive parity work and
   do not gate it.
6. Adopt incrementally on a real matrix: move one domain out of the prompt table into `domains:`, run,
   and diff answers against the previous results file. The `domain` field in the output confirms the
   rule fired.

UI parity phases, landing after `add-ui-answer-batch` and `add-answer-build-column-selection` are
applied (they own the screens and the build wizard this extends):

7. Swap `ui/lib/manifest.ts` onto a YAML reader behind the unchanged `parseManifest` API, keeping the
   existing tests green and adding the block-scalar, nested-sequence, and unknown-field cases. Ship
   this on its own — it fixes the `prompt: |` defect and changes no other behaviour.
8. Carry `domains` and `source` through the TS manifest type and the posted body, add client-side
   validation matching `CompileDomains`, and add the TS resolver against the shared fixture.
9. Add the preview routing summary with per-pattern match counts and unrouted questions.
10. Show the applied `domain` on result cards, and emit routing plus the stub from `serializeManifest`
    and the build wizard.
11. Run the `ui-conventions` pass over the new affordances — both themes, keyboard-only, `--vf-*`
    tokens, all four view states — then verify parity directly: run one manifest from the CLI and the
    same manifest from the UI and confirm the recorded domains agree.

**Rollback:** delete or comment out the `domains:` block. Compiled behaviour reverts to today's
prompts and queries with no code change, because the no-domains path is the untouched path.

## Open Questions

- Should a resolved domain's `context` also reach the keyword-rewrite call
  (`rewriteSearchQuery`, `batch.go:250`) so the rewriter knows the domain when expanding a two-word
  label? Plausibly a further retrieval gain, but it adds a second place the domain influences
  behaviour and a second thing to explain. Deferred: ship keyword inheritance first and measure
  before widening.
- Is `Requirement domain: …` the best turn wording for the injected paragraph, given operators'
  system prompts already say "answer within that domain"? Worth one A/B on a real matrix before the
  string is treated as settled.
- Where should the shared Go/TS resolution fixture live, and in what format? A JSON file read by both
  `go test` and the `tsx --test` suite is the obvious answer, but the repo has no precedent for a
  cross-language fixture and the root `.gitignore` excludes `*.json` broadly — so the path needs
  checking with `git check-ignore` and probably an explicit un-ignore. Decided during phase 8, not
  before.
