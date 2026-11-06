# Proposal

## Why

Search never comes back empty. The hybrid pipeline's neural arm always returns its nearest
neighbours, so a query for a term that appears nowhere in the knowledge bases (e.g. `kafka`)
still fills every result slot with unrelated chunks, presented as if they matched. The
`chat-search` spec already has a "No matching chunks" scenario, but "matching" is undefined
and the scenario can never happen against a non-empty base. Users read the padded results as
real matches and lose trust in search.

Separately, `POST /1.0/search`, `k search`, and `/search` apply the result count to **each**
knowledge base, so searching two bases with a count of 10 can return 20 hits. That
contradicts the REST spec's existing "at most that many hits" scenario.

## What Changes

- **Server-owned match rule.** The search response marks each hit with a new `matched`
  boolean: true when its relevance score (the cross-encoder rerank score) is greater than 0.
  The server is the only place that knows the rule, so clients never compare scores themselves
  and a future change to the reranker or threshold only touches the server.
- **The REST response shape is otherwise unchanged:** same request, same list of hits, same
  order. `matched` is an added field.
- **The result count caps the total across all searched bases**, for `POST /1.0/search`,
  `k search --top`, and `/search -k`, which brings the implementation into line with the
  existing REST spec. A multi-base search that returned up to N×count hits now returns at most
  count. To see more, raise the count.
- **CLI and UI show matching hits only.** Every other returned hit is a **similar result**:
  hidden by default behind a toggle, with a message saying how many similar results don't
  match the query. When nothing matches, they say that no results match the query, followed by
  the same toggle.
  - `k search` and in-chat `/search`: a new `--show-similar` flag/option.
  - UI `/search/` page: a show/hide toggle, collapsed by default and on every new search.
- **CLI/UI parity is specified and tested.** A new requirement says the CLI and UI split and
  summarize results identically. A shared test fixture is checked by both the Go and the
  TypeScript tests.
- **Not changed:** the chat RAG loop keeps today's retrieval exactly (15 per active base, no
  threshold, no cap across bases).

No breaking changes. Clients that ignore `matched` keep working; they just receive at most
`count` hits for multi-base searches, as the spec always said.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `chat-search`: defines matching hits (from the server's `matched` flag), hides similar
  results behind `--show-similar` in `/search`, makes the "No matching chunks" scenario
  reachable, makes `-k` a total across bases, and adds `k search` (which has no spec of its
  own today) plus a CLI/UI parity requirement.
- `rest-api-knowledge`: the "Hybrid search" requirement gains the per-hit `matched` flag, and
  the result count is a total across bases.
- `local-ui-app`: search results show matches only, with a collapsed similar-results toggle;
  the no-hits state accounts for similar results.

## Impact

- **External services:** OpenSearch only (no query changes, only how the response is merged
  and annotated). The inference server and Tika are not touched; chat RAG is unchanged.
- **Config:** no new config keys. The threshold is a server-side code constant.
- **Code:** `cmd/cli/basic/knowledge/search.go` (match flag, capped search entry point),
  `cmd/cli/basic/knowledge.go` (`k search`), `cmd/cli/basic/chat/search.go` and
  `chat/remote.go` (`/search`), `internal/api/handlers_search.go`,
  `internal/apiclient/resources.go`, `ui/lib/api/search.ts`, `ui/components/SearchScreen.tsx`,
  plus a shared test fixture under `testdata/`.
- **User-facing surfaces:** new `k search --show-similar` flag, new `/search --show-similar`
  option, new UI toggle, new `matched` field in the REST response, and changed count
  semantics for multi-base searches. Documentation to update: `docs/usage.md`
  (`knowledge search` and the chat `/search` section), `docs/rest-api.md` (`POST /1.0/search`),
  `docs/local-ui.md` (search page), the `/search` usage text and inline hint in the REPL, and
  the swagger annotation on `handleSearch`.
