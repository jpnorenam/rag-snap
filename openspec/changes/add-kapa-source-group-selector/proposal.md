## Why

`add-kapa-retrieval-wiring` makes kapa.ai reachable through the daemon: it resolves kapa
credentials in `ragd`, adds `GET /1.0/kapa/source-groups`, lets a daemon chat session select
source groups, carries `kapa_source_groups` through daemon-routed batch answering, and adds a
picker to the answer-batch screen. Several surfaces that use knowledge sources are still
local-only after that change:

- **The browser chat screen** has no way to pick kapa.ai source groups, even though the daemon
  session will accept a selection.
- **Saved chats** record the active knowledge bases but not the kapa.ai selection, so resuming a
  chat silently loses its kapa.ai scope.
- **One-shot search** — `k search`, `POST /1.0/search`, and the Search page — only queries local
  knowledge bases, so a user cannot preview what kapa.ai would contribute to an answer.
- **The REPL's `/search`** ignores groups selected with `/use-kapa`, and refuses to run unless a
  local base is active, even though the chat loop in the same session retrieves from kapa.ai.

This change brings those surfaces to parity, on top of the contract `add-kapa-retrieval-wiring`
defines.

## Dependency

This change **depends on `add-kapa-retrieval-wiring`** and must be implemented and archived
after it. It reuses, and does not redefine:

- the credential rule and the daemon's startup-resolved kapa client (`kapa-retrieval`,
  `knowledge.ResolveKapaClient`);
- `GET /1.0/kapa/source-groups` and its unconfigured / empty / failure conditions
  (`rest-api-kapa`);
- the daemon chat session's source-group control message (`rest-api-chat`), which this change
  extends with a start-time selection;
- batch answering's handling of `kapa_source_groups` (`rest-api-answer`);
- the rules that selection has no default, that local hits precede kapa.ai hits, and that an
  unusable selection is reported rather than ignored (`kapa-retrieval`).

Where an earlier draft of this change disagreed with `add-kapa-retrieval-wiring`, the newer change
wins. In particular: the API key is environment-only (`KAPA_API_KEY`); an empty selection means no
kapa.ai retrieval, never "all groups"; an unconfigured integration is reported rather than hidden;
and merged results put local hits first rather than interleaving by score.

## What Changes

- **Chat screen selector.** `ui/components/ChatScreen.tsx` gains a kapa.ai source-group row of
  toggle chips, below and visually distinct from the knowledge-base row. It uses the source-group
  listing endpoint, drives the session's source-group control message, and renders the same
  view states as the answer-batch picker, including a credentials hint when unconfigured.
- **Start-time selection.** `POST /1.0/chat` accepts an initial `kapa_source_groups` list, the way
  it accepts `bases`. Sessions still start with nothing selected unless the client names groups.
- **Saved chats keep their kapa.ai scope.** `chatstore.Chat` gains the selected group ids, saved
  and restored like `bases`. Resuming while kapa.ai is unconfigured reports that the saved
  selection could not be applied.
- **One-shot search includes kapa.ai on request.** `k search` gains `--kapa-groups <ids>`;
  `POST /1.0/search` gains `kapa_groups`; the Search page gains the same selector row as chat.
  Kapa.ai is queried only for the named groups. Results list local hits first, then kapa.ai hits
  tagged with the `kapa-canonical` label and a fixed `kapa.ai` base. If kapa.ai cannot be applied,
  the local results are still returned with a warning.
- **REPL `/search` parity.** `/search` includes the groups selected with `/use-kapa`, runs when
  either kind of source is active, and orders results the way the chat loop merges them.

### Out of scope

Everything `add-kapa-retrieval-wiring` owns (see Dependency); a shared `KapaSelector` component;
reconciling saved group ids against the live listing at resume; caching the source-group listing.

## Capabilities

### New Capabilities

None. `rest-api-kapa` and `kapa-retrieval` are introduced by `add-kapa-retrieval-wiring`.

### Modified Capabilities

- `rest-api-chat`: extends the session source-group requirement from `add-kapa-retrieval-wiring`
  with a start-time selection in `POST /1.0/chat`; RAG grounding covers kapa.ai context; resume
  restores the kapa.ai selection.
- `chat-history`: saved chats persist and restore the kapa.ai source-group selection.
- `chat-search`: `/search` includes selected kapa.ai groups and accepts either kind of source.
- `rest-api-knowledge`: `POST /1.0/search` accepts kapa.ai source-group ids.
- `local-ui-app`: kapa.ai selector rows on the chat screen and the Search page; kapa.ai result
  cards on the Search page.

## Impact

- **External services**: OpenSearch, the inference server, and Tika are used exactly as today.
  Kapa.ai (`api.kapa.ai`), added as a retrieval source by `add-kapa-retrieval-wiring`, is now also
  queried by chat sessions started from the browser and by one-shot search.
- **Config keys**: none new. Uses `kapa.enabled` and `kapa.project.id` (`package`-scoped,
  user-overridable) and the `KAPA_API_KEY` / `KAPA_PROJECT_ID` environment variables, all as
  defined by `add-kapa-retrieval-wiring`.
- **User-facing surfaces**:
  - New CLI flag `k search --kapa-groups`.
  - Changed REPL behavior: `/search` includes kapa.ai.
  - New chat websocket frame field and `POST /1.0/chat` / `POST /1.0/search` request fields.
  - New UI affordances: the selector rows on the chat screen and Search page.
- **Documentation to update**:
  - `docs/usage.md`: `k search --kapa-groups` and `/search` with kapa.ai.
  - `k search --help` and the `/search` usage text.
  - `apps/completion.bash`: the new flag.
  - `docs/rest-api.md` and `rest-api.yaml`: the new request fields and the search warning.
  - `docs/local-ui.md`: the chat and Search page selectors.
- **Code**:
  - `cmd/cli/basic/chat/turn.go`, `cmd/cli/basic/chat/search.go`
  - `cmd/cli/basic/knowledge.go`
  - `internal/api/handlers_chat.go`, `internal/api/handlers_search.go`, `internal/api/response.go`
  - `internal/chatstore/store.go`
  - `internal/apiclient/resources.go`, `internal/apiclient/chat.go`
  - `ui/lib/api/chat.ts`, `ui/lib/api/search.ts`, `ui/lib/api/envelope.ts`
  - `ui/components/ChatScreen.tsx`, `ui/components/SearchScreen.tsx`, `ui/app/globals.scss`
