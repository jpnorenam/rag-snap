## 1. Chat store package

- [x] 1.1 Create `internal/chatstore`: `Chat` record type (`id`, `title`, `created_at`, `updated_at`, `model`, `bases`, `turns[{role,content}]`), store constructed from a directory path; per-chat `<id>.json` files written 0600 via temp+rename, directory created 0700
- [x] 1.2 Implement `Save` (new id + created_at on first save; update-in-place preserving id/created_at thereafter; optional title override; derive default title from first user turn, whitespace-collapsed and truncated), `List(search string)` (summaries newest-first by `updated_at`, case-insensitive substring match over title+turn content, skip-and-log corrupt files), `Get(id)`, `Delete(id)`
- [x] 1.3 Add a helper to strip `<think>…</think>` spans from assistant content before persisting, and a converter between `openai.ChatCompletionMessageParamUnion` history (minus system prompt) and store turns
- [x] 1.4 Unit-test the store with `t.TempDir()`: round-trip, title derivation, update-in-place, search filter, corrupt-file skip, delete, empty-transcript rejection

## 2. Daemon: REST resource and session integration

- [x] 2.1 Mount the store at `$SNAP_COMMON/ragd/chats/` (resolve path like `promptsPath`; degrade to a clear save error when unresolvable) and wire it into `api.Server`
- [x] 2.2 Add handlers + routes: `GET /1.0/chats` (with `search` query param), `GET /1.0/chats/{id}` (404 unknown), `DELETE /1.0/chats/{id}` (404 unknown), using the standard response envelope and existing auth
- [x] 2.3 Extend `chat.LiveSession` with an accessor exposing its user/assistant turns and current bases for snapshotting, and a way to seed history + a pinned chat id at construction
- [x] 2.4 Extend `POST /1.0/chat` with optional `resume` id: load the record, drop bases whose index no longer exists (report dropped ones), seed the LiveSession, pin the chat id, and include the restored transcript + effective bases in the session metadata; 404 on unknown id
- [x] 2.5 Add the `save` websocket control message (optional `title`): snapshot via the LiveSession accessor, save through the store (create or update-in-place on the pinned/first-saved id), reply with a `saved` frame carrying id + title; reject empty sessions and report store failures as `error` frames without closing the session
- [x] 2.6 Extend handler tests: chats CRUD + search, resume seeding (including dropped-base reporting and unknown-id 404), save control message happy path, empty-session rejection, and save-after-resume updating the original record
- [x] 2.7 Document the new endpoints, the `resume` field, and the `save`/`saved` frames in `rest-api.yaml`

## 3. CLI: slash commands in both REPL modes

- [x] 3.1 Register `/save` (syntax `[title]`) and `/history` in `slashCommands` in `cmd/cli/basic/chat/commands.go` so autocomplete, ghost text, hints, and the unknown-command listing pick them up in both modes
- [x] 3.2 Add a shared restored-transcript renderer (dim role labels + content) and a shared `huh`-based history picker (newest-first, built-in filtering, title + relative updated time; cancel = no-op; friendly message when the store is empty)
- [x] 3.3 Direct REPL (`client.go`): `/save` snapshots `params.Messages` (minus system prompt, think stripped) into the client-local store at `<UserConfigDir>/rag-cli/chats/`; `/history` picks from the local store, swaps in `[fresh system prompt] + saved turns`, restores active bases (dropping missing ones with a notice), prints the transcript, and pins the id for update-in-place saves
- [x] 3.4 Remote REPL (`remote.go`): `/save` sends the `save` control message and prints the `saved` ack; `/history` lists via `GET /1.0/chats` (new `internal/apiclient` chats methods), then closes the current session and starts a new one with `resume`, rendering the restored transcript and updated active bases from the session metadata; surface a clear "not supported by this ragd" on 404
- [x] 3.5 Extend `commands_test.go` for the new registry entries and any pure helpers (title/transcript rendering, think stripping already covered in 1.4)

## 4. UI: composer slash commands and history panel

- [x] 4.1 Add `ui/lib/api/chats.ts` (typed `ChatSummary`/`SavedChat`, `listChats(search?)`, `getChat`, `deleteChat` via `envelope.ts`) and extend `ui/lib/api/chat.ts` with the `resume` start field, the `save` control, and the `saved`/restored-transcript frame types
- [x] 4.2 Composer slash handling in `ChatScreen.tsx`: intercept `/`-prefixed input (never sent as a prompt), keyboard-navigable filtering hint list above the composer, inline available-commands message for unknown commands
- [x] 4.3 `/save [title]`: send the `save` control over the open websocket, show a positive notification with the returned title; surface the empty-session rejection and store-failure errors
- [x] 4.4 History panel: opened by `/history` and a History control; lists `GET /1.0/chats` newest-first (title, relative time, model, turn count), `p-search-box` filter debounced into the `search` param, Escape/outside-click close with focus return, shared empty-state with `/save` guidance
- [x] 4.5 Resume from the panel: close the current socket, `POST /1.0/chat` with `resume`, hydrate the transcript view and KB chips from the response, continue the conversation on the new socket
- [x] 4.6 Delete from the panel via the shared confirm modal naming the chat title; `DELETE /1.0/chats/{id}` on confirm, row kept with the API error on failure
- [x] 4.7 Verify `ui-conventions` compliance: both themes, keyboard-only pass over composer hints + panel, `--vf-*` tokens only, and the panel's loading/empty/error/populated states

## 5. Docs and validation

- [x] 5.1 Update usage docs for the new surface: `docs/usage.md` chat section (slash commands, store locations, daemon/daemonless split), chat command `--help` text where it lists in-chat commands, and `apps/completion.bash` if it enumerates slash commands
- [x] 5.2 Run `make all` (tidy fmt vet lint test build) and the UI build/lint — tidy/fmt/vet/test/build all pass and the UI static export builds; new Go code is golangci-lint-clean (remaining lint hits are pre-existing baseline in untouched files)
- [ ] 5.3 Build the snap, install with `--dangerous`, and validate end-to-end: save/history/resume in the remote REPL and UI sharing one store, daemonless direct REPL using the local store, chats surviving a snap refresh, and delete from the UI panel — **manual QA step (requires snapcraft build + `sudo snap install`); not run autonomously**
