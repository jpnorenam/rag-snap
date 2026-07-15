# RAG Snap

A CLI-based RAG (Retrieval-Augmented Generation) tool, packaged as a strictly-confined snap
(`rag-cli`), that lets you build local knowledge bases and chat with them. It's a thin
orchestrator over three external services:

- **OpenSearch** (the `knowledge` store) — embeddings, ingest/search pipelines, indexes, ML models.
- **An inference server** (the `chat` backend) — either a local
  [Inference snap](https://github.com/canonical/inference-snaps) or a third-party
  OpenAI-compatible API, e.g. [AWS Bedrock](docs/bedrock_guide.md).
- **Apache Tika** (the `tika` service) — text extraction from documents. Bundled with the snap.

You interact with it either through the CLI (`rag-cli.rag`) or a local browser UI served by the
`ragd` daemon.

## Typical flow

`knowledge init` (sets up pipelines/models) → `k create <name>` → `k ingest <name> <source-id>
--file|--url` → `chat` (activate knowledge bases with `/use-knowledge`, ask questions). `k
export`/`k import` move knowledge bases between machines without re-embedding.

## Getting started

See **[INSTALL.md](INSTALL.md)** for the full install and configuration walkthrough — prerequisites,
installing the snap, configuring backends, secrets, and enabling the browser UI.

## Documentation

- [INSTALL.md](INSTALL.md) — install and configuration walkthrough
- [docs/usage.md](docs/usage.md) — full CLI reference
- [docs/local-ui.md](docs/local-ui.md) — browser UI reference
- [docs/rest-api.md](docs/rest-api.md) — REST API (`ragd`) reference
- [docs/bedrock_guide.md](docs/bedrock_guide.md) — AWS Bedrock API key walkthrough
