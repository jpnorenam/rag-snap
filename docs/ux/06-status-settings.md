# 06 — UX guidelines: `add-ui-status-settings`

`/status/` — service health, models, endpoints, and configuration. Parity with `status --format` and `config get/set`. Read `00-foundation.md` first.

## Page structure

One page, two zones: **Status** on top (glanceable), **Configuration** below. If both grow, split with `p-tabs` (Status | Configuration) — but start stacked; tabs only if the page exceeds ~2 screens.

## Status zone

### Services grid
One compact card per service (`.status-card`, standard tokens), in fixed order: **OpenSearch** (knowledge store) · **Inference server** (chat backend) · **Tika** (text extraction) · **ragd** (daemon):

- `.app-status-dot` + state word: **Running** (positive) / **Unreachable** (negative) / **Not configured** (muted). Color never carries meaning alone — the word is always present.
- The resolved endpoint URL in `p-text--small u-text--muted`, copyable.
- Per-card detail line: OpenSearch → embedding + rerank **model IDs** (copyable `p-code-snippet`, parity with `status` output and `k init`); Inference → detected **LLM model name**; Tika → version if reported; ragd → API version + enabled listeners (socket / loopback).

### Refresh
A **Refresh** `p-button--base` with last-checked relative timestamp. No auto-polling by default (this is a diagnostics page, not a monitor); refresh on mount + on demand.

### Degraded-state guidance
When a service is unreachable, the card grows a one-line hint with the CLI diagnostic: e.g. "Check the service: `snap services rag-cli`" or the relevant config key (`knowledge.http.host`). This mirrors the CLI's suggestion helpers in `cmd/cli/common/`.

## Configuration zone

### Browser (parity: `config get`)
- A filterable table: **Key** (dot-namespaced, monospace) · **Value** · **Layer** (chip: `package` plain / `user` `p-chip--positive`).
- A `p-search-box` filters keys client-side.
- Deprecated keys are hidden (same list the CLI hides).
- Secrets are never shown: the env-var-backed credentials (`OPENSEARCH_USERNAME`/`PASSWORD`, `CHAT_API_KEY`) are not config keys and must not appear; if the API ever returns something secret-shaped, render `••••` — but the correct fix is server-side redaction.

### Editing (parity: `config set`)
- Inline edit per row: pencil `p-button--base` (`aria-label="Edit <key>"`) swaps the value cell to an input + Save/Cancel. Saving writes the **user** layer (the CLI semantic: user overrides package).
- Unknown-key creation is not offered (CLI rejects unknown user keys) — there is no "add key" button.
- A user-layer value shows **Revert to package value** in its row menu (confirm modal; body shows both values).
- Validation errors from the daemon render as field-level `p-form-validation__message` on the row.
- **Authorization**: if the API reports the caller may not write (per this change's authorization design decision), render the whole zone read-only with an information notification explaining how to edit (`sudo rag-cli.rag set <key>=<value>`). Never show edit affordances that will always fail.
- After a successful save, if the changed key affects a service connection, surface a caution notification: "Changes may require the service to reconnect — check Status above."

## States
Foundation §7. Status cards each degrade independently (one unreachable service must not error the page). Config table: loading skeleton rows; error state offers the CLI fallback.

## Accessibility
- Services grid is a `<ul>` of cards; state changes after Refresh announced via a polite live region ("Status updated").
- The config table keeps header cells (`<th scope="col">`); inline editing moves focus into the input and back to the pencil on cancel/save.

## Definition of done (UX)
Foundation checklist, plus:
- [ ] Status output matches `rag-cli.rag status` for the same machine (same services, endpoints, models)
- [ ] Model IDs and endpoints copyable
- [ ] Config edit writes the user layer only; revert-to-package works; unknown keys impossible to create
- [ ] Read-only mode renders when the API denies writes; no dead edit buttons
- [ ] No secret values rendered anywhere on the page
