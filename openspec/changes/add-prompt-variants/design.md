# Design: add-prompt-variants

## Context

The daemon owns three prompt slots (`chat_system_prompt`, `answer_system_prompt`, `source_rules`), each holding at most one anonymous override, persisted as a single overrides-only JSON file at `$SNAP_COMMON/ragd/prompts.json` (`internal/api/prompts.go`). The REST API exposes `GET/PUT/DELETE /1.0/prompts/{name}`. Sessions and batches snapshot the resolved prompts once at start (`handlers_chat.go`, `handlers_answer.go`), so an edit never shifts a running conversation. The batch manifest already supports a one-off inline `prompt:` with `source_rules` force-appended (`cmd/cli/basic/chat/batch.go`). The daemonless CLI reads a separate client-local `~/.config/rag-cli/prompts.json` the daemon cannot see.

Users iterate on prompts per task (RFPs, presales calls) and need to name, keep, switch between, and audit them.

## Goals / Non-Goals

**Goals:**

- Named variants per generation slot with append-only version history and a per-slot active pointer.
- Full backward compatibility: existing API clients, the current UI, and existing stored overrides keep working without intervention.
- Provenance: saved chats and exported batch results record which `variant@version` ran.
- Selection at session/batch start where the slot is implied by context (chat → `chat_system_prompt`, batch → `answer_system_prompt`).

**Non-Goals:**

- Variants for `source_rules` (grounding guardrail stays a single global override, though it gains version history for rollback).
- Prompt export/import for team sharing (future; the per-variant file format must not preclude it).
- Mid-session prompt switching (`/use-prompt`) — selection is at start only, preserving snapshot semantics.
- Variant bundles ("profiles") spanning multiple slots.
- Variants in the daemonless CLI path — the legacy client-local file stays frozen as a single-override fallback.

## Decisions

### D1: Slot-scoped named variants, not cross-slot profiles

A variant belongs to exactly one slot. The named use cases (RFP → answer slot, presales chat → chat slot) each touch a single slot, and the slot is inferable from context at selection time, so `rag chat --prompt presales-call` needs no slot argument. Profiles (bundles across slots) were considered and rejected for now: they need partial-bundle fallback semantics and no current use case requires them; they can be layered on later as a named set of variant pointers.

### D2: Linear, immutable version history; restore appends

Every save of a variant appends a version `{version, created_at, value}`. The head is always the effective value. Saving content byte-identical to the head is a no-op (no version spam). "Restore version n" appends a new head with version n's content (git-revert style) rather than moving a pointer — history stays linear and never lies about what was effective when, which is what makes `variant@version` provenance trustworthy. No version cap initially: prompts are small text and identical-save dedupe bounds growth in practice.

### D3: Per-slot active pointer; explicit selection overrides it

`active.json` maps each generation slot to a variant name or `""` (built-in default). Resolution at session/batch start: explicit request selection → that variant's head; otherwise the active pointer → variant head or built-in default. This mirrors how knowledge bases work (a default set, overridable per session) and keeps "switch the machine into presales mode" a one-command operation. Per-session-only selection (no global pointer) was rejected: it would break the current UI/CLI flows that assume a persistent customization.

### D4: Store layout — one JSON file per variant (chatstore pattern)

```
$SNAP_COMMON/ragd/prompts/
├── active.json                      {"chat_system_prompt": "presales-call", "answer_system_prompt": ""}
├── chat_system_prompt/<name>.json   one file per variant: {name, slot, created_at, updated_at, versions[]}
├── answer_system_prompt/<name>.json
└── source_rules/override.json       versioned single override, same file shape
```

Follows `internal/chatstore`: a corrupt file loses one variant, not the store; deletes are a single unlink. Variant names are validated (`^[a-z0-9][a-z0-9-]{0,63}$`) on lookup so a name from a request path can never escape the store directory (chatstore `idPattern` precedent). Writes stay atomic (temp file + rename, as `saveLocked` does today). A single grown `prompts.json` was rejected: version history makes it a growing multi-record file where one torn write loses everything.

`default` is a reserved variant name — always available, never editable or deletable; it denotes the built-in of the running release, so improved defaults still ship to non-customizers (preserving the current overrides-only principle). Named variants are user content and may legitimately duplicate a default; the PUT-equals-default-clears-override rule applies only to the legacy write-through path.

### D5: Legacy endpoints reinterpreted, not versioned or removed

The existing four endpoints keep their observable semantics over the new model:

- `GET /1.0/prompts` and `GET /1.0/prompts/{slot}` — per-slot view, now also carrying the active variant name and the variant name list. `value` remains the effective (resolved) text; `customized` remains "effective ≠ built-in default".
- `PUT /1.0/prompts/{slot}` — writes through to the active variant (new version on its history). When the default is active, it creates and activates a variant named `custom` — byte-for-byte today's behavior from the current UI/CLI's perspective. A value identical to the built-in default while `custom` is active resets to default (legacy clear semantics).
- `DELETE /1.0/prompts/{slot}` — sets the active pointer to default; variants are preserved.

This means the shipped UI and `prompt init` work unmodified on day one of the daemon change. New endpoints:

```
GET    /1.0/prompts/{slot}/variants                   summaries (name, updated_at, version count)
POST   /1.0/prompts/{slot}/variants                   create {name, value}
GET    /1.0/prompts/{slot}/variants/{name}            head value + metadata
PUT    /1.0/prompts/{slot}/variants/{name}            save → new version (identical head = no-op)
DELETE /1.0/prompts/{slot}/variants/{name}            delete; 409 conflict if active
GET    /1.0/prompts/{slot}/variants/{name}/versions   full history
POST   /1.0/prompts/{slot}/variants/{name}/restore    {version} → appends new head
PATCH  /1.0/prompts/{slot}                            {"active": "<name>" | ""}
```

Error mapping extends `respondPromptError`: unknown slot/variant → 404, invalid name or empty value → 400, deleting the active variant → 409 (explicit "activate something else first" beats silent fallback), persistence failure → 500. Variant endpoints on `source_rules` → 404 (no variants for the guardrail slot).

### D6: One-way lazy migration of the legacy store

On first prompt-store access after upgrade, if `$SNAP_COMMON/ragd/prompts.json` exists: each non-empty generation-slot override becomes variant `custom` v1 for that slot and is activated; a non-empty `source_rules` override becomes `source_rules/override.json` v1. The legacy file is then renamed aside (`prompts.json.migrated`) so migration never re-runs and rollback evidence is kept. Migration failures degrade to built-in defaults with a log line, never a daemon crash (matching the current corrupt-store behavior).

### D7: Provenance is a recording, not a pin

The chat session request gains optional `"prompt": "<variant-name>"`; the batch manifest/request gains `prompt_ref: <name>` (mutually exclusive with inline `prompt:` — both set is a 400/validation error). At snapshot time the daemon resolves the selection and stamps the resolved reference (`presales-call@3`, or empty for default) into: the saved chat record (`chatstore.Chat` gains a `Prompt string` field, omitempty so old records stay readable) and the batch results metadata/export. Resume keeps re-resolving fresh — the record says what *did* run; a resumed session picks up the user's latest iteration. Pinning-on-resume was rejected: it would silently run stale prompts and complicate deletion.

Inline manifest `prompt:` keeps its current behavior (custom text + `source_rules` appended); `prompt_ref` resolves the variant head and uses it as the system prompt exactly as the slot value would be used (it replaces `answer_system_prompt`, not the inline-custom path).

### D8: CLI surface

`prompt init` remains the interactive front door, gaining one level: select slot → select variant / create new / default → edit, restore, or activate. Scriptable subcommands added under `prompt` (respecting the fixed Cobra ordering conventions):

```
rag prompt list                                  slots, variants, active markers
rag prompt save <slot> <name> [--file f | -]     new version from file or stdin
rag prompt use <slot> <name> | --default         activate
rag prompt history <slot> <name>                 version list
rag prompt restore <slot> <name> <version>       append old content as new head
rag prompt delete <slot> <name>
rag chat --prompt <name>                         per-session selection (slot implied)
```

Management commands name the slot explicitly; usage-side commands never do. All new subcommands are daemon-only (clear error suggesting the daemon when absent), consistent with D-non-goal on the daemonless path; `prompt init` keeps its existing daemonless fallback for the legacy file.

### D9: UI — extend the Prompts page within existing primitives

Per the `ui-conventions` skill: each `PromptCard` gains a variant selector (active marked), save-as-new-variant, activation, and a version-history view with restore. `ui/lib/api/prompts.ts` grows typed calls for the new endpoints. No new pages, no new navigation entries; Vanilla components and the existing card/drawer patterns only.

## Risks / Trade-offs

- [Legacy `PUT` creating a magic `custom` variant may surprise users who later see it in the list] → It is exactly their existing override with a name; `prompt list` and the UI show it as a normal variant they can rename by re-saving under a new name. Documented in the `prompt` help text.
- [Two writers (legacy PUT and variant PUT) to the same variant file] → All store mutations serialize behind the existing store mutex; per-file atomic writes prevent torn files.
- [Unbounded version history] → Deduped identical saves bound growth in practice; a cap can be added later without format changes (versions array is ordered).
- [409 on deleting the active variant may annoy scripts] → Deliberate: silent fallback to default would change generation behavior as a side effect of a delete. The error names the fix (`prompt use <slot> --default` first).
- [Migration renames the legacy file; a snap revert to the old revision loses customizations made before upgrade] → The `.migrated` file is kept alongside; reverting users can rename it back. Noted in release notes.
- [Provenance field in exported batch results changes the JSON shape] → Additive field only; the `local-ui-app` spec's answer-review data contract is preserved (existing fields untouched).

## Migration Plan

1. Daemon ships store rewrite + migration + new endpoints with legacy reinterpretation (no client changes required; shipped UI and CLI keep working).
2. CLI and UI ship variant surfaces in the same snap revision (single artifact — snap bundles daemon, CLI, and UI together, so no cross-version skew in practice; the legacy reinterpretation still protects any external API clients).
3. Rollback: snap revert restores the old binary; the old daemon ignores the new `prompts/` directory and finds no `prompts.json` (migrated aside), degrading to built-in defaults — safe, and the `.migrated` file allows manual restoration.

No snapcraft.yaml changes: no new interfaces, plugs, bundled binaries, or hooks. No new config keys (store is file-based under `$SNAP_COMMON`, not snapctl). No new secrets.

## Open Questions

- Should `prompt list` (CLI) also show version counts and updated-at, or stay minimal? (Cosmetic; decide during implementation.)
- Whether the UI history view shows full-text diffs between versions or just timestamps + preview. Start with timestamps + preview; diffs are a follow-up.
