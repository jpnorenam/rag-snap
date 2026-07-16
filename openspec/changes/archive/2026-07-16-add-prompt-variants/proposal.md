# Proposal: add-prompt-variants

## Why

Users tailor system prompts to recurring tasks — answering RFPs, assisting on presales calls, support triage — but each prompt slot holds exactly one anonymous override, so switching tasks means pasting prompts back and forth and losing the previous text. Batch users already work around this with inline `prompt:` fields in YAML manifests scattered on disk: unnamed, unversioned, and with no record of which prompt produced the answers that were sent to a customer.

## What Changes

- Each of the two generation prompt slots (`chat_system_prompt`, `answer_system_prompt`) gains **named variants** (e.g. `presales-call`, `rfp-govco-2026`) with an **append-only version history**: every save appends a version, identical saves are no-ops, and restoring an old version appends a new head with its content.
- Each slot keeps an **active pointer**: the built-in default or one named variant. Sessions and batches resolve prompts through it, still snapshotting at start.
- `source_rules` stays a single global override (no variants — it is the grounding guardrail) but gains the same version history for rollback.
- The REST API adds variant CRUD, version history, restore, and activation endpoints under `/1.0/prompts/{slot}`. The existing four prompt endpoints keep their exact current semantics, reinterpreted over the new model (a legacy `PUT` writes through to the active variant, creating and activating a variant named `custom` when the default is active).
- The daemon prompt store moves from a single `prompts.json` to a per-variant file layout under `$SNAP_COMMON/ragd/prompts/`, with a one-way lazy migration of the legacy override file (each non-empty override becomes variant `custom` v1, activated).
- Chat sessions accept an optional variant name at start (`rag chat --prompt <name>`, a UI selector, an API request field); the slot is implied by the context. Saved chats record the resolved `name@version` as provenance (informational — resume keeps re-resolving fresh).
- Batch manifests gain `prompt_ref: <name>` alongside the existing inline `prompt:` (mutually exclusive); exported results record the resolved `name@version`.
- The CLI `prompt` command grows scriptable subcommands (`list`, `save`, `use`, `history`, `restore`, `delete`) and `prompt init` gains a variant-selection level. **New user-facing surfaces**: the new `prompt` subcommands, the `--prompt` flag on `chat`, the `prompt_ref` manifest key, and the extended Prompts page in the web UI. Documentation to update: the `prompt`/`chat`/`answer` Cobra help text, README.md usage section, and the swagger route annotations in `internal/api`.
- The daemonless CLI path is intentionally frozen on the legacy `~/.config/rag-cli/prompts.json` single-override file; variants are a ragd feature.

Not in scope (noted as future extensions): prompt export/import for team sharing, mid-session `/use-prompt` switching, variant bundles ("profiles") spanning multiple slots.

## Capabilities

### New Capabilities

- `prompt-cli`: CLI surface for managing and selecting prompt variants — the extended `prompt init` flow, the scriptable `prompt list/save/use/history/restore/delete` subcommands, and variant selection at chat/batch start (`chat --prompt`, manifest `prompt_ref`).

### Modified Capabilities

- `rest-api-prompts`: slots gain named variants with version history and an active pointer; new variant/version/activation endpoints; legacy endpoints reinterpreted (write-through to active variant); store layout changes to per-variant files with migration from the legacy override file.
- `rest-api-chat`: session start accepts an optional prompt variant name; the session records the resolved `name@version`.
- `rest-api-answer`: the batch request/manifest accepts `prompt_ref` (mutually exclusive with inline `prompt`); results carry the resolved prompt provenance.
- `chat-history`: the saved chat record gains a prompt provenance field, persisted and returned on read.
- `local-ui-app`: the Prompts page grows variant management — variant selector per slot, save-as-new-variant, version history with restore, and activation.

## Impact

- **External services**: none. OpenSearch, the inference server, and Tika are untouched; this change is confined to the daemon's file-backed prompt store, the REST API, the CLI, and the web UI.
- **Config keys**: none added. The prompt store is file-based under `$SNAP_COMMON`, not snapctl config; secrets are unaffected.
- **Code**: `internal/api/prompts.go` (store rewrite), `internal/api/handlers_prompts.go` + `server.go` routes, `internal/api/handlers_chat.go`, `internal/api/handlers_answer.go`, `internal/apiclient`, `internal/chatstore` (provenance field), `cmd/cli/basic/prompt.go` (subcommands), `cmd/cli/basic/chat.go` (`--prompt` flag), `cmd/cli/basic/chat/batch.go` (`prompt_ref`), `ui/components/PromptsScreen.tsx` / `PromptCard.tsx`, `ui/lib/api/prompts.ts`.
- **Compatibility**: existing API clients and the current UI keep working unchanged against the reinterpreted legacy endpoints; existing customized prompts are migrated losslessly to a `custom` variant. Old exported batch results and saved chats (without provenance) remain readable.
