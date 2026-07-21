# Tasks — fix-engine-init-model-reporting

## 1. Report identifiers as they resolve

- [x] 1.1 Add `knowledge.InitHooks` (`OnEmbeddingModel`, `OnRerankModel`, nil-tolerant) and thread it through `Init`/`InitPipelines`; drop the `fmt.Printf` hints from `Init` and fire the hooks after the enclosing `withProgress` call so nothing is written while a spinner repaints
- [x] 1.2 Add `Server.recordModelID` (`internal/api/handlers_engine.go`): publish `<meta_key>` and `<meta_key>_persisted` to the operation, persist to `storage.PackageConfig` best-effort, log the outcome, never return an error
- [x] 1.3 Drive `recordModelID` from the hooks in `handleEngineInit`, keep a post-init safety net for an identifier no hook reported, and remove the persist-failure `return`s that used to fail the operation
- [x] 1.4 Add `Operation.MetadataString` (`internal/api/operations.go`) and `apiclient.Operation.MetadataBool`

## 2. Clients report what they have

- [x] 2.1 Add `common.SuggestSetModelID(key, value)` and use it for the manual-configuration hint in both modes
- [x] 2.2 Add `printEngineInitResult` to the CLI: print on success and on failure, fall back to the configured value when metadata reports none, say so explicitly when it has neither, and state whether the package configuration was updated
- [x] 2.3 Direct-mode `k init` passes hooks that print each identifier plus the set command, preserving today's output
- [x] 2.4 UI engine gate: surface identifiers on failure too, snippet only the unpersisted ones, and confirm "ready and configured" otherwise (`EngineGate.tsx`, `EngineInitResult` type)

## 3. Daemon output hygiene

- [x] 3.1 `common.StartProgressSpinner` / `StartUpdatableSpinner` no-op when stdout is not a TTY, so ragd's journal stops collecting spinner frames

## 4. Verification

- [x] 4.1 Unit tests for `recordModelID`: persisted path, failed-write path (identifier still reported, flagged unpersisted, operation unaffected), empty identifier ignored
- [x] 4.2 `make all` (lint reports only pre-existing issues in untouched code) and `npx tsc --noEmit` for the UI
- [ ] 4.3 Validate in an installed snap: `snapcraft clean go-cli && snapcraft -v`, install, `snap start rag-cli.ragd`, `rag-cli.rag k init` → both identifiers printed and confirmed saved; `rag-cli.rag get knowledge.model` matches; `snap logs rag-cli.ragd` has one line per model and no spinner frames
- [ ] 4.4 Direct-mode parity: `snap stop rag-cli.ragd`, `rag-cli.rag k init` → identifiers plus the set command, printed cleanly after each spinner stops
- [ ] 4.5 UI pass on the engine gate in both themes, keyboard-only, per the `ui-conventions` skill
- [ ] 4.6 Refresh the `k init` output example in `docs/usage.md`
