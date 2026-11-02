# Design

## Context

See proposal.md for the motivation.

Every search surface calls `OpenSearchClient.Search` (`cmd/cli/basic/knowledge/search.go`):
`k search` and the REST handler directly, in-chat `/search` locally or through the daemon, and the
UI through `POST /1.0/search`. `Search` sends one hybrid query per index with `size: k`, then
merges the per-index results by score **without capping**, so N bases can return up to N×k hits.

The search pipeline (`buildSearchPipelineBody`) always ends with the `rerank` processor, which
replaces each hit's `_score` with the cross-encoder's output. The default cross-encoder,
`ms-marco-MiniLM-L-12-v2`, is a binary relevance classifier trained on MS MARCO. Its raw output
is a logit, where sigmoid(score) estimates P(relevant), so 0 is the model's own decision
boundary (P = 0.5).

The chat RAG loop (`chat/rag.go`) also calls `Search` (k = 15 per active base) and must not
change.

## Goals / Non-Goals

**Goals:**
- The match rule exists in exactly one place: Go, on the search path. The UI only reads a flag.
- The CLI and UI split results identically, enforced by tests.
- Chat RAG retrieval unchanged.

**Non-Goals:**
- A configurable threshold. It's a fixed constant; making it configurable later would need a
  new `package` key seeded by the install hook (a `user` set rejects unknown keys).
- Changing the pipeline's hybrid weights, normalization, or candidate pool.
- Paging. Users who want more results raise the count.
- Changes to ingestion or kapa.ai retrieval.

## Decisions

### The server owns the threshold and annotates each hit

`knowledge.MatchScoreThreshold = 0.0` and `knowledge.IsMatch(score) bool` (`score > 0`).
`SearchHit` gains `Matched bool`, set in `hybridSearch` for every hit. Chat RAG never reads it,
so its retrieval and prompt formatting are unchanged.

- *Alternative: each client compares `score > 0`.* Rejected. It duplicates the rule in Go and
  TypeScript, and clients would have to know the score is a cross-encoder logit. If the reranker
  or threshold changes, only the server should need to change.
- *Alternative: threshold on the normalized hybrid score.* Rejected. `min_max` is relative to each
  response; the best hit always scores about 1.
- *Alternative: require a BM25 hit.* Rejected. It drops real semantic matches.

### Count caps the total, in a new entry point; `Search` stays as is

Add `SearchTop(ctx, indexes, query, lexicalQuery, embeddingModelID, k)`: it calls the same
per-index query as `Search` (`size: k` each), merges by score, and truncates to `k`. Since each
index's top k contains every member of the global top k from that index, truncating after the
merge yields the exact global top k.

The REST handler, `k search` (direct path), and `/search` (local path) switch to `SearchTop`.
`chat/rag.go` keeps calling `Search`, which returns up to 15 per base exactly as today.

- *Alternative: cap inside `Search`.* Rejected. That would silently shrink chat context
  whenever more than one base is active.
- *Alternative: cap in each client.* Rejected. The API would keep violating its spec, and
  every client would have to remember to trim.

### REST: one added field, nothing else

`searchResult` gains `Matched bool \`json:"matched"\``. Request, ordering, and list shape
are unchanged. The count cap is a fix toward the existing spec, not a contract change.
`apiclient.SearchHit` gains `Matched *bool \`json:"matched"\`` so a missing field (an older
`ragd`) can be told apart from `false`. The CLI, `ragd`, and UI ship together in the snap, so
skew is unlikely, but a missing flag defaults to matching, which is today's display behaviour.

### One split function per language, pinned by a shared fixture

- Go: `knowledge.SplitByMatch(hits) (matched, similar)`, a stable, order-preserving partition. The
  daemon paths of `k search` and `/search` convert `apiclient.SearchHit` (nil → matching) and
  use the same partition.
- TypeScript: `splitResults(results)` in `ui/lib/searchSplit.ts`, with `matched` optional
  (`undefined` → matching).
- Fixture: `testdata/search_split_cases.json` at the repo root. Each case lists hits (score,
  `matched` present/true/false/absent, an id) and the expected matched ids, similar ids, and
  counts. Go tests (`cmd/cli/basic/knowledge/search_split_test.go`) and TS tests
  (`ui/lib/searchSplit.test.ts`, picked up by the existing `lib/*.test.ts` glob) both load it.
  A divergence in either fails that suite. Server-side `IsMatch` is tested against the scores
  in the same fixture, so the fixture's `matched` values are what the server would emit.

Message wording differs only where the surface does (a CLI flag vs a UI button). The core
phrases are shared constants per language and asserted by the fixture's expected strings:
`No results match "<query>".` and `<N> similar result(s) don't match the query.`

### CLI rendering

- `k search`: a new `--show-similar` bool flag (Cobra adds it to completion automatically).
- `/search`: `parseSearchArgs` accepts `--show-similar` anywhere among the tokens and returns
  `{k, showSimilar, terms}`. The usage text and inline hint become
  `[-k N] [--show-similar] <query>`.
- Output: matched hits in each command's existing format. Then either the notice
  (`… Use --show-similar to show them.`) or, with the flag, a separator
  `── Similar results (do not match the query) ──` followed by the similar hits in the same
  format, numbered on from the matches.

### UI

Following the `ui-conventions` skill:

- `SearchScreen.tsx` uses `splitResults`. Matching cards render as today; the live region counts
  matches only.
- When similar results exist: a `p-text--small u-text--muted` message with a `p-button--base`
  toggle ("Show similar results" / "Hide similar results"), `aria-expanded` and `aria-controls`
  on the container. Similar cards reuse the result-card markup with a modifier class (muted via
  existing `--vf-*` tokens, checked in both themes) and a non-interactive "Not matching"
  `p-chip`. Their ranks continue from the matches, as in the CLI.
- The toggle is component state, reset on each search, and not kept in the URL.
- No-hits state: "No results match …", naming the bases. It drops the "raise the Results count"
  suggestion, since a higher count can't create matches, and shows the toggle beneath.

### Snap, config, secrets

No new config keys, secrets, snap interfaces, bundled binaries, or hook changes.

## Risks / Trade-offs

- **A different reranker changes what 0 means.** One that outputs probabilities (0 to 1) would
  mark everything as matching, which reverts to today's display behaviour rather than hiding
  results. → `IsMatch` documents the logit assumption. Because the rule is server-side, a fix is
  one constant.
- **Unverified: OpenSearch may not return raw logits.** → Task 6.2 checks real `k search` output
  for negative scores before release. If every score falls between 0 and 1, the threshold
  becomes 0.5 (still one constant).
- **Borderline relevant chunks get hidden.** Short keyword queries score lower than questions,
  so a useful chunk at −0.3 becomes a similar result. → It's one toggle away. Chat RAG, where
  missing context hurts most, is unaffected.
- **Multi-base searches return fewer hits than before** (count is a total). → Intended, and
  documented in usage and REST docs; users raise the count.

## Migration Plan

Nothing to migrate: no stored data, index, pipeline, or config changes. Roll back by
reverting the change.
