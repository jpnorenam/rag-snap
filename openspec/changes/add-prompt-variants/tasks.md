# Tasks: add-prompt-variants

## 1. Daemon prompt store rewrite

- [x] 1.1 Define the new store types in `internal/api/prompts.go` (or a split-out file): variant record `{name, slot, created_at, updated_at, versions[]}`, per-slot active pointers, name validation regexp, reserved-name `default` handling
- [x] 1.2 Implement the per-variant file layout under `$SNAP_COMMON/ragd/prompts/` (`active.json`, `<slot>/<name>.json`, `source_rules/override.json`) with atomic temp-file+rename writes and corrupt-record-degrades-alone loads
- [x] 1.3 Implement version semantics: append-on-save, identical-head no-op, empty-value rejection, restore-appends-new-head
- [x] 1.4 Implement resolution: explicit selection → variant head; else active pointer → variant head or built-in default; `source_rules` override or default
- [x] 1.5 Implement the one-way lazy migration of legacy `prompts.json` (non-empty overrides → activated `custom` v1 / `source_rules` override v1; rename legacy file to `.migrated`; failures degrade to defaults with a log line)
- [x] 1.6 Unit-test the store: CRUD, versioning, restore, activation, delete-active conflict, migration, corrupt-record degradation, reserved/invalid names (extend `internal/api/prompts_test.go`)

## 2. Daemon API surface

- [x] 2.1 Reimplement the legacy handlers over the new store: GET list/get views gain `active` + variant-name list; PUT writes through to the active variant (creating/activating `custom` from default; default-equal value clears `custom`); DELETE resets the active pointer preserving variants
- [x] 2.2 Add variant handlers and routes in `handlers_prompts.go` / `server.go`: variants list/create/get/update/delete, versions list, restore, and `PATCH /1.0/prompts/{slot}` activation; extend `respondPromptError` (404 unknown, 400 invalid, 409 delete-active, 404 variants-on-source_rules); add swagger route annotations
- [x] 2.3 Handler tests covering the delta-spec scenarios, including legacy-endpoint back-compat against the shipped UI/CLI request shapes (extend `handlers_prompts_test.go`)

## 3. Selection and provenance in chat and batch

- [x] 3.1 `POST /1.0/chat`: accept optional `prompt` variant name; resolve at session start (404 on unknown, no session started); thread the resolved reference (`name@version` or empty) into the session for the save path
- [x] 3.2 `chatstore.Chat`/`Summary`: add omitempty `Prompt` provenance field, persisted and returned on read; confirm old records load and resume unchanged (store tests)
- [x] 3.3 Batch: add `prompt_ref` to the manifest/request types (`batch.go`, `handlers_answer.go`); reject `prompt` + `prompt_ref` together before answering; resolve at operation start (slot-value path, not the inline+source_rules path); stamp resolved provenance into results metadata and JSON export
- [x] 3.4 Update `internal/apiclient` for the new endpoints, the chat `prompt` field, and provenance fields

## 4. CLI

- [x] 4.1 Add `prompt list/save/use/history/restore/delete` subcommands in `cmd/cli/basic/prompt.go` — daemon-only with a clear suggest-the-daemon error; preserve the fixed command order in `cmd/cli/main.go` and the `prompt` group placement
- [x] 4.2 Extend the daemon path of `prompt init` with the variant level (pick variant / new / default → edit, activate, restore); leave the daemonless fallback untouched
- [x] 4.3 Add `--prompt <name>` to `chat` (remote/daemon mode), failing before session start on unknown variants
- [x] 4.4 Wire `prompt_ref` through the CLI batch path (`answer batch` manifest parsing already lands via 3.3; verify the direct-mode error when `prompt_ref` is used without a daemon)

## 5. Web UI

- [x] 5.1 Extend `ui/lib/api/prompts.ts` with typed calls for variants, versions, restore, and activation
- [x] 5.2 Extend `PromptCard`/`PromptsScreen`: active-variant name on the card, variant selector with activation, save-as-new-variant with client-side name validation, version-history view with restore, delete for non-active variants; `source_rules` card gets history/restore only
- [ ] 5.3 Verify `ui-conventions` compliance: both themes, keyboard-only pass, `--vf-*` tokens only, all four view states (loading skeletons, error, content, edit)

## 6. Docs and validation

- [x] 6.1 Update usage docs to the new surfaces: `docs/usage.md`, `prompt`/`chat`/`answer` Cobra help text, and `apps/completion.bash` for the new `prompt` subcommands and `chat --prompt`
- [x] 6.2 Run `make all` (tidy fmt vet lint test build) clean
- [ ] 6.3 Build the snap, install it (`snapcraft -v && sudo snap install --dangerous ./rag-cli_*.snap`), and validate end-to-end on a real install: legacy-store migration, variant CRUD/activation from CLI and UI, `chat --prompt`, batch `prompt_ref` with provenance in the exported JSON, and legacy-endpoint back-compat
- [ ] 6.4 Verify snap-revert safety manually: old revision degrades to defaults with the `.migrated` file recoverable
