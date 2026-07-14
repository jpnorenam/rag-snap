# Tasks: add-prompts-api-and-ui

Before starting: read `docs/ux/00-foundation.md`, then `docs/ux/05-prompts.md` (this change's UX
doc). UI conventions are codified in the `ui-conventions` skill.

## 1. Daemon prompt store (rest-api-prompts persistence)

- [x] 1.1 Add a file-backed prompt store in `internal/api` at `$SNAP_COMMON/ragd/prompts.json` (same `$SNAP_COMMON` resolution + temp-dir fallback pattern as `token.go`): mutex-guarded, atomic temp+rename writes (0600), `PromptConfig` JSON schema, storing overrides only
- [x] 1.2 Implement resolution semantics: absent/empty field → built-in default; unparseable store → log a warning, resolve to defaults, report not customized; a stored value byte-identical to the default is cleared, not kept
- [x] 1.3 Unit-test the store outside a snap (via the `$SNAP_COMMON` env override): set/reset/persist-across-reopen, corrupt-file fallback, default-equality clearing

## 2. REST endpoints (rest-api-prompts surface)

- [x] 2.1 Add `handlers_prompts.go`: `GET /1.0/prompts` (fixed order, `{name, value, default, customized}`), `GET /1.0/prompts/{name}`, `PUT /1.0/prompts/{name}` (reject empty/whitespace-only), `DELETE /1.0/prompts/{name}` (idempotent); unknown names → 404 listing valid names
- [x] 2.2 Register the routes in `server.go`'s shared `/1.0` route set behind `requireAuth` (both unix-socket and loopback listeners), and advertise the `prompts` API extension
- [x] 2.3 Handler tests: list/get shapes, customized flag transitions, empty-value 400, unknown-name 404, reset no-op, auth required on loopback

## 3. Daemon chat/answer honor stored prompts

- [x] 3.1 Swap `chat.LoadPrompts()` for the store resolver in `handlers_chat.go` (session start) and `handlers_answer.go` (operation start); which template feeds which call is unchanged (chat → `chat_system_prompt`; batch → `answer_system_prompt`, plus `source_rules` only for a custom manifest prompt), and running sessions/operations keep their resolved prompts
- [x] 3.2 Tests: a customized prompt is used by a new chat session and a new batch operation; an edit mid-session/mid-run does not change the one in flight (incl. a wire-level test asserting the stored prompt is the system message on the daemon's inference request)

## 4. CLI `prompt init` daemon-first

- [x] 4.1 Add prompt methods to `internal/apiclient` (list/get/put/delete mirroring the endpoint shapes)
- [x] 4.2 Rewire `prompt init` (`cmd/cli/basic/prompt.go`): when `daemonClient()` finds ragd, load current + default values from the API and save via PUT (offer reset via DELETE when customized), preserving the existing `huh` select/edit flow verbatim; keep the local-file path unchanged as the daemonless fallback (command order in `cmd/cli/main.go` untouched — no new commands)
- [x] 4.3 Add the migration offer: on the daemon path, when the legacy local `prompts.json` has values differing from defaults and the daemon store still has those prompts as default, confirm before PUTting them; never migrate silently
- [x] 4.4 Update `prompt`/`prompt init` `--help`/long text: drop the unconditional `~/.config/rag-cli/prompts.json` promise, state the daemon-first behavior and the fallback (`apps/completion.bash` needs no change — no new commands or flags)

## 5. UI Prompts page

- [x] 5.1 Add `ui/lib/api/prompts.ts` (`listPrompts`, `savePrompt`, `resetPrompt`) and a `putSync` sibling in `ui/lib/api/envelope.ts` following the existing `request()` pattern
- [x] 5.2 Build `ui/app/prompts/page.tsx` + `PromptCard`: three stacked `<section>` cards in fixed order, H2 + Default/Customized text chip, ~4-line effective-prompt preview (`p-code-snippet__block` with fade) + Edit; styles in `globals.scss` under `// --- prompts ---` as `.prompt-card` BEM, colors via `--vf-*` tokens
- [x] 5.3 Edit mode: labelled monospace textarea (min-height ~16 lines, autogrow), `<details>` "View default prompt" (read-only, copyable), Save (`p-button--positive`, disabled until dirty), Cancel, reset-to-default (`p-button--base`, only when customized) through the shared `ConfirmModal`; Escape = Cancel; one card in edit mode at a time
- [x] 5.4 Dirty guards: confirm on switching cards / in-app navigation with unsaved edits + `beforeunload`; save success shows "Prompt saved. New chats and batch runs will use it."; save failure keeps the textarea content with a retry notification
- [x] 5.5 Loading = three fixed-height skeleton cards (no shift); load error = foundation §7 with editing blocked; flip the sidebar "Prompts" entry to `enabled` in `ui/components/Sidebar.tsx`

## 6. Documentation

- [x] 6.1 Document the prompt endpoints in `docs/rest-api.md` and `rest-api.yaml` (`make spec` regenerates the OpenAPI from the `swagger:route` annotations; `make spec-check` passes)
- [x] 6.2 Document the Prompts page in `docs/local-ui.md` (and drop Prompts from the "Soon" list)
- [x] 6.3 Update `docs/usage.md`: `prompt init` daemon-first behavior, store precedence (daemon store vs. legacy local file), and the migration offer

## 7. Validation

- [x] 7.1 Run `make all` (tidy fmt vet lint test build) and `cd ui && npm run build` — tidy/fmt/vet/test/build and the UI export all pass. `make lint` fails on **164 pre-existing** repo-wide issues (no CI lint gate today); every file this change touches is lint-clean, and `cmd/cli/basic/prompt.go` is now cleaner than its baseline (its 2 pre-existing `nilerr` hits and missing doc comment are fixed).
- [x] 7.2 Build the snap (cleaned the `go-cli` part first per the stale-part gotcha) and verify the change actually shipped: the packed `rag-cli_0.0.5_amd64.snap` contains the prompt routes in `bin/ragd` and the new Prompts page in its embedded UI (`prompt-card`).
- [x] 7.3 In-snap validation against the installed snap (rev x5), over the real unix socket. **Verified:** the `prompts` API extension is advertised; `GET /1.0/prompts` returns the three templates in CLI order, all reporting default; `PUT` customizes (`customized=true`); an empty/whitespace value is rejected 400 with the "use DELETE to reset" message; an unknown name is 404 naming the three valid prompts; a value equal to the default clears the override (`customized=false`); the store at `$SNAP_COMMON/ragd/prompts.json` is written 0600 and holds **only the override** (159 bytes, not a copy of all three defaults); `DELETE` restores the shown default **byte-for-byte** (845 chars) and a second `DELETE` is a 200 no-op; the loopback listener refuses `/1.0/prompts` without the token (403) and serves it with one; the daemon serves the Prompts page's JS chunk containing all of its copy. Daemon left with all three prompts back at their defaults.
      **Not covered here:** (a) restart persistence — needs `sudo snap restart rag-cli.ragd` (the property is covered by a store unit test); (b) a customized prompt reaching the LLM on *this* machine — blocked by an environment gap, see the note below.
- [x] 7.4 UI verified as far as is possible without a browser: the installed daemon serves the Prompts page and its chunk (titles, "View default prompt", "Reset to default", "Discard unsaved changes", and the post-save sentence all present). Statically verified: only sanctioned Vanilla patterns (every class confirmed present in `vanilla-framework@4.51`), zero hardcoded hex, all four view states, no clickable `<div>`s, no `window.confirm`. **Left for a human at the browser:** both themes, keyboard-only walkthrough, 620px layout (tasks 8.2–8.4).

### Environment note (found during 7.3) — not caused by this change

`knowledge.model.embedding` is **not configured** on this machine, so retrieval is unavailable
(`POST /1.0/search` → 500 "embedding model is not configured"). Two consequences:

- A batch run returns the fixed no-context answer **without calling the LLM**, so a customized
  `answer_system_prompt` cannot be observed here.
- `handleChatStart`'s pre-existing guard fell back to `"You are a helpful assistant."` when
  retrieval was unavailable, **ignoring `chat_system_prompt` entirely** in that state — while the
  new UI told the user "New chats and batch runs will use it". **Resolved by task 9**: a
  configured prompt (customization *or* built-in default) is now sent unconditionally.

## 9. Follow-up: the configured chat prompt is always sent

The fix landed in two steps. First, a `chat.SystemPromptFor` helper honoured a *customized*
prompt without retrieval but kept the generic fallback for the uncustomized default. Validation
showed that was still wrong — with prompts at their defaults and OpenSearch down, "Who are you?"
answered as a generic LLM, while the prompts API displayed a default that never ran. The fallback
is now deleted entirely.

- [x] 9.1 Send the configured `chat_system_prompt` unconditionally at both call sites —
      `handleChatStart` (daemon) and the direct CLI REPL in `chat/client.go`, which had the same
      bug against the local prompts file. No helper, no hidden substitute prompt.
- [x] 9.2 Amend the `rest-api-chat` delta: the system prompt is the stored value (customization or
      built-in default) regardless of retrieval availability; scenarios for both.
- [x] 9.3 Wire-level test on a retrieval-less daemon: the built-in default is the system message
      when uncustomized; the customized prompt after a PUT.
- [x] 9.4 End-to-end against the **real** inference backend (Bedrock `mistral-large-3`, retrieval
      unavailable): a `chat_system_prompt` customized with a begin-with-`ZEBRA-42:` rule produced
      `ZEBRA-42: …` on a fresh session (impossible unless the prompt reached the model); with the
      default text sent, "Who are you?" answered with the Canonical-assistant persona. Prompts
      reset afterwards; daemon left with all defaults.
- [ ] 9.5 **Follow-up product decision (out of scope here):** with the RAG default prompt and no
      retrieved context, the model can answer from parametric memory while *claiming*
      `[CANONICAL]` context (observed live). Prefixing the turn with the existing "No relevant
      context was retrieved" note (already used on the KB-active-but-zero-hits path) converts that
      into an honest refusal (also verified live) — but applying it to every retrieval-less turn
      changes deliberate no-KB chat too, so it needs a deliberate decision, not a drive-by.

## 8. Definition of done (UX) — from docs/ux/00-foundation.md + docs/ux/05-prompts.md

Implemented and verified by reading the code / building; the four items needing a running browser are left unchecked deliberately (see 7.3/7.4).

- [x] 8.1 All four view states implemented per foundation §7; mutations follow §7's in-flight/success/failure rules (Save disabled + "Saving…" in flight; failure keeps the draft)
- [ ] 8.2 Looks correct in light **and** dark themes (`is-dark`) — needs a browser
- [ ] 8.3 Usable at 620px (collapsed rail) — no horizontal page scroll — needs a browser
- [ ] 8.4 Keyboard-only walkthrough passes (§9); focus management on modals/routes verified — needs a browser (focus handling comes from the shared `ConfirmModal`; the editor takes focus on open; Escape cancels)
- [x] 8.5 Only sanctioned patterns (§6) used; every Vanilla class verified to exist in the installed framework; no new pattern introduced, so `docs/ux/00-foundation.md` needs no addition
- [x] 8.6 All colors via `--vf-*` tokens; zero hardcoded hex (the preview fade is a `mask-image`, so it needs no color at all)
- [x] 8.7 Empty states include the CLI-equivalent command — n/a: there is no empty state, the three prompts always exist
- [x] 8.8 Default vs customized state visible per prompt (text chip, not color alone); the built-in default is viewable and copyable in a `<details>` while editing
- [x] 8.9 Reset-to-default flows through the shared confirm modal and restores the daemon's default (the same `default` string the card displayed)
- [x] 8.10 Unsaved-changes guards on card switch, in-app navigation (`useUnsavedGuard`), and page unload (`beforeunload`)
- [x] 8.11 Post-save copy accurately reflects when the prompt takes effect — prompts resolve at session/operation start (enforced by a test), so "New chats and batch runs will use it" is true
- [x] 8.12 Monospace editing area: the textarea inherits Vanilla's `%vf-input-elements` colors (this change overrides only font/spacing), so its colors are token-driven in both themes
