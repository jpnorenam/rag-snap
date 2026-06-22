## 1. Dispatcher and command registration

- [x] 1.1 Add `cmdSearch = "/search"` constant in `cmd/cli/basic/chat/commands.go` and append it to `slashCommands`.
- [x] 1.2 Change `handleSlashCommand` to split verb from args (`strings.Cut` on the first space) and switch on the verb; keep `/use-knowledge` working with empty args.
- [x] 1.3 Confirm readline autocomplete and `slashHinter` pick up `/search` from `slashCommands` (no change needed beyond 1.1 if both iterate the slice).

## 2. Search handler

- [x] 2.1 Add `cmd/cli/basic/chat/search.go` with a `handleSearch(args string, session *Session)` entry point.
- [x] 2.2 Parse the optional `-k N` flag and the remaining query terms; default `k` to `defaultRAGTopK`. Reject non-positive / non-integer `N` and empty queries with a usage message (no search).
- [x] 2.3 Validate preconditions mirroring `retrieveContext`: nil `KnowledgeClient`, empty `ActiveIndexes` (advise `/use-knowledge`), empty `EmbeddingModelID` (retrieval unavailable).
- [x] 2.4 Call `session.KnowledgeClient.Search(ctx, session.ActiveIndexes, terms, terms, session.EmbeddingModelID, k)` — verbatim terms for both query and lexicalQuery; no `rewriteSearchQuery`, no inference call.
- [x] 2.5 Handle the zero-hits case with a "no results" message.

## 3. Result formatting

- [x] 3.1 Add a human-facing formatter (not `formatContext`) that, per hit, prints score, KB name via `knowledge.KnowledgeBaseNameFromIndex`, source ID, created date, the `sourceLabel` provenance tag, and the full untruncated content.
- [x] 3.2 Ensure results render in score-descending order (already guaranteed by `Search`) and are visually separated.

## 4. Validation

- [x] 4.1 Run `make all` (tidy fmt vet lint test build) locally. tidy/fmt/vet/test/build pass; new `search.go` is lint-clean (remaining golangci-lint findings are pre-existing in other chat files).
- [x] 4.2 Build and install the snap; in `rag-cli.rag chat`, verify `/search <terms>`, `/search -k N <terms>`, invalid `-k`, empty query, no-active-bases, and full-content output against a populated knowledge base.
- [x] 4.3 Confirm `/search` does not append to conversation history and never contacts the inference server (e.g. works with the chat backend stopped but OpenSearch up).

## 5. Inline syntax hint (argument discoverability)

- [x] 5.1 Replace `slashCommands []string` with a `slashCommand{name, syntax}` registry; update the `slashHinter` and autocomplete consumers to use `.name`.
- [x] 5.2 Add `syntaxHint` (pure resolver) and a `syntaxPainter` readline Painter that renders the command's argument syntax as dimmed inline ghost text after the cursor; wire `Painter` into the chat `rlConfig`.
- [x] 5.3 Add a unit test for `syntaxHint`; confirm `/search` shows the `[-k N] <query>` hint and it clears once the query begins.
