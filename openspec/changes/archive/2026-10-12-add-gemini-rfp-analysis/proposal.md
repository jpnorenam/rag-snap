## Why

Turning an RFP into a runnable batch manifest is today the most expensive manual step in the whole
workflow. `answer batch --build` automates only the mechanical half — Tika extraction plus regex
question detection — and deliberately leaves everything semantic to the operator: the RFP-level
`prompt` (the hand-written `Telekom_Malaysia_prompt.txt` and `Weird_RFP_prompt.txt` in this repo run
50–83 lines each), the `knowledge_bases` selection, the `kapa_source_groups`, and the `domains` block,
which `--build` emits only as a commented-out stub of candidate patterns. A manifest like
`MeitY_RFP.yaml` or `Telekom_Malaysia_0_Full.yaml` costs hours of reading and drafting per RFP, and
that cost is paid again for every new bid.

A document-capable frontier model can read the RFP directly and produce all four parts in one pass.
The blocker has never been the model — it is that the model has no idea what rag-cli is, what a
manifest means, which knowledge bases exist, or how domain routing resolves. Supply that context and
the step collapses from hours to minutes.

## What Changes

- **A new LLM analysis mode on the existing build path.** `answer batch --build <document>` gains an
  `--analyze` mode that sends the RFP — plus any number of `--context <file>` supporting documents —
  to a document-capable model and writes a *complete* manifest: `questions`, `knowledge_bases`,
  `kapa_source_groups`, a single RFP-level `prompt`, and a populated (not stubbed) `domains` block.
  The existing Tika+regex `--build` behaviour is untouched and remains the default.

- **Documents reach the model without losing content.** PDFs are sent as raw bytes so the model reads
  layout, tables and figures natively. Office formats are converted only as far as they must be, to a
  table-preserving intermediate rather than flattened plain text — the columns that carry a
  requirement's description, current-environment note and evaluator guidance are exactly what makes a
  good manifest, and plain-text extraction is what destroys them.

- **The model is grounded in rag-cli's actual semantics.** The request carries the manifest contract,
  the domain resolution rules (glob matched against `id`, falling back to `source` for bare numeric
  ids, most specific pattern wins), worked examples drawn from real manifests, and — critically — the
  **live** catalogue of knowledge bases and kapa source groups read from the running services, so the
  model selects real values instead of inventing plausible ones. Kapa source groups are opaque UUIDs
  and cannot be guessed at all.

- **A two-phase flow, mirroring the existing build wizard.** Phase one uploads the documents and
  returns a cheap triage: detected RFP identity, section structure, question-count estimate, whether
  OpenStack is in scope, and whether questions carry multiple answer fields. Phase two takes the
  operator's answers to a short interview and generates the manifest from the documents already
  uploaded. This reuses the `answer/build` → `answer/build/extract` two-pass pattern and its staging
  store, and it is what lets the OpenStack question be asked *only when the RFP actually requires it*.

- **A short operator interview whose answers become manifest content.** Desired answer length in words
  (default 100); whether that length is a hard limit that must never be exceeded or a soft target that
  may be slightly exceeded; and, when triage found OpenStack in scope, whether to present Sunbeam or
  Charmed OpenStack.

- **Non-negotiable rules baked into every generated prompt.** That it is better to answer "no context
  available" or state that a technology is NOT covered than to give a false or fabricated answer; and
  that answers are direct, with no filler or preamble. These are generated *into* the manifest's
  `prompt`, and complement the existing hard guarantees they must not contradict — the fixed
  no-context answer emitted when retrieval returns nothing, and the source rules appended to every
  custom prompt.

- **A manifest check.** `answer batch --check <manifest>` validates a manifest without running it or
  calling any model: question and domain counts, domain patterns that match no question (the silent
  routing failure this repo has already hit), duplicate ids and duplicate match patterns, knowledge
  bases and kapa source groups that do not exist, and whether the prompt still carries the no-context
  rule and a stated word budget. It works on any manifest, including the hand-written ones.

- **The same flow in the browser UI.** The answer screen's build wizard gains the analysis path:
  upload the RFP and its context files, review triage, answer the interview, review the generated
  manifest, then run or download it.

- **New daemon endpoints** for the two phases and the check, exposed as tracked async operations like
  the existing build endpoints.

Not a breaking change: every existing manifest, command, flag and endpoint behaves exactly as before.

## Capabilities

### New Capabilities

- `answer-manifest-analysis`: Generating a complete batch manifest from RFP documents with a
  document-capable LLM — how documents are conveyed without fidelity loss, how the model is grounded
  in the manifest contract and the live knowledge-base and kapa catalogues, the two-phase triage and
  generation flow, the operator interview, the rules baked into the generated prompt, and the
  guarantees on the emitted manifest.
- `answer-manifest-check`: Validating a batch manifest without running it — structural integrity,
  routing reachability, resolvability of referenced knowledge bases and kapa source groups, and
  prompt-content assertions. Read-only, no model calls.

### Modified Capabilities

- `rest-api-answer`: Adds the analysis triage and generation endpoints and the check endpoint, as
  tracked async operations carrying their results on operation metadata.
- `local-ui-app`: Adds the analysis path to the answer screen's build wizard — document and
  context-file upload, triage review, the interview form, generated-manifest review with check
  results surfaced.

## Impact

**External services.** No new *kind* of dependency: analysis speaks to an **OpenAI-compatible
inference endpoint**, the same protocol the chat backend already uses, reached with the client already
in the tree. What is new is that the endpoint must be document-capable, which a local inference server
is not — so analysis takes its own endpoint configuration that falls back to the chat backend when
unset. The existing three services are affected as follows: **Tika** gains a new caller for
table-preserving conversion of Office formats on the path to the model, because no model API accepts a
spreadsheet; **OpenSearch** is read-only here, enumerating knowledge bases for the catalogue and the
check; the **inference server** is untouched as the answering backend, and analysis never runs through
the RAG answering path.

**No new Go dependency.** The vendored `openai-go/v3` already provides everything needed: a file
content part for the PDF, JSON-schema response format, an explicit output-token ceiling, per-request
reasoning effort, and the per-choice finish reason.

**New secret.** `ANALYSIS_API_KEY`, falling back to `CHAT_API_KEY` when unset, passed by environment
variable and never config — consistent with `OPENSEARCH_USERNAME` / `OPENSEARCH_PASSWORD` /
`CHAT_API_KEY`. An operator whose chat backend is already document-capable needs no second key.

**New config keys**, all seeded as `package` keys by the install hook so a `user` set can override
them: whether analysis is enabled, the model id, the analysis endpoint (empty meaning "use the chat
backend"), the reasoning effort for each of the two model calls, and the interview defaults for answer
length and hard-versus-soft.

**Code.** `cmd/cli/basic/answer.go` and a new sibling for the analysis path; `cmd/cli/basic/rfp/`
gains the manifest-generation and check logic plus embedded contract and example assets;
`cmd/cli/basic/processing/tika.go` for table-preserving conversion; a new analysis package wrapping
the existing client; `internal/api/` handlers, routes, a staging store and feature flags;
`ui/components/answer/BuildWizard.tsx` and `ui/lib/api/answer.ts`.

**Snap.** No new interfaces or plugs — outbound HTTPS and `home` file access are already used by the
kapa and build paths — and no new module, so no packaged size change. The install hook seeds the new
package config keys.

**User-facing surfaces and documentation.** New flags (`--analyze`, `--context`, `--check`) on
`answer batch`, new daemon endpoints, and a new UI path. Requires updates to `docs/usage.md`,
`docs/rest-api.md`, the `rest-api.yaml` swagger source, `apps/completion.bash`, and the command's
`--help`/usage text.
