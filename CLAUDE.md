# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A CLI-based RAG (Retrieval-Augmented Generation) tool, packaged as a strictly-confined snap named `rag-cli` (binary `rag-cli.rag`). It lets users build local knowledge bases and chat with them. It is a thin orchestrator over three external services:

- **OpenSearch** (the `knowledge` store) — embeddings, ingest/search pipelines, indexes, ML models. Accessed via `opensearch-go/v4`.
- **An inference server** (the `chat` backend) — either a local Inference snap or a third-party OpenAI-compatible API (e.g. AWS Bedrock). Accessed via `openai-go/v3`.
- **Apache Tika** (the `tika` service) — text extraction from documents. Bundled and run as the `tika-server` snap daemon.

The Go module path is `github.com/jpnorenam/rag-snap` (note: differs from the repo/snap name `rag-cli`).

## Common commands

```bash
make build          # build binary to ./bin/cli
make run ARGS="..." # go run ./cmd/cli with args
make test           # go test ./...
make lint           # golangci-lint run ./...  (config in .golangci.yml)
make all            # tidy fmt vet lint test build
go test ./pkg/utils/ -run TestName   # run a single test

snapcraft -v        # build the .snap
sudo snap install --dangerous ./rag-cli_*.snap
```

There are only a couple of `_test.go` files (`pkg/snap_store`, `pkg/utils`); most of the codebase is untested.

## Runtime requirement: snapctl-backed config

All configuration is stored and read through `snapctl` (`pkg/storage/snapctl_storage.go` is the only `storage` backend wired up in `storage.NewConfig`). This means **config-touching commands only work when running inside the snap**; `make run` / `go run` outside a snap context will fail on any `snapctl get/set`. Build and install the snap to exercise those paths end-to-end.

Config has two layers with precedence (lowest → highest): `package` (set by install hook / maintainer via `set --package`) then `user` (overrides). User `set` rejects unknown keys — a key must already exist as a package key. See `pkg/storage/config.go`. Keys are dot-namespaced and flattened, e.g. `chat.http.host`, `knowledge.http.tls`, `knowledge.model.embedding`, `tika.http.port`, `gdrive.client.id`.

Secrets are passed via **environment variables**, not config: `OPENSEARCH_USERNAME`, `OPENSEARCH_PASSWORD`, `CHAT_API_KEY`.

## Architecture / code layout

Entry point `cmd/cli/main.go` builds a Cobra command tree. A single `common.Context{Verbose, Debug, Config}` is threaded into every command constructor. Commands are grouped:

- **`cmd/cli/basic/`** — user-facing commands: `status`, `chat`, `answer`, `knowledge` (alias `k`), `prompt`.
  - `basic/chat/` — interactive REPL (readline + huh), the RAG retrieval/rerank loop (`rag.go`), prompt templates (`prompts.go`), and in-session slash commands like `/use-knowledge` (`commands.go`). A `Session` struct holds mutable chat state (clients, active indexes, model IDs).
  - `basic/knowledge/` — the `OpenSearchClient` wrapper and all knowledge-base operations: pipelines, indexes, models, ingest/bulk, search, export/import (uses bundled `elasticdump`), and Google Drive import (`gdrive*.go`, OAuth2).
  - `basic/processing/` — document ingestion pipeline: download, Tika extraction (`tika.go`), HTML conversion (trafilatura), chunking (`chunker.go`), GitHub/Gitea source fetchers.
  - `basic/rfp/` + `answer.go` — structured batch Q&A ("answer batch") driven by a YAML manifest, exporting JSON results.
- **`cmd/cli/config/`** — `get` / `set` commands over the storage layer.
- **`cmd/cli/common/`** — shared `Context`, spinner, prompts, error/suggestion helpers.
- **`cmd/cli/others/`** — hidden `run` (subprocess launcher used by the snap) and `debug` commands.
- **`pkg/storage/`** — config abstraction (interface + snapctl backend + flattening/precedence).
- **`pkg/snap_store/`, `pkg/utils/`, `pkg/constants/`** — supporting utilities (snap store metadata, PCI/arch detection, etc.).

`snap/` holds `snapcraft.yaml` (parts build the Go CLI, fetch Tika 3.1.0 with GPG verification, vendor a Node tarball for elasticdump) and lifecycle `hooks/` (`install` seeds package config and OpenSearch env defaults). `apps/` holds shell entrypoints staged into the snap (`tika-start.sh`, `chat.sh`, `completion.bash`).

## Typical user flow (for context when reasoning about commands)

`knowledge init` (sets up pipelines/models, prints model IDs to configure) → `k create <name>` → `k ingest <name> <source-id> --file|--url` → `chat` (activate bases with `/use-knowledge`, ask questions). `k export`/`k import` move bases between machines without re-embedding.

## Conventions

- CI (`.github/workflows/snap-publish.yml`): push to `main` publishes the snap to the `edge` channel; a published GitHub release publishes to `stable`. There is no test/lint gate in CI — run `make all` locally before pushing.
- Commands are added to the root in a fixed order with `cobra.EnableCommandSorting = false`; preserve grouping/order when adding commands.
- `CLAUDE.md` and `.claude/` (skills, commands) are tracked and shared across the team; `PLAN.md` and `*.snap` build artifacts are gitignored and stay local-only.
