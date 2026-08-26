# Proposal: add-answer-domain-routing

## Why

RFP compliance matrices arrive as terse requirement labels — `SR-IOV`, `Create VM`, `Cinder Driver
Support` — whose meaning lives in the spreadsheet's *surrounding* columns, not in the label. The
question ID encodes that lost context (`C*` is Enhanced Platform Awareness, `J2.*` is a named server
manufacturer, `GIS*` is a security-baseline deliverable), so operators have started writing
ID→domain routing tables into their batch `prompt` and instructing the model to "use the question ID
to infer which domain the row belongs to."

**The model never receives the ID.** `RunBatch` builds the user turn from `q.Question` alone
(`cmd/cli/basic/chat/batch.go:276` → `buildRAGPrompt`, `cmd/cli/basic/chat/rag.go:328`); `q.ID` is
used only to stamp `BatchResult` for output correlation. Every such routing table is inert
instruction, and the operator has no way to tell — the answers look plausible because the model
re-derives a domain from the label, which is precisely what the prompt tells it not to do. The
manifest's `source` field is dropped even harder: the `--build` extractor writes it into every
manifest and `chat.BatchQuestion` has no field to receive it.

Passing the ID through would only relocate the problem: the model would still perform a
15-row prefix lookup on every question, unverifiably, and retrieval would gain nothing. The routing
is a deterministic mapping, so it belongs in the manifest and in Go — resolved once per question,
recorded in the results, and reused to steer retrieval.

## What Changes

- **New optional `domains:` block in the batch manifest.** Each entry carries a `match` glob tested
  against the question ID, a `context` snippet describing the domain, and optional `keywords`.
  Resolution is **longest-match-wins**, so `J1.*` beats `J*` without ordering discipline, and
  `match: "*"` serves as an explicit catch-all. A question matching no entry behaves exactly as
  today.
- **The resolved domain is injected into the per-question user turn**, not the system prompt: the
  system prompt stays byte-identical across every question in the batch so it remains a cacheable
  prefix (material on a 200-row batch against a metered backend such as Bedrock), and per-row
  scoping belongs beside the row it scopes. The question ID is included in that turn alongside it.
- **Domain `keywords` are merged into the retrieval query** for every question in the domain,
  through the existing `mergeKeywords` seam (`cmd/cli/basic/chat/batch.go:252`). Per-question
  `keywords` keep priority; domain keywords are inherited. This is the change's largest expected
  quality effect: a two-token label like `SR-IOV` currently retrieves on two tokens and can fall
  through to the fixed no-context answer (`batch.go:266`).
- **`source` round-trips.** `chat.BatchQuestion` gains the `Source` field the extractor has always
  written, and it serves as the fallback match key when a question's ID is a bare sequence number —
  the exact case operators' routing tables already special-case.
- **Batch results record the applied domain.** `BatchResult` gains a `domain` field, so the JSON
  output states which rule produced each answer rather than requiring the operator to trust that
  routing fired. This sits alongside the existing `prompt` provenance field.
- **The build flows emit a commented `domains:` stub** derived from the distinct ID prefixes and sheet
  names observed, so extracted manifests come out ready to fill in rather than requiring the block to
  be hand-authored.
- **Full CLI/UI parity.** The web UI accepts, previews, posts, and re-serialises domain routing, shows
  the applied domain in results review, and emits the stub from its build wizard. Reaching parity
  requires replacing the UI's hand-rolled line-based manifest parser (`ui/lib/manifest.ts`) with a real
  YAML reader: it cannot represent a nested `domains:` block, and its unknown-top-level-key branch
  would swallow one silently. That replacement also fixes an existing defect it was found to have —
  a `prompt:` written as a block scalar is currently read as the literal string `"|"` with its body
  discarded, which affects every prompt-bearing manifest in this repo's working tree.
- Backward compatible: a manifest with no `domains:` block produces byte-identical prompts and
  retrieval queries to today, so existing manifests do not change meaning.

## Capabilities

### New Capabilities

- `answer-batch-domains`: the batch manifest's domain-routing block — schema, glob matching and
  longest-match-wins precedence, ID/`source` match keys, injection of the resolved domain into the
  question turn, inheritance of domain keywords into the retrieval query, and per-result domain
  provenance. Shared by the CLI's direct run path and the daemon, since both drive `chat.RunBatch`.

### Modified Capabilities

- `rest-api-answer`: MODIFY the batch-answering requirement so `POST /1.0/answer/batch` accepts the
  manifest's `domains` block and applies it on the daemon path identically to the CLI's direct path;
  MODIFY the results requirement so retrievable results carry the applied domain per question
  alongside the question, answer, model, and timestamp.
- `local-ui-app`: ADD requirements for reading manifests with a full YAML reader (rather than the
  current subset), carrying and previewing domain routing before a run, showing the applied domain in
  results review, and emitting routing plus the build stub when the UI writes a manifest. These are
  additive; the change does not alter the answer-batch requirements added by `add-ui-answer-batch`,
  though it extends the surfaces those requirements describe — see Impact for the sequencing.

## Impact

**External services.** Touches two of the three: the **inference server** (the user turn sent to
`chat/completions` gains a domain preamble and the question ID) and **OpenSearch** (the lexical and
semantic retrieval queries gain inherited domain keywords). **Tika is not affected** — the `--build`
extraction path only gains a stub emitter downstream of extraction.

**Config keys.** None added. Domain routing is manifest data, not configuration, so no `package` or
`user` scoped keys are introduced and the snapctl layer is untouched.

**User-facing surfaces and their documentation.**
- The `answer batch` YAML manifest schema is a user-authored file format. `docs/usage.md` §"YAML
  schema" (~L1131) must document `domains`, its match semantics and precedence, and the
  `questions[].source` field — and, while there, the existing `questions[].keywords` field, which
  the schema table omits today.
- `docs/usage.md` §`answer batch --build` (~L1223) must document the emitted `domains:` stub.
- No new CLI command or flag, so the fixed root-command ordering
  (`cobra.EnableCommandSorting = false`) is unaffected.

**Code.**
- `cmd/cli/basic/chat/batch.go` — `BatchManifest` gains `Domains`; `BatchQuestion` gains `Source`;
  `BatchResult` gains `Domain`; new resolution + longest-match logic; the `RunBatch` loop applies it
  to both the prompt and the keyword merge.
- `cmd/cli/basic/chat/rag.go` — `buildRAGPrompt` takes the resolved domain and question ID.
- Two transport mirrors of the manifest type must carry `domains` and `source`, or the block is
  silently dropped exactly as `source` is today: `internal/api/handlers_answer.go:36-59`
  (`batchQuestionRequest`/`toManifest`) and `cmd/cli/basic/answer_api.go:59`
  (`batchQuestionJSON`/`batchManifestJSON`).
- `ui/lib/manifest.ts` — replaced with a YAML-reader-backed parse and a typed validation layer,
  preserving the existing `ManifestParseError` contract that screens render as field-level errors.
  **Adds one UI dependency** (a YAML library; the UI currently has five runtime deps and no YAML
  reader). See `design.md` decision 8 for why extending the hand-rolled parser was rejected.
- `ui/lib/api/answer.ts` — `BatchManifest` gains `domains`; the `source` comment documenting the drop
  as intended becomes obsolete.
- `ui/lib/types.ts` + `ui/components/answer/QACard.tsx` — `QAItem` gains an optional `domain`, rendered
  when present following the existing optional-`sources` pattern.
- `ui/components/answer/ManifestRunner.tsx` — preview gains the routing summary with per-pattern match
  counts.
- `ui/components/answer/BuildWizard.tsx` + `serializeManifest` — emit routing and the stub.

**Sequencing.** The UI phases extend the answer-batch screens added by the unarchived
`add-ui-answer-batch` change (and the build wizard that `add-answer-build-column-selection` modifies).
Both are essentially complete in the working tree. The `local-ui-app` delta here is purely additive so
it does not conflict with their pending deltas, but the UI phases should land after those changes are
applied.

**Dual resolution.** Preview match counts mean domain resolution exists in Go and in TypeScript. The
Go side stays authoritative — the preview is advisory and the result's `domain` field is the record —
and both implementations are tested against one shared fixture to keep them from drifting.
- `rest-api.yaml` — request and result schemas.
- `cmd/cli/basic/rfp/extractor.go` + `cmd/cli/basic/rfp.go` — the `domains:` stub emitter.

**Testing.** Domain resolution (glob matching, longest-match precedence, catch-all, ID vs `source`
fallback) is pure and table-testable with no snapctl or service dependency — worth real unit tests in
a repo where coverage is otherwise sparse. End-to-end batch behaviour still requires a built and
installed snap.
