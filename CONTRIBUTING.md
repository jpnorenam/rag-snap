# Contributing to rag-cli

Thanks for your interest in contributing! `rag-cli` is a CLI-based RAG tool
packaged as a snap. This document covers how to set up a development
environment, the change workflow we follow (OpenSpec), and our stance on
AI-assisted contributions.

By contributing, you agree that your contributions are licensed under the
project's [GPL-3.0](LICENSE) license.

## AI-assisted contributions are welcome

We actively welcome code written with the help of AI coding agents such as
**[Claude Code](https://claude.com/claude-code)** and **[OpenCode](https://opencode.ai)**.
This repository is set up for them: it ships an OpenSpec configuration
(`openspec/config.yaml`), agent skills under `.claude/`, and an `.opencode/`
configuration so the agent has the project context it needs.

We only ask that you treat AI as an assistant, not an author of record:

- **You are responsible for everything you submit.** Read, understand, and test
  AI-generated code before opening a PR. "The model wrote it" is not a review.
- **Disclose significant AI authorship.** Keep the agent's trailer on commits it
  co-wrote, e.g. `Co-Authored-By: Claude <noreply@anthropic.com>`.
- **Don't paste confidential data into third-party models.** Note that when
  `rag-cli` itself is configured against a third-party inference API (e.g. AWS
  Bedrock), prompts and retrieved context leave your machine — the same caution
  applies to your development workflow.
- The OpenSpec workflow below is the recommended way to drive an agent through a
  non-trivial change.

## Development setup

### Prerequisites

- Go 1.24+
- `snapcraft` and `snapd` (for building/installing the snap)
- `golangci-lint` (for linting; config is in `.golangci.yml`)
- For running end to end: an OpenSearch snap, an inference server (a local
  Inference snap or a third-party OpenAI-compatible API), and the bundled Tika
  service. See the [README](README.md) for full service setup.

### Common commands

```bash
make build              # build the binary to ./bin/cli
make run ARGS="status"  # go run ./cmd/cli with arguments
make test               # go test ./...
make lint               # golangci-lint run ./...
make all                # tidy + fmt + vet + lint + test + build
go test ./pkg/utils/ -run TestName   # run a single test
```

There is **no test/lint gate in CI**, so please run `make all` locally before
pushing.

### Building and validating the snap

```bash
snapcraft -v
sudo snap install --dangerous ./rag-cli_*.snap
```

> **Important:** all configuration is read and written through `snapctl`
> (see `pkg/storage/`). Any code path that touches config only works when
> running **inside the installed snap** — `make run` / `go run` will fail on
> `snapctl get/set` outside a snap context. When your change touches config,
> validate it from an installed snap, not just `make run`.

## The OpenSpec workflow

For anything beyond a trivial fix, we use [OpenSpec](https://github.com/openspec)
to capture *what* and *why* before writing code. This keeps changes reviewable
and gives AI agents the context they need. Project-wide context and per-artifact
rules live in [`openspec/config.yaml`](openspec/config.yaml) — read it before
proposing.

A change moves through these stages. Each is available both as a slash command
in an agent (`/opsx:*`) and via the `openspec` CLI:

1. **Explore** (`/opsx:explore`) — optional. Think through the problem with the
   agent as a sounding board *before* committing to an approach. No code is
   written in this stage.
2. **Propose** (`/opsx:propose`) — create a change and generate its artifacts:
   - `proposal.md` — what & why
   - `design.md` — how
   - `tasks.md` — implementation steps
3. **Apply** (`/opsx:apply`) — implement the tasks from the change.
4. **Sync** (`/opsx:sync`) — sync delta specs back into the main specs.
5. **Archive** (`/opsx:archive`) — finalize and archive the completed change.

Useful CLI commands:

```bash
openspec new change "<name>"          # scaffold a new change (kebab-case name)
openspec list --json                  # list active changes
openspec status --change "<name>" --json   # artifact build order and status
```

When proposing, follow the per-artifact rules in `openspec/config.yaml` — for
example, state which external services (OpenSearch / inference server / Tika) a
change touches, and call out any new config keys and whether they are `package`
or `user` scoped.

## Pull requests

- Branch off `main` and open a PR against `main`. Merging to `main` publishes
  the snap to the `edge` channel; a published GitHub release publishes to
  `stable`.
- Keep PRs focused. Link the OpenSpec change (or describe the proposal) in the
  description.
- Run `make all` and confirm the snap builds.
- Commit messages follow the existing style: a short imperative summary, with a
  `feat:` / `fix:` prefix where it fits (see `git log`).
- Preserve the fixed command order in `cmd/cli/main.go`
  (`cobra.EnableCommandSorting = false`) when adding commands.

## Questions

Open a GitHub issue for bugs, feature ideas, or workflow questions. Thanks for
contributing!
