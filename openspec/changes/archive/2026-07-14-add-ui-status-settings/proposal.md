# Proposal: add-ui-status-settings

## Why

The CLI already answers "is my stack healthy and how is it configured?" via `status --format` and `config get/set`, but the browser UI has no equivalent — the sidebar's **Status** entry is still a "Soon" placeholder ([Sidebar.tsx:35](../../../ui/components/Sidebar.tsx#L35)), and the daemon exposes no status or config endpoints (only the coarse `backends` readiness booleans in `GET /1.0`). This change ships the `/status/` diagnostics + configuration page (parity plan, per `docs/ux/06-status-settings.md`) and the two REST capabilities it needs. It also closes a diagnostics gap the CLI itself has: `status` prints only the *configured* embedding/rerank model IDs from snapctl config — it never asks OpenSearch which ML models are actually **deployed**, so a stale or missing model ID is invisible until a search fails. The status API reports the live deployed-model list (`POST /_plugins/_ml/models/_search`, `model_state: DEPLOYED`) alongside the configured IDs so users can see at a glance whether the configured models are really available.

## What Changes

- **New REST capability `rest-api-status`**: `GET /1.0/status` returns, per service (**OpenSearch**, **inference server**, **Tika**, plus ragd self-info), a reachability state (`running` / `unreachable` / `not configured`), the resolved endpoint URL, and per-service detail:
  - OpenSearch → configured embedding + rerank model IDs **and**, when reachable, the live list of `DEPLOYED` ML models (id, name, algorithm, model_version, model_group_id) from `_plugins/_ml/models/_search` — so the UI can show what is actually available and flag configured IDs that are not deployed.
  - Inference → detected LLM model name (same probe as CLI `status`).
  - Tika → version if reported.
  - ragd → API version and enabled listeners (socket / loopback).
  Health checks are HTTP-level probes (not just TCP dialability), each degrading independently — one unreachable service must not fail the endpoint.
- **New REST capability `rest-api-config`**: `GET /1.0/config` returns the merged snapctl-backed config as key/value/layer (`package` or `user`) triples, with deprecated keys hidden (same list the CLI hides) and secret values redacted server-side — service credentials live in env vars, never config, but one config key **is** secret-shaped (`gdrive.client.secret`, seeded by the install hook), so the endpoint SHALL redact it (write-only key) rather than assume the store is secret-free. `PUT /1.0/config/{key}` writes the **user** layer with the CLI's semantics (unknown keys rejected — the key must exist as a package key); `DELETE /1.0/config/{key}` reverts a user override to the package value. Whether loopback-token callers may write (vs. socket-peercred callers only) is this change's authorization design decision; the API reports write permission so the UI can render read-only.
- **New `/status/` UI page** (per `docs/ux/06-status-settings.md`): a Status zone of per-service cards (state dot + word, copyable endpoint, per-card detail line including copyable model IDs and the deployed-models list, degraded-state CLI hints, Refresh with last-checked timestamp, no auto-polling) above a Configuration zone (filterable key/value/layer table, inline edit writing the user layer, revert-to-package with confirm modal, read-only mode when the API denies writes). The sidebar's "Status" entry flips from placeholder to live route.
- **Feature detection**: `api_extensions` gains entries for the new status and config surfaces, per the `rest-api-server` convention (additive, no version bump).

No breaking changes: existing endpoints keep their contracts; `GET /1.0`'s `backends`/`config` summary is unchanged; CLI `status` and `config get/set` behavior is untouched.

## Capabilities

### New Capabilities

- `rest-api-status`: the daemon status resource — per-service health probes, resolved endpoints, configured model IDs, detected LLM name, live deployed OpenSearch ML model list, ragd listener/self info; independence of per-service degradation.
- `rest-api-config`: read the merged config with layer provenance (deprecated keys hidden, secrets impossible), write the user layer with package-key validation, revert user overrides; the write-authorization rule and how the API advertises it.

### Modified Capabilities

- `local-ui-app`: adds the Status & Configuration page requirement set (service cards, deployed-models display, refresh semantics, config table with inline user-layer editing, revert, read-only mode, accessibility) and flips the sidebar "Status" entry from placeholder to live route.

## Impact

- **Code**:
  - `internal/api/` — new `handlers_status.go` + `handlers_config.go`; route registration in `server.go`; `api_extensions` additions. Status probes reuse the existing backend URL resolution ([config.go](../../../internal/api/config.go)) and add HTTP-level checks.
  - `cmd/cli/basic/knowledge/` — a new exported `OpenSearchClient` method listing `DEPLOYED` ML models, reusing the existing `newAuthenticatedRequest` + `/_plugins/_ml/models/_search` machinery in [models.go](../../../cmd/cli/basic/knowledge/models.go); the LLM-name probe reuses `chat.FindModelName` as CLI `status` does.
  - `pkg/storage` / `cmd/cli/config` — the deprecated-key list is consumed by the daemon (exported from `cmd/cli/config`); `pkg/storage` gains an additive read-only per-layer accessor so the API can report layer provenance (today only merged reads exist). No existing behavior changes.
  - `ui/` — new `ui/app/status/page.tsx` + screen components, `ui/lib/api/status.ts` / `config.ts`, sidebar flip, styles in `globals.scss`.
- **APIs**: new `GET /1.0/status`, `GET /1.0/config`, `PUT /1.0/config/{key}`, `DELETE /1.0/config/{key}`. No changes to existing endpoint shapes.
- **External services**: touches **OpenSearch** (new read-only touchpoint: ML models search for deployed models; plus an HTTP health probe) and, read-only, the **inference server** (model-name detection, an existing probe) and **Tika** (health/version probe). No writes to any of the three.
- **Config**: **no new config keys**. Config *writes* through the API are new: they call `snapctl set` in the daemon's (root) context, landing in the same user layer the CLI writes — same validation (unknown keys rejected, deprecated keys blocked). Secrets remain env-var-only and are never readable or writable via the API.
- **User-facing surface**: new UI page (browser); no new CLI commands, flags, or slash commands (`apps/completion.bash` unaffected). Documentation to update: `docs/rest-api.md` + `rest-api.yaml` (new endpoints), `docs/local-ui.md` (Status page).
- **UX guidelines**: governed by `docs/ux/00-foundation.md` and `docs/ux/06-status-settings.md`; tasks carry that doc's Definition-of-done checklist (parity with `rag-cli.rag status`, copyable model IDs/endpoints, user-layer-only edits, read-only mode, no secrets rendered).
- **Dependencies**: none added (Go or npm).
