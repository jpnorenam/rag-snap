# Tasks: add-answer-domain-routing

## 1. Manifest types and domain resolution (Go core)

- [x] 1.1 Add `Domain` type (`Match`, `Context`, `Keywords KeywordList`) and `Domains []Domain` to `chat.BatchManifest` in `cmd/cli/basic/chat/batch.go`.
- [x] 1.2 Add `Source string` (`yaml:"source,omitempty"`) to `chat.BatchQuestion`, replacing the silent drop.
- [x] 1.3 Add `Domain string` (`json:"domain,omitempty"`) to `chat.BatchResult`.
- [x] 1.4 Implement `CompileDomains([]Domain) (*DomainSet, error)`: reject an entry with neither `context` nor `keywords`, reject duplicate `match` patterns, precompute each pattern's literal (non-wildcard) character count, and return a set whose lookup is a no-op for a nil/empty list.
- [x] 1.5 Implement `(*DomainSet).Resolve(id, source string) *Domain`: uppercase both sides and match with `path.Match` over `*`/`?`; pick the greatest literal count, breaking ties to document order; fall back to matching `source` only when `id` matches `^[0-9]+$` and matched nothing.
- [x] 1.6 Extend `mergeKeywords` to `mergeKeywords(generated string, leading ...[]string)`, preserving global case-insensitive dedup and first-occurrence ordering; update its doc comment for the new tiering.
- [x] 1.7 Table-test `CompileDomains` and `Resolve`: `J1.*` over `J*`, `C*` over `*`, `gis*` matching `GIS3`, literal-count ties resolving to document order, numeric-id fallback to `source`, id match not re-matched against `source`, no-match returning nil, both validation rejections. No snapctl or service dependency — these must pass under plain `go test`.
- [x] 1.8 Test `mergeKeywords` three-tier precedence: question keywords lead, then domain, then generated, with a case-differing duplicate collapsing to its first position.

## 2. Wire resolution into the batch pipeline

- [x] 2.1 Change `buildRAGPrompt` in `cmd/cli/basic/chat/rag.go` to accept the resolved domain context and question id, emitting the `Requirement domain: …` paragraph only when a domain resolved and the `Question [id]:` form only when an id exists — a question with neither must produce today's exact string.
- [x] 2.2 Call `CompileDomains` at the top of `RunBatch` and return its error before the question loop, so no path can skip validation.
- [x] 2.3 In the `RunBatch` loop, resolve each question once, pass the domain's keywords to `mergeKeywords` after the question's own, and pass the domain context plus id to `buildRAGPrompt`.
- [x] 2.4 Stamp `Domain` on both `BatchResult` construction sites — the no-context short-circuit and the post-generation path — so provenance is present even when the answer is the fixed no-context response.
- [x] 2.5 Call `CompileDomains` from `LoadBatchManifest` so the CLI reports a malformed block at load time, before any client or service connection.
- [x] 2.6 Verify the no-domains path is byte-identical: assert the generated system prompt and user turn for a manifest without `domains` match the pre-change strings.

## 3. Transports

- [x] 3.1 Add `domains` to `batchManifestRequest` and `source` to `batchQuestionRequest` in `internal/api/handlers_answer.go`, and carry both through `toManifest`.
- [x] 3.2 Surface a `CompileDomains` failure from the batch handler as a 400 before the operation is created, matching how a `prompt_ref`/`prompt` conflict already fails fast.
- [x] 3.3 Add `domains` and `source` to `batchManifestJSON`/`batchQuestionJSON` in `cmd/cli/basic/answer_api.go` so the CLI's daemon-backed path forwards them.
- [x] 3.4 Confirm `runBatchRemote`'s results decode picks up the new `domain` field and that the written JSON carries it.
- [x] 3.5 Add the round-trip test: decode a manifest carrying `domains` and `questions[].source`, marshal to the API request shape, convert back via `toManifest`, and assert both survive intact.
- [x] 3.6 Update `rest-api.yaml`: `domains` on the batch manifest schema, `source` on the question schema, `domain` on the result schema, and the 400 case for an invalid `domains` entry.

## 4. CLI build-flow stub

- [x] 4.1 In `rfp.WriteManifest` (`cmd/cli/basic/rfp/extractor.go`), emit a commented `domains:` stub between the existing header comment and the encoder output, listing the distinct question-id prefixes and `source` values observed. Add nothing to `rfp.Manifest`.
- [x] 4.2 Test that a built manifest re-parses with no `domains` entries, so an unedited stub cannot affect a run.

## 5. Web UI — YAML reader swap (parity phase 1)

Sequenced after `add-ui-answer-batch` and `add-answer-build-column-selection` are applied. Lands on its own: no domain behaviour, only a faithful reader.

- [x] 5.1 Add the YAML dependency to `ui/package.json` (`yaml`, eemeli/yaml — pure JS, browser-safe). Note in the commit message that this is the UI's sixth runtime dependency and why (design.md decision 8).
- [x] 5.2 Reimplement `parseManifest` in `ui/lib/manifest.ts` over the YAML reader, keeping the exported signature and the `ManifestParseError` type and message shape so no caller or screen changes. Move the inline field validation (non-empty `question`, at least one question) into a typed validation layer over the parsed document.
- [x] 5.3 Keep every existing case in `ui/lib/manifest.test.ts` unchanged — they are the regression suite for the swap. Add cases for: a block-scalar `prompt` read with its full body (the defect this fixes), a nested block sequence of mappings, `keywords` in both inline and block sequence form, and an unknown top-level field still tolerated.
- [x] 5.4 Teach `serializeManifest` to emit a block scalar for any multi-line value instead of folding it into a double-quoted scalar with raw newlines, and test that a multi-line prompt round-trips through serialise → parse unchanged.
- [x] 5.5 Verify no other module depended on the old parser's quirks: check every `parseManifest`/`serializeManifest` caller and run the UI suite and lint.

## 6. Web UI — domain routing (parity phase 2)

- [x] 6.1 Add `domains` to `BatchManifest` and confirm `source` on `BatchQuestion` in `ui/lib/api/answer.ts`; delete the now-false "the daemon and the CLI reader both ignore it" claim from the `source` comment.
- [x] 6.2 Read `domains` in `parseManifest` and emit it in `serializeManifest`, so routing survives an upload → preview → download round trip.
- [x] 6.3 Add client-side validation mirroring `CompileDomains`: reject an entry with neither `context` nor `keywords`, and reject duplicate `match` patterns, both as `ManifestParseError` field-level errors that prevent the API call.
- [x] 6.4 Implement the TS resolver (glob over `*`/`?`, case-insensitive, greatest literal count, ties to document order, `source` fallback for `^[0-9]+$` ids) as a pure function in `ui/lib/`.
- [x] 6.5 Create the shared `(patterns, id, source) → expected match` fixture and assert both the Go `Resolve` test and the TS resolver test against it. Resolve the fixture's location and format first (design.md open question) and confirm with `git check-ignore` that the root `.gitignore` does not exclude it.
- [x] 6.6 Confirm the posted run body carries `domains` and `source` — `ManifestRunner.tsx` spreads `{ ...manifest, temperature }`, so verify nothing strips them.

## 7. Web UI — preview and results (parity phase 3)

- [x] 7.1 Add a routing summary to the pre-run preview in `ui/components/answer/ManifestRunner.tsx`: per entry, its `match` pattern, its context, and the count of questions it would apply to.
- [x] 7.2 Make questions the routing reaches nothing for identifiable in the preview, and label the resolution advisory (the run's recorded domain is authoritative).
- [x] 7.3 Add an optional `domain` to `QAItem` in `ui/lib/types.ts`.
- [x] 7.4 Render `domain` on the result card in `ui/components/answer/QACard.tsx`, following the existing optional-`sources` pattern: shown only when present, no placeholder when absent, distinct from the answer text and the collapsible provenance section.
- [x] 7.5 Assert an older results file carrying no `domain` values still renders.
- [x] 7.6 Emit the commented `domains:` stub from the UI build flow (`ui/components/answer/BuildWizard.tsx` + `serializeManifest`), matching the CLI's stub text, and test that the built manifest re-parses with no routing.
- [x] 7.7 Run the `ui-conventions` check over the new affordances: both themes, keyboard-only navigation, `--vf-*` tokens only, and all four view states on the preview and results surfaces.

## 8. Documentation

- [x] 8.1 Document `domains` in the `docs/usage.md` "YAML schema" section (~L1131): the schema block, the field table, glob syntax, longest-literal-count precedence with the `J1.*`/`J*` example, the `*` catch-all, and the numeric-id `source` fallback.
- [x] 8.2 Add the missing `questions[].keywords` and new `questions[].source` rows to the same field table.
- [x] 8.3 Document the `domain` field in the results JSON, alongside the existing `prompt` provenance field.
- [x] 8.4 Document the emitted stub in the `answer batch --build` section (`### answer batch --build`, ~L1220), and add it to the sample generated manifest in that section (~L1314) so the documented output matches what the extractor now writes.
- [x] 8.5 Add guidance to keep domain keywords few and domain-defining, per the over-steering trade-off in `design.md`.
- [x] 8.6 Document the routing preview and the per-card domain in `docs/local-ui.md`, and state that the preview's resolution is advisory while the results' domain is authoritative.
- [x] 8.7 Confirm no `--help` text or `apps/completion.bash` change is needed (no new command or flag) and that `cmd/cli/main.go` command ordering is untouched.

## 9. Validation

- [ ] 9.1 Run `make all` (tidy, fmt, vet, lint, test, build) and fix anything it reports.
- [ ] 9.2 Run the UI test suite and lint.
- [ ] 9.3 Build and install the snap (`snapcraft -v`, `sudo snap install --dangerous ./rag-cli_*.snap`) and run a real manifest end to end — the batch path needs snapctl config plus live OpenSearch and inference endpoints, so `go test` cannot cover it.
- [ ] 9.4 In the installed snap, verify against a real matrix: a `domains:` block routes as expected, the `domain` field appears in the results JSON, a manifest without the block produces unchanged output, and an invalid entry fails before the first question is answered.
- [ ] 9.5 Verify the daemon path matches the direct path: run the same manifest with `ragd` running and with it stopped, and confirm the routing and recorded domains agree.
- [ ] 9.6 Verify CLI/UI parity on one real manifest: run it with `rag-cli.rag answer batch` and again through the web UI, and confirm the recorded domain per question agrees in both result sets and that the preview's match counts matched the outcome.
