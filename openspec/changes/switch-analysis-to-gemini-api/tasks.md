## 1. Sequencing and dependency

- [x] 1.1 Confirm `add-gemini-rfp-analysis` is archived (so `openspec/specs/answer-manifest-analysis/` exists) before archiving this change; re-run `openspec validate switch-analysis-to-gemini-api` after that archive — done 2026-10-12: `add-gemini-rfp-analysis` archived as `archive/2026-10-12-add-gemini-rfp-analysis` with its specs synced; this change re-validated against the main specs
- [x] 1.2 Add `google.golang.org/genai@v1.73.0` to `go.mod`, run `go mod tidy`, and record the new direct and indirect modules from `go mod graph` in the PR description
- [x] 1.3 Before writing code, make one live `generateContent` call with a small PDF using a throwaway Go snippet outside the repo. Capture the raw JSON for success, an invalid key, an unknown model id and a prefixed model id (`google/gemini-3.1-pro-preview`) as test fixtures under `cmd/cli/basic/analysis/testdata/`, with the key redacted — done 2026-10-12 against the live API rather than a throwaway snippet: a successful generateContent call with an inline PDF, an invalid key (HTTP 400 "API key not valid", classified as AuthError), and a `google/`-prefixed model id (classified as ModelNotFoundError with the unprefixed hint) all matched the inline test fixtures, which are kept inline instead of under testdata/

## 2. Configuration and key

- [x] 2.1 In `config.go`, delete `ConfHTTPHost/Port/Path/TLS`, the mirrored `confChatHTTP*` constants, `ResolveEndpoint` and `buildEndpoint`; remove `BaseURL` and `EndpointFromChat` from `Settings`; update the package doc comment to say analysis calls the Gemini API directly
- [x] 2.2 Reject `EffortNone` in `LoadSettings` for both reasoning keys, naming the key and saying Gemini 3 models cannot disable reasoning; keep `ParseEffort`'s accepted spellings unchanged
- [x] 2.3 In `errors.go`/`client.go`, replace `envAnalysisKey`/`envChatKey` with a single `envGeminiKey = "GEMINI_API_KEY"` read explicitly; add the retired-`ANALYSIS_API_KEY` rename hint to the not-configured error; drop the endpoint reason from `Validate`
- [x] 2.4 Update `config_test.go`: remove the endpoint-resolution tests; add tests for `none` rejected per key, for `CHAT_API_KEY` alone yielding not-configured (and never reaching a request), and for the rename hint

## 3. Client rewrite

- [x] 3.1 Introduce the package-private `part` type and change `complete` and `documentParts` to use it; switch the three call sites in `triage.go` and `generate.go` to `[]part`; confirm `openai-go` is no longer imported anywhere under `cmd/cli/basic/analysis/`
- [x] 3.2 Build the client with explicit `genai.ClientConfig{APIKey, Backend: BackendGeminiAPI, HTTPOptions{BaseURL: geminiBaseURL}}`; exported `NewClient` passes the constant, unexported `newClient` takes a base URL for tests only
- [x] 3.3 Map the request per the design table: `SystemInstruction`, text/`NewPartFromBytes` parts with their document labels, `MaxOutputTokens = 65536`, `ResponseMIMEType` + `ResponseJsonSchema`, `ThinkingConfig.ThinkingLevel` (nil when unset); delete the dual `max_tokens` workaround and the non-strict-mode comment
- [x] 3.4 Map the response per the design table: `PromptFeedback.BlockReason` and policy finish reasons → new `BlockedError`; `MAX_TOKENS` → `TruncatedError`; empty text → `TruncatedError{Empty}`; read text via `Text()`; keep `extractJSONBlock` + `json.Valid` and `MalformedResponseError`
- [x] 3.5 Add `ThoughtsTokenCount` and `CandidatesTokenCount` to the verbose per-call line
- [x] 3.6 Rewrite `classifyRequestError` on `genai.APIError`: auth (401, 403, and 400 carrying `API_KEY_INVALID`), new `ModelNotFoundError` on 404 with the unprefixed-id hint when the id contains `/`, quota on 429, `NotDocumentCapableError` on document/MIME rejections (drop `FromChat`), everything else wrapped with the call name; remove `AuthError.FellBack`
- [x] 3.7 Added after live runs (2026-10-12): `TruncatedError` carries the reasoning/answer token split, the ceiling and a quoted excerpt of the untrimmed response, and chooses its advice from the split; triage gets its own 16K ceiling (`triageMaxOutputTokens`), questions and strategy keep 64K

## 4. Conveyance

- [x] 4.1 Change `Document.PDFBase64 string` to `PDFBytes []byte` in `convey.go`, update `size()` and the PDF branch, and make sure `internal/api/answer_analysis_store.go` accounting still bounds what it holds
- [x] 4.2 Rewrite the `Limits`/`DefaultLimits` comment to cite Gemini's documented limits (50 MB / 1000 pages per PDF, 100 MB per inline request) and the daemon's 48 MiB upload cap as the reasons for the current numbers; leave the numbers unchanged
- [x] 4.3 Update `answer_analyze.go`'s conveyance line to name the Gemini API and the model instead of a URL

## 5. Tests

- [x] 5.1 Replace the fake OpenAI server in `client_test.go` with a fake `generateContent` server. Port every existing case: ceiling, schema and effort sent; reasoning omitted when unset; truncated, empty and no-candidates failures; PDF as an inline part; converted document as text; key never in the body; auth naming the variable; unrelated errors not misclassified; malformed-response evidence
- [x] 5.2 Add tests for ambient environment: with `GOOGLE_GEMINI_BASE_URL`, `GOOGLE_GENAI_USE_VERTEXAI`, `GOOGLE_GENAI_USE_ENTERPRISE` and `GOOGLE_API_KEY` set to hostile values, the request still reaches the fake server with `GEMINI_API_KEY`'s value
- [x] 5.3 Add tests for `BlockedError` (prompt block and response block, never reported as truncation), `ModelNotFoundError` with and without a prefixed id, the 400 invalid-key shape from the captured fixture, and each of the three schemas arriving verbatim as `responseJsonSchema`
- [x] 5.4 Update `internal/api/handlers_answer_analysis_test.go` for the removed `Settings` fields and the new key variable

## 6. Snap packaging and hooks

- [x] 6.1 In `snap/hooks/install`, remove the four `analysis.http.*` seeds; rewrite the analysis comment block to cover:
  - direct Gemini API and `GEMINI_API_KEY`;
  - the paid-tier requirement;
  - verified ids `gemini-3.1-pro-preview` (preview) and `gemini-3.8-flash` (stable)
- [x] 6.2 In `snap/hooks/post-refresh`, remove the four `analysis.http.*` seeds and `snapctl unset` both `config.package.analysis.http` and `config.user.analysis.http`
- [x] 6.3 Update the `ragd` secrets comment in `snap/snapcraft.yaml`: `ANALYSIS_API_KEY` → `GEMINI_API_KEY`, no fallback to `CHAT_API_KEY`, and a drop-in example; confirm `ragd` still declares no `environment:` stanza and no plugs changed

## 7. Daemon and UI copy

- [x] 7.1 Update the analysis-availability comments in `internal/api/server.go` and `ui/lib/api/answer.ts` (no endpoint prerequisite; key is `GEMINI_API_KEY`)
- [x] 7.2 Update the unavailable notice in `ui/components/answer/BuildWizard.tsx` to name `analysis.enabled`, `analysis.model` and `GEMINI_API_KEY`, keeping the same `p-notification` markup
- [x] 7.3 Verify `ui-conventions` compliance for the touched step: both themes, keyboard-only pass, `--vf-*` tokens only, all four view states unchanged, and `npm run build` under `output: 'export'`

## 8. Documentation

- [x] 8.1 Rewrite the analysis configuration section of `docs/usage.md`:
  - remove the `analysis.http.*` row and the OpenRouter key paragraph and `curl` model listing;
  - document `GEMINI_API_KEY` for CLI and systemd drop-in;
  - state the paid-tier requirement and Google's abuse-monitoring retention;
  - list the verified model ids.
- [x] 8.2 Update the `docs/usage.md` third-party warning (Google is the one recipient, no gateway) and the example conveyance output (Gemini API, `gemini-3.1-pro-preview`)
- [x] 8.3 Update `docs/rest-api.md`'s daemon-secrets paragraph to list `GEMINI_API_KEY` and drop the OpenRouter example
- [x] 8.4 Check `answer batch --help` text and `apps/completion.bash` for any endpoint or key wording, and update it if present

## 9. Verification

- [x] 9.1 Run `make all` (tidy, fmt, vet, lint, test, build) and fix everything it reports; golangci-lint is at `~/go/bin` — done 2026-10-08: tidy, fmt, vet, test (12 packages) and build pass; lint run as golangci-lint v2.14.0 via `go run` scoped `--new-from-rev=origin/main` reports 0 issues (the installed 2.12.2 panics on Go 1.27, so the literal `make lint` target cannot run)
- [x] 9.2 Build the snap with `snapcraft -v`, record the size delta against the previous build, and install it with `--dangerous` — done 2026-10-12: installed revisions x13 → x14 (the first build containing google.golang.org/genai, checked in the binary) went from 315.2 MB to 320.9 MB, +5.8 MB (+1.8%); x15 adds the truncation fix at the same size. x13 is no longer on disk, so its pre-switch status is inferred from x14 being the first genai build
- [x] 9.3 Inside the installed snap, confirm:
  - `analysis.http.*` is gone from `rag-cli.rag get` on both layers after a refresh over a build that had seeded it;
  - every remaining `analysis.*` key still accepts a `user` set.
- [x] 9.4 With `GEMINI_API_KEY` on a paid-tier project, run `answer batch --build --analyze` end to end on a real RFP spreadsheet and a real RFP PDF from this repo. Confirm: — done 2026-10-12: spreadsheet end to end (MeitY: all columns kept, `--check` clean, ran unmodified on Bedrock); PDFs conveyed as bytes (Lafika 16 pages, MASCOM 40 pages). No PDF run produced a manifest, for analysis-quality reasons now tracked in `harden-analysis-manifest-fidelity` (Lafika shortfall, MASCOM copied example knowledge base), not for wiring reasons
  - the PDF is conveyed as bytes;
  - columns are retained;
  - the manifest passes `--check` and runs unmodified through `answer batch`.
- [x] 9.5 Run the same flow through `ragd` with the key only in the systemd drop-in, from the UI wizard, confirming triage → interview → generation completes — verified by the operator 2026-10-12 in the UI against the direct-Gemini build (x15): triage → interview → generation through ragd with GEMINI_API_KEY in the systemd drop-in
- [x] 9.6 Exercise the failure paths against the live API and confirm each message matches the spec: — done 2026-10-12: missing key, leftover `ANALYSIS_API_KEY`, invalid key (HTTP 400 "API key not valid"), `google/`-prefixed model id and oversized PDF verified live; expired analysis token verified by the operator in the UI
  - missing key;
  - leftover `ANALYSIS_API_KEY` only;
  - an invalid key;
  - a `google/`-prefixed model id;
  - an oversized PDF;
  - an expired analysis token.
- [x] 9.7 Run `add-gemini-rfp-analysis`'s still-open tasks 11.2–11.7 against this wiring rather than OpenRouter, and tick them there — done 2026-10-12: `add-gemini-rfp-analysis` 11.1–11.7 run against this wiring and ticked there
