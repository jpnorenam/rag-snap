## 1. Configuration groundwork

- [x] 1.1 Confirm no new Go module is needed: `openai-go/v3` v3.11.0 already provides `FileContentPart` (with `FileData`/`Filename`), `ReasoningEffort`, `ResponseFormatJSONSchemaParam`, `MaxCompletionTokens` and per-choice `FinishReason`
- [x] 1.2 Add the `analysis.*` package keys to `snap/hooks/install` alongside the existing `kapa.*` block: `analysis.enabled` (`false`), `analysis.model`, `analysis.http.host`/`port`/`path`/`tls` (all empty), `analysis.reasoning.questions` (`low`), `analysis.reasoning.strategy` (`high`), `analysis.answer.words` (`100`), `analysis.answer.limit` (`soft`)
- [x] 1.3 Ensure the same keys are seeded on `snap/hooks/post-refresh` so `analysis.*` is settable on an already-installed snap, and verify a `user` set is accepted for each (a `user` set is rejected without a `package` layer)
- [x] 1.4 Add config-key constants and typed getters next to the existing `knowledge.ConfKapa*` constants, with the documented defaults applied when a key is unset
- [x] 1.5 Resolve the analysis endpoint by falling back to `chat.http.*` when every `analysis.http.*` key is empty, mirroring how `serverApiUrls` composes the chat URL, and cover the fallback with a unit test
- [x] 1.6 Extend the `snapcraft.yaml` secrets comment to cover `ANALYSIS_API_KEY` with the same systemd drop-in recipe, and confirm the `ragd` app still declares **no** `environment:` stanza

## 2. Analysis client

- [x] 2.1 Create the analysis package, constructing an `openai-go` client against the resolved analysis endpoint with the configured model id; read the key from `ANALYSIS_API_KEY` then `CHAT_API_KEY` explicitly, with no reliance on library environment auto-detection
- [x] 2.2 Return a distinct, typed "not configured" error when `analysis.enabled` is false or no key is present, naming the variables consulted, and assert in a test that no key value appears in the error string
- [x] 2.3 Convey PDFs as `FileContentPart` with base64 `FileData` and the original `Filename`, and convey converted Office content as text parts in the same request
- [x] 2.4 Check the provider's document size and page limits before conveying, failing with a message naming the document, the limit and the measured value; keep the limits per-provider rather than hardcoding one vendor's numbers
- [x] 2.5 Implement the two generation calls (`questions`, `strategy`) with `ResponseFormatJSONSchemaParam`, an explicitly set `MaxCompletionTokens`, and per-call `ReasoningEffort` from config; keep the schemas shallow
- [x] 2.6 Inspect `FinishReason` on every response and fail on truncation, on an **empty** response at the ceiling (never treating it as "found nothing"), and on a well-formed response missing a required manifest field — each naming the call that failed
- [x] 2.7 Detect and report a backend that accepts no documents with a message naming the backend and saying it is not document-capable, rather than surfacing the provider's raw rejection
- [x] 2.8 Unit-test schema round-tripping, truncation, empty-response and missing-field detection against recorded response fixtures, with no network access

## 3. Document conveyance

- [x] 3.1 Add a Markdown table renderer over the existing `rfp.SheetTable` rows: every row, every column, each cell under its column heading, each table labelled with its sheet name — no column selection and no row truncation
- [x] 3.2 Wire XLSX and CSV conveyance through the existing `rfp.ParseTikaHTMLSheets` / `rfp.XLSXSheetNames` parsing into that renderer, preferring workbook sheet names over Tika's mangled labels
- [x] 3.3 Wire DOCX/DOC conveyance through Tika HTML into Markdown headings and tables; do not upload HTML as a file part (its tags are stripped by the model)
- [x] 3.4 Wire PDF conveyance to raw bytes, base64-encoded once at staging time so both generation calls reuse the encoded form
- [x] 3.5 Build the per-document conveyance report (format detected, path taken, whether converted) and return it with triage
- [x] 3.6 Unit-test the renderer on a spreadsheet fixture whose requirement column is flanked by description, current-environment and evaluator-guidance columns, asserting every column and the sheet name survive

## 4. Grounding payload

- [x] 4.1 Write the embedded contract brief: every manifest field and its meaning, and the domain resolution rules exactly as the runtime applies them (glob against `id`, fall back to `source` only for a bare sequence number, most specific pattern wins)
- [x] 4.2 Add an abridged worked-example manifest as an embedded asset, derived from a real manifest in this repo and sanitised of customer-identifying content
- [x] 4.3 Embed both with `//go:embed` so the contract ships with the binary that consumes it, and add a test that fails if the embedded contract's field list diverges from the manifest struct tags
- [x] 4.4 Build the live catalogue at request time: knowledge bases from OpenSearch and kapa source groups (names **and** identifiers) from `KapaClient.ListSourceGroups`, failing with the unreachable service named rather than sending an empty catalogue
- [x] 4.5 Validate the model's returned `knowledge_bases` and `kapa_source_groups` against the catalogue that was sent, reporting any value not in it

## 5. Interview and prompt assembly

- [x] 5.1 Implement the interview model: answer length in words (default from config), hard-limit-versus-soft-target as an explicit choice, and OpenStack flavour carried only when triage reported OpenStack in scope
- [x] 5.2 Append the two non-negotiable rules to the generated prompt after generation rather than requesting them of the model — preferring "no context available" / "NOT covered" over fabrication, and directness with no filler
- [x] 5.3 Render the answer length into the prompt together with its enforcement mode, so a length never appears without stating whether it is a hard limit or a soft target
- [x] 5.4 Render the chosen OpenStack flavour as the offering to describe, and assert the other flavour is not presented as the proposed solution
- [x] 5.5 Render multi-field answering guidance into the prompt when triage reported questions carrying more than one field
- [x] 5.6 Unit-test prompt assembly for each interview combination, asserting the mandatory rules survive a strategy response that omits or contradicts them

## 6. Manifest check

- [x] 6.1 Implement the check over a loaded manifest using `chat.CompileDomains` and the run's own `Resolve`, so check and run cannot disagree
- [x] 6.2 Implement the structural findings: missing/empty questions, empty question field, `domains` entry with no `match` or with neither `context` nor `keywords`, duplicate `match` patterns, duplicate question ids, question with no id
- [x] 6.3 Implement per-domain match counts, the zero-match warning worded as possibly intentional, and the unrouted-question warning for a manifest that declares domains
- [x] 6.4 Implement reference resolution for `knowledge_bases` and `kapa_source_groups` as defects, with a near-miss name suggestion for a close knowledge-base match
- [x] 6.5 Implement the prompt assertions (no-context rule present, answer length with its enforcement mode) as warnings, reported as not applicable for a manifest with no inline `prompt` or one using `prompt_ref`
- [x] 6.6 Implement the two-severity result with a clean / warnings-only / defective distinction, and degrade gracefully when the knowledge store is unreachable by reporting reference checks as not performed rather than passing
- [x] 6.7 Unit-test the check against the manifests already in this repo, including one that deliberately reserves a zero-match pattern, asserting it reports a warning and not a defect

## 7. CLI surface

- [x] 7.1 Add `--analyze`, repeatable `--context`, and `--check` to `answer batch`, keeping extraction the default when `--analyze` is absent and preserving the fixed command order in `cmd/cli/main.go`
- [x] 7.2 Wire the CLI two-phase flow: conveyance and triage, the `huh` interview conditioned on triage, then generation, printing the conveyance report and the triage before the interview
- [x] 7.3 Print the check findings with the generated manifest before offering to run it, and write the manifest only after validation, never over a previously valid file on failure
- [x] 7.4 Record provenance in the emitted manifest header (produced by analysis, and the model that produced it) via the existing `rfp.WriteManifest` header, emitting a populated `domains` block rather than the commented-out stub
- [x] 7.5 Make `--check` exit non-zero on defects and zero on warnings-only, so it is usable in a script
- [x] 7.6 On any analysis failure, state that the existing extraction path remains available

## 8. Daemon API

- [x] 8.1 Add the analysis staging store modelled on `internal/api/answer_build_store.go`, holding the conveyed document content under a token with a short TTL, in memory only, and bounding both per-request and total held bytes with rejection before conveyance rather than after
- [x] 8.2 Add the triage endpoint accepting a multipart upload of the RFP plus any number of context documents, running as a tracked async operation whose metadata carries the triage, the per-document conveyance report, and the token with its expiry
- [x] 8.3 Add the generation endpoint accepting a token plus interview answers, running as a tracked async operation whose metadata carries the generated manifest in the shape the batch endpoint accepts, its check findings, and the generating model
- [x] 8.4 Fail an unknown or expired token with a message stating the session expired and the documents must be uploaded again
- [x] 8.5 Add the synchronous check endpoint accepting any manifest the batch endpoint accepts, returning counts, per-domain match counts and findings by severity, modifying nothing
- [x] 8.6 Add the new capability names to the advertised list in `internal/api/server.go`, advertising analysis as available only when it can actually run, and register the routes beside the existing `answer/build` routes
- [x] 8.7 Assert in a handler test that the API key appears in no response body, no operation metadata and no error message
- [x] 8.8 Test that a generated manifest posted straight back to the batch endpoint is accepted with no field dropped in transit

## 9. Browser UI

- [x] 9.1 Add typed verbs and view interfaces for the three endpoints to `ui/lib/api/answer.ts`, going through `envelope.ts` (`postAsyncForm` / `postAsync` / `postSync`) and normalising null arrays to `[]`
- [x] 9.2 Add the path chooser to the `BuildWizard` upload step with extraction as the default, and show the analysis choice as unavailable with a reason when the daemon does not advertise it
- [x] 9.3 Add context-document selection for the analysis path: any number of files, each removable before submit, with the primary RFP document kept distinguishable
- [x] 9.4 Swap the wizard's later steps for the analysis steps rather than layering them, so a column-selection step and an interview are never both presented
- [x] 9.5 Build the triage review step showing the triage and the per-document conveyance report, with an abandon action and a recoverable expired-session error that returns to upload
- [x] 9.6 Build the interview form as `p-form p-form--stacked`, answer length prefilled from config, hard-versus-soft as an explicit choice, OpenStack control rendered only when triage reported it in scope, and field-level validation via `p-form-validation is-error`
- [x] 9.7 Build the generated-manifest review: question count, selected knowledge bases and kapa source groups, domains with per-domain match counts in an `overflow-x: auto` wrapper, and the full prompt in its own scrollable region
- [x] 9.8 Render check findings at both severities distinguished by icon and text rather than colour alone, with `role="alert"` on defects and the zero-match pattern reading as possibly intentional
- [x] 9.9 Wire the download and run actions, keeping download available for a manifest carrying defects, and make generated questions editable before running with findings reflecting what will run
- [x] 9.10 Add the standalone check entry point on the answer screen, reusing the same findings surface for a supplied manifest
- [x] 9.11 Add the new styles to `ui/app/globals.scss` under a feature comment using only `--vf-*` tokens, and update `.claude/skills/ui-conventions/SKILL.md` with the path chooser and findings-list patterns in this same change
- [x] 9.12 Verify `ui-conventions` compliance: both themes, keyboard-only walkthrough, all four view states on every new step, no horizontal page scroll at 620px, and `npm run build` under `output: 'export'` — verified by the operator 2026-10-12 in the UI against the direct-Gemini build (x15)

## 10. Documentation

- [x] 10.1 Document the analysis flow in `docs/usage.md`: the two phases, the interview, `--analyze` / `--context` / `--check`, the `analysis.*` keys including the endpoint fallback to `chat.http.*`, and `ANALYSIS_API_KEY`/`CHAT_API_KEY` for both the CLI and the daemon systemd drop-in
- [x] 10.2 Document that Office formats are converted locally because no model API accepts them, so a spreadsheet that works when dropped into a vendor's chat UI is not evidence the API accepts it
- [x] 10.3 State plainly in `docs/usage.md` that analysis sends document content to a third-party API — and to a gateway when one is used, an additional party to the data — that it is opt-in and disabled by default, and that the endpoint is configurable
- [x] 10.4 Update the `answer batch` `--help` and usage text for the three new flags
- [x] 10.5 Add the new flags to `apps/completion.bash`
- [x] 10.6 Document the three endpoints in `rest-api.yaml` and `docs/rest-api.md`, matching the existing `answer/build` entries

## 11. Validation

- [x] 11.1 Run `make all` (tidy, fmt, vet, lint, test, build) and fix everything it reports — done 2026-10-08: tidy, fmt, vet, test (12 packages) and build pass; lint run as golangci-lint v2.14.0 via `go run` scoped `--new-from-rev=origin/main` reports 0 issues (the installed 2.12.2 panics on Go 1.27, so the literal `make lint` target cannot run)
- [x] 11.2 Build and install the snap, then exercise the config paths inside it: confirm the five `analysis.*` package keys exist, that a `user` set is accepted for each, and that they survive a refresh — verified 2026-10-12 in the installed snap: the six `analysis.*` keys that remain after `switch-analysis-to-gemini-api` removed `analysis.http.*` are present after a refresh with user values retained, and a `user` set is accepted for each of the six
- [x] 11.3 Run the CLI flow end to end inside the installed snap against a real RFP spreadsheet from this repo, and confirm the conveyed representation retains the columns beside the requirement — verified 2026-10-08 against the direct-Gemini wiring: `MeitY_RFP.xlsx` in the installed snap, report "converted to Markdown tables: 1 sheet(s), 5 data row(s), all columns kept"
- [x] 11.4 Run the flow against a real RFP PDF from this repo and confirm the PDF is conveyed as bytes rather than converted — verified 2026-10-08: `Lafika_RFI.pdf` reported "native — conveyed as PDF bytes, 16 pages"
- [x] 11.5 Run `--check` over the existing hand-written manifests in this repo and confirm the reserved zero-match patterns report as warnings, not defects
- [x] 11.6 Confirm a generated manifest runs unmodified through `answer batch`, and that its answers respect the stated word budget and its enforcement mode — verified 2026-10-08: `test-rfp/meity-gemini.yaml` ran unmodified on Bedrock Mistral (`batch-results-20261008-121741.json`); soft 100-word target gave mean 120, max 147 words, none near the 2x bound. Content issues found are tracked in `harden-analysis-manifest-fidelity`
- [x] 11.7 Confirm the failure paths: absent API key, oversized PDF, expired analysis token, and a truncated generation, each producing the specified message and no written manifest — done 2026-10-12: absent key and oversized PDF ("40.0 MiB, above the 32.0 MiB per-document limit") verified live; expired analysis token verified by the operator in the UI; truncated generation accepted by the operator as covered by unit tests (truncated, empty-at-ceiling and empty-on-stop responses fail and write nothing)
- [x] 11.8 Confirm no API key value appears in verbose output, error messages, operation metadata or the emitted manifest
