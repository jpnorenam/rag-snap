# Tasks

## 1. Match rule, capped search, and the shared fixture

- [x] 1.1 Create `testdata/search_split_cases.json` with cases covering: all matching; none matching; mixed; a score of exactly 0; a hit with `matched` absent; empty result. Each case lists hits (id, score, matched true/false/absent), the expected matched ids, similar ids, and the core notice strings. Verify the file parses (`python3 -m json.tool`).
- [x] 1.2 In `cmd/cli/basic/knowledge/search.go`, add `MatchScoreThreshold`, `IsMatch`, and `SearchHit.Matched` (set in `hybridSearch`). Verify with `search_split_test.go`: `IsMatch` agrees with every fixture hit that has a `matched` value, and a score of 0 does not match.
- [x] 1.3 Add `SplitByMatch(hits)` and the shared message helpers (`NoMatchesMessage`, `SimilarNotice`, `SimilarSeparator`). Verify by running every fixture case through `SplitByMatch` and the helpers in `search_split_test.go` and asserting the ids, order, and strings.
- [x] 1.4 Add `SearchTop(...)` (merge, then truncate to k) as a pure, testable merge step plus the client method, leaving `Search` untouched. Verify with a unit test that merges two indexes' hits and returns the exact global top k, and with `git diff` showing no change to `Search` or `chat/rag.go`.

## 2. REST API and client

- [x] 2.1 Add `matched` to `searchResult` and switch `handleSearch` to `SearchTop`. Update the swagger comment. Verify with a handler test (following `handlers_knowledge_test.go`) that every hit carries `matched`, that ordering is unchanged, and that `count` caps hits across two bases.
- [x] 2.2 Add `Matched *bool` to `apiclient.SearchHit` and a conversion helper (nil → matching) that feeds `SplitByMatch`. Verify with a client test that decodes a response with and without the field.
- [x] 2.3 Update `docs/rest-api.md` for `POST /1.0/search`: the `matched` field and its rule, and that `count` is a total across bases. Verify the documented example matches the handler test's output.
- [x] 2.4 Document the match threshold (score is a reranker logit, `matched` when score > 0 exclusive, owned by the server) in the `handleSearch` swagger annotation and a "The match threshold" section of `docs/rest-api.md`. Verify `make spec-check` reports `rest-api.yaml` in sync.

## 3. CLI `k search`

- [x] 3.1 Add `--show-similar` to `searchCommand`. Use `SearchTop` on the direct path and the converted daemon hits on the daemon path, rendering through `SplitByMatch` and the shared helpers. Verify `go build ./...`, that `rag-cli.rag k search --help` lists `--show-similar`, and with a rendering unit test for: matches only; matches + hidden similar; none + hidden similar; `--show-similar` order.
- [x] 3.2 Update the `knowledge search` section of `docs/usage.md` (matching rule, similar results, `--show-similar`, `--top` is a total across bases). Verify every documented flag appears in `--help`.

## 4. In-chat `/search`

- [x] 4.1 Change `parseSearchArgs` in `cmd/cli/basic/chat/search.go` to return `{k, showSimilar, terms}` and accept `--show-similar` anywhere. Verify with tests: flag before, between, and after terms; combined with `-k`; flag with no terms is rejected.
- [x] 4.2 Switch `handleSearch` to `SearchTop` and make `remoteSearch` use the converted hits. Render both through `SplitByMatch` and the shared helpers. Verify with formatting tests for the same four cases as 3.1.
- [x] 4.3 Update `searchUsage` and the inline hint to `[-k N] [--show-similar] <query>`. Verify by updating the hint case in `commands_test.go`, then update the chat `/search` section of `docs/usage.md`.

## 5. UI search page

- [x] 5.1 Add optional `matched?: boolean` to `SearchResult` in `ui/lib/api/search.ts`, and `splitResults` plus the message strings in `ui/lib/searchSplit.ts`. Verify with `ui/lib/searchSplit.test.ts`, which loads `../testdata/search_split_cases.json` and asserts every case's ids, order, and strings (`npm test`).
- [ ] 5.2 In `SearchScreen.tsx`, render matching cards only, with the live region counting them. When similar results exist, add the message with a `p-button--base` toggle (`aria-expanded`/`aria-controls`) and similar cards with a "Not matching" `p-chip` and a muted modifier class. Reset the toggle on each search. Verify manually against `ragd` with an absent term and a present term.
- [ ] 5.3 Update the no-hits state: "No results match …" naming the bases, with no "raise the Results count" hint, and the toggle beneath. Verify manually with an absent term.
- [ ] 5.4 Verify compliance with the `ui-conventions` skill: light and dark themes, a keyboard-only pass (toggle operable, focus stays in the query input after submit), only `--vf-*` tokens in new SCSS, and initial/loading/no-hits/no-KBs/error states still distinct. Update the search page section of `docs/local-ui.md`.

## 6. Integration

- [x] 6.1 Run `make all` and `cd ui && npm run lint && npm test && npm run build`, and verify all pass. (Lint is scoped to this change's lines with `golangci-lint run --new-from-rev=HEAD`, which reports 0 issues; the pre-existing repo-wide issues and the broken `next lint` script are handled in a separate PR. Formatting, `go vet`, `go test ./...`, `make build`, `make spec-check`, `npm test` and `npm run build` pass.)
- [ ] 6.2 Build and install the snap (`snapcraft -v`, `sudo snap install --dangerous ./rag-cli_*.snap`). Against a real knowledge base, first confirm `k search kafka` shows **negative** scores; if every score falls between 0 and 1, stop and revisit the threshold. Then verify: the absent term reports no matches plus a similar count; `--show-similar` reveals them; a present term shows matches; `--bases a,b --top 3` returns 3 in total; the daemon path, in-chat `/search`, and the UI show the same split for the same query; and a chat question still retrieves the same context as before (`--verbose` count unchanged).

## Workflow follow-up

- Archive with `/opsx:archive` once implemented and reviewed. This merges the deltas into `openspec/specs/chat-search`, `rest-api-knowledge`, and `local-ui-app`.
- `chat-search`'s Purpose still reads "TBD". Update it, since the capability now covers `k search` and CLI/UI parity.
