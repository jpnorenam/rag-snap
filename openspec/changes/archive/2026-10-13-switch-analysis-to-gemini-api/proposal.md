## Why

RFP analysis (`answer batch --build --analyze`, change `add-gemini-rfp-analysis`) currently reaches Gemini
through OpenRouter. For compliance reasons, confidential RFP content must go **directly to Google's
Gemini API under our own API key**, with no gateway as an extra data processor. This has to happen
before the analysis branch ships, because a gateway-shaped feature would otherwise go out by default.

Pointing the existing OpenAI-compatible client at Google does not work. Gemini's own
OpenAI-compatibility layer still documents image and audio input only, with no PDF or `file` part. That
gap is why the original design chose a gateway. The client therefore has to change, not just the URL.

## What Changes

- **BREAKING (unreleased feature):** analysis calls the native Gemini API (`generateContent` on
  `generativelanguage.googleapis.com`) through the official `google.golang.org/genai` Go SDK, replacing
  the `openai-go` chat-completions call to a gateway. PDFs go as inline document parts; converted
  spreadsheets and DOCX still go as Markdown text, unchanged.
- **BREAKING:** the analysis endpoint is no longer configurable. The `analysis.http.host` / `port` /
  `path` / `tls` package keys are removed, along with the fallback to the chat backend's endpoint.
  Analysis can only reach Google.
- **BREAKING:** the key is read from `GEMINI_API_KEY` only. The `ANALYSIS_API_KEY` variable and the
  fallback to `CHAT_API_KEY` are removed. That fallback would send a Bedrock or other chat-backend
  credential to Google. A leftover `ANALYSIS_API_KEY` gets a rename hint rather than being silently
  ignored.
- Ambient SDK configuration is neutralised. The base URL, backend and key are pinned explicitly, so
  `GOOGLE_GEMINI_BASE_URL`, `GOOGLE_GENAI_USE_VERTEXAI`, `GOOGLE_API_KEY` and similar variables on the
  host cannot reroute or re-authenticate RFP traffic.
- Requests are stateless and leave nothing stored server-side. No Files API uploads, and the
  Interactions API (which stores requests for 55 days by default) is not used.
- Reasoning effort maps onto Gemini `thinking_level`. `none` is rejected at config load, because no
  Gemini 3 model can turn thinking off. Output ceiling, JSON-schema output, truncation detection and
  local validation keep their current guarantees, re-expressed in Gemini terms.
- Error reporting is re-derived for Gemini:
  - an invalid key (which Gemini reports as HTTP 400, not 401);
  - an unknown model id, including a leftover OpenRouter id such as `google/gemini-3.1-pro-preview`;
  - a prompt or response blocked by safety filtering, as a distinct failure.
- Documentation, install/post-refresh hooks, the `snapcraft.yaml` secrets comment and the UI's
  "analysis unavailable" copy are updated to the direct-Gemini configuration. They now state that the key
  must belong to a **billing-enabled (paid-tier)** project, because Google's terms let unpaid-tier
  content be used for product improvement and human review.

No new config keys are added. `analysis.enabled`, `analysis.model`, `analysis.reasoning.*` and
`analysis.answer.*` keep their names, `package` scope and defaults. `analysis.model` still has no
compiled-in default; the documented values become bare Gemini ids (`gemini-3.1-pro-preview`,
`gemini-3.8-flash`).

**Out of scope:** replacing Mistral as the chat/answering model on Bedrock. That model belongs to the
inference server (`chat.*`), which analysis explicitly does not touch. It is tracked separately because
Claude on Bedrock is not documented on the OpenAI-compatible route the chat backend uses today.

## Capabilities

### New Capabilities

_None._

### Modified Capabilities

- `answer-manifest-analysis`: the configuration-and-authentication requirement changes. The endpoint is
  fixed to the Gemini API instead of defaulting to the chat backend, the key comes from one
  Gemini-specific variable with no fallback, ambient SDK environment cannot redirect traffic, and no
  request content is stored by the provider beyond the call. A new requirement pins analysis traffic to
  the Gemini API and makes a safety-blocked response a named failure. This capability is introduced by
  the still-open change `add-gemini-rfp-analysis`, so that change must be archived before this one.

## Impact

- **External services:** none of the three backends changes. OpenSearch, the inference server and Tika
  are untouched; Tika still converts Office formats for analysis exactly as before. Analysis talks to a
  fourth, analysis-only endpoint: Google's Gemini API.
- **Code:** `cmd/cli/basic/analysis/` — `client.go`, `config.go` and `errors.go` are rewritten.
  `convey.go` stores PDF bytes raw, not base64. `triage.go` and `generate.go` stop building `openai`
  content parts. Tests move from a fake OpenAI server to a fake Gemini server. `internal/api/server.go`
  and `handlers_answer_analysis.go` only change comments and copy.
- **Dependencies:** adds `google.golang.org/genai` (v1.73.0) and its transitive Google auth modules.
  This is a snap size delta to measure. `openai-go` stays, because chat and answering still use it.
- **Config:** removes four `package` keys (`analysis.http.*`) from the install hook and the post-refresh
  seeding. Post-refresh also unsets them on installs that already seeded them from this branch.
- **Secrets:** `GEMINI_API_KEY` replaces `ANALYSIS_API_KEY` on the `ragd` systemd drop-in. The `ragd`
  app still declares no `environment:` stanza.
- **User-facing surfaces:** no new command or flag. The `--analyze` conveyance line now names the Gemini
  API rather than a URL, and error messages change. The UI's "analysis unavailable" notice in
  `BuildWizard.tsx` changes copy. Documentation that must change:
  - `docs/usage.md` (analysis configuration section and example output)
  - `docs/rest-api.md` (daemon secrets)
  - the install hook comment block.
