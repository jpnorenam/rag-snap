## Context

`add-gemini-rfp-analysis` built RFP analysis on `openai-go/v3` against an OpenAI-compatible endpoint,
with OpenRouter as the reference provider. That choice rested on one fact: OpenRouter accepts a PDF in
a `file` content part, and Gemini's own OpenAI-compatibility layer does not. Compliance now requires
RFP content to go straight to Google under our own key, with no gateway in the path.

The OpenAI-compatibility page (last updated 2026-09-02) was re-checked for this change. It still
documents `image_url` and `input_audio` parts and nothing for documents, so the cheap fix of repointing
`analysis.http.*` at `generativelanguage.googleapis.com/v1beta/openai` is not viable. Every PDF would be
rejected, or would be undocumented behaviour at best.

What the native Gemini API offers, verified against current documentation and the `google.golang.org/genai`
v1.73.0 source:

- **PDF input inline**, as a `Blob{MIMEType: "application/pdf", Data: []byte}` part. Limits are 50 MB or
  1000 pages per PDF and 100 MB per inline request; each page costs 258 tokens. On Gemini 3, text
  embedded in the PDF is extracted and not billed.
- **JSON-schema output** via `GenerateContentConfig.ResponseMIMEType` + `ResponseJsonSchema` (takes
  `any`, so the existing `map[string]any` schemas pass through unchanged).
- **Reasoning control** via `ThinkingConfig.ThinkingLevel` (`MINIMAL`/`LOW`/`MEDIUM`/`HIGH`). The docs
  state that reasoning cannot be turned off for Gemini 3 models.
- **An output ceiling** via `MaxOutputTokens`, which covers thinking tokens as before.
- **Finish reasons** as typed constants (`FinishReasonMaxTokens`, `FinishReasonSafety`, …), plus
  `PromptFeedback.BlockReason` when the prompt itself is blocked.
- **Two generation APIs.** `generateContent` is stateless; Google calls it legacy but "fully supported".
  The newer **Interactions API** is recommended for new projects but **stores every interaction by
  default** (`store=true`; 55 days on the paid tier, 1 day free) and had breaking changes in May 2026.

The SDK also resolves configuration from the environment when it is not given explicitly:
`GEMINI_API_KEY`/`GOOGLE_API_KEY`, `GOOGLE_GENAI_USE_VERTEXAI`/`GOOGLE_GENAI_USE_ENTERPRISE`, and
`GOOGLE_GEMINI_BASE_URL`. Explicit `ClientConfig` values win over all of them (`getBaseURL` returns
`HTTPOptions.BaseURL` first, and an explicit `APIKey` is never overwritten).

Google's Gemini API terms distinguish paid from unpaid use. On **unpaid** services, submitted content may
be used to improve Google products, and human reviewers may read it. On **paid** services, prompts and
responses are not used for that, and are processed under the Data Processing Addendum. Google still logs
them "for a limited period of time" for abuse detection.

Current model ids: `gemini-3.1-pro-preview` (Pro tier, preview only) and `gemini-3.8-flash` (newest
stable). Direct ids carry no `google/` prefix; OpenRouter's did.

The code seam is narrow. Only `client.go` imports `openai-go`. `triage.go` and `generate.go` build
`[]openai.ChatCompletionContentPartUnionParam` in three places and call `c.complete(...)`. Conveyance,
prompting, decoding, grounding, check and write are provider-neutral already.

## Goals / Non-Goals

**Goals:**

- RFP content reaches only Google's Gemini API, under the operator's key, with no gateway.
- Nothing on the host can silently redirect that traffic or swap its credential.
- Nothing is stored provider-side beyond the call by any facility rag-cli controls.
- Every guarantee the gateway version made survives:
  - PDF bytes conveyed natively, Office formats as table-preserving Markdown;
  - explicit output ceiling and per-call reasoning;
  - truncation and empty-response detection, and local validation;
  - the key never disclosed.
- Failures stay diagnosable without reading Google's docs: bad key, wrong model id, blocked content.

**Non-Goals:**

- Changing the chat/answering backend (`chat.*`), including replacing Mistral on Bedrock.
- Supporting more than one analysis provider, or keeping an endpoint override for a gateway or proxy.
- Vertex AI. It is the same SDK with a different backend; see Open Questions.
- Changing triage, generation, the interview, the check, the REST surface or the UI flow.
- Files API uploads, context caching, or the Batch API.

## Decisions

### Call `generateContent` through the official `google.golang.org/genai` SDK

The client becomes a `*genai.Client` and each call is `client.Models.GenerateContent(ctx, model, contents, cfg)`.

*Why `generateContent` over the Interactions API.* Interactions stores requests for 55 days unless every
call sets `store=false`. That is a compliance default rag-cli would have to defeat on every call, and one
forgotten flag in a future edit would retain RFPs silently. `generateContent` has no storage mode to
forget. It also has every primitive this feature needs, and its Go types have been stable across the
releases this repo would track, while Interactions changed shape in May 2026. The "legacy" label is the
cost, accepted because Google says it remains fully supported. If it is ever retired, the swap is
confined to `client.go` (see the seam decision below), and Interactions with `store=false` is the
documented successor.

*Why not the OpenAI-compatibility layer.* No documented PDF input (see Context).

*Why not hand-written REST.* The SDK is Google's supported client, and it carries the typed request,
response and error shapes the classification below depends on. A hand-rolled client would re-derive
those and drift.

### Pin the destination, backend and credential explicitly

`NewClient` builds `genai.ClientConfig{APIKey: key, Backend: genai.BackendGeminiAPI,
HTTPOptions: genai.HTTPOptions{BaseURL: geminiBaseURL}}`, with `geminiBaseURL` a package constant
(`https://generativelanguage.googleapis.com/`). Because each field is explicit, the SDK's environment
resolution never applies, so `GOOGLE_GEMINI_BASE_URL`, `GOOGLE_GENAI_USE_VERTEXAI` and `GOOGLE_API_KEY`
on the host are inert. A test sets those variables to hostile values and asserts the request still lands
on the fake server with the expected key.

The base URL is not configuration. Keeping `analysis.http.*` as an "override" would recreate exactly the
path compliance asked to remove: a `rag set` pointing analysis back at a gateway. Tests need a seam, so
the package keeps an unexported `newClient(s Settings, baseURL string, …)` that only `_test.go` files call
with an `httptest` URL. `NewClient` is the exported wrapper that passes the constant.

### One key variable, `GEMINI_API_KEY`, no fallback

`resolveKey` reads `GEMINI_API_KEY` and nothing else. The fallback to `CHAT_API_KEY` was reasonable when
analysis and chat could share a gateway. Now that analysis always goes to Google and chat goes to Bedrock
or a local snap, the fallback would send a non-Google credential to Google. The useful case it served
(one key for both) no longer exists.

The name matches Google's documentation and the operator's mental model ("I have a Gemini API key").
Because the key is passed explicitly, the SDK's own reading of the same variable never happens, and
neither does its `GOOGLE_API_KEY` precedence, so behaviour cannot depend on an unrelated tool's key.

`ANALYSIS_API_KEY` is not read. If it is set while `GEMINI_API_KEY` is not, the not-configured error adds
a rename hint. Dev installs from this branch already have the old name in a systemd drop-in, and a silent
"no key" would send them hunting.

`AuthError` loses its `FellBack` field and the "key belongs to the other provider" branch, since there is
only one source now.

### Keep the SDK out of triage and generation behind a neutral part type

`client.go` gains a package-private `part` type (text, or PDF bytes plus filename) and
`complete(ctx, call, system string, parts []part, schemaName string, schema map[string]any, effort Effort)`.
`documentParts(conv)` returns `[]part`, and the three call sites in `triage.go` and `generate.go` change
only their slice type. The genai import stays in `client.go`, which keeps a future move to Interactions
(or Vertex) a one-file change. It also means the tests of triage, generation and decoding do not need to
know the wire format.

### Request mapping

| Concern | Gateway version | Gemini version |
|---|---|---|
| System prompt | `SystemMessage` | `cfg.SystemInstruction` (a `Content` with one text part) |
| Document label + text | `TextContentPart` | `genai.NewPartFromText` |
| PDF | `FileContentPart`, base64 data URL | `genai.NewPartFromBytes(raw, "application/pdf")`, preceded by its label part |
| Output ceiling | both `max_tokens` and `max_completion_tokens` | `cfg.MaxOutputTokens` — one field, so the dual-spelling workaround and its comment go. 65536 for questions and strategy; **16384 for triage**, added after live runs on `Dell_MASCOM.pdf` ran triage away to 61–62K answer tokens twice. Healthy triage used at most 3,910 reasoning + 633 answer tokens |
| Structured output | `response_format: json_schema`, non-strict | `cfg.ResponseMIMEType = "application/json"`, `cfg.ResponseJsonSchema = schema` |
| Reasoning | `reasoning_effort` | `cfg.ThinkingConfig = &ThinkingConfig{ThinkingLevel: …}`; nil when `EffortUnset`. `IncludeThoughts` stays false |

The comment justifying non-strict mode goes away: Gemini enforces `ResponseJsonSchema` natively. Local
validation stays regardless; it is the only defence against a response that is valid JSON but
semantically short, and the spec requires it.

The schemas are already shallow `map[string]any` JSON Schema. A unit test sends each of the three
(`triageSchema`, `questionsSchema`, `strategySchema`) through the fake server and asserts they arrive
verbatim. Live acceptance by Gemini is a verification task, because the subset Gemini accepts is narrower
than full JSON Schema and that can only be confirmed against the real API.

### Effort mapping, with `none` rejected at load

`ParseEffort` keeps its spellings. A new `thinkingLevel(Effort)` maps `minimal`/`low`/`medium`/`high` onto
the four `ThinkingLevel` constants. `none` is rejected in `LoadSettings` with the key name and the reason
("Gemini 3 models cannot disable reasoning"), because the docs say so for every current model and a
provider 400 mid-run is a worse place to learn it. `minimal` passes through even though 3.1 Pro may
refuse it; that is model-specific, so it surfaces as the provider's error naming the key, as today.
Defaults stay `low` (questions) and `high` (strategy).

### Response and finish-reason mapping

The existing invariant holds: **an empty response is never an absence**. A truncation error carries the
reasoning/answer token split, the ceiling, and a quoted head-and-tail excerpt of the *untrimmed* text. Its
advice follows the split: reasoning-dominated means lower reasoning effort; answer-dominated means a
runaway response, for which reasoning advice is wrong. The mapping becomes:

| Provider signal | Result |
|---|---|
| No candidates and `PromptFeedback.BlockReason` set | new `BlockedError{Call, Reason}` — the prompt itself was blocked |
| `FinishReasonMaxTokens` | `TruncatedError` (with `Empty` when no text came back) |
| `SAFETY`, `RECITATION`, `BLOCKLIST`, `PROHIBITED_CONTENT`, `SPII`, and other policy reasons the SDK defines | `BlockedError{Call, Reason}` |
| `STOP` with no text | `TruncatedError{Empty: true}`, unchanged behaviour |
| `STOP` with text that is not valid JSON | `MalformedResponseError`, unchanged |
| Any other reason | `MalformedResponseError` carrying the reason, which already routes "no usable reason" to a retry hint |

Text comes from the response's `Text()` helper, which concatenates the candidate's text parts and skips
thought parts (checked in the v1.73.0 source). `containsAnyFold`'s spelling list for truncation is simplified to the typed constant.
`MalformedResponseError.looksTruncated` keeps the loose match, because it also formats reasons it did not
classify.

`BlockedError` is a separate type rather than a `MalformedResponseError` reason. The remedy differs: a
blocked RFP needs a human to look at the content, while retrying or lowering effort does nothing. The
message must say that, and the spec forbids reporting a block as truncation.

In verbose mode the per-call line adds `UsageMetadata.ThoughtsTokenCount` and `CandidatesTokenCount`.
The repo's recurring failure is reasoning consuming the budget, and those two numbers are the direct
evidence for it.

### Error classification from `genai.APIError`

`classifyRequestError` switches on `errors.As(err, &genai.APIError{})`:

- **Auth:** `Code` 401 or 403, **or** `Code` 400 whose status/message carries `API_KEY_INVALID` / "API key
  not valid". Gemini reports a malformed or revoked key as 400 `INVALID_ARGUMENT`, not 401, so a
  status-only check would misfile the most common auth failure as a generic bad request. The result is
  `AuthError` naming `GEMINI_API_KEY`. A 403 additionally hints that the Generative Language API may not
  be enabled on the key's project.
- **Model:** `Code` 404 → new `ModelNotFoundError{Model}`, naming `analysis.model`. If the id contains `/`
  (for example `google/gemini-3.1-pro-preview`, `~google/gemini-pro-latest`), it adds "direct Gemini
  model ids are unprefixed, e.g. `gemini-3.1-pro-preview`". This is the predictable failure for every dev
  install that configured OpenRouter.
- **Quota:** `Code` 429 → error naming the model and saying the project's quota or rate limit was hit.
  An unpaid-tier project hits this quickly, which gives a second reason to mention the paid tier.
- **Document rejection:** `Code` 400 whose message names the MIME type or document input →
  `NotDocumentCapableError`, kept for a model id that exists but is not a text+document model (a TTS or
  image model, for instance). Its `FromChat` field and chat-fallback paragraph go away. The signal list is
  rebuilt from Gemini's wording and verified live, not carried over from OpenRouter's.
- **Anything else:** wrapped with the call name, as today.

### Conveyance stores raw PDF bytes; limits unchanged

`Document.PDFBase64 string` becomes `PDFBytes []byte`. `Blob.Data` is raw bytes and the SDK encodes on
send, so storing base64 meant decoding it again on each of three calls. It also made the staging store a
third larger than the documents it held. `Document.size()` and the store's accounting follow.

`DefaultLimits` (32 MiB and 600 pages per PDF, 48 MiB per request) already sit inside Gemini's documented
limits (50 MB / 1000 pages per PDF, 100 MB inline per request). They also match the daemon's 48 MiB upload
cap, so the numbers stay. The comment calling them "the conservative intersection of providers" is
rewritten to cite Gemini's limits and the daemon cap as the reasons.

*Why not the Files API for large PDFs.* It retains uploads server-side between calls, which is what this
change exists to avoid. Inline conveyance within the 48 MiB daemon cap covers every RFP in this repo.

### Configuration: remove `analysis.http.*`, keep everything else

The install hook stops seeding `analysis.http.host/port/path/tls`, and `ResolveEndpoint`, `buildEndpoint`
and the mirrored `confChatHTTP*` constants are deleted. `Settings.BaseURL` and `EndpointFromChat` go, and
`Validate` drops its endpoint reason.

The snapctl backend layers `user` over `package`, and a `user` set is rejected for a key with no
`package` layer. So once the package keys are gone, nobody can set them again. Dev installs from this
branch may still hold either layer, so post-refresh runs `snapctl unset` on both
`config.package.analysis.http` and `config.user.analysis.http`. A stale `user` value cannot then shadow
anything or show up in `rag get`. This runs only on refresh and is harmless where the keys were never
seeded.

No new keys. `analysis.model` keeps no compiled-in default. The install-hook comment's "verified working
values" become `gemini-3.1-pro-preview` (preview; Google gives at least two weeks' notice before
deprecating a preview) and `gemini-3.8-flash` (stable).

### Secrets and snap packaging

The only secret is `GEMINI_API_KEY`, on the environment only: exported for the CLI, and in the `ragd`
systemd drop-in for the daemon. The `snapcraft.yaml` secrets comment is updated to say so. The `ragd` app
**still declares no `environment:` stanza**, for the existing reason: `snap run` would clobber the
drop-in. There are no new plugs; `ragd` and the CLI already hold `network`, which is all an HTTPS call
needs. There are no new bundled binaries and no new hooks, only edits to `install` and `post-refresh`.

The new Go module and its transitive set (`cloud.google.com/go`, `cloud.google.com/go/auth` and their
indirect dependencies) add to the snap. The size delta is measured in the tasks rather than assumed.

### Compliance posture is documented, not inferred

rag-cli cannot tell from the API whether a key belongs to a billing-enabled project. `docs/usage.md`, the
install-hook comment and the `snapcraft.yaml` secrets comment state the requirement plainly:

- use a key from a **paid-tier** project;
- Google still retains prompts briefly for abuse monitoring under the paid terms, which rag-cli cannot
  turn off.

The `--analyze` third-party warning in `docs/usage.md` drops its gateway sentence and names Google as the
one party that receives the documents.

### UI: copy only

`BuildWizard.tsx`'s unavailable notice changes from "a document-capable endpoint, a model id and an API
key — check analysis.enabled, analysis.model and ANALYSIS_API_KEY" to name `GEMINI_API_KEY` and drop the
endpoint. It is still a `p-notification`; there is no new visual pattern, so `ui-conventions` needs no
update, and the change must not introduce colour or layout changes. The matching comments in
`ui/lib/api/answer.ts` and `internal/api/server.go` follow.

## Risks / Trade-offs

- [Google retires `generateContent` in favour of Interactions] → The swap is confined to `client.go`
  behind the neutral `part` type, and Interactions with `store=false` is the named successor. A test
  asserting "no storage" would then have to assert `store=false` on every request.
- [Gemini error wording drifts, so auth, model or document classification misses a case] → Misses
  degrade to the wrapped provider message, never to a wrong diagnosis. The classification table is
  verified against the live API in the tasks, with captured responses kept as test fixtures.
- [The Pro model is preview-only and can be deprecated on short notice] → `analysis.model` stays
  configuration, `ModelNotFoundError` names the key to change, and `gemini-3.8-flash` is documented as
  the stable fallback.
- [The key is on an unpaid-tier project and RFP content is used for product improvement] → It cannot be
  detected from the API, so it is documented prominently, and the compliance owner confirms the project's
  billing state before `analysis.enabled=true` is set anywhere (see Migration).
- [New dependency weight in the snap] → Measured; `openai-go` is not removed because chat still needs it.
- [JSON Schema subset mismatch: Gemini rejects a keyword the schemas use] → Caught on the first live
  triage call (loud 400, cheap), and fixed by simplifying the schema, never by turning schema output off.
- [Removing `analysis.http.*` removes a debugging/proxy escape hatch] → Intentional. Tests use the
  unexported seam, and a corporate egress proxy still works through the standard `HTTPS_PROXY` handling
  of Go's HTTP transport.

## Migration Plan

The feature has not shipped: `main` seeds no `analysis.*` key, so no edge or stable install needs
migrating. For dev installs of this branch:

1. Rename the systemd drop-in variable from `ANALYSIS_API_KEY` to `GEMINI_API_KEY` (or re-export it for the
   CLI). The not-configured error says so if this is missed.
2. Change `analysis.model` from an OpenRouter id (`google/…`) to a direct one (`gemini-3.1-pro-preview`).
   The model-not-found error says so if this is missed.
3. Refresh the snap; post-refresh unsets the four removed keys on both layers.
4. Before enabling analysis anywhere, have the compliance owner confirm the key's project is
   billing-enabled.

Sequencing: `add-gemini-rfp-analysis` is archived first, so `answer-manifest-analysis` exists in
`openspec/specs/` for this change's delta to apply to. That change's open end-to-end tasks (11.2–11.7)
are run against the **new** wiring, since running them against OpenRouter would verify a path that is
being removed.

Rollback is a snap revert. Nothing persists outside config, and staged analysis sessions are in-memory.

## Open Questions

- **Vertex AI instead of the Gemini API?** If compliance wants data residency, a GCP-project contract,
  or IAM identities rather than an API key, Vertex AI is the stronger fit. It uses the same SDK with
  `Backend: BackendVertexAI` and a project/location, so the change would be contained. It is not done
  here because the stated requirement is "Gemini with an API key".
- **Should `minimal` reasoning be rejected for Pro-tier ids too?** Today it passes through and fails at
  the provider. Rejecting it would mean model-specific rules in config validation, which ages as badly as
  a compiled-in model id.
