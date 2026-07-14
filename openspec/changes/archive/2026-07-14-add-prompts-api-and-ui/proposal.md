# Proposal: add-prompts-api-and-ui

## Why

The three RAG system prompts (`chat_system_prompt`, `answer_system_prompt`, `source_rules`) are customized via `prompt init`, which writes a **client-local** file (`os.UserConfigDir()/rag-cli/prompts.json`). The daemon cannot read that file — and since `chat` and `answer batch` now prefer the daemon whenever it is running ([chat.go:47-49](../../../cmd/cli/basic/chat.go#L47), [answer.go:74](../../../cmd/cli/basic/answer.go#L74)), daemon-routed runs silently fall back to `chat.LoadPrompts()` in the *daemon's* context ([handlers_chat.go:100](../../../internal/api/handlers_chat.go#L100), [handlers_answer.go:116](../../../internal/api/handlers_answer.go#L116)), which resolves to the service's own home and always yields the built-in defaults. **User prompt customizations are silently ignored on every daemon-routed chat and batch run today** — a latent parity bug, not just a missing UI screen. This change gives prompts a daemon-owned store with a REST API, makes daemon chat/answer honor it, and ships the `/prompts/` UI page (parity plan Change 5, stories 5.1–5.3).

## What Changes

- **New REST capability `rest-api-prompts`**: read the three prompt templates (effective value, built-in default, and a customized flag per prompt), update one (`PUT`), and reset one to its default (`DELETE`). Prompts persist in a daemon-owned file under `$SNAP_COMMON` (same precedent as the ragd socket and localhost token), surviving daemon restarts and snap refreshes.
- **Daemon chat/answer honor stored prompts**: `POST /1.0/chat` sessions use the stored `chat_system_prompt` and `source_rules`; `POST /1.0/answer/batch` uses the stored `answer_system_prompt` and `source_rules`. Prompts are resolved when the session/operation starts — mid-session edits apply to *future* sessions and runs, and the UI copy says so.
- **New `/prompts/` UI page**: three stacked prompt cards (fixed CLI-select order) showing a Default/Customized chip and a preview of the effective prompt; edit-in-place with a monospace textarea, a viewable/copyable built-in default, Save/Cancel, reset-to-default behind a confirm modal, and unsaved-changes guards. The sidebar's "Prompts" entry flips from a "Soon" placeholder to a live route.
- **CLI `prompt init` becomes a daemon client**: when ragd is reachable it reads/writes the daemon store through the new API (single source of truth shared with the UI); the legacy local file remains only as the fallback for daemonless direct runs. When local customizations exist that the daemon store doesn't have, `prompt init` offers a one-time migration (re-save to the daemon); nothing is migrated silently.

No breaking changes: the JSON schema of the prompts file is unchanged, existing endpoints keep their contracts, and daemonless CLI behavior is untouched.

## Capabilities

### New Capabilities

- `rest-api-prompts`: daemon-owned prompt-template store and its REST surface — list prompts with defaults and customized state, update a prompt, reset a prompt to its built-in default; persistence and validation rules.

### Modified Capabilities

- `rest-api-chat`: the RAG-grounding requirement changes — chat sessions SHALL use the daemon-stored prompt templates (`chat_system_prompt`, `source_rules`) instead of client-local defaults, resolved at session start.
- `rest-api-answer`: the batch-run requirement changes — batch generation SHALL use the daemon-stored `answer_system_prompt` and `source_rules`, resolved at operation start.
- `local-ui-app`: adds the Prompts page requirement set (cards, edit flow, reset, dirty-tracking, post-save semantics) and flips the sidebar "Prompts" entry from placeholder to live route.

## Impact

- **Code**:
  - `internal/api/` — new prompts store (file-backed under `$SNAP_COMMON/ragd/`) + `handlers_prompts.go`; route registration in `server.go`; `handlers_chat.go` / `handlers_answer.go` switch from `chat.LoadPrompts()` to the store.
  - `cmd/cli/basic/prompt.go` + `internal/apiclient/` — `prompt init` gains a daemon-first path and the migration offer; help text updated (it currently promises `~/.config/rag-cli/prompts.json` unconditionally).
  - `ui/` — new `ui/app/prompts/page.tsx`, prompt-card components, `ui/lib/api/prompts.ts`, `putSync`/`deleteSync` usage in `ui/lib/api/envelope.ts` (adding `putSync` as the first change to need it), sidebar flip, styles in `globals.scss`.
- **APIs**: new `GET /1.0/prompts`, `PUT /1.0/prompts/{name}`, `DELETE /1.0/prompts/{name}`. No changes to existing endpoint shapes; `POST /1.0/chat` and `POST /1.0/answer/batch` change behavior (which prompts feed generation), not contract.
- **External services**: **none of the three services gains a new touchpoint** — OpenSearch is not touched; Tika is not touched; the inference server is unchanged (stored prompts feed the *existing* chat/answer inference calls). Everything new is daemon-internal.
- **Config**: **no new config keys** (neither `package` nor `user` scope). Prompts are deliberately *not* snapctl config — they are long multi-line texts, and user-scope `set` would require seeding them as package keys and root gating; a `$SNAP_COMMON` file matches how ragd already persists state. No new snap interfaces, plugs, or hooks; no new secrets/environment variables.
- **User-facing surface**: new UI page (browser); changed CLI behavior for `prompt init` (daemon-first) with updated `--help` text. No new CLI commands/flags or slash commands, so `apps/completion.bash` is unaffected. Documentation to update: `docs/rest-api.md` + `rest-api.yaml` (new endpoints), `docs/local-ui.md` (Prompts page), `docs/usage.md` (`prompt init` daemon behavior + migration note).
- **UX guidelines**: governed by `docs/ux/00-foundation.md` and `docs/ux/05-prompts.md`; the design links them and tasks carry their Definition of done checklist. One UX-doc adjustment: the "CLI customizations not yet migrated" notification is driven by the CLI-side migration offer, because the daemon cannot read per-user CLI files (per-user snap data under strict confinement) — see design.
- **Dependencies**: none added (Go or npm).
