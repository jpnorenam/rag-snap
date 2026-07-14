# Tasks: add-ui-status-settings

## 1. Storage and shared plumbing

- [x] 1.1 Add a read-only per-layer accessor to `pkg/storage.Config` (e.g. `GetAllFromLayer(confType)`) reusing the existing flattening; unit-test it in `pkg/storage` alongside the existing tests (mock/fake storage, both layers, override-equal-to-package case)
- [x] 1.2 Export the deprecated-config key list from `cmd/cli/config` (today unexported `deprecatedConfig` in `get.go`) so the daemon consumes the same list; keep CLI behavior identical
- [x] 1.3 Add `ListDeployedModels(ctx)` to `knowledge.OpenSearchClient` in `cmd/cli/basic/knowledge/models.go` — `POST /_plugins/_ml/models/_search` with `size: 1000`, `_source` limited to name/model_state/algorithm/model_version/model_group_id, `term model_state: DEPLOYED` — returning `[]DeployedModel{ID, Name, Algorithm, ModelVersion, ModelGroupID}`

## 2. REST API — status

- [x] 2.1 Implement `internal/api/handlers_status.go`: `GET /1.0/status` running the four entries with concurrent probes and a 2–3 s per-probe timeout — OpenSearch (client root-info check + configured embedding/rerank IDs with `deployed` flags + deployed-model list), inference (`chat.FindModelName`), Tika (`GET /version`), ragd self-info (API version, socket path, loopback address when enabled; never the token); states `running` / `unreachable` / `not configured`, independent degradation
- [x] 2.2 Register the route with `requireAuth` in `server.go` and append the `status` entry to `apiExtensions`
- [x] 2.3 Add `handlers_status_test.go` covering: all-up shape, one backend down (others unaffected, sync success), unconfigured endpoint (no probe), configured-but-undeployed model flag, token absent from payload, auth rejection

## 3. REST API — config

- [x] 3.1 Implement `internal/api/handlers_config.go`: `GET /1.0/config` (sorted key/value/layer entries from per-layer reads, deprecated keys dropped, `writable` field), `PUT /1.0/config/{key}` (user-layer `Set`, unknown/deprecated keys → client error with field-suitable message), `DELETE /1.0/config/{key}` (user-layer `Unset`, client error when no override)
- [x] 3.2 Implement key-shape redaction (final segment `secret`/`password`/`token` → fixed marker, key stays listed and writable) and unit-test it against the full install-hook-seeded key set, including `gdrive.client.secret`
- [x] 3.3 Register the three routes with `requireAuth` in `server.go` and append the `config` entry to `apiExtensions`
- [x] 3.4 Add `handlers_config_test.go` covering: layer provenance (package-only, user override, override equal to package value), deprecated hidden on GET and rejected on PUT, unknown key rejected, write-then-read shows `user` layer, revert restores package / errors without override, redacted read + writable redacted key, unauthenticated write rejected

## 4. UI — status page

- [x] 4.1 Add `ui/lib/api/status.ts` and `ui/lib/api/config.ts`: typed views mirroring the daemon payloads, thin verbs over the existing `getSync`/`putSync`/`deleteSync` envelope helpers
- [x] 4.2 Create `ui/app/status/page.tsx` + `StatusScreen` with the Status zone: `<ul>` of `.status-card`s in fixed order (OpenSearch · Inference · Tika · ragd), `.app-status-dot` + state word, copyable endpoint, per-card details (copyable `p-code-snippet` model IDs, deployed-model list with not-deployed caution, LLM name, Tika version, ragd listeners), unreachable-card CLI hints, Refresh button + relative last-checked timestamp, fetch on mount + on demand only, polite live region announcing updates
- [x] 4.3 Build the Configuration zone: semantic table (monospace key, value, layer chip `p-chip`/`p-chip--positive`, `<th scope="col">`), `p-search-box` client-side filter, masked rendering of redacted values, loading/error states independent of the Status zone with the CLI fallback in the error state
- [x] 4.4 Implement inline editing: pencil `p-button--base` (`aria-label="Edit <key>"`) swapping to input + Save/Cancel with focus management, PUT on save, field-level `p-form-validation__message` preserving input on daemon errors, no add-key affordance, "Revert to package value" confirm modal showing both values → DELETE, caution notification after saving a connection-affecting key, full read-only mode with information notification when `writable` is false
- [x] 4.5 Flip the sidebar `STATUS_ITEM` to a live route in `ui/components/Sidebar.tsx`; add status-page styles under `// --- status ---` in `globals.scss`
- [x] 4.6 Verify compliance with the `ui-conventions` skill: both themes (`is-dark`), keyboard-only pass (edit flow focus in/out, modal trap/escape, copy buttons), `--vf-*` tokens only, all four view states for both zones, no horizontal page scroll

## 5. Docs and validation

- [x] 5.1 Document the new endpoints in `docs/rest-api.md` and `rest-api.yaml` (`GET /1.0/status`, `GET/PUT/DELETE /1.0/config…`, the two new `api_extensions`); document the Status page in `docs/local-ui.md` (no CLI command/flag changes, so `docs/usage.md` and `apps/completion.bash` are untouched — confirm)
- [x] 5.5 Fix the SPA deep-link bug found in validation: `internal/webui` served the root (chat) page for every exported route, so `/ui/status/` and `/ui/prompts/` rendered Chat on reload/deep-link; serve a route's own `index.html` and cover it with a regression test
- [x] 5.2 Run `make all` (tidy fmt vet lint test build) and the UI build/export
- [x] 5.3 Build the snap (clean the go-cli part first — snapcraft can ship a stale cached binary; grep the packed .snap to confirm the new routes shipped), install with `--dangerous`, and validate end-to-end on a real install: status endpoint against live OpenSearch/inference/Tika (deployed-model list matches `_plugins/_ml/models/_search`), config edit/revert via the UI landing in `snapctl` user layer (`rag-cli.rag get <key>` agrees), secret key masked, one-service-down degradation
- [x] 5.4 Check the `/status/` page against the Definition of done in `docs/ux/06-status-settings.md` (parity with `rag-cli.rag status`, copyable IDs/endpoints, user-layer-only edits, read-only mode, no secrets rendered)
