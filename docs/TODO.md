# RAG Snap — Roadmap & Development Backlog

Items are grouped by milestone. Within each milestone, order reflects priority.
Legend: `[ ]` = open · `[x]` = done · `[-]` = deferred/out-of-scope for now

---

## v0.0.1 — Release readiness

### Testing
* [ ] Add `go test` unit tests for `processing/chunker.go` — table-driven, cover prose splits, table atomicity, overlap, empty input
* [ ] Add unit tests for `chat/rag.go` — `stripThinkTags`, `formatConversationForRewrite`, `buildRAGPrompt`, `rewriteSearchQuery` (mock the OpenAI client)
* [ ] Add unit tests for `knowledge/search.go` — `buildSearchBody` field shape, `formatContext` output
* [ ] Add `golangci-lint` as a `Makefile` target (`make lint`) and document it in `CONTRIBUTING.md`
* [x] Add `go test` for `pkg/utils`
* [ ] Create an extensive real-world knowledge base for manual end-to-end testing before release

### Document formats
* [ ] Verify and document support for DOCX, XLSX, PPTX via Tika (ingest a sample of each; note any formatting losses)

### Docs
* [x] Write usage docs (`docs/usage.md`) — knowledge commands, chat, workflow
* [ ] Write `CONTRIBUTING.md` — build steps, test commands, coding conventions, PR checklist

---

## v0.1 — Quality & reliability

### Ingestion correctness
* [ ] **Duplicate detection on ingest**: before bulk-indexing, look up the source checksum in the metadata index; if unchanged, skip re-ingest and print an informative message. If changed, offer `--force` to replace.
* [ ] **`knowledge reingest` command** (or `--update` flag on `ingest`): `forget` + re-ingest in one step, preserving the same source ID. Avoids the manual two-step.
* [ ] **Ingest progress for large documents**: emit chunk count as indexing proceeds (OpenSearch bulk API already returns per-shard counts — surface them).
* [ ] **Context timeout on all HTTP calls**: currently `context.Background()` is passed everywhere. Add a configurable deadline (default 120 s) so a hung server does not block the CLI indefinitely. Plumb through `cmd/cli/common/context.go`.

### Search quality
* [ ] **Score threshold filter**: drop search hits below a configurable relevance threshold (default 0.3) before injecting them into the RAG prompt. Low-score chunks add noise and can mislead the model. Expose as `config set chat.min-score 0.3`.
* [ ] **Deduplication of near-identical chunks**: after reranking, drop consecutive hits that share the same `source_id` and have overlapping content (Jaccard similarity > 0.85). This commonly happens when multiple overlapping chunks from the same paragraph are retrieved.
* [ ] **`knowledge status` command**: show pipeline health, registered models, and model deployment state. Currently `knowledge init` errors are opaque; a `status` sub-command gives users a diagnostic tool.

### Code quality
* [ ] Introduce a typed error taxonomy (`common/errors.go`): distinguish `UserError` (bad input, actionable message) from `SystemError` (infra failure, suggest logs). Use this in `RunE` handlers to control exit codes (1 = user error, 2 = system error).
* [ ] Replace ad-hoc `fmt.Printf` diagnostic output with a levelled logger that respects `--verbose`; avoid mixing progress output with structured data output.
* [ ] Extract the OpenSearch client interface (`OpenSearchClientI`) so unit tests can mock it without a live cluster.

---

## v0.2 — Retrieval quality

### Chunking improvements
* [ ] **Token-aware chunking**: replace the character-limit chunker with a token-counting one (using the embedding model's tokenizer, or a conservative `chars/4` estimate). Embedding models have token limits (typically 512); oversized chunks are silently truncated by the model, losing the tail. `DefaultChunkSize = 1024 chars` is close to the limit for many models.
* [ ] **Heading context injection into prose chunks**: `ChunkMarkdown` already tracks `currentHeading` for tables but drops it for prose blocks. Prepend the nearest H1/H2 heading to each prose chunk so search results carry section context (e.g. `## Authentication\n\nThe login flow…`).
* [ ] **Code block awareness**: treat fenced code blocks (` ``` `) as atomic units like tables — never split mid-block. Currently a long code example can be cut in the middle of a function, breaking the chunk's meaning.

### Chat / inference
* [ ] **Configurable system prompt**: `config set chat.system-prompt "You are a Canonical support engineer…"`. The current hardcoded `"You are a helpful assistant."` is too generic for domain-specific deployments.
* [ ] **`--temperature` and `--max-tokens` flags on `chat`**: expose the most impactful inference parameters without requiring config changes.
* [ ] **Conversation length management**: the message history grows unbounded. After N turns (configurable, default 20), summarise older turns into a single system message to prevent context-window overflow and keep latency stable.
* [ ] **Configurable hybrid search weights** (`config set chat.bm25-weight 0.3`, default matches current pipeline): the 0.3/0.7 BM25/neural split is hard-coded in `buildSearchPipelineBody`. Some corpora (technical docs with precise terminology) benefit from higher BM25 weight.

### UX
* [ ] **`/clear` slash command**: reset conversation history without exiting the session. Useful when switching topics mid-session.
* [ ] **`/sources` slash command**: after each LLM response, display the source IDs and scores of the chunks that were injected. Currently the verbose flag prints this to stderr; `/sources` should show it cleanly on demand.
* [ ] **Search result scores in `knowledge search` output**: already printed in verbose mode; make them visible by default to help users calibrate their knowledge bases.

---

## v0.3 — Ingestion breadth

### New input sources
* [ ] **Directory ingest**: `knowledge ingest <base> <source-id> --dir <path> [--glob "**/*.md"]` — recursively ingest all matching files, using relative paths as sub-source IDs.
* [ ] **Batch ingest from manifest**: `knowledge ingest <base> --manifest manifest.yaml` — YAML list of `{source_id, file|url}` entries, ingested sequentially with progress reporting and a summary of failures.
* [ ] **Recursive URL crawl**: `--url <url> --depth 2` — follow same-origin `<a href>` links up to the given depth. Gate behind an explicit opt-in flag; default behaviour (depth=0) is unchanged.
* [ ] **GSuite / Google Workspace integration**: `--gdoc <doc-id>` — export a Google Doc as plain text via the Drive API. Requires OAuth2 credentials stored in snap config.

### Format improvements
* [ ] **Markdown passthrough**: if the input file is already `.md`, skip Tika and feed it directly to the Markdown chunker. Tika's HTML-to-text round-trip loses Markdown structure.
* [ ] **Image alt-text extraction**: Tika can extract image alt text; include it in the chunked content for diagrams and screenshots.

### Knowledge base maintenance
* [ ] **`knowledge export <base> <output.tar.gz>`**: dump the index mapping, source metadata, and all raw chunk content to a portable archive.
* [ ] **`knowledge import <base> <archive.tar.gz>`**: restore a previously exported base. Enables backup, migration between clusters, and sharing curated bases.
* [ ] **`knowledge update-all <base>`**: for each source in the metadata index, re-fetch (for URLs) or re-stat (for files) and re-ingest if the checksum has changed. Useful as a scheduled refresh job.

---

## v0.4 — Platform & ecosystem

### Snap
* [ ] **arm64 support**: add `arm64` to `platforms:` in `snapcraft.yaml` and verify Tika + JVM work on arm64. OpenSearch snap already supports arm64.
* [ ] **Snap store listing**: write `snap/snapcraft.yaml` `description`, screenshots, and categories for the Snap Store page.
* [ ] **Auto-connection interfaces**: request auto-connection for `home` in the Snap Store submission (currently requires manual `snap connect`).

### CI / CD
* [ ] **GitHub Actions pipeline**: build + unit test on every PR (`go build ./...`, `go test ./...`, `golangci-lint`).
* [ ] **Snap build in CI**: `snapcraft` in a `ubuntu:24.04` container to catch packaging regressions.
* [ ] **Integration test suite**: a Docker Compose environment with OpenSearch + a mock inference server, run on PR merge to main.

### Batch / automation
* [ ] **`knowledge batch-qa <base> <source-id>`** (original TODO): extract question/answer pairs from an ingested document by prompting the LLM, then write them back as a synthetic source for improved retrieval on FAQ-style queries.
* [ ] **Non-interactive `chat` mode** (`--prompt <text>`, `--no-interactive`): single-shot query suitable for scripting. Output to stdout, exit 0 on success.

---

## Development best practices

### Testing conventions
* Use table-driven tests (`[]struct{ name, input, want }`) for all pure functions.
* Mock the OpenSearch client via an interface; never require a live cluster in unit tests.
* Integration tests (requiring live services) go in `*_integration_test.go` and are guarded by `//go:build integration`.
* Aim for ≥ 80 % line coverage on `processing/` and `chat/` packages before v0.1.

### Error handling
* All errors returned from `RunE` should be either a `UserError` (print message only, no stack) or wrapped with `%w` for chain inspection.
* Never `log.Fatal` or `os.Exit` inside library packages — only at the CLI entry point.
* Always read and close `resp.Body` even on non-OK HTTP responses; currently some error paths discard unread bodies.

### HTTP / OpenSearch client
* Propagate `context.Context` with a deadline from the CLI layer down to every HTTP call.
* Centralise HTTP error parsing in the OpenSearch client rather than repeating `io.ReadAll(resp.Body)` / status-check patterns in every method.
* Add retry logic (exponential back-off, max 3 attempts) for transient 5xx responses from OpenSearch.

### Snap & confinement
* Test every new flag and feature inside the snap (not just `go run`) — confinement blocks paths that work in devmode.
* Never add a new `stage-package` without checking its licence and size impact on the snap.
* Document any new interface plug in `docs/usage.md` so users know what to `snap connect`.

### Versioning
* Align `snap/snapcraft.yaml` `version:` with git tags; automate via `craftctl set version=$(git describe --tags)` in `override-pull`.
* Keep a `CHANGELOG.md` (Keep a Changelog format) updated with every PR that changes user-visible behaviour.
