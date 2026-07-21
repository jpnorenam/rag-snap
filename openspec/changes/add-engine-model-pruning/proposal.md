# Add engine model inventory and pruning

## Why

Nothing in the snap can see or reclaim a deployed OpenSearch ML model. Every deployed model is
resident in memory on each ML worker node (~250 MB for the embedding model, ~130 MB for the
re-ranker), and there is no `_undeploy` or model delete call anywhere in the codebase — once a
model is deployed it stays deployed forever.

`knowledge init` deduplicates by an exact model group + name + `model_version` match, which holds
for a normal sequential re-run. It does not hold when:

- two inits run at once (nothing serializes `POST /1.0/knowledge-engine`, so the browser UI and the
  CLI can both miss the model in the search and both register one);
- an init is interrupted and re-run inside the `.plugins-ml-model` refresh window, so the search
  misses a model that OpenSearch is still registering server-side;
- the model name or version the engine targets changes, leaving the previous model deployed with
  nothing pointing at it.

Each of those strands a model that costs memory indefinitely, and today the only way to find or
remove one is to curl OpenSearch's ML API by hand.

## What Changes

- **Model inventory.** New `k models` lists the models in the engine's model group with their
  deployment state, size, worker-node count, and the engine role each serves (embedding, rerank, or
  none), and calls out how much memory unused deployed models are holding.
- **Reclaim.** New `k models prune` undeploys and deletes every model no configuration key points
  at, after a confirmation; `k models remove <id>` does one model and refuses an in-use model
  without `--force`.
- **Daemon endpoints.** `GET /1.0/knowledge-engine/models` and
  `DELETE /1.0/knowledge-engine/models/{id}` (with `?force=true`), so the daemon-routed CLI and any
  future UI use the same inventory and the same in-use guard.
- **Remove the dead `k init` flags.** `--sentence-transformer` / `--cross-encoder` were printed and
  then ignored — both call sites passed empty strings. They are removed rather than wired up:
  selecting a model is only safe once switching one prunes the previous deployment.

Out of scope: serializing concurrent inits, redeploying models stuck in `DEPLOY_FAILED` /
`PARTIALLY_DEPLOYED` (init still returns those IDs as-is), automatic pruning during init, and any
browser-UI surface for the inventory.

## Capabilities

### New Capabilities

- `knowledge-engine-models`: the model lifecycle the engine does not own — what is registered and
  resident, which models the engine actually uses, and how an operator reclaims the rest.

### Modified Capabilities

- `rest-api-knowledge`: adds the engine model listing and deletion endpoints.

## Impact

- **Services touched:** OpenSearch only — the ML plugin's `_search`, `_undeploy`, and model delete
  APIs. The inference server and Tika are untouched.
- **Config:** no new keys. The existing `knowledge.model.embedding` / `knowledge.model.rerank`
  become the definition of "in use", which is what protects a model from a prune. No new secrets.
- **Code:** new `cmd/cli/basic/knowledge/models_inventory.go` (list/undeploy/delete + `ModelRole`),
  new `cmd/cli/basic/knowledge_models.go` (the `k models` tree), `internal/api/handlers_engine.go`
  and `server.go` (two routes), `internal/apiclient/resources.go` (`EngineModel`).
- **User-facing surfaces:** new `knowledge models` command tree; `knowledge init` loses two flags.
  `docs/usage.md` and `docs/rest-api.md` are updated; `apps/completion.bash` sources Cobra's
  generated completion, so it needs no change.
- **Compatibility:** `k init -s <model>` / `-c <model>` now error instead of being silently ignored.
  Since the flags never had an effect, no working invocation changes behavior.
