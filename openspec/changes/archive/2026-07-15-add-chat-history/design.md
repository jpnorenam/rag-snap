# Design — add-chat-history

## Context

Chat state today lives only in memory: the direct REPL keeps
`params.Messages` in `cmd/cli/basic/chat/client.go`, and daemon-backed sessions
(UI, CLI remote mode) keep it in `chat.LiveSession` (`cmd/cli/basic/chat/turn.go`),
created per websocket connection in `internal/api/handlers_chat.go`. Nothing is
persisted; closing the surface loses the conversation.

The daemon already has the persistence pattern we need: the prompt store
(`internal/api/prompts.go`) persists JSON under `$SNAP_COMMON/ragd/`, with a
client-local fallback (`os.UserConfigDir()`-based, `cmd/cli/basic/chat/prompts.go`)
for daemonless runs — the two homes are distinct under strict confinement, and the
daemon cannot read the user's `$HOME`. Claude Code's session history (per-project
transcript files, listed and resumed by a picker) is the UX model.

Constraints that shape the design:

- Strict snap confinement: the daemon writes `$SNAP_COMMON`; the CLI writes its own
  user config dir. There is no path both can share directly — sharing happens through
  the REST API.
- Config is snapctl-only with package/user precedence; adding config keys means
  touching the install hook. This feature needs none — store paths are fixed.
- No new secrets; the existing loopback token/peercred auth already gates `/1.0/chats`.
- UI changes must follow `.claude/skills/ui-conventions/SKILL.md` (Vanilla classes,
  shared primitives in `ui/components/common/`, api modules via `envelope.ts`).

## Goals / Non-Goals

**Goals:**

- `/save [title]` and `/history` (search → pick → resume) in all three chat surfaces:
  direct REPL, remote REPL, UI chat screen.
- One shared history for the daemon-backed surfaces (UI + remote CLI), via the API.
- Durable storage that survives snap refresh; graceful degradation when unavailable.
- Resume restores transcript, knowledge-base set, and save-in-place identity.

**Non-Goals:**

- Autosave of every session (Claude Code does this; we start with explicit `/save` —
  the store schema doesn't preclude adding autosave later).
- Merging the daemon store with the CLI-local store, or migrating between them.
- A dedicated UI route/sidebar entry for history (it's a panel on the chat screen).
- Indexing chat content into OpenSearch for retrieval or semantic search.
- Multi-user separation: the store is per-machine, matching the daemon's trust model.

## Decisions

### 1. One store implementation, two mount points

A new package `internal/chatstore` implements the store against a directory path.
The daemon mounts it at `$SNAP_COMMON/ragd/chats/` (sibling of `prompts.json`, same
refresh-survival rationale); the direct REPL mounts it at
`<UserConfigDir>/rag-cli/chats/` (sibling of the CLI `prompts.json` fallback).

*Alternative considered:* daemon-only store, with the direct REPL calling the API.
Rejected: the direct REPL exists precisely for when ragd is not running; it must not
grow a daemon dependency. The prompt store set this precedent and it works.

### 2. One JSON file per chat, snapshot semantics

Each saved chat is `chats/<id>.json`: `{id, title, created_at, updated_at, model,
bases[], turns[{role, content}]}`. `/save` rewrites the whole file (write-temp +
rename). Listing reads summaries from all files; a file that fails to parse is
logged and skipped so one corrupt record never breaks listing.

*Alternative considered:* append-only JSONL per session (Claude Code's format).
Rejected: JSONL fits streaming autosave; our unit of work is an explicit whole-
conversation snapshot with update-in-place and rename, where rewrite-one-document is
simpler and atomic. Per-chat files (vs one big `chats.json`) keep corruption blast
radius small and make delete trivial.

IDs are opaque random hex (crypto/rand); ordering uses `updated_at`, so IDs carry no
meaning. Files and the directory are created 0600/0700 — transcripts are user data.

### 3. Transcript is stored without system prompt or reasoning

Persisted `turns` are the user/assistant exchange only. The system prompt is NOT
stored: on resume it is freshly resolved (daemon prompt store / CLI fallback), so
prompt customizations apply to resumed sessions the same way they apply to new ones.
Assistant `content` is the final answer with `<think>` spans stripped — reasoning
adds bulk and is not needed to continue a conversation.

Resume rebuilds the session message list as `[fresh system prompt] + saved turns`,
then the normal turn loop takes over.

### 4. Sharing goes through the API; the daemon owns the shared store

New REST resource (spec `rest-api-chats`):

- `GET /1.0/chats[?search=]` — summaries, newest-first by `updated_at`.
- `GET /1.0/chats/{id}` — full record.
- `DELETE /1.0/chats/{id}` — remove.

Search is a case-insensitive substring scan over title + turn content, done
server-side in the store (it must read the files anyway; local scale is tens to
hundreds of chats, so O(n) scanning is fine and avoids any index to keep consistent).

Session integration (spec delta `rest-api-chat`):

- `POST /1.0/chat` gains optional `"resume": "<id>"`. The daemon seeds
  `LiveSession` history and active bases from the record, validates bases against
  existing indexes (dropping missing ones, reported in metadata), returns the
  restored transcript in the operation/session metadata so the client can render it,
  and pins the session's `chatID` for save-in-place.
- New websocket control `{"type":"save","title":"..."}` → server frame
  `{"type":"saved","id":"...","title":"..."}`. `LiveSession` grows an accessor for
  its user/assistant turns so the handler can snapshot it.

*Alternative considered:* client-side save (UI/remote CLI POST the transcript to a
plain CRUD endpoint). Rejected: the daemon already owns session history
(`rest-api-chat`: "Daemon owns chat session state"); saving server-side keeps the
saved record canonical and identical for every client, and the control-message
pattern matches `set-active-kbs`.

### 5. CLI surfaces

- Registry: add `{name: "/save", syntax: "[title]"}` and `{name: "/history"}` to
  `slashCommands` in `commands.go` — autocomplete, ghost text, and hints come free.
- Direct REPL: `/save` snapshots `params.Messages` to the local store; `/history`
  lists the local store in a `huh` select with its built-in filtering, then swaps
  `params.Messages`/active bases and prints the restored transcript.
- Remote REPL: `/save` sends the `save` control message and prints the `saved`
  ack; `/history` calls `GET /1.0/chats` via `internal/apiclient`, picks with the
  same `huh` UI, closes the current websocket session, and starts a new one with
  `resume`, printing the restored transcript from the session metadata.
- Restored-transcript rendering is shared: dim role labels + content, then the
  normal prompt.

### 6. UI surfaces (per ui-conventions)

- `ui/lib/api/chats.ts`: typed `ChatSummary`/`SavedChat` mirroring daemon views,
  `listChats(search?)`, `getChat(id)`, `deleteChat(id)` through `envelope.ts`;
  `ui/lib/api/chat.ts` gains the `resume` field on start and the `save` control /
  `saved` frame types.
- Composer: input beginning with `/` is intercepted (never sent as a prompt). A
  small hint list above the composer mirrors the REPL (`chat-hints` feature-prefixed
  classes in `globals.scss`), keyboard-navigable (`aria-activedescendant` pattern).
- History panel: an anchored panel on the chat screen (same interaction contract as
  the operations panel: Escape/outside-click close, `aria-expanded` toggle), with a
  `p-search-box` filter debounced into `GET /1.0/chats?search=`. Delete goes through
  the shared confirm modal from `ui/components/common/`. Empty state uses the shared
  empty-state component with the `/save` hint.
- Resume: close the current socket, `POST /1.0/chat` with `resume`, hydrate
  `messages` from the returned transcript, set the KB chips from the restored set.

### 7. Packaging

No snapcraft changes: no new interfaces/plugs, no bundled binaries, no hook edits,
no new config keys, no new environment variables. `$SNAP_COMMON/ragd/` and the CLI
config dir are already writable by their respective processes.

## Risks / Trade-offs

- [Two disjoint histories (daemon store vs direct-REPL local store) may confuse a
  user who switches modes] → The `/history` header states which store is shown
  ("saved on this machine via ragd" vs "local, daemonless"); docs explain the split.
  The stores use the same schema, leaving room for a later import/merge.
- [Last-write-wins if two live sessions were resumed from the same chat and both
  save] → Acceptable for a single-user local tool; store writes are atomic
  (temp+rename) so the record is never torn. Documented in the spec via
  save-in-place semantics.
- [O(n) content search degrades with very large histories] → Bounded in practice
  (local, explicit saves); summaries avoid re-reading transcripts for plain listing
  only when no search term is given. Revisit only if real usage shows pain.
- [Restored transcript in `POST /1.0/chat` metadata grows response size for long
  chats] → Transcripts are text-only and local (unix socket/loopback); acceptable.
  If it ever isn't, the client can fall back to `GET /1.0/chats/{id}`.
- [Stripping `<think>` loses information some users might want to review] →
  Deliberate: history is for continuing conversations, not auditing reasoning; the
  live surface still shows think content during the session.

## Migration Plan

Purely additive: new endpoints, new control message, new files under existing
writable directories. Older clients ignore the new frames; older daemons reject the
new endpoints with 404, which the CLI surfaces as "not supported by this ragd".
Rollback = downgrade the snap; stored chat files are inert to old code and survive
under `$SNAP_COMMON` for a later upgrade.

## Open Questions

- None blocking. Autosave (Claude Code parity) and a chat-history import/export are
  deliberate follow-ups, both compatible with the store schema chosen here.
