# Tasks

## 1. Citation model and resolver chain (knowledge)

- [x] 1.1 Add `knowledge.Citation` (Key, Label, Href, Locator, Base, Tag, Via `json:"-"`) and a `Citation` field on `SearchHit` (design D1); verify `go build ./...` and that `POST /1.0/search` JSON now carries a `citation` object
- [x] 1.2 Add `SourceMetadata.SourceURI` (`source_uri`, keyword, omitempty) to the struct and `buildSourcesIndexBody`, plus an idempotent `EnsureSourceURIMapping` (modelled on `EnsureLabelMapping`) called from engine init and ingest (D4); verify by running it twice against an existing metadata index with no error and checking the mapping via `GET rag-snap-metadata/_mapping`
- [x] 1.3 Add a shared temp-name pattern constant next to the `CreateTemp` call sites (`rag-crawl-`, `rag-github-`, `ragd-upload-`) and the title sanity check (D3); verify with table tests covering junk titles (`Microsoft Word - x.docx`, empty, equals file name, `untitled`) and temp names
- [x] 1.4 Implement the `CitationResolver` interface and `KapaResolver`, `MetadataResolver` (one `_mget` for distinct source ids; errors logged, never returned), and `SourceIDResolver` (total), applied in order and filling only empty fields (D2, D3); verify with unit tests over fake metadata covering every link/label scenario in `specs/source-citations/spec.md`, including a failed `_mget` and a source with no metadata doc
- [x] 1.5 Run the chain inside `OpenSearchClient.Search` and `KapaClient.Search` before they return; verify `k search` and `POST /1.0/search` return citations for local and Kapa hits
- [x] 1.6 Show the resolved citation (label, link, then source ID when it differs) in place of the bare source ID in the CLI search output: `/search` (direct and remote) and `k search` (direct and daemon); verify with a `SourceLine` unit test

## 2. Ingest records the source URI

- [x] 2.1 Add `IngestOptions.SourceURI`, record it in metadata, and set `FileName` from the original input (basename of `MetadataPath`, last URL path segment, or empty) instead of the temp file (D5); verify with a unit test on the metadata built for URL, file, and repo inputs
- [x] 2.2 Record the URL as `source_uri` for CLI `--url` and UI/API URL ingests; verify by ingesting a URL and reading `GET /1.0/knowledge/<kb>/sources/<id>`
- [x] 2.3 Add `RepoEntry.WebURL` for GitHub (`/blob/<branch>/<path>`) and Gitea (`/src/branch/<branch>/<path>`) built where the branch is resolved; verify with unit tests alongside `repo_source_test.go`
- [x] 2.4 Fix CLI batch repo ingest (`knowledge/batch.go`) to pass `MetadataPath = entry.Path` and `SourceURI = entry.WebURL`, and the UI repo path (`handlers_ingest.go:336`) to pass `SourceURI`; verify a batch-ingested repo file's metadata has the repo-relative `file_path` and the web `source_uri`
- [x] 2.5 Fix UI upload ingest to record `header.Filename` as `file_path`, and accept an optional `source_uri` form field validated as absolute http(s) (400 on invalid, before any operation starts); verify with an API handler test for the valid, invalid, and absent cases
- [x] 2.6 Add `k ingest --uri <url>` (only with `--file`; rejected with `--url`; validated before Tika runs); update `--help`, `docs/usage.md`, `README.md` ingest section, and `apps/completion.bash`; verify `k ingest kb id --file x.pdf --uri file:///x` fails with the http(s) error and a valid URI is stored

## 3. Markers, registry, and answer validation (chat core)

- [x] 3.1 Add `CitedSource{Marker, Citation}` and `CitationRegistry` (key→n, next, rebuild-from-sources) (D6); verify with unit tests for stable numbering across calls and rebuild with `next = max+1`
- [x] 3.2 Change `formatContext` to prefix each chunk with `[<n>] <TAG> <label>` (dropping the score), and `retrieveContext` to return `(text, []CitedSource)`; update the three callers to compile with the existing behavior; verify with a `formatContext` test where two chunks of one document share a marker
- [x] 3.3 Implement `citeAnswer` (tolerant marker parsing and normalization, dropping unknown markers, first-cited order, withheld when context was supplied and nothing is cited) and a batch marker-stripping helper (D7); verify with table tests for every scenario in the "Answers cite only retrieved documents", "Grounded answers without citations are withheld", and "Batch answers are free of citation markers" requirements
- [x] 3.4 Add the fixed `ragCitationRules` and send them with retrieved context only, via `buildRAGPrompt(…, cite)`, leaving system prompts exactly as resolved (D8, revised); verify with tests that a custom variant's system prompt is unchanged, that the rules follow the context and precede the question, that the empty-context prompt carries no rules, and that the batch system prompt is identical across questions of one run
- [x] 3.5 Remove every marker from an answer to a turn without retrieval, and run `citeAnswer` on every chat answer (REPL and live session), not only grounded ones (D7, D8); verify with a `citeAnswer` case where nothing was retrieved and the answer contains `[1]`

## 4. Batch answers carry sources

- [x] 4.1 Add `Sources []CitedSource` (`json:"sources"`) to `BatchResult`. For each question use a fresh registry, run `citeAnswer`, strip markers from `answer`, and use the no-answer response with empty `sources` both when nothing was retrieved and when the answer is withheld; verify by extending `batch_test.go` with a fake LLM reply containing `[1][7]`, and with one containing no markers
- [x] 4.2 Confirm the API results (`POST /1.0/answer/batch`) carry `sources` with no handler-specific code; document the `sources` field in the `answer batch` output description in `docs/usage.md` and `docs/rest-api.md`; verify by running a small batch through the API and inspecting the results JSON

## 5. Chat REPL, live session, and websocket

- [x] 5.1 Direct REPL (`client.go`): hold a session-wide registry on `Session`, run `citeAnswer` after streaming, print a dimmed Sources block, and on withheld print the notice plus the no-answer response and store the no-answer response in history (D9); verify by hand against an installed snap, with one cited answer and one forced-uncited answer (e.g. a prompt variant that forbids markers)
- [x] 5.2 `LiveSession.Prompt` (`turn.go`): same registry and validation. Emit the `sources` frame before the first token, and extend the `done` frame with `sources`, `withheld`, and replacement `content`; add `Sources`/`Withheld` to `chatServerMessage`; verify with a websocket handler test asserting frame order and contents for a cited answer, a withheld answer, and a no-RAG answer
- [x] 5.3 Remote REPL (`remote.go`): render the `sources` and `done` frames identically to direct mode; verify by running `rag-cli.rag chat` in remote mode against ragd and comparing its output with direct mode for the same prompt
- [x] 5.5 Start the remote REPL's daemon session with the `default` base active and print the active set (D8a); verify `rag-cli.rag chat` through ragd prints `Active knowledge bases: default`
- [x] 5.4 Update `docs/usage.md` (chat: Sources block, withheld answers, `[#]` markers) and the chat websocket frame list in `docs/rest-api.md`; verify the documented frame shapes match the handler test fixtures

## 6. Saved chats carry sources

- [x] 6.1 Add `chatstore.Source` and `Turn.Sources` (`omitempty`) with no `cmd/cli` imports (D10); verify that an existing saved-chat JSON fixture without `sources` still loads in a `store` test
- [x] 6.2 Keep a per-session slice of sources parallel to the assistant messages; thread it through `historyToTurns`/`turnsToHistory`, `/save`, `Restore`, and the registry rebuild; verify with `history_test.go` round-trip tests that sources survive save→load and that a resumed session numbers a new document `max+1`
- [x] 6.3 Show sources on restored assistant turns in `renderTranscript`; verify by hand by resuming a saved chat in the REPL

## 7. Web UI

- [x] 7.1 Add `Citation`/`CitedSource` to `ui/lib/types.ts`, with `QAItem.sources?` optional; verify `npm run build` and that the existing `QACard.test.tsx` passes unchanged
- [x] 7.2 Extend `Markdown` with an optional `citations` map: the inline parser recognizes `[<n>]` before links and renders a `p-chip` anchor or span (with `aria-label`/`title` set to the label, `rel="noopener noreferrer"`) only for known markers (D11); verify with component tests for a known marker, an unknown marker, and a marker next to `(…)`
- [x] 7.3 `ChatScreen`: seed the citations map from the `sources` frame; on `done`, set the cited list, render a Sources list, and handle withheld (replace content plus a caution notice); render sources on restored turns; verify with a ChatScreen test driven by fake websocket frames
- [x] 7.4 Search page: link the header label when there is an `href` and move the `source_id` to the footer; verify by hand and in a test with one linked and one unlinked hit
- [x] 7.5 `IngestModal`: add an optional Source URL field for uploads only, with client-side http(s) validation, sent as `source_uri`; verify the field is hidden for URL ingest and blocks submission on invalid input
- [x] 7.6 `QACard`/`ResultsReview`: render the per-answer sources list, keeping older results files that lack `sources` working; verify by extending `QACard.test.tsx` with and without sources
- [x] 7.8 `ChatScreen`: preselect the `default` chip when that base exists and nothing is selected, and drop markers that match no retrieved/cited document from the displayed answer (D8a); verify with a `chatMessages` test for a marker in a turn with nothing retrieved
- [ ] 7.7 Update `docs/local-ui.md` (chat source chips, "No relevant sources" notice, Source URL field, batch sources); verify compliance with the `ui-conventions` skill: both themes, a keyboard-only pass over the chips and Sources links, `--vf-*` tokens only, and all four view states on the touched screens

## 7a. Revision: numeric markers, uncited chat answers kept

- [x] 7a.1 Markers become `[1]`, `[2]`, … (an `S` prefix is tolerated); parsing skips code and word-glued brackets and caps numbers at three digits, in `citeAnswer` and the UI `normalizeMarkers`/`Markdown` (D7); verify with `citeAnswer`, `chatMessages`, and `Markdown` tests for code, `arr[1]`, `[2024]`, `[S1]`, and `[1](…)`
- [x] 7a.2 Uncited chat answers are kept and flagged `unsourced` (REPL notice, remote REPL, `done` frame `unsourced`, UI "No relevant sources" line), while batch still withholds (D7, D9, D11); verify with the websocket handler test, the `citeAnswer` cases, and `batch_test.go`
- [x] 7a.3 Update `docs/usage.md`, `docs/rest-api.md`, `docs/local-ui.md`, the ui-conventions skill, and the spec deltas; verify `openspec validate add-source-citations --strict`

## 8. Integration

- [x] 8.1 Run `make all` (tidy fmt vet lint test build) and `npm run build` in `ui/`; verify both succeed
- [ ] 8.2 Build and install the snap. Then:
  - ingest one URL, one file with `--uri`, and one GitHub repo;
  - import a pre-change exported base;
  - run chat (direct and remote), `/search`, `answer batch`, and the UI chat;
  - verify each scenario in `specs/source-citations/spec.md`, including links only where a URL was recorded and the imported base falling back to `source_id`.
- [x] 8.3 Run `openspec validate add-source-citations --strict`; verify it passes

## Workflow follow-up

- Sync delta specs and archive the change after review (`/opsx:archive`).
