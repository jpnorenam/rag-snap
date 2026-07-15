## Why

Chat sessions are ephemeral today: closing the REPL or the UI chat screen discards the
conversation, and there is no way to revisit a useful exchange or pick it up later. Users
who build up context in a session (active knowledge bases, a line of questioning) must
start from scratch every time. Persisting chats locally — the way Claude Code keeps
per-project session transcripts that can be listed and resumed — makes the chat surface
usable for real, multi-day work.

## What Changes

- Add a **persistent chat store** owned by the ragd daemon (alongside the existing prompt
  store under `$SNAP_COMMON/ragd/`), holding saved chat transcripts: id, title, timestamps,
  model, active knowledge bases, and the full message list.
- Add two **in-chat slash commands**, available in the CLI REPL (both direct and remote
  modes) and in the UI chat screen:
  - `/save [title]` — save (or update) the current conversation to the chat store; title
    defaults to a summary derived from the first user prompt.
  - `/history` — search saved chats (filter-as-you-type over title and content), pick one,
    and resume it: the transcript and active knowledge-base set are restored into the
    current session and the conversation continues from there.
- Extend the **REST API** with a saved-chats resource (`/1.0/chats`): list, search, get,
  and delete saved chats; extend `POST /1.0/chat` so a session can start from a saved chat
  (resume). The UI and the CLI's remote mode both drive the feature through this API.
- The CLI's **direct (daemonless) REPL** persists to a client-local fallback store,
  mirroring how prompt customizations degrade when ragd is not available.
- New user-facing surfaces: the two slash commands in the REPL slash-command registry
  (autocomplete, hints, `/help` listing) and in the UI composer; a history picker
  (interactive `huh` selector in the CLI, a panel in the UI).

Services touched: **inference server** only indirectly (a resumed transcript is replayed as
conversation context on the next turn); **OpenSearch** and **Tika** are untouched. The
change is confined to the CLI chat package, the ragd daemon, and the UI.

New config keys: **none**. The store lives at a fixed path under `$SNAP_COMMON/ragd/`
(daemon) with a client-local fallback for daemonless runs; no snapctl keys are added.

Documentation to update: `README.md` (chat section — slash commands), `rest-api.yaml`
(new `/1.0/chats` endpoints and the resume parameter on `POST /1.0/chat`), and the UI
follows the patterns in `docs/ux/`.

## Capabilities

### New Capabilities

- `chat-history`: saving, listing/searching, and resuming chat conversations — the store
  semantics (what a saved chat contains, title derivation, update-in-place), the `/save`
  and `/history` slash commands, and the resume behavior across CLI REPL modes.
- `rest-api-chats`: the daemon's saved-chats REST resource — `GET /1.0/chats` (list with
  optional search filter), `GET /1.0/chats/{id}`, `DELETE /1.0/chats/{id}`, and the
  save control message on the chat websocket.

### Modified Capabilities

- `rest-api-chat`: `POST /1.0/chat` gains an optional resume-from-saved-chat parameter;
  the daemon seeds the new session's history and active knowledge bases from the saved
  chat. A new `save` control message persists the running session server-side.
- `local-ui-app`: the chat screen gains slash-command handling in the composer (`/save`,
  `/history` with a filtering hint list, matching the REPL affordance) and a chat-history
  panel to search, resume, and delete saved chats.

## Impact

- **Code**: `cmd/cli/basic/chat/` (slash-command registry, direct-REPL save/resume,
  remote-REPL control messages, history picker), `internal/api/` (chat store, new
  handlers, chat-session seeding), `internal/apiclient/` (chats resource client),
  `ui/components/ChatScreen.tsx` and `ui/lib/api/` (composer slash commands, history
  panel, chats client module).
- **API**: new `/1.0/chats` endpoints; backward-compatible extension of `POST /1.0/chat`
  and the chat websocket control-message set.
- **Storage**: new JSON/JSONL files under `$SNAP_COMMON/ragd/chats/` (daemon) and the
  client-local equivalent for daemonless CLI runs. Saved chats contain user conversation
  content; they stay on the machine and are never sent anywhere except the loopback API.
- **Dependencies**: none new; reuses `huh` (picker), `readline` (slash hints), existing
  websocket plumbing.
