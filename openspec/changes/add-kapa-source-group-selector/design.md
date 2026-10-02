## Context

`add-kapa-retrieval-wiring` (the prerequisite) gives the daemon a kapa.ai client resolved once at
startup in `internal/api/config.go` through the pure `knowledge.ResolveKapaClient`, exposes
`GET /1.0/kapa/source-groups`, accepts a source-group control message on daemon chat sessions,
threads `kapa_source_groups` through daemon-routed batch answering, and adds a retrieval-warning
sink to `chat.Session`. This change builds the remaining surfaces on that foundation and
introduces no second way to build or configure a kapa client.

State of the code this design is written against (main at `8f5bb24`):

- `chat.Session` (`cmd/cli/basic/chat/commands.go`) already has `KapaClient` and
  `ActiveKapaGroups`. `retrieveContext` (`rag.go`) runs local and kapa.ai retrieval
  concurrently and merges them local-first (`allHits = localHits + kapaHits`), with no
  re-sort.
- `KapaClient.Search` (`cmd/cli/basic/knowledge/kapa.go`) tags every hit
  `Index: KapaIndexName`, `Label: LabelKapa` (`"kapa-canonical"`). It also assigns a
  rank-derived `Score` of `1/(i+1)`, which is not comparable with OpenSearch scores.
- `chat.NewLiveSession` (`turn.go`) takes no kapa parameters. `LiveSession` exposes
  `SetActiveBases`/`ActiveBases`.
- `handlers_chat.go`: `chatStartRequest` has `Bases`; `chatControlMessage` and
  `chatServerMessage` carry `Bases`; the `set-active-kbs`/`active-kbs` pair; resume uses
  `filterExistingBases` and reports via `meta["chat"]`.
- `chatstore.Chat` has `Bases`, `Turns`, and `Prompt` (variant provenance, `omitempty`).
- `handlers_search.go`: `searchRequest{Query, Bases, Count}`. `searchResult` has `Label`, not a
  provenance field, and derives `Base` from `KnowledgeBaseNameFromIndex`, which errors for
  `kapa-canonical` (the error is discarded, leaving `Base` empty). `Count` is applied per base;
  merged hits are not truncated.
- REPL `/search` (`chat/search.go`) rejects the command when `ActiveIndexes` is empty, then
  renders hits through `formatSearchResults`, which tags each hit with
  `knowledge.LabelTag(hit.Label)`.
- `k search` (`cmd/cli/basic/knowledge.go`) has `-k/--top` ("Number of results per index").
- `syncResponse` (`internal/api/response.go`) is `{type, status, status_code, metadata}`;
  `POST /1.0/search` puts the bare hit array in `metadata`.

## Goals / Non-Goals

**Goals:**
- Select kapa.ai source groups from the browser chat screen, mid-session, independently of local
  bases.
- Persist and restore a chat's kapa.ai selection.
- Let one-shot search (`k search`, `POST /1.0/search`, the Search page) and the REPL `/search`
  include kapa.ai, consistently with what the chat loop retrieves.
- Follow every rule `add-kapa-retrieval-wiring` sets for kapa.ai behavior.

**Non-Goals:**
- Anything `add-kapa-retrieval-wiring` owns: credential resolution, the listing endpoint, the
  session control message itself, batch answering, hooks, the answer-batch picker.
- Configuring kapa.ai from the UI.
- Reranking across sources or changing labels (`knowledge-labels` owns `kapa-canonical`).
- Caching the source-group listing (an open question in the prerequisite).

## Decisions

### 1. Reuse the prerequisite's client and endpoint; build nothing parallel

The chat, search, and resume handlers take the daemon's startup-resolved kapa client from the
prerequisite. The browser fetches groups from `GET /1.0/kapa/source-groups` through the
`ui/lib/api/kapa.ts` module that change adds. The CLI's direct paths (`k search`, REPL `/search`)
use `buildKapaClient`, which the prerequisite rewrites to delegate to `ResolveKapaClient`.

*Why:* an earlier draft of this change added a lazily cached client to `clientCache` that read
`kapa.api.key` from config. That conflicts with the newer change on three points: the API key is
environment-only, resolution happens at startup, and the enablement rule lives in one place. The
newer change wins.

### 2. Start-time selection extends the session control message

`chatStartRequest` gains `KapaSourceGroups []string` (`kapa_source_groups`), mirroring `Bases`.
`NewLiveSession` gains `kapaClient` and `activeKapaGroups` parameters; `LiveSession` gains
`SetActiveKapaGroups`/`ActiveKapaGroups`, structured like the base accessors.

On the wire, this change fixes the details the prerequisite leaves open:

- the control message type is `set-active-kapa-groups`;
- the acknowledgement frame is `active-kapa-groups`;
- both carry `kapa_groups` (group **ids**).

Ids rather than names travel because `KapaClient.Search` filters by id and there is no name→id
lookup like `FullIndexName`; the UI holds `{id, name}` pairs and displays names. If the
prerequisite lands first with different names, adopt its names and update this section.

A session with no explicit selection starts with none, as the prerequisite requires.

### 3. Reporting a selection that cannot be applied

Following `kapa-retrieval`, a selection made while no client exists is reported, never accepted
silently:

- **Mid-session:** the `active-kapa-groups` acknowledgement carries the effective selection
  (empty) plus an `error` explaining that kapa.ai is not configured. The session keeps running
  on its local bases.
- **At start or resume:** `meta["chat"]` reports the effective selection and a
  `kapa_unavailable` flag next to `dropped_bases`. The UI and the CLI's remote client show it
  as a notice.

*Why not a separate `error` frame:* the UI already treats `error` frames as failed turns; the
acknowledgement is where the effective selection belongs, mirroring `active-kbs`.

### 4. Saved chats store group ids as-is

`chatstore.Chat` gains `KapaGroups []string` (`json:"kapa_groups,omitempty"`). Saving records
`live.ActiveKapaGroups()`; resume restores it without existence checks. Unlike a deleted local
base, a stale kapa.ai id simply matches nothing, so there is no `filterExistingBases` equivalent.
`omitempty` keeps older records byte-identical and they resume with no selection, the same
compatibility the `Prompt` field relies on.

### 5. Search follows the prerequisite's selection and ordering rules

An earlier draft of this change made `k search --kapa` with no groups, and
`POST /1.0/search` with `kapa_groups: []`, mean "all groups". It also interleaved local and
kapa.ai hits by score. Both conflict with `kapa-retrieval` and are dropped:

- **Explicit groups only.** `k search` gains only `--kapa-groups <id,...>`; there is no bare
  `--kapa`. `POST /1.0/search` queries kapa.ai only when `kapa_groups` is non-empty. Absent and
  empty mean the same thing, so a hand-rolled client sending `[]` cannot widen a search to a
  whole project.
- **Local first.** Results are the local hits, sorted by score as today, followed by kapa.ai
  hits in kapa.ai's order. This matches `retrieveContext`, and kapa.ai's scores are rank-derived
  (`1/(i+1)`), so interleaving by score would be meaningless.
- **Concurrent.** Both retrievals run concurrently, as in `retrieveContext`.
- **Kapa.ai alone.** A search naming groups but no bases runs without an embedding model; the
  "embedding model unavailable" error applies only when bases are requested.
- **Count.** `-k/--top` and `count` already mean "per local base"; the same value is passed as
  kapa.ai's single `limit`. The main spec's "at most that many hits" is corrected to say so.

Group-id discovery for `k search` is the cost of explicit selection: today the ids are shown by
`/use-kapa` and the listing endpoint. See Open Questions.

### 6. Kapa.ai hits in search responses

Hits already carry `Label: "kapa-canonical"`, which `searchResult.Label` passes through, so no
new provenance field is needed. `handleSearch` sets `Base: "kapa.ai"` for
`h.Index == knowledge.KapaIndexName` instead of calling `KnowledgeBaseNameFromIndex`; that call
errors for this index and today would leave `Base` empty. `source_id` keeps the per-hit URL.
`apiclient.SearchHit` and `ui/lib/api/search.ts`'s `SearchResult` need no new fields.

### 7. Reporting kapa.ai problems on search without breaking the response

When groups are requested but no client exists, or the kapa.ai call fails, the local hits are
still returned. `syncResponse` gains an optional `warnings []string` (`omitempty`), set by a new
`respondSyncWarnings`. Existing responses are unchanged and existing clients ignore the field.
`ui/lib/api/envelope.ts` exposes the warnings to callers; `apiclient` returns them so `k search`
in daemon mode prints them to stderr; direct-mode `k search` and REPL `/search` print them as the
prerequisite does for direct runs.

*Alternatives rejected:*
- Changing `metadata` from an array to an object breaks every existing search client.
- Failing the request contradicts the prerequisite's rule that kapa.ai problems degrade to local
  results.

### 8. REPL `/search`

`handleSearch` (`chat/search.go`):

- **Preconditions.** Rejects only when neither `ActiveIndexes` nor `ActiveKapaGroups` is
  non-empty, with guidance naming `/use-knowledge` and `/use-kapa`. The knowledge-client and
  embedding check applies only when local bases are active.
- **Retrieval.** Runs both retrievals concurrently and prints local hits followed by kapa.ai
  hits. `formatSearchResults` already tags kapa.ai hits via `LabelTag(hit.Label)`.
- **Failures.** A kapa.ai failure prints a warning and keeps the local results.
- **Remote mode.** The CLI's remote `/search` (`remoteSearch` in `chat/remote.go`) passes the
  session's selected groups to `apiclient.Search` and prints any warnings.

### 9. UI: two more selector rows, with the prerequisite's view states

Following `.claude/skills/ui-conventions/SKILL.md` and the prerequisite's D8:

- **Chat screen.** `ChatScreen.tsx` renders a `.kapa-selector` row below `.kb-selector`, with
  its own label ("Kapa.ai sources") and status dot. It uses toggle chips
  (`p-chip`/`p-chip--positive` + `p-chip__value`), keyed by id and showing the name.
- **Search page.** `SearchScreen.tsx` renders the same row in its scope area. It round-trips the
  selection through a `g` URL parameter next to `q`/`b`/`k`, sends `kapa_groups` only when
  non-empty, and shows response warnings as a caution notification above the results.
- **View states.** Loading, empty, loaded, and error (with retry), plus unconfigured rendered as
  a one-line credentials hint, matching the answer-batch picker. An earlier draft hid the row
  entirely when kapa.ai was unavailable; the newer change requires guidance instead, so the row
  is kept.
- **Styles.** Under the prerequisite's `// --- kapa ---` group in `globals.scss`, `--vf-*`
  tokens only.
- **No shared component yet.** No `KapaSelector` is extracted: the answer-batch, chat, and
  Search screens each render their own row, as the knowledge-base selectors do today. Extract
  one only if the three drift.

## Snap packaging and config

- **Config and secrets.** No new config keys or secrets. Config is read only through the
  prerequisite's startup resolution (snapctl-backed, `package` then `user` precedence), and
  `KAPA_API_KEY` stays an environment variable: a systemd drop-in for `ragd`, the shell for the
  CLI.
- **Packaging.** No new plugs or bundled binaries, and no `environment:` stanza (it would
  override the drop-in). No hook changes beyond the prerequisite's.
- **CLI credentials file.** The CLI's new `pkg/credentials` file fallback supports only
  `OPENSEARCH_*` and `CHAT_API_KEY`. Whether `KAPA_API_KEY` belongs there is the prerequisite's
  decision; this change does not add it.

## Risks / Trade-offs

- **Group ids are opaque for `k search`.** Without "all groups", a CLI user needs ids.
  → `/use-kapa` and the listing endpoint expose them; see Open Questions.
- **The selector rows add an upstream call per chat and Search page load.** Each mount lists
  groups from kapa.ai. → Same cost the answer-batch picker accepts; caching is an open question
  in the prerequisite.
- **Stale saved group ids return nothing.** A group deleted in kapa.ai silently stops
  contributing after resume. → Acceptable for now; reconciliation against the listing is a
  possible follow-up.
- **`/search` output changes for sessions with both sources active.** Kapa.ai hits now appear
  after local hits. → Intended: `/search` previews what the chat loop retrieves.
- **The prerequisite may choose different wire names.** → Decision 2 says to adopt them.

## Migration Plan

Implement after `add-kapa-retrieval-wiring` is merged, in independently shippable steps:

1. Daemon session (Decisions 2–4): `LiveSession`, `POST /1.0/chat`, acknowledgement, resume, saved
   chats.
2. Search (Decisions 5–7): `k search`, `POST /1.0/search`, warnings, `apiclient`.
3. REPL `/search` (Decision 8).
4. UI (Decision 9): chat row, then Search page.
5. Docs.

No data migration: `kapa_groups` is an additive, `omitempty` field.

**Verification.**
- Run `make all` and `npm test` in `ui/`.
- Build and install the snap, then supply `KAPA_API_KEY` through the drop-in.
- Exercise each surface with kapa.ai configured, unconfigured, and unreachable.

## Open Questions

- Should `k search` offer a way to list group ids (for example `k search --list-kapa-groups`),
  or should the prerequisite's open question about advertising `/use-kapa` cover discovery?
- Should resume warn when a saved group id is no longer in the listing, once caching exists?
