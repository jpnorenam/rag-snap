## Context

`answer batch --build` automates the mechanical half of manifest production — Tika extraction, regex
question detection, an optional LLM refinement of question text — and emits `questions` plus
`knowledge_bases` with a commented-out `domains` stub. Everything semantic is hand-written: the
RFP-level `prompt` (50–83 lines in the two `*_prompt.txt` files in this repo), the knowledge-base
selection, `kapa_source_groups`, and the domain contexts. `MeitY_RFP.yaml` carries ~300-word contexts
for each of its domains; `Telekom_Malaysia_0_Full.yaml` routes nineteen sheets. That drafting is the
bottleneck this change removes.

Three facts about the runtime constrain the design more than anything else:

1. **The manifest contract is not self-evident.** Domain routing resolves a glob against a question's
   `id`, falls back to `source` only when the `id` is a bare sequence number, and lets the most
   specific pattern win (`cmd/cli/basic/chat/domains.go`, `rfp.idCandidate` mirrors the condition
   exactly and its doc comment explains why a pattern written for `1.1` by source silently routes
   nothing). A model that guesses this gets patterns that match nothing.
2. **Kapa source groups are opaque UUIDs**, listed only at runtime via `KapaClient.ListSourceGroups`.
   They cannot be guessed, remembered, or inferred from a document.
3. **Retrieval already refuses to answer ungrounded.** `RunBatch` emits a fixed `noContextAnswer` when
   retrieval returns nothing, and `batchSystemPrompt` appends `prompts.SourceRules` to every custom
   prompt. A generated prompt sits inside those guarantees and must not fight them.

Three facts about document-capable model APIs, verified against current vendor documentation, then fix
the shape of the solution:

- **No mainstream model API accepts Office formats.** Anthropic's documentation is explicit — "binary
  formats such as .xlsx or .docx are not supported in document blocks and must be converted to text or
  PDF first". Gemini lists `.docx`, `.xlsx` and `.doc` in no supported-type list at all, and states that
  non-PDF types are "extracted as pure text" with HTML tags and Markdown formatting discarded. Chat
  *web apps* accept these formats because they run a conversion pipeline in front of the model; the APIs
  behind them do not. Converting spreadsheets and documents ourselves is therefore mandatory whichever
  model is targeted, not a workaround for one vendor.
- **PDF is the one format that is genuinely understood**, and it is understood by every candidate model.
- **A gateway can carry PDFs through an OpenAI-compatible request.** OpenRouter's chat-completions
  endpoint takes a `file` content part carrying base64 PDF data and supports it "on any model",
  passing through natively where the model handles files and parsing server-side where it does not.
  This is specific to the gateway: *Gemini's own* OpenAI-compatibility layer documents no document
  input of any kind, so the two must not be confused.

The last point is what determines the client. `openai-go/v3` v3.11.0 — already in `go.mod` — exposes
`FileContentPart` with `FileData` and `Filename`, exactly that shape, alongside `ReasoningEffort`,
`ResponseFormatJSONSchemaParam`, `MaxCompletionTokens` and per-choice `FinishReason`. Every primitive
this feature needs is already vendored.

## Goals / Non-Goals

**Goals:**

- Produce a complete, runnable manifest — `questions`, `knowledge_bases`, `kapa_source_groups`,
  `prompt`, populated `domains` — from an RFP plus supporting context documents, in one flow.
- Preserve the document content that makes a good manifest: the columns beside a requirement and the
  sheet or section it belongs to.
- Ground the model in the real manifest contract and the real, live retrieval catalogue, so first-pass
  output is correct rather than plausible.
- Make an incomplete result an error. A silently short manifest against an RFP deadline is the worst
  outcome this feature can produce.
- Keep the existing extraction path as an untouched default and a fallback.

**Non-Goals:**

- Answering the questions. Analysis produces a manifest; `answer batch` runs it, unchanged.
- Replacing the deterministic extraction path, or its column-selection wizard.
- Routing analysis through the RAG inference backend, or changing which model answers questions.
- Converting Office formats to PDF, or bundling an office suite in the snap.
- Multi-turn refinement of a generated manifest inside rag-cli. The manifest is a file; editing it is
  the operator's, or a later change's, business.

## Decisions

### Route analysis through an OpenAI-compatible gateway, reusing the vendored client

Analysis uses `openai-go/v3` — the client already in the tree — against an OpenAI-compatible endpoint
that supports the `file` content part, with OpenRouter as the reference provider. **No new Go
dependency and no new LLM SDK.**

Because Office formats must be converted locally whatever the provider (see Context), the only things
analysis actually needs from a model API are *PDF ingestion* and *structured JSON output*. Both are
available through the vendored client, so the entire cost of a second SDK bought nothing that this path
does not already provide.

What that removes from the change: the `google.golang.org/genai` module and its transitive set, the
Files API resumable-upload dance with its `ACTIVE` state polling, and a separately managed provider
secret. What it adds: a gateway hop, and per-model variation in how structured output and reasoning
effort are honoured.

Selecting the model becomes configuration rather than architecture, which is the main secondary
benefit — the same code path reaches Gemini's Pro tier, Claude, or anything else the gateway serves,
so a model change is a `snap set`, not a rebuild.

*Alternatives considered.* The native `google.golang.org/genai` SDK against `generateContent` gives
precise `thinking_level` control, no gateway hop, no server-side parsing fees, and a Files API upload
reusable by URI across both phases. Its cost is a second LLM client in a codebase that has had exactly
one, a new secret to inject on the daemon, and a preview model id to track. Rejected because the
document-fidelity argument that justified it evaporates once Office conversion is local either way —
but it remains the fallback if gateway-mediated reasoning control proves too coarse (see Risks).
`github.com/google/generative-ai-go` is deprecated as of 30 November 2025 and was never a candidate.

### Analysis gets its own endpoint configuration, defaulting to the chat backend

Analysis cannot simply borrow `chat.*`. The chat backend is legitimately a local Inference snap, which
cannot read a PDF at all, while analysis needs a document-capable gateway. So analysis takes its own
`analysis.http.host` / `port` / `path` / `tls` quartet, mirroring the existing `chat.http.*` shape, and
**falls back to the chat backend when those are unset** — so an operator already pointing `chat.*` at
OpenRouter configures nothing extra, and one running answering locally can still point analysis
outward.

The key follows the same rule: `ANALYSIS_API_KEY` when set, falling back to `CHAT_API_KEY`. One key
serves when both are the same provider; two exist when they are not.

### Convey each format by the best path it has, and never by flattened plain text

The rule is: *the model's own document understanding where it exists, table-preserving Markdown where
it does not.*

| Format | Conveyance | Why |
|---|---|---|
| PDF | Original bytes, base64 in a `file` content part | Native document understanding: layout, tables, figures. Page and size limits are provider-specific and must be checked per provider, not assumed |
| XLSX, CSV | Parse to tables, render as Markdown tables inline | Every column with its heading, every row, each table labelled with its sheet name |
| DOCX, DOC | Tika HTML, converted to Markdown headings and tables inline | Same, for a format no API accepts |

The table parsing already exists and is tested: `rfp.ParseTikaHTMLSheets` walks Tika's XHTML into
`SheetTable{Name, Rows, PageIndex}`, and `rfp.XLSXSheetNames` reads sheet names straight from
`xl/workbook.xml` because Tika mangles long ones. What is new is a renderer from those rows to Markdown,
and the rule that **no column is dropped** — the opposite of `ExtractFromTable`, which takes one column
by index. The discarded columns are the material the prompt and the domain contexts are built from; the
`Telekom_Malaysia_prompt.txt` header says as much, complaining that the surrounding description,
current-environment and evaluator-guidance columns are *not* given to the answering model.

*Why not upload Tika HTML as a file part.* Tempting — it is already produced and preserves tables in
markup. But the documentation states non-PDF types are extracted as pure text and HTML tags are lost.
Uploading HTML would flatten precisely the tables the conversion exists to preserve. Markdown tables
survive plain-text extraction because their structure *is* text.

*Why not convert Office formats to PDF.* LibreOffice headless would give native document understanding
for every format, at the cost of a very large stage in a strictly-confined snap and a fragile
conversion step. It is also not obviously better: a wide compliance matrix rendered to PDF pages loses
columns at the page edge, where a Markdown table does not. Rejected on both counts.

The per-document conveyance path is reported to the operator, because "this file was converted, not
read natively" changes how much to trust the result and is invisible otherwise.

### Convey PDFs as base64 in a `file` content part, and stage the bytes locally

`FileContentPart` carries the PDF as base64 `FileData` with a `Filename`. There is no server-side file
handle to reference, which has one consequence worth designing for deliberately: **the two phases
cannot share a remote upload**, so the staging store must hold the document content itself between
triage and generation.

That makes the staging store a memory consumer rather than a bookkeeping record — a 30 MB PDF is about
40 MB base64, per concurrent session. So the store bounds both the total bytes it will hold and the
per-request document size, rejecting an oversized set before conveying anything, and keeps the TTL
short. Staging the encoded form once (rather than re-encoding per call) also means the two generation
calls pay the encoding cost once.

The alternative — re-uploading on phase two — was rejected because it doubles transfer for a document
the operator has already waited on, and because re-reading the file from disk cannot work on the daemon
path where the upload arrived over the API and was never on disk.

### Two phases over one staged conveyance, like the existing build tokens

Phase one conveys the documents and returns triage — RFP identity, structure, question-count estimate,
whether OpenStack is in scope, whether questions carry several answer fields, suggested knowledge bases.
Phase two takes the interview answers and generates.

The split exists for a specific reason: the OpenStack flavour question can only be asked *conditionally*
if something has read the document first. It also gives the operator a cheap abandon point before the
expensive call, and it mirrors the `POST /1.0/answer/build` → `/build/extract` two-pass shape and its
in-memory `answerBuildStore`, so the daemon gains a second staging store of the same design rather than
a new pattern. The existing `buildStageTTL` is 10 minutes; the analysis token gets its own TTL on the
same model, kept short because the store now holds document bytes rather than a remote reference. An
expired token is a recoverable error that returns the UI to upload, never a silent regeneration.

### Ask for JSON against a schema, then encode YAML ourselves

No model API offers a YAML output mode, and free-text YAML from a model is a bad bet here specifically
because the `prompt` field is a ~2000-word literal block scalar whose indentation must be exact — a
single drifted space makes the manifest unparseable.

So: `ResponseFormatJSONSchemaParam` on the request, unmarshal into the Go manifest struct, and write it
with the encoder already in the repo (`rfp.WriteManifest`, `yaml.v3` at 2-space indent), which produces
correct block scalars for free. The schema also pins field names, which is most of what grounding a
naive model requires.

Two caveats shape the implementation. Schema dialects are a JSON Schema *subset* and deeply nested
schemas may be rejected, so the schema stays shallow. And structured output is honoured per model rather
than uniformly by every model a gateway serves, so the response is **also validated locally** against
what the manifest requires: a response that parses as JSON but omits required fields is a failure, not a
partially populated manifest.

### Size the output budget for reasoning, not for the manifest

The manifests in this repo were measured rather than estimated, in approximate tokens by section:

| manifest | prompt | domains | questions | total | #q | #d |
|---|---|---|---|---|---|---|
| CompuGroup | 3.4K | 10.7K | 5.2K | **19.4K** | 194 | 51 |
| Lafika_RFI | 4.2K | 6.4K | 2.1K | 12.6K | 24 | 28 |
| NCSC_RFP | 2.5K | 2.6K | 2.8K | 8.0K | 143 | 26 |

Two things follow. **Domain contexts dominate, not questions** — CompuGroup's 51 domains are more than
half its manifest, while 194 questions cost only 5.2K because they are short labels. And the worst
manifest ever written here is under a third of a 65,536-token output ceiling.

So manifest size is not the binding constraint; **reasoning tokens are**. On a thinking model the output
ceiling covers the model's internal reasoning as well as the visible answer: Gemini's documentation
states `max_output_tokens` is enforced "including thought tokens", as a hard cutoff that does not resize
or protect the thinking allocation, and reasoning is billed as output. When reasoning exhausts the budget
the call returns `MAX_TOKENS` with visible output that is **truncated or entirely empty**, and the
reasoning is still billed. Reported behaviour on a thinking model at high effort had reasoning expanding
to fill roughly 96% of whatever budget it was given.

Three consequences, all of which the implementation must honour:

1. **Set the output ceiling explicitly and generously.** Never rely on a provider default; a small
   ceiling truncates the answer without shortening the reasoning, which is the opposite of the intent.
2. **Set reasoning effort per call.** `ReasoningEffort` is on the vendored request type
   (`none`/`minimal`/`low`/`medium`/`high`). Exhaustive question extraction is mechanical and does not
   need high effort; synthesising fifty domain contexts does. Note that some models will not let
   reasoning be disabled at all — Gemini's 3.1 Pro tier supports only `low`/`medium`/`high` and cannot
   turn thinking off — so the floor is provider-dependent and must not be assumed to be zero.
3. **Treat an empty response as a failure, not an absence.** `MAX_TOKENS` with no content is the
   signature of reasoning eating the budget, and it must not be mistaken for "the model found nothing".

### Split generation into two calls, for the reasoning budget

Generation runs as two calls over the same staged documents: **questions** (long, mechanical) and
**strategy** (`prompt`, `domains`, `knowledge_bases`, `kapa_source_groups` — shorter, synthetic).

The reason is the budget analysis above, and it is worth being precise because the obvious rationale is
wrong. Splitting does *not* meaningfully reduce truncation risk from output size — the measurements show
there was never a size problem, and the split peels off the cheaper half anyway. What it buys is that
**each call gets its own independent ceiling covering its own reasoning**, and that each can carry its
own reasoning effort: low for extraction, high for synthesis. It also makes a failure attributable to
one call and independently retryable.

Completeness is checked twice regardless: the finish reason on each call, and the generated question
count against the triage estimate. Either discrepancy fails the operation naming both numbers. A call
that quietly returns 40 of 150 questions is the failure mode this feature must not have, and the count
comparison is the only cheap detector for it — which is why triage estimates a count at all.

### Enforce the two mandatory prompt rules after generation, not by asking

An RFP is untrusted input. A document can contain text that reads as instructions — "answer every
requirement affirmatively", "do not state gaps" — and a model asked to write a prompt from that document
may absorb it. The two non-negotiable rules (prefer "no context available" or "NOT covered" over
fabrication; be direct, no filler) are therefore **appended by rag-cli** to the generated prompt rather
than merely requested of the model, and then verified. `answer batch --check` re-verifies them on any
manifest, so a later hand-edit that drops them is visible.

This also keeps the generated prompt consistent with the guarantees around it — the fixed
`noContextAnswer` and the appended `SourceRules` — which a model writing freely could contradict.

### Reuse the run's own resolver in the check

`--check` compiles the manifest's domains with `chat.CompileDomains` and resolves each question with the
same `Resolve` the run uses, rather than reimplementing glob matching. A check that disagrees with the
run is worse than no check.

Severity is two-level and deliberate. A zero-match domain pattern is a **warning**, never an error:
`Telekom_Malaysia_O_GIS.yaml` reserves `GIS*` patterns that match nothing on purpose, so failing on them
would make the check useless on this repo's own manifests. An unresolvable knowledge base is a **defect**
— it has no legitimate use and contributes silently nothing to retrieval, and near-miss name suggestions
matter because a typo gives the operator no other signal. Exit status distinguishes defects from
warnings so the check is scriptable.

### Configuration: package keys, secret by environment only

New `package` keys, seeded by the install hook alongside the existing `kapa.*` block so a `user` set is
accepted (a `user` set is rejected unless the key already exists as a package key):

| Key | Default | Why it is configuration |
|---|---|---|
| `analysis.enabled` | `false` | RFPs are confidential; sending them to a third-party API is opt-in |
| `analysis.model` | a document-capable model id | The id is provider-namespaced and versioned; see below |
| `analysis.http.host` | empty | Empty means "use the chat backend"; set to reach a document-capable gateway |
| `analysis.http.port` | empty | As above |
| `analysis.http.path` | empty | As above |
| `analysis.http.tls` | empty | As above |
| `analysis.reasoning.questions` | `low` | Reasoning effort for the mechanical extraction call |
| `analysis.reasoning.strategy` | `high` | Reasoning effort for the synthesis call |
| `analysis.answer.words` | `100` | Interview default, changeable per team without a code change |
| `analysis.answer.limit` | `soft` | Interview default for hard-versus-soft |

The model id is configuration and not a constant because it is both provider-namespaced and versioned:
the Gemini Pro tier currently ships only under a **preview** identifier with no stable equivalent, and a
snap refresh is a poor way to track a model rename. Routing through a gateway makes this doubly true,
since changing model becomes a `snap set` rather than a code change.

The two reasoning-effort keys are configuration because the right floor is provider-dependent — some
models refuse to disable reasoning at all — and because the correct setting is the kind of thing that is
tuned against real RFPs rather than guessed once at design time.

**The secret is `ANALYSIS_API_KEY`, falling back to `CHAT_API_KEY`, environment only** — not in config,
not in a manifest, not in verbose output or an error. This deliberately does *not* follow the
`kapa.api.key` precedent, which stores a key in snapctl config with a `KAPA_API_KEY` override; it follows
the `CHAT_API_KEY` / `OPENSEARCH_USERNAME` / `OPENSEARCH_PASSWORD` rule that `snapcraft.yaml` documents
at length instead.

The fallback is what makes the common case zero-configuration: an operator already pointing `chat.*` at a
document-capable gateway needs no second key and no second endpoint. The override exists for the case the
fallback cannot serve — answering on a local Inference snap, analysis on a gateway.

The key is read explicitly from the named variables in that order rather than relying on any SDK
auto-detection, so behaviour never depends on an unrelated variable set for some other tool on the same
machine.

For the daemon the key goes on the service environment via a systemd drop-in, exactly as
`snapcraft.yaml` already documents for the other three secrets. **No `environment:` stanza is added to
the `ragd` app** — the existing comment explains why: `snap run` applies it after systemd and would
clobber the operator's drop-in with an empty value.

### Snap packaging

Nothing changes. No new plugs or interfaces — `ragd` already holds `network` and the CLI already reads
operator files. Tika is already bundled and already a dependency of the build path. **No new Go module**,
so no snap size delta to watch. The only packaging change is the install hook seeding the new package
keys.

### UI: extend the existing wizard, do not add a screen

The analysis path is a mode of `ui/components/answer/BuildWizard.tsx`, chosen on its upload step, with
the later steps swapped rather than layered — so a user never sees both a column-selection step and an
interview. Following `ui-conventions`:

- Upload and generation are daemon **operations**: `postAsyncForm` / `postAsync` through
  `ui/lib/api/envelope.ts` and the shared operations tracking, not a hand-rolled poll inside the screen.
  `ui/lib/api/answer.ts` gains typed verbs mirroring the daemon views.
- The interview is `p-form p-form--stacked` with `p-form__group` and `<label>`s; the invalid answer
  length uses `p-form-validation is-error` with `__message` at the field.
- Findings render as `p-notification--negative` / `--caution` with `role="alert"` on defects, and are
  distinguished by **icon and text, not colour alone** — a zero-match pattern must read as "may be
  intentional".
- The generated prompt is shown in full in its own scrollable region rather than a truncated preview,
  since it is the field most likely to need an edit.
- All four view states on the new steps; both themes; usable at 620px with the wide domain table in its
  own `overflow-x: auto` wrapper; colours from `--vf-*` tokens only.
- The path chooser and the findings list are new visual patterns, so `.claude/skills/ui-conventions/SKILL.md`
  is updated in the same change, as that skill requires.

## Risks / Trade-offs

- **Confidential RFP content leaves the machine**, and now traverses a gateway as well as a model
  provider. → `analysis.enabled` defaults to `false`, the endpoint is configurable so an operator can
  point at a provider or proxy they have an agreement with, and the flow reports exactly which files are
  conveyed before generation. Opt-in is the whole mitigation, and it must be documented plainly in
  `docs/usage.md` — including that a gateway is an additional party to the data.
- **Prompt injection from the RFP into the generated prompt.** → The two mandatory rules are appended by
  rag-cli after generation rather than trusted to the model, and `--check` re-verifies them.
- **Reasoning control is gateway-mediated and varies by model.** This is the main cost of routing through
  a gateway, and the reasoning-budget analysis is what makes it matter. → Effort is a config key per call
  rather than a constant, the finish reason is inspected on every response, and an empty response is a
  failure. If effort proves too coarse to keep long RFPs inside the budget, the rejected native-SDK
  alternative is the documented fallback — the conveyance and check layers are provider-neutral, so
  swapping the client is contained.
- **Structured output is honoured unevenly across models a gateway serves.** → The response is validated
  locally against what the manifest requires, not trusted because it parsed.
- **Server-side PDF parsing may bill per page** when the selected model has no native PDF support, and is
  chosen by the gateway on capability rather than by us. → Prefer a natively PDF-capable model in the
  packaged default, and surface the model actually called so an unexpected cost is attributable.
- **The model id is a moving target**, more so now that any gateway-served model is reachable. →
  `analysis.model` is a config key; errors and the status surface name the model actually called.
- **Silent truncation on a long RFP.** → Split calls each with their own budget, explicit output ceiling,
  finish-reason inspection, empty-response detection, and the count-against-triage comparison; failure
  rather than a short manifest.
- **Office fidelity still depends on Tika.** → The conveyance report says which documents were converted;
  the deterministic extraction path stays available as a fallback.
- **The staging store now holds document bytes in daemon memory.** → Bounded total and per-request size,
  rejected before conveyance rather than after, and a short TTL. Expiry is a spec'd recoverable error; the
  UI returns to upload rather than dead-ending.
- **A large PDF makes an expensive call.** → Triage is the cheap phase and is separately abandonable;
  size and page limits are checked before any call, per provider rather than assumed.
- **The `--check` heuristics could mislead.** → Prompt assertions and zero-match patterns are warnings
  with named findings, never automatic edits; the check never modifies a manifest.

## Migration Plan

Purely additive. Every existing manifest, command, flag and endpoint behaves as before; the new flags
(`--analyze`, `--context`, `--check`), endpoints and UI step are inert until used, and `analysis.enabled`
is `false` on a fresh install and on refresh.

Because a `user` set is rejected for a key with no `package` layer, the new keys must be seeded by the
install hook — and on **refresh** of an existing installation the seeding path must run too, or
`analysis.*` will be unsettable on machines that already have the snap. The `post-refresh` hook is the
place to confirm this, consistent with the note that `--package` is needed for new config keys on
refresh.

Rollback is a snap revert; nothing persists outside config, and staged analysis tokens are in-memory
only.

## Open Questions

- Should the generated prompt be offered as a stored `answer_system_prompt` variant (usable via
  `prompt_ref`) rather than only inline in the manifest? The prompt store already exists and would give
  generated prompts versioning and reuse across manifests for the same bid. Deferred: it widens this
  change into `rest-api-prompts`, and inline `prompt` is what every manifest in this repo uses today.
- Whether the questions call should page for very large RFPs (continue from the last id) rather than
  failing when it truncates. Failing loudly is correct for the first version; paging is the natural
  follow-up if real RFPs hit the ceiling.
- Whether `--check` should also warn when a manifest's `knowledge_bases` omits a base the triage
  suggested. Useful, but it requires keeping triage output alongside the manifest, which nothing
  currently does.
