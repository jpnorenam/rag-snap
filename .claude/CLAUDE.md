# rag-snap

## Purpose

CLI tool for local, privacy-first Retrieval-Augmented Generation. Users manage knowledge bases backed by OpenSearch (vector + lexical search), ingest documents (PDF, HTML, plain text), and chat with an OpenAI-compatible LLM that is automatically grounded in retrieved context.

Distributed as a **Ubuntu snap package** (`rag-cli`). External runtime dependencies: OpenSearch snap, an OpenAI-compatible inference endpoint (local or remote), and Apache Tika (bundled in snap).

## Tech Stack

- **Go 1.24** — CGO disabled, trimpath builds ([Makefile:8](Makefile#L8))
- **Cobra** — CLI framework ([cmd/cli/main.go](cmd/cli/main.go))
- **opensearch-go/v4** — vector + lexical knowledge store
- **openai-go/v3** — OpenAI-compatible chat API client
- **go-snapctl** — snap configuration backend
- **gopkg.in/yaml.v3** — batch ingestion config
- **Tika v3** — document content extraction (bundled, started as snap service)

Full dependency list: [go.mod](go.mod)

## Key Directories

| Path | Purpose |
|------|---------|
| [cmd/cli/main.go](cmd/cli/main.go) | Root Cobra command, global flags, command registration |
| [cmd/cli/basic/](cmd/cli/basic/) | Core commands: `knowledge`, `chat`, `status` |
| [cmd/cli/basic/knowledge/](cmd/cli/basic/knowledge/) | OpenSearch client, index/pipeline/model management, search, bulk indexing, batch YAML |
| [cmd/cli/basic/knowledge/gdrive.go](cmd/cli/basic/knowledge/gdrive.go) | Google Drive API client: URL parsing, folder listing, archive download |
| [cmd/cli/basic/knowledge/gdrive_auth.go](cmd/cli/basic/knowledge/gdrive_auth.go) | OAuth2 loopback + PKCE flow, token cache (`~/.config/rag-cli/gdrive-token.json`), silent refresh |
| [cmd/cli/basic/chat/](cmd/cli/basic/chat/) | Chat REPL, RAG context retrieval, OpenAI client |
| [cmd/cli/basic/processing/](cmd/cli/basic/processing/) | Document ingestion pipeline: download, Tika, chunking |
| [cmd/cli/common/](cmd/cli/common/) | Shared types (`Context`), spinner, prompts, errors |
| [cmd/cli/config/](cmd/cli/config/) | `get`/`set` config subcommands |
| [pkg/storage/](pkg/storage/) | Config storage interface + Snapctl and file-based backends |
| [pkg/snap_store/](pkg/snap_store/) | Snap store API utilities |
| [snap/](snap/) | Snap packaging: `snapcraft.yaml`, lifecycle hooks |
| [docs/](docs/) | End-user usage guide and TODO list |
| [.github/workflows/snap-publish.yml](.github/workflows/snap-publish.yml) | CI: builds snap and publishes to store |

## Build & Test Commands

```bash
make build          # compile → bin/cli
make run            # go run ./cmd/cli (dev)
make test           # go test ./...
make test-verbose   # go test -v ./...
make test-coverage  # go test -cover ./...
make vet            # go vet ./...
make tidy           # go mod tidy
make all            # tidy → fmt → vet → test → build
```

Snap build: `snapcraft` (uses Go plugin; downloads and GPG-verifies Tika JAR)

## CLI Command Tree

```
rag-cli.rag                 (snap); bin/cli (dev build)
├── status                  health check for all services
├── chat [model]            interactive RAG chat (alias: c)
├── knowledge               knowledge base management (alias: k)
│   ├── init                register ML models, create OpenSearch pipelines
│   ├── list [--sources]    list knowledge bases or ingested sources
│   ├── create <name>       create a new knowledge base
│   ├── ingest              ingest a single file (--file), URL (--url), or batch (--batch)
│   ├── search              semantic+lexical search (--bases, --top-k)
│   ├── metadata            show source document metadata
│   ├── forget              remove a source from the index
│   ├── delete              delete an entire knowledge base
│   ├── export <name>       back up a knowledge base to a directory or .tar.gz archive
│   └── import [name]       restore from a local export (--input) or Google Drive (--url)
├── get <key>               read a config value
└── set <key> <value>       write a config value
```

## Runtime Configuration

Config keys use dot-path namespacing. Two tiers exist: package defaults (set by snap hooks) and user overrides (`rag set`).

| Key prefix | Service |
|-----------|---------|
| `chat.http.*` | Inference API (default port 8324) |
| `knowledge.http.*` | OpenSearch (default port 9200, TLS on) |
| `tika.http.*` | Tika (default port 9998) |
| `knowledge.model.*` | Embedding and re-rank model IDs (set by `knowledge init`) |
| `gdrive.client.id` | Google Drive OAuth2 client ID (set by user after install) |
| `gdrive.client.secret` | Google Drive OAuth2 client secret (set by user after install) |

Environment variables: `OPENSEARCH_USERNAME`, `OPENSEARCH_PASSWORD`, `CHAT_API_KEY`.

For Google Drive import, credentials are configured at runtime (no rebuild needed):
- Snap: `sudo rag set gdrive.client.id=<id>` and `sudo rag set gdrive.client.secret=<secret>`
- Dev: set `GOOGLE_DRIVE_CLIENT_ID` / `GOOGLE_DRIVE_CLIENT_SECRET` env vars (takes priority over snap config)

## Additional Documentation

Check these files when working on the relevant areas:

| Topic | File |
|-------|------|
| Architectural patterns, design decisions, conventions | [.claude/docs/architectural_patterns.md](.claude/docs/architectural_patterns.md) |
| End-user usage examples | [docs/usage.md](docs/usage.md) |
| Open tasks / known issues | [docs/TODO.md](docs/TODO.md) |
