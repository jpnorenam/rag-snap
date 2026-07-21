# Tasks — add-engine-model-pruning

## 1. Model inventory in the knowledge package

- [x] 1.1 Add `ModelInfo` (id, name, version, state, size, worker nodes, role), `ModelInfo.Deployed`, and `ModelRole(id, embeddingID, rerankID)` in `models_inventory.go`, with unit tests for the role mapping and the deployed-state classification
- [x] 1.2 Add `ListModels` — searches the model group, excludes model chunk documents (`must_not exists chunk_number`) and the payload fields from `_source`, and returns an empty list when the group does not exist
- [x] 1.3 Add `UndeployModel` and `DeleteModel` (undeploy first, tolerating a not-deployed model, then delete)

## 2. Daemon endpoints

- [x] 2.1 `GET /1.0/knowledge-engine/models` returning the inventory with roles resolved from config
- [x] 2.2 `DELETE /1.0/knowledge-engine/models/{id}` with the in-use guard (`?force=true` to override), logging each removal
- [x] 2.3 `apiclient.EngineModel`, `ListEngineModels`, `DeleteEngineModel`; regenerate `rest-api.yaml`

## 3. CLI

- [x] 3.1 `k models` listing (daemon-first, direct fallback), sorted in-use first, with the unused-memory summary
- [x] 3.2 `k models prune [--yes]` — confirm, then remove every model with no role
- [x] 3.3 `k models remove <id> [--force]` — in-use guard on both transports
- [x] 3.4 Remove the dead `--sentence-transformer` / `--cross-encoder` flags from `k init` and explain in its help that re-running is safe
- [x] 3.5 Update `docs/usage.md` (command table, new section, init flags removed) and the endpoint table in `docs/rest-api.md`

## 4. Verification

- [x] 4.1 `go test ./...`, `go vet`, `make spec-check`
- [ ] 4.2 In an installed snap with OpenSearch running: `k models` lists both models with `IN USE` set; `k models prune` reports nothing to prune on a clean install
- [ ] 4.3 Strand a model deliberately (register a second copy, or clear `knowledge.model.rerank`), confirm it shows as unused with its memory called out, prune it, and confirm `k models` and OpenSearch's `_plugins/_ml/models/_search` both show it gone
- [ ] 4.4 Confirm `k models remove <in-use-id>` is refused without `--force`, both with ragd running and stopped
- [ ] 4.5 Confirm search and ingest still work after a prune (the in-use models were untouched)
