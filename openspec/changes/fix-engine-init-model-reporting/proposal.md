# Fix knowledge-engine init model reporting

## Why

With ragd running, `rag-cli.rag k init` completes and prints neither the embedding nor the rerank
model identifier, so the operator is left with nothing to configure and no confirmation that the
daemon configured it for them. With ragd stopped the same command prints both.

The cause is structural, not a single bad branch: the daemon resolved both identifiers a third of
the way into an eight-step initialization but published them only once, at the very end, in the
same code path that persisted them to config — and the CLI read that publication only on success.
Anything between the models being deployed and the last index being created (a pipeline error, a
template error, or a `snapctl set` the daemon is not allowed to make) discarded identifiers that
had already been resolved, and a persist failure additionally turned a successful initialization
into a failed operation. The `run sudo rag set --package …` hints the direct-mode path prints were
being written to the daemon's stdout, where they land in the journal instead of the operator's
terminal.

## What Changes

- **Report each identifier when it is resolved, not when the task ends.** `knowledge.Init` gains
  hooks that fire as soon as the embedding and rerank models are deployed. The daemon publishes to
  the operation metadata and persists to `package` config from those hooks, so a later failure can
  no longer hide an identifier that already exists.
- **Persistence becomes best-effort and self-reporting.** A failed config write no longer fails the
  initialization; the operation reports the identifier with a companion
  `<key>_persisted: false` flag so clients know the operator still has to set it.
- **Clients report what they have, on both outcomes.** `k init` prints the identifiers on success
  *and* on failure, falling back to the configured value when the daemon reported none, and states
  either that the package configuration was updated or the exact command to update it. The UI's
  engine gate does the same, and only shows a copyable snippet for identifiers the daemon could not
  store.
- **Progress spinners are terminal-only.** `common.StartProgressSpinner` /
  `StartUpdatableSpinner` no-op when stdout is not a TTY, so the daemon's journal stops collecting
  animation frames, and the daemon logs one structured line per resolved model instead.

Out of scope: direct-mode `k init` writing config itself (it has no root), and any change to which
models are registered.

## Capabilities

### Modified Capabilities

- `rest-api-knowledge`: engine-init reports identifiers as they resolve, including on a failed
  operation, and reports whether each was persisted rather than failing when it cannot be.
- `local-ui-app`: the engine gate surfaces resolved identifiers on failure too, and distinguishes
  "already configured" from "you must set this".

## Impact

- **Services touched:** OpenSearch only (unchanged calls; only when their results are reported).
  The inference server and Tika are untouched.
- **Config:** no new keys. The existing `package`-scoped `knowledge.model.embedding` and
  `knowledge.model.rerank` are still what init writes; the change is that a failed write is
  survivable and visible. No new secrets.
- **Code:** `cmd/cli/basic/knowledge/client.go` (`InitHooks`, `Init`/`InitPipelines` signatures),
  `cmd/cli/basic/knowledge.go` (init output), `cmd/cli/common/{spinner,suggestions}.go`,
  `internal/api/handlers_engine.go` (`recordModelID`), `internal/api/operations.go`,
  `internal/apiclient/operations.go`, `ui/components/knowledge/EngineGate.tsx`,
  `ui/lib/api/knowledge.ts`.
- **User-facing surfaces:** `k init` output changes (it now names the configuration key and states
  whether it was written) and the UI engine-gate notification wording changes. No flags added or
  removed, so `apps/completion.bash` is unaffected; `docs/usage.md` init output examples need a
  refresh.
- **Compatibility:** the operation metadata keys `embedding_model_id` / `rerank_model_id` keep
  their meaning; the `_persisted` flags are additive, and a client that ignores them behaves as
  before.
