# Design: add-ui-status-settings

## Context

The daemon already resolves the three backend URLs from snapctl config ([internal/api/config.go](../../../internal/api/config.go)), keeps a coarse TCP-dialability readiness map (`backendState.snapshot()` in [backend.go](../../../internal/api/backend.go)), and reports both in `GET /1.0`. The CLI `status` command ([cmd/cli/basic/status.go](../../../cmd/cli/basic/status.go)) reports snapd service states, endpoints, the configured embedding/rerank model IDs, and the LLM name detected via `chat.FindModelName`. Nothing today queries OpenSearch for which ML models are actually deployed, and nothing exposes config read/write over the API. The UI shell has a bottom-pinned "Status" placeholder route (`/status/`) waiting to flip live.

Constraints that shape the design:

- **snapctl-only config**: all reads/writes go through `pkg/storage` → `snapctl`; this works in the daemon (it runs inside the snap, as root), so API config writes are just `Config.Set(key, value, UserConfig)` — same code path and validation as CLI `config set`. Nothing here works under `go run` outside the snap.
- **Two-layer precedence**: `package` then `user`; user `set` rejects unknown keys. `pkg/storage.Config` currently exposes only *merged* reads (`Get`/`GetAll`), so layer provenance (needed for the UI's layer chip and "Revert to package value") requires a new read-only accessor.
- **One config key is a secret**: `gdrive.client.secret` is seeded as a package key by the install hook. The service credentials (`OPENSEARCH_USERNAME`/`PASSWORD`, `CHAT_API_KEY`) are env vars and can never appear in config, but the gdrive secret can — the API must redact it.
- **UI conventions**: the `/status/` page follows `docs/ux/06-status-settings.md`, `docs/ux/00-foundation.md`, and the `ui-conventions` skill (Vanilla classes, envelope-based API client with `getSync`/`putSync`/`deleteSync` already present, four view states, no new dependencies).

## Goals / Non-Goals

**Goals:**

- `GET /1.0/status`: live, per-service health + details, including the deployed OpenSearch ML model list, degrading per-service.
- `GET /1.0/config`, `PUT /1.0/config/{key}`, `DELETE /1.0/config/{key}`: merged config with layer provenance; user-layer writes with CLI-identical validation; revert.
- `/status/` UI page implementing the UX doc, sidebar entry flipped live.
- `api_extensions` entries so clients can feature-detect.

**Non-Goals:**

- No auto-polling/monitoring (UX doc: refresh on mount + on demand only); no events-websocket integration for status.
- No changes to CLI `status`/`config` behavior or output (the CLI does not switch to the daemon endpoints in this change).
- No config-key creation, package-layer writes, or snap service management (start/stop) via the API.
- No remote exposure changes; listeners and auth model are untouched.

## Decisions

### 1. Status is probed live per request, not served from `backendState`

`GET /1.0/status` runs the probes at request time, in parallel, with a short per-probe timeout (2–3 s), and returns whatever it got — each service independently `running` / `unreachable` / `not configured` (URL unresolvable). `backendState`'s polled TCP snapshot stays as-is for `GET /1.0`.

- *Why*: the page has explicit Refresh semantics — the user clicks Refresh and expects a *current* answer, and the detail payloads (deployed models, LLM name, Tika version) require HTTP calls anyway; TCP dialability can't distinguish "port open" from "service answering".
- *Alternative — reuse/extend the poller*: rejected; it would make the poller heavier (HTTP + OpenSearch auth on every tick, forever) to serve a page that is rarely open.

Probes:
- **OpenSearch**: reuse the daemon's cached `knowledge.OpenSearchClient` (env-var auth, TLS handling already there). Health = a cheap authenticated GET (server root/info). Detail = configured embedding/rerank model IDs from config + the deployed-model list (Decision 2). A configured model ID absent from the deployed list is surfaced as a per-model `deployed: false` flag so the UI can warn.
- **Inference**: `chat.FindModelName(url)` — the same best-effort probe CLI `status` uses; success ⇒ running + LLM name.
- **Tika**: `GET <tika-url>/version` (tika-server's plain-text version endpoint); success ⇒ running + version string.
- **ragd**: no probe — the daemon reports itself: API version and the enabled listeners (socket path; loopback enabled/address), sourced from the running `Server` (same data `configSummary()` already assembles, minus the token — the status payload never includes the localhost token).

### 2. Deployed models come from a new `OpenSearchClient` method

Add an exported method on `knowledge.OpenSearchClient` (e.g. `ListDeployedModels(ctx)`), using the existing `newAuthenticatedRequest` machinery in [models.go](../../../cmd/cli/basic/knowledge/models.go) to `POST /_plugins/_ml/models/_search` with:

```json
{
  "size": 1000,
  "_source": ["name", "model_state", "algorithm", "model_version", "model_group_id"],
  "query": { "term": { "model_state": "DEPLOYED" } }
}
```

returning `[]DeployedModel{ID, Name, Algorithm, ModelVersion, ModelGroupID}` (`_id` + `_source` fields). The status handler embeds this list verbatim in the OpenSearch service entry.

- *Why in `knowledge` and not raw HTTP in the daemon*: auth (env creds), TLS-insecure handling, and the `_ml` request plumbing already live there; `findModelInGroup` uses the same search API. One client, one place.
- *Why filter to `DEPLOYED`*: parity with the requesting use case (what can I actually use *now*); registered-but-undeployed models are noise for a status page. `size: 1000` is effectively "all" for a local cluster.

### 3. Config API shape: list with provenance; writes are user-layer `Set`, revert is user-layer `Unset`

- `GET /1.0/config` → `{ "writable": bool, "keys": [ { "key", "value", "layer" } ] }`, sorted by key. Built from two per-layer reads (Decision 4): a key present in the user layer reports `layer: "user"` (its user value, which is the effective one), else `layer: "package"`. Deprecated keys (list exported from `cmd/cli/config`, today unexported `deprecatedConfig` in [get.go](../../../cmd/cli/config/get.go)) are dropped, exactly as CLI `config get` drops them.
- `PUT /1.0/config/{key}` with `{ "value": "..." }` → `storage.Config.Set(key, value, UserConfig)`. Unknown keys are rejected by the existing `Set` guard (the key must exist in the merged view) → 400 with the validation message, which the UI renders as a field-level error. Deprecated keys are rejected the same way `config set` rejects them. Values are strings, like the CLI.
- `DELETE /1.0/config/{key}` → `Unset(key, UserConfig)`; 404-equivalent error if there is no user-layer value to revert.
- **Secret redaction**: a small server-side denylist — any key whose final segment is `secret`, `password`, or `token` (today that matches exactly `gdrive.client.secret`) has its value replaced by `"<redacted>"` in `GET`; the key still appears (so the user layer chip/revert works) and stays writable (write-only semantics). Redaction is by key shape, not value shape — deterministic and testable.
- *Alternative — expose config inside `GET /1.0` like LXD*: rejected; the existing `config` field of `GET /1.0` is a deliberately tiny diagnostics summary, and a full read/write resource deserves its own path + extension string.

### 4. `pkg/storage` gains a read-only per-layer accessor

Add one method to the `Config` interface, e.g. `GetAllFromLayer(confType) (map[string]any, error)`, implemented by reading `config.<layer>` from snapctl storage and flattening — the same code `loadConfigs` already runs per layer, minus the merge. No changes to `Set`/`Get`/`Unset` semantics.

- *Why not diff merged-vs-package to infer provenance*: a user override set to the same value as the package value would be invisible, and "revert" would wrongly hide; explicit per-layer reads are exact.

### 5. Authorization: all authenticated callers may write; the API says so

Config writes are gated by `requireAuth` like every other mutating endpoint (prompts `PUT`/`DELETE` precedent). The loopback bearer token is minted for exactly the peercred-trusted grantees, so a token-authenticated browser is the same principal as a socket caller — there is no basis for a stricter write gate today. The `writable` field in `GET /1.0/config` (true for any caller that can read it, at present) is the *seam*: if a future change introduces read-only principals (e.g. a viewer token), the UI already renders the whole Configuration zone read-only with the `sudo rag-cli.rag set …` guidance when `writable: false`, per the UX doc — no dead edit buttons.

### 6. UI structure

- **Page**: `ui/app/status/page.tsx` → `<StatusScreen>` in `ui/components/`. Stacked zones (Status, then Configuration) — no tabs at this size, per the UX doc.
- **Status zone**: a `<ul>` services grid of `.status-card`s in fixed order (OpenSearch · Inference · Tika · ragd): `.app-status-dot` + state word (word always present; color never alone), copyable endpoint (`p-text--small u-text--muted`), per-card detail line. The OpenSearch card lists the configured model IDs as copyable `p-code-snippet` blocks and the deployed-models list (name, algorithm, version) — with a caution note when a configured ID is not in the deployed list. Unreachable cards grow the one-line CLI hint (`snap services rag-cli` / relevant config key), mirroring `cmd/cli/common` suggestions. Refresh `p-button--base` + relative last-checked timestamp; fetch on mount + on demand; a polite live region announces "Status updated".
- **Configuration zone**: semantic `<table>` (Key monospace · Value · Layer chip: `package` plain `p-chip`, `user` `p-chip--positive`), `p-search-box` client-side filter, inline edit per row (pencil `p-button--base` with `aria-label="Edit <key>"` → input + Save/Cancel, focus managed per UX doc), row menu "Revert to package value" for user-layer rows behind a confirm modal showing both values, field-level `p-form-validation__message` on daemon 400s. Redacted values render as `••••` and are still editable (write-only). After a save that touches a `*.http.*` key, show the caution notification pointing back at the Status zone.
- **API client**: `ui/lib/api/status.ts` and `ui/lib/api/config.ts`, typed views mirroring the daemon payloads, via the existing `getSync`/`putSync`/`deleteSync` envelope verbs. Both zones degrade independently (foundation §7): status-card fetch failure ≠ config table failure ≠ page error.
- Styles in `globals.scss` under `// --- status ---`; icons via the existing `NavIcon` pattern; both themes verified.

### 7. Feature detection

Append `status` and `config` to `apiExtensions` (one entry each — they ship together but are independent surfaces). Convention per `rest-api-server`: additive, no version bump.

## Risks / Trade-offs

- **[Live probes add latency]** A fully-down stack means the endpoint waits out per-probe timeouts. → Probes run concurrently with a 2–3 s cap, so worst case ≈ one timeout, not the sum; the UI shows its loading state meanwhile and never auto-polls.
- **[`GET /1.0/status` triggers outbound calls on every request]** A misbehaving client could hammer backends. → Probes are cheap reads, loopback/socket callers are already trusted; acceptable for a local daemon. No caching layer until proven necessary.
- **[Key-shape redaction could over- or under-match]** A future key ending in `token` that isn't secret would be redacted; a secret under an unanticipated name would leak. → The denylist is centralized and unit-tested against the full seeded key set; the UX doc's client-side `••••` fallback remains as a second net, and new package keys are added by maintainers who can extend the list in the same commit.
- **[`size: 1000` model search is unpaginated]** A cluster with >1000 deployed models would truncate. → Not a realistic local-RAG scenario; the payload notes no pagination contract, so it can be added compatibly later.
- **[Daemon imports more of `cmd/cli`]** Exporting `deprecatedConfig` deepens the `internal/api` → `cmd/cli/config` dependency. → That dependency already exists ([internal/api/config.go](../../../internal/api/config.go) imports it); exporting a slice adds no new edge.
- **[Snapd service states differ from probe states]** CLI `status` reports snapd's view (`snapctl services`); the API reports reachability. These can disagree (service up, port firewalled — or Tika down but external OpenSearch up). → Intentional: the page's question is "can *this daemon* use the service", which reachability answers better; the degraded-state hint still points at `snap services rag-cli` for the snapd view.

## Migration Plan

Additive only: new endpoints, new extension strings, one new storage read method, one new UI route. No data migration, no config schema change, no snapcraft.yaml / interface / hook changes, no new secrets (existing env-var secrets untouched). Rollback = revert the commit; nothing persists.

## Open Questions

- None blocking. (Naming nit for implementation: `DELETE /1.0/config/{key}` uses dots in the path segment — fine for `net/http` routing since dots aren't separators; confirm the apiclient/openapi doc renders it cleanly.)
