# Design: add-prompts-api-and-ui

UX authority: [`docs/ux/00-foundation.md`](../../../docs/ux/00-foundation.md) (read first) and
[`docs/ux/05-prompts.md`](../../../docs/ux/05-prompts.md) (this change's doc). Where this design
and those docs overlap, the UX docs win; UI conventions are also codified in the `ui-conventions`
skill (`.claude/skills/ui-conventions/SKILL.md`).

## Context

The three RAG prompt templates live in code as defaults
([prompts.go](../../../cmd/cli/basic/chat/prompts.go): `PromptConfig`, `DefaultPrompts()`) and are
customized per-user by `prompt init`, which writes `os.UserConfigDir()/rag-cli/prompts.json`.
Under strict confinement that resolves inside the invoking user's snap data dir — a file only that
user's CLI processes can read.

The daemon reuses the same loader: `handlers_chat.go:100` and `handlers_answer.go:116` call
`chat.LoadPrompts()`, which in the ragd service context resolves to the *service's* home
(`$SNAP_COMMON/home/snap_daemon`, per snapcraft.yaml) where no file exists — so daemon-routed runs
always use built-in defaults. Meanwhile the CLI's `chat` and `answer batch` prefer the daemon
whenever it is running ([chat.go:47](../../../cmd/cli/basic/chat.go#L47),
[answer.go:74](../../../cmd/cli/basic/answer.go#L74)). Net effect: on any machine with ragd
active, `prompt init` customizations are dead weight.

Existing precedent for daemon-owned state: the unix socket at `$SNAP_COMMON/ragd/unix.socket`
(`internal/api/socket.go`) and the localhost token file under `$SNAP_COMMON`
(`internal/api/token.go`, with a `$SNAP_COMMON`-env override that tests use).

Constraints that shape everything below:

- Config is snapctl-only with package→user precedence; user `set` rejects keys that don't already
  exist as package keys, and `config set` is root-gated. Multi-line prompt bodies fit that model
  badly.
- The daemon cannot read per-user CLI files (distinct `$HOME`s, strict confinement) — any
  "migrate my CLI prompts" flow must be driven by a process running *as the user*.
- UI: static export, no new dependencies, Vanilla patterns only, all four view states.

## Goals / Non-Goals

**Goals:**

- One authoritative prompt store owned by the daemon; chat and batch-answer runs routed through
  the daemon honor it.
- Full CRUD-minus-create over the three prompts via REST (read with defaults + customized state,
  update, reset), consumed by both the UI and `prompt init`.
- `/prompts/` UI page per `docs/ux/05-prompts.md`; sidebar entry goes live.
- A deliberate, user-visible migration path from the CLI-local file — never silent.

**Non-Goals:**

- No per-user prompts: the daemon store is machine-global, like every other daemon resource
  (knowledge bases, operations). Accepted for a local, effectively single-user tool.
- No prompt versioning/history, no templating variables, no fourth prompt.
- No change to *daemonless* CLI behavior: direct `chat`/`answer` runs (no ragd) keep reading the
  local file via `chat.LoadPrompts()`.
- No mid-session re-resolution: a running chat session or batch operation keeps the prompts it
  started with.

## Decisions

### D1: Prompts persist as a JSON file under `$SNAP_COMMON/ragd/`, not snapctl config

New store in `internal/api` (e.g. `prompts.go`): a mutex-guarded, file-backed store at
`$SNAP_COMMON/ragd/prompts.json` (same `$SNAP_COMMON`-env resolution + temp-dir fallback as
`token.go`, so tests run outside a snap). File schema is exactly the existing `PromptConfig` JSON
(`source_rules`, `answer_system_prompt`, `chat_system_prompt`) — one shape everywhere. Only
customized values are stored; an absent/empty field means "use the built-in default" (the same
fill-from-defaults semantics `LoadPrompts` already has). Writes are atomic (write temp file in the
same dir, then rename), mode 0600, and the store re-reads on each resolution — no cache to
invalidate, and a file this size makes caching pointless.

*Why not snapctl config:* prompt bodies are long multi-line texts, not dot-namespaced scalars;
user-scope `set` would require seeding all three as package keys in the install hook and would
root-gate edits, which contradicts the UI editing flow over the authenticated loopback. The
`$SNAP_COMMON` file matches how ragd already persists the socket and token, survives snap
refreshes (unlike `$SNAP_DATA`, which is per-revision), and keeps `pkg/storage` untouched.
*Why not `$SNAP_DATA`:* refresh/revert semantics — prompts must not silently revert with a
rollback of the snap revision.

### D2: REST surface — `GET /1.0/prompts[/{name}]`, `PUT /1.0/prompts/{name}`, `DELETE /1.0/prompts/{name}`

All sync responses, all behind `requireAuth`, registered in `server.go`'s shared route set (so
both the unix socket and loopback listener get them). `{name}` is one of the three fixed keys;
anything else is a 404 with a message listing the valid names.

- `GET /1.0/prompts` → array of three views, in the CLI's fixed select order:
  `{ "name", "value", "default", "customized" }` where `value` is the *effective* prompt,
  `default` is the built-in, and `customized` is whether a stored override exists. Returning the
  default alongside the value is what lets the UI render the "View default prompt" affordance and
  the chip without a second endpoint.
- `GET /1.0/prompts/{name}` → the single view (used by `prompt init`).
- `PUT /1.0/prompts/{name}` with `{ "value": "…" }` → stores the override. Empty/whitespace-only
  values are rejected 400 (reset is explicit, not a side effect of clearing a textarea). A value
  byte-identical to the built-in default clears the override instead of storing a copy — so
  `customized` never lies, and a future release's improved default isn't shadowed by a stale
  identical copy.
- `DELETE /1.0/prompts/{name}` → removes the override; the effective value returns to the
  built-in default. Idempotent (deleting a non-customized prompt is a 200 no-op).

*Why PUT/DELETE per prompt, not one PUT of the whole trio:* the UI edits one card at a time and
the CLI edits one selection at a time; per-prompt writes can't clobber a concurrent edit of a
*different* prompt, and DELETE gives reset an honest, cache-friendly verb. This is also the first
change to need `putSync` in the UI envelope — add it as a sibling of `getSync`/`postSync`/
`deleteSync`, per foundation §5.

### D3: The daemon resolves prompts at session/operation start

`handlers_chat.go` and `handlers_answer.go` swap `chat.LoadPrompts()` for the store's resolver at
the point they already load prompts: chat resolves the config when the session starts; answer
resolves it when the batch operation starts. Running sessions/operations are unaffected by later
edits.

Which template actually feeds which call is existing behavior and is *not* changed here — only the
source of the values is. For the record: chat reads `chat_system_prompt` only
([handlers_chat.go:103](../../../internal/api/handlers_chat.go#L103), mirroring
[client.go:117](../../../cmd/cli/basic/chat/client.go#L117)); batch reads `answer_system_prompt`,
and `source_rules` is appended *only* when the manifest carries a custom `prompt`
([batch.go:218-223](../../../cmd/cli/basic/chat/batch.go#L218)). Injecting `source_rules` into
chat would be a behavior change beyond this change's remit and would diverge from the CLI REPL.

**Amendment (in-snap validation finding):** both chat paths carried a guard that replaced the
system prompt with `"You are a helpful assistant."` whenever retrieval was unavailable — silently
discarding a *customized* prompt, and (as validation against the real backend showed) also lying
by omission in the default case: the prompts API displayed one default while sessions ran a hidden
substitute. The rule is now the simplest one: **the configured `chat_system_prompt` is sent
unconditionally** — customization or built-in default, with or without retrieval. No helper, no
hidden prompt; the guard is deleted at both call sites (daemon handler and direct REPL).

Live evidence against Bedrock `mistral-large-3` with retrieval unavailable informed this: the
default RAG prompt without retrieval answers "Who are you?" with the intended persona and refuses
ungroundable trivia, but for on-topic questions it can answer from parametric memory while
*claiming* `[CANONICAL]` context — fabricated provenance. Prefixing the turn with the existing
"No relevant context was retrieved" note (already injected on the KB-active-but-zero-hits path)
converts that into an honest refusal. Extending that note to every retrieval-less turn also
changes the shape of deliberate no-KB chat, so it is recorded as a **follow-up product decision**,
not smuggled into this change.

*Why not re-resolve per turn:* it would make the UI's post-save message ("new chats and batch
runs will use it") wrong, create mid-conversation behavior shifts the user didn't ask for, and
complicate the session state the spec says the daemon owns. The UX doc explicitly calls the
post-save copy load-bearing; this decision is what makes it true.

### D4: `prompt init` becomes daemon-first; migration is a CLI-side offer

`prompt init` follows the same pattern as `chat`/`answer`: if `daemonClient()` finds a running
ragd, the select/edit flow reads current + default values from `GET /1.0/prompts` and saves via
`PUT` (reset via `DELETE` can be offered in the same flow when the prompt is customized). Without
a daemon it falls back to today's local-file behavior unchanged. Help text drops the unconditional
`~/.config/rag-cli/prompts.json` promise and states both paths.

**Migration:** when running daemon-first and the legacy local file contains values that (a) differ
from the built-in defaults and (b) differ from the daemon's stored values for prompts the daemon
still has as default, `prompt init` opens with a `huh` confirm: "You have CLI prompt
customizations the daemon doesn't use. Re-save them to the daemon now?" Accepting PUTs them;
declining proceeds without copying (and the offer naturally reappears only while the divergence
persists — once pushed, the daemon is customized and condition (b) goes false). Nothing is
migrated silently, and the local file is left in place for daemonless runs.

**UX-doc deviation (recorded):** `docs/ux/05-prompts.md` sketches a one-time in-UI
"re-save your CLI customizations" notification, contingent on "the API should surface this". The
daemon cannot see per-user CLI files, so the API cannot honestly surface it; the migration hint
lives in `prompt init` (the only process that can read the file) and in `docs/usage.md` instead.
The UI page ships without that notification — per the UX doc's own rule to never promise what the
backend doesn't do.

### D5: UI page anatomy follows `docs/ux/05-prompts.md` exactly

`ui/app/prompts/page.tsx` + a `PromptCard` component; `ui/lib/api/prompts.ts` exposing
`listPrompts()`, `savePrompt(name, value)`, `resetPrompt(name)` over the envelope helpers. Three
stacked `<section>` cards in the fixed order (`chat_system_prompt`, `answer_system_prompt`,
`source_rules`), each: H2 + Default/Customized chip (`p-chip` / `p-chip--positive`, text label);
collapsed preview of the effective prompt (~4 lines, `p-code-snippet__block`, fade-out) + Edit;
expanded: monospace `<textarea>` (label wired, min-height ~16 lines, autogrow), a `<details>`
"View default prompt" rendering the built-in default read-only/copyable, footer Save
(`p-button--positive`, disabled until dirty), Cancel, and — only when customized — Reset to
default (`p-button--base`) behind the shared `ConfirmModal`. One card in edit mode at a time;
switching cards or navigating with unsaved changes prompts via the shared confirm modal, plus a
`beforeunload` guard. Escape in edit mode = Cancel (with dirty confirm). Post-save positive
notification: "Prompt saved. New chats and batch runs will use it." (true by D3). Save failure
keeps the textarea content and shows a negative notification with retry. Loading = three
fixed-height skeleton cards; error = foundation §7 with editing blocked; there is no empty state
(defaults always exist). Styles in `globals.scss` under `// --- prompts ---` as `.prompt-card`
BEM; textarea colors from `--vf-*` tokens (verified in both themes).

### D6: Reset restores the *shown* default

The reset confirm modal and the `<details>` default view both render the `default` string the API
returned with the list — the same string the daemon will serve after the DELETE. This makes the UX
checklist item "restores the shown default exactly" structurally true even across a snap refresh
that changed the built-in default (the page shows the new default because the daemon serves it).

## Risks / Trade-offs

- **[Two stores can diverge]** Daemonless CLI runs read the local file; daemon runs read
  `$SNAP_COMMON`. → Deliberate: the daemon store is authoritative whenever ragd is involved
  (which is the default path); `prompt init` daemon-first + the migration offer converge users
  onto it, and `docs/usage.md` states the precedence plainly. Killing the local file entirely
  would break prompts for daemonless setups.
- **[Machine-global prompts]** Any local user holding the loopback token (or a socket peer) can
  edit prompts that affect everyone's daemon-routed runs. → Same trust model as every existing
  endpoint (knowledge bases are equally shared); noted in `docs/rest-api.md`, no extra gating —
  prompts are not more sensitive than KB deletion, which is already ungated beyond auth.
- **[Last-write-wins on concurrent edits]** Two UI tabs editing the same prompt clobber each
  other silently. → Accepted for a local tool; per-prompt PUT (D2) already confines the blast
  radius to the one prompt being edited. No ETags/If-Match until someone actually hits this.
- **[Store file corruption]** A truncated/hand-edited `prompts.json` must not take down chat.
  → Same forgiving posture as `LoadPrompts`: unparseable store logs a warning and resolves to
  defaults; the API surfaces `customized: false` rather than erroring. Atomic writes (D1) prevent
  the daemon itself from producing a torn file.
- **[Defaults drift across releases]** A customized prompt pins old text while defaults improve.
  → Inherent to customization; the UI's always-visible default view (D5/D6) is the mitigation —
  users can diff by eye and re-base. No auto-merge.
- **[`prompt init` UX regression risk]** Rewiring it to the API changes a working flow. → The
  select/edit interaction (`huh` forms) is preserved verbatim; only the load/save I/O changes, and
  the daemonless path is untouched.

## Migration Plan

1. Land the store + REST endpoints + handler swap (daemon behavior fixed even before any client
   ships), then `prompt init` daemon-first + migration offer, then the UI page + sidebar flip,
   then docs (`docs/rest-api.md`, `rest-api.yaml`, `docs/local-ui.md`, `docs/usage.md`).
2. Validate in-snap: `make all`, `cd ui && npm run build`, `snapcraft -v` (clean the go part —
   see the stale-part gotcha), `sudo snap install --dangerous`, then: customize a prompt in the
   UI → new `rag-cli.rag chat` session reflects it; `prompt init` against the daemon; migration
   offer from a pre-seeded legacy file; reset from both UI and CLI; daemon restart keeps the
   customization.
3. Rollback: `git revert` — no config keys, interfaces, plugs, or hooks changed; an orphaned
   `$SNAP_COMMON/ragd/prompts.json` is inert for reverted code (old daemon never reads it).

No new snap interfaces, plugs, bundled binaries, or hook changes in snapcraft.yaml. No new
secrets; the existing env-var secrets (`OPENSEARCH_*`, `CHAT_API_KEY`) are untouched. snapctl
config schema is untouched (no new `package` or `user` keys).

## Open Questions

- None blocking. (Whether `prompt init` should also offer deleting the legacy local file after a
  successful migration is deferred — leaving it is harmless and keeps daemonless fallback
  working.)
