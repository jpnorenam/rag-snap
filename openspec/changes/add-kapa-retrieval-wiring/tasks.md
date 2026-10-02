## 1. Shared credential resolution

- [ ] 1.1 Add `ResolveKapaClient(enabled bool, projectID, apiKey string) *KapaClient` to `cmd/cli/basic/knowledge/kapa.go` — pure, no config or environment access (design D1)
- [ ] 1.2 Add `cmd/cli/basic/knowledge/kapa_test.go` covering the resolution table: enabled with both values, missing key, missing project, explicitly disabled, unset-enabled-defaults-to-true
- [ ] 1.3 Rewrite `buildKapaClient` in `cmd/cli/basic/common.go` to read `kapa.enabled` and `kapa.project.id` plus `KAPA_API_KEY`/`KAPA_PROJECT_ID` and delegate to `ResolveKapaClient`
- [ ] 1.4 Remove the `kapa.api.key` config read from `buildKapaClient` (**BREAKING** — see task 5.3 for the hook side)

## 2. Carry `kapa_source_groups` through the daemon

- [ ] 2.1 Add `kapa_source_groups` to `batchManifestJSON` and to `batchManifestBody` in `cmd/cli/basic/answer_api.go`
- [ ] 2.2 Add `kapa_source_groups` to `batchManifestRequest` and to `toManifest` in `internal/api/handlers_answer.go`
- [ ] 2.3 Resolve kapa config in `internal/api/config.go` at server construction — `kapa.enabled`, `kapa.project.id`, `KAPA_API_KEY`, `KAPA_PROJECT_ID` — and build the client via `ResolveKapaClient` (design D2)
- [ ] 2.4 Replace the hardcoded `nil` kapa client at `internal/api/handlers_answer.go` with the resolved client and delete the "not yet wired" comment
- [ ] 2.5 Extend the round-trip test to enumerate every optional `chat.BatchManifest` field so a newly added field that is not threaded through fails the test (design D4)
- [ ] 2.6 Add a handler test asserting a posted `kapa_source_groups` reaches the run manifest, following the pattern in `internal/api/handlers_answer_test.go`

## 3. Report an unusable or failing integration

- [ ] 3.1 Add an optional retrieval-warning sink to `chat.Session`; nil preserves today's verbose-only printing in `retrieveContext` (design D5)
- [ ] 3.2 Wire the sink to stderr on the direct CLI path and to operation metadata in the daemon
- [ ] 3.3 Add the pre-flight check: source groups selected but no client available reports it and continues against local knowledge bases — in both `handleAnswerBatch` and the direct `ProcessBatchChat` path
- [ ] 3.4 Test that a kapa retrieval failure for one question is reported and does not abort the remaining questions

## 4. Source-group listing endpoint

- [ ] 4.1 Add `GET /1.0/kapa/source-groups` in `internal/api/handlers_kapa.go` returning each group's id and display name, de-duplicated, following upstream pagination to completion (design D3)
- [ ] 4.2 Register the route in `internal/api/server.go` behind `requireAuth`, in the existing grouping style
- [ ] 4.3 Distinguish unconfigured, configured-but-empty, and upstream-failure in the response so a client can tell them apart (design D9)
- [ ] 4.4 Add `internal/api/handlers_kapa_test.go` covering the four listing scenarios plus the unauthenticated rejection
- [ ] 4.5 Document the endpoint in `rest-api.yaml` and `docs/rest-api.md`, and add `kapa_source_groups` to the documented batch manifest body

## 5. Snap hooks and config keys

- [ ] 5.1 Factor the `package`-key registration shared by `install` and `post-refresh` into a sourced shell fragment staged by `snapcraft.yaml`, registering idempotently and never overwriting an operator's value (design D7)
- [ ] 5.2 Seed `kapa.enabled` and `kapa.project.id` from `post-refresh` as well as `install`
- [ ] 5.3 Stop registering `kapa.api.key` in `install`, and `snapctl unset` it at both layers from `post-refresh` (design D6)
- [ ] 5.4 Bump the snap version in `snap/snapcraft.yaml`
- [ ] 5.5 Confirm no new plug, interface, or `environment:` stanza is added — `network` already covers `api.kapa.ai` on both apps, and a hardcoded `KAPA_API_KEY` would override the operator's systemd drop-in

## 6. Daemon chat session selection

- [ ] 6.1 Accept a source-group selection control message on the daemon chat connection, independent of the active-knowledge-bases message, defaulting to nothing selected (design D10)
- [ ] 6.2 Apply the session's selection to retrieval for subsequent prompts, and report a selection that cannot be applied when the integration is unconfigured
- [ ] 6.3 Test selecting, changing mid-session, independence from the knowledge-base selection, and the unconfigured case

## 7. Browser UI

- [ ] 7.1 Add `ui/lib/api/kapa.ts` using `getSync` from `envelope.ts`, with a typed interface mirroring the daemon view and null arrays normalised to `[]` (design D8)
- [ ] 7.2 Add a source-group picker to the answer-batch screen using toggle chips (`p-chip` / `p-chip--positive` + `p-chip__value`), matching the KB selector in `ChatScreen.tsx`
- [ ] 7.3 Show the selection in the pre-run preview alongside the run's other scope
- [ ] 7.4 Implement all four view states plus the unconfigured case as guidance (credentials required, naming the `set --package` and drop-in hints) rather than an error or an empty picker; render `ApiError.code === 0` as the standard daemon-unreachable message
- [ ] 7.5 Read and write `kapa_source_groups` in `ui/lib/manifest.ts`, preserving an existing selection when the listing cannot be retrieved
- [ ] 7.6 Add styles to `ui/app/globals.scss` under a `// --- kapa ---` group, colours from `--vf-*` tokens only
- [ ] 7.7 Extend `ui/lib/manifest.test.ts` (or `batchManifest.test.ts`) with a `kapa_source_groups` round-trip case
- [ ] 7.8 Verify compliance with the `ui-conventions` skill: both themes via the `is-dark` toggle, keyboard-only pass over the chips, `--vf-*` tokens throughout, all four view states

## 8. Documentation

- [ ] 8.1 Document the `kapa_source_groups` manifest field and `/use-kapa` in `docs/usage.md`
- [ ] 8.2 Update the secrets recipe in `INSTALL.md` and `docs/local-ui.md`: `KAPA_API_KEY` in the root-only systemd drop-in, the removed `kapa.api.key` key and its migration, and the `snap restart rag-cli.ragd` needed after a config or drop-in change
- [ ] 8.3 Check whether `apps/completion.bash` needs updating for the changed `k`/chat surface; no new Cobra command is added, so the fixed root command order in `cmd/cli/main.go` is unchanged

## 9. Verification

- [ ] 9.1 Run `make all` (tidy, fmt, vet, lint, test, build) — CI has no test or lint gate
- [ ] 9.2 Run `npm test` in `ui/`
- [ ] 9.3 Build and install the snap (`snapcraft -v`, `sudo snap install --dangerous ./rag-cli_*.snap`) — every config path needs snapctl and cannot be exercised by `go run`
- [ ] 9.4 Verify on an installation predating the keys that a refresh makes `kapa.enabled` and `kapa.project.id` appear, that an operator-set value survives a refresh, and that `kapa.api.key` is gone
- [ ] 9.5 End-to-end: set `kapa.project.id`, supply `KAPA_API_KEY` via the drop-in, run the same manifest in direct mode and through the daemon, and confirm `--verbose` reports non-zero kapa hits on both paths
- [ ] 9.6 Verify the unconfigured path: run a manifest selecting source groups with no key present and confirm the run reports it and still answers from local knowledge bases
