## 0. Prerequisite

- [x] 0.1 Confirm `add-kapa-retrieval-wiring` is implemented and merged: `knowledge.ResolveKapaClient`, the daemon's startup-resolved kapa client, `GET /1.0/kapa/source-groups`, `ui/lib/api/kapa.ts`, the session source-group control message, and the `chat.Session` warning sink all exist
- [x] 0.2 If the prerequisite chose different wire names than `set-active-kapa-groups` / `active-kapa-groups` / `kapa_groups`, update design.md Decision 2 and use its names below

## 1. LiveSession

- [x] 1.1 Add `kapaClient *knowledge.KapaClient` and `activeKapaGroups []string` parameters to `NewLiveSession` (`cmd/cli/basic/chat/turn.go`), wiring them into `Session{KapaClient, ActiveKapaGroups}`
- [x] 1.2 Add `SetActiveKapaGroups([]string)` and `ActiveKapaGroups() []string` to `LiveSession`, mirroring `SetActiveBases`/`ActiveBases`
- [x] 1.3 Test that a `LiveSession` prompt with groups selected and a client present retrieves from kapa.ai through `retrieveContext`, local hits first

## 2. Daemon chat session

- [x] 2.1 Add `KapaSourceGroups []string` (`kapa_source_groups`) to `chatStartRequest` (`internal/api/handlers_chat.go`)
- [x] 2.2 In `handleChatStart`, pass the prerequisite's kapa client and the initial groups (request value, or the resumed chat's `KapaGroups`) to `NewLiveSession`; default to no groups
- [x] 2.3 Add `KapaGroups []string` (`kapa_groups`) to `chatControlMessage` and `chatServerMessage`; handle `set-active-kapa-groups` in `runChatSession` and reply with an `active-kapa-groups` acknowledgement carrying the effective selection (design Decision 2)
- [x] 2.4 When no kapa client exists, acknowledge with an empty effective selection and an `error` explaining kapa.ai is not configured; keep the session running (design Decision 3)
- [x] 2.5 Report the effective selection and a `kapa_unavailable` flag in `meta["chat"]` at start and resume, next to `dropped_bases`
- [x] 2.6 Tests: selection changes retrieval mid-session; independent of `set-active-kbs`; start-time selection; unconfigured selection is reported, not accepted silently

## 3. Saved chats

- [x] 3.1 Add `KapaGroups []string` with `json:"kapa_groups,omitempty"` to `chatstore.Chat` (`internal/chatstore/store.go`)
- [x] 3.2 Save `live.ActiveKapaGroups()` alongside `Bases` in `runChatSession`'s save branch
- [x] 3.3 Restore `resumed.KapaGroups` as-is on resume (no `filterExistingBases` equivalent)
- [x] 3.4 Tests: save/resume round-trips `KapaGroups`; a record without the field resumes with no selection; resume while unconfigured reports the selection as not applied

## 4. Daemon and CLI chat clients

- [x] 4.1 `internal/apiclient/chat.go`: accept initial kapa groups on chat start, send `set-active-kapa-groups`, and surface the acknowledgement's effective selection and error
- [x] 4.2 `cmd/cli/basic/chat/remote.go`: show the resume notice when `kapa_unavailable` is set

## 5. Search: `POST /1.0/search` and `k search`

- [x] 5.1 Add an optional `Warnings []string` (`warnings,omitempty`) to `syncResponse` and a `respondSyncWarnings` helper in `internal/api/response.go` (design Decision 7)
- [x] 5.2 Add `KapaGroups []string` (`kapa_groups`) to `searchRequest` (`internal/api/handlers_search.go`); query kapa.ai only when it is non-empty, concurrently with the local search
- [x] 5.3 Only require the embedding model when bases are requested, so a kapa.ai-only search runs without it
- [x] 5.4 Return local hits followed by kapa.ai hits; set `Base: "kapa.ai"` for `knowledge.KapaIndexName` hits instead of calling `KnowledgeBaseNameFromIndex` (design Decision 6)
- [x] 5.5 When groups are requested but no client exists, or the kapa.ai call fails, return the local hits with a warning
- [x] 5.6 Update the `swagger:route` comment and run `make spec`
- [x] 5.7 `internal/apiclient/resources.go`: add kapa groups to `Client.Search` and return the response's warnings
- [x] 5.8 `k search` (`cmd/cli/basic/knowledge.go`): add `--kapa-groups` (comma-separated ids). Direct mode: build the client with `buildKapaClient`, search concurrently, and print local then kapa.ai hits. Daemon mode: pass through `apiclient`. In both modes, print warnings to stderr
- [x] 5.9 Tests: local + kapa.ai ordering; kapa.ai-only search without an embedding model; empty and absent `kapa_groups` both skip kapa.ai; unconfigured and failing kapa.ai return local hits plus a warning; responses without warnings are unchanged

## 6. REPL `/search`

- [x] 6.1 In `handleSearch` (`cmd/cli/basic/chat/search.go`), reject only when both `ActiveIndexes` and `ActiveKapaGroups` are empty, with guidance naming `/use-knowledge` and `/use-kapa`
- [x] 6.2 Apply the knowledge-client/embedding check only when local bases are active
- [x] 6.3 Run local and kapa.ai retrieval concurrently and print local hits followed by kapa.ai hits; on a kapa.ai failure, print a warning and keep the local results
- [x] 6.4 Pass the session's selected groups through `remoteSearch` (`chat/remote.go`) and print any warnings
- [x] 6.5 Update `searchUsage` if it implies local-only scope
- [x] 6.6 Tests: kapa.ai only; both sources (ordering); neither (guidance); kapa.ai failure keeps local results

## 7. Browser UI

- [x] 7.1 `ui/lib/api/chat.ts`: add `kapa_source_groups` to `ChatStartOptions`; add `kapa_groups` and `kapa_unavailable` to `RestoredChat`; add `"active-kapa-groups"` to `ChatFrameType` and `kapa_groups` to `ChatFrame`; add `setActiveKapaGroups(ids)` to `ChatConnection`
- [x] 7.2 `ui/lib/api/envelope.ts` and `ui/lib/api/search.ts`: expose response warnings; add an optional `kapaGroups` argument to `search()` that is sent only when non-empty
- [x] 7.3 `ChatScreen.tsx`: add a `.kapa-selector` row below `.kb-selector`, loaded through the prerequisite's `listKapaSourceGroups()`, using `p-chip`/`p-chip--positive` toggle chips keyed by id and showing names
- [x] 7.4 `ChatScreen.tsx`: implement loading, empty, loaded, error (with retry), and unconfigured (credentials hint) states, and render `ApiError.code === 0` as the standard daemon-unreachable message
- [x] 7.5 `ChatScreen.tsx`: wire chip toggles to `setActiveKapaGroups`; handle `active-kapa-groups` and the non-fatal `warning` frame (a failed kapa.ai request, from `add-kapa-retrieval-wiring` D12) in `handleFrame`; show the acknowledgement's error as a notice, not a failed turn; restore groups and show the `kapa_unavailable` notice on resume
- [x] 7.6 `SearchScreen.tsx`: add the same selector row and states; round-trip the selection through a `g` URL parameter next to `q`/`b`/`k`; send group ids only when non-empty
- [x] 7.7 `SearchScreen.tsx`: show a fixed "kapa.ai" chip for hits whose `base` is `kapa.ai`; never link their source id; show response warnings as a caution notification above the results
- [x] 7.8 Add styles under the `// --- kapa ---` group in `ui/app/globals.scss`, using `--vf-*` tokens only
- [x] 7.9 Add UI tests for the URL round-trip and for warning handling in `search()`

## 8. Documentation

- [x] 8.1 `docs/usage.md`: `k search --kapa-groups`, and `/search` including selected kapa.ai groups
- [x] 8.2 `k search --help` and `searchUsage` text
- [x] 8.3 `apps/completion.bash`: add `--kapa-groups` (no new Cobra command, so the fixed command order in `cmd/cli/main.go` is unchanged)
- [x] 8.4 `docs/rest-api.md` and `rest-api.yaml`: `kapa_source_groups` on `POST /1.0/chat`, the `set-active-kapa-groups` / `active-kapa-groups` frames, `kapa_groups` on `POST /1.0/search`, and the `warnings` field
- [x] 8.5 `docs/local-ui.md`: the chat and Search page kapa.ai selectors

## 9. Verification

- [ ] 9.1 Run `make all` (tidy, fmt, vet, lint, spec-check, test, build) and `npm test` in `ui/`
- [ ] 9.2 Build and install the snap; set `kapa.project.id` and supply `KAPA_API_KEY` via the `ragd` drop-in. Config paths need snapctl, so this cannot be verified with `go run`
- [ ] 9.3 Configured: list groups on the chat and Search pages; toggle mid-session; confirm retrieval uses only the selected groups; save and resume preserves the selection
- [ ] 9.4 Unconfigured (no `KAPA_API_KEY`): the credentials hint shows on both pages; selecting groups via the API, or resuming a chat with groups, reports them as not applied; search returns local hits with a warning
- [ ] 9.5 Verify `k search --kapa-groups <id>` in direct mode and daemon mode, and that `--top` applies per local base and to kapa.ai
- [ ] 9.6 Verify REPL `/search` with kapa.ai alone and combined with local bases, in direct mode and remote mode
- [ ] 9.7 Verify `ui-conventions` compliance on `ChatScreen.tsx` and `SearchScreen.tsx`: both themes, keyboard-only pass over the chips, `--vf-*` tokens only, all view states including unconfigured, no horizontal scroll at 620px
