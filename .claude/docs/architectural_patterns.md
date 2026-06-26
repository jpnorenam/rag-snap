# Architectural Patterns

## 1. Cobra CLI with Shared Context

Commands are organized hierarchically using [cobra](https://github.com/spf13/cobra). A `common.Context` struct is constructed at the root and passed down to every subcommand, carrying runtime flags and the resolved `storage.Config`.

- Root setup: [cmd/cli/main.go](cmd/cli/main.go)
- Context type: [cmd/cli/common/context.go](cmd/cli/common/context.go)
- `PersistentPreRunE` propagates `--verbose`, `--debug`, `--config` before any subcommand runs ([cmd/cli/main.go:90](cmd/cli/main.go#L90))

Command groups are declared with `cobra.Group` and assigned to commands via `GroupID` for organized `--help` output.

## 2. Interface-Backed Configuration with Two-Tier Precedence

`pkg/storage` defines a private `storage` interface implemented by two backends:

| Backend | File | When used |
|---------|------|-----------|
| `SnapctlStorage` | [pkg/storage/snapctl_storage.go](pkg/storage/snapctl_storage.go) | Default (production snap) |
| `FileConfig` | [pkg/storage/file_config.go](pkg/storage/file_config.go) | `--debug` flag / testing |

`Config` wraps two instances of this interface — `PackageConfig` (snap defaults written by hooks) and `UserConfig` (user overrides) — and always returns user values first. See [pkg/storage/config.go](pkg/storage/config.go).

Config keys follow a namespaced dot-path convention: `<service>.http.<field>` (e.g., `chat.http.host`, `knowledge.http.port`).

## 3. Service URL Construction

All service addresses are resolved through a single helper rather than hardcoded per-call:

- `buildServiceURL(host, port, path, secure)` → [cmd/cli/basic/common.go:56](cmd/cli/basic/common.go#L56)
- `serverApiUrls(ctx)` returns a `map[string]string` with keys `"openai"`, `"opensearch"`, `"tika"` → [cmd/cli/basic/common.go:133](cmd/cli/basic/common.go#L133)

Both functions read from `ctx.Config` so callers never inspect config keys directly.

## 4. OpenSearch Client as a Capability Object

`knowledge.OpenSearchClient` wraps the raw `opensearchapi.Client` and holds resolved model IDs and pipeline names as fields. It is constructed once per command invocation and passed to all knowledge sub-operations.

- Type definition: [cmd/cli/basic/knowledge/client.go:1](cmd/cli/basic/knowledge/client.go#L1)
- Credentials come from `OPENSEARCH_USERNAME` / `OPENSEARCH_PASSWORD` env vars, not config

All index naming follows the pattern `rag-snap-context-{suffix}`, defined as constants in [cmd/cli/basic/knowledge/indexes.go:19](cmd/cli/basic/knowledge/indexes.go#L19).

## 5. Pipeline-Based Document Ingestion

`processing.Ingest()` is the single entry point for adding a document. It runs a linear sequence of stages, each returning an error that aborts the pipeline:

```
File/URL → checksum+size validation → Tika extraction → HTML→Markdown
         → metadata extraction (non-fatal) → chunking → OpenSearch bulk index
```

- Orchestration: [cmd/cli/basic/processing/ingest.go](cmd/cli/basic/processing/ingest.go)
- Chunker: [cmd/cli/basic/processing/chunker.go](cmd/cli/basic/processing/chunker.go) (default 1024 chars, 200 overlap)
- Tika client: [cmd/cli/basic/processing/tika.go](cmd/cli/basic/processing/tika.go)
- Bulk indexer: [cmd/cli/basic/knowledge/bulk.go](cmd/cli/basic/knowledge/bulk.go)

Both single-document ingest (`--file`/`--url`) and batch ingest (`--batch`) ultimately call `processing.Ingest()`.

## 6. Batch YAML Pattern

Batch ingestion uses a declarative YAML schema parsed into a struct, then iterated:

```yaml
version: "1.0"
jobs:
  - type: file | url
    source: <path or URL>
    name: <document ID>
    target_kb: <knowledge base name>
```

- Parser + runner: [cmd/cli/basic/knowledge/batch.go](cmd/cli/basic/knowledge/batch.go)
- Errors per job are reported and continue; the batch does not abort on a single failure.

## 7. Hybrid Search (Semantic + Lexical + Re-rank)

Search goes through two OpenSearch pipelines registered during `knowledge init`:

1. **Ingest pipeline** — runs the embedding model at index time, storing a vector alongside the text chunk.
2. **Search pipeline** — runs the re-ranker (cross-encoder) at query time to re-order BM25 + kNN results.

- Pipeline setup: [cmd/cli/basic/knowledge/pipelines.go](cmd/cli/basic/knowledge/pipelines.go)
- Search execution: [cmd/cli/basic/knowledge/search.go](cmd/cli/basic/knowledge/search.go)
- Model registration (with polling for readiness): [cmd/cli/basic/knowledge/models.go](cmd/cli/basic/knowledge/models.go)

The embedding dimension is fixed at 768 ([cmd/cli/basic/knowledge/indexes.go:23](cmd/cli/basic/knowledge/indexes.go#L23)).

## 8. RAG Context Injection into Chat

Chat sessions retrieve relevant chunks before forwarding a user message to the LLM:

- `retrieveContext()` calls `knowledge.Search()` across configured bases → [cmd/cli/basic/chat/rag.go](cmd/cli/basic/chat/rag.go)
- Retrieved text is prepended to the system prompt sent to the OpenAI-compatible API
- If search fails or no bases are configured, the session continues without context (graceful degradation)
- Chat client: [cmd/cli/basic/chat/client.go](cmd/cli/basic/chat/client.go)

## 9. Error Wrapping Convention

All functions wrap errors with `fmt.Errorf("operation description: %w", err)`. This pattern is used consistently across all packages, preserving error chains for `errors.Is`/`errors.As` unwrapping.

## 10. Progress Spinner Closure Pattern

Long-running operations use a closure-based spinner:

```go
stop := common.StartProgressSpinner("message")
defer stop()
```

- Implementation: [cmd/cli/common/spinner.go](cmd/cli/common/spinner.go)

The `stop` function is called via `defer` or explicitly before printing results, ensuring the spinner clears before output is written.
