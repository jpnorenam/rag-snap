## Why

The kapa.ai integration is fully implemented on the direct-CLI path — a retrieval client, a
`/use-kapa` slash command, parallel search merged into the RAG context, and a
`kapa_source_groups` manifest field — but it is unreachable in practice. `answer batch` prefers
the `ragd` daemon whenever it is running, and the daemon drops kapa in three independent places:
the CLI→daemon request body has no `kapa_source_groups` field, the daemon's manifest type has no
such field either, and `handlers_answer.go` passes a hardcoded `nil` kapa client with the comment
"kapa.ai retrieval is not yet wired into the daemon backend/client config". The browser UI has no
kapa awareness at all, so any manifest edited there silently loses the field.

Compounding this, the `kapa.*` config keys are seeded only by the one-shot `install` hook, so every
installation that reached its current revision by `snap refresh` has no kapa keys at all and
`buildKapaClient` returns `nil` regardless of path. The net effect is a feature that appears
shipped, is compiled into the released snap, and cannot fire — RFP batches answer without their
kapa.ai grounding and give no indication that a configured source is missing.

## What Changes

- **Carry `kapa_source_groups` end to end.** Add the field to the CLI→daemon JSON body and to the
  daemon's posted-manifest type, so a manifest run through the daemon is the same manifest the
  direct path runs. This is the same class of defect already fixed for `target_kb` and `source`.
- **Build a real kapa client daemon-side.** Resolve kapa configuration and credentials inside
  `ragd` and pass a live client to `chat.RunBatch`, replacing the hardcoded `nil`. Daemon-backed
  interactive chat sessions gain the same capability, so `/use-kapa` works against the daemon.
- **Expose source groups over REST.** A new read endpoint lists the project's kapa source groups
  so a client can offer a picker instead of requiring hand-written group IDs. Today
  `KapaClient.ListSourceGroups` is reachable only from the in-process CLI.
- **Add a source-group picker to the browser UI** on the answer-batch screen, and round-trip
  `kapa_source_groups` through the UI's YAML reader and writer so editing a manifest in the
  browser preserves it.
- **Seed `kapa.*` keys in the `post-refresh` hook** as well as `install`, so refreshed
  installations stop landing silently unconfigured. The hook currently seeds nothing.
- **BREAKING: remove the `kapa.api.key` config key.** The API key becomes environment-only
  (`KAPA_API_KEY`), matching the project rule that secrets travel by environment variable and never
  through config, and matching how `CHAT_API_KEY` and the OpenSearch credentials are already
  handled. `buildKapaClient` stops reading the config key, and the `install` hook stops registering
  it. Operators who set `kapa.api.key` must move the value to the environment; the daemon reads it
  from a root-only systemd drop-in, the CLI from the shell.
- **Surface a configured-but-unusable state.** When source groups are selected and credentials are
  missing, the run reports it rather than silently answering without kapa grounding.

### Out of scope

Adding kapa.ai to the `status` page's per-service health cards, and any change to how kapa hits are
labeled or prioritised in prompts — labeling is already specified under `knowledge-labels` and stays
as is.

## Capabilities

### New Capabilities

- `kapa-retrieval`: the kapa.ai integration as a behavioral contract — how credentials and project
  identity resolve, how source groups are discovered and selected, how kapa hits are retrieved in
  parallel with local OpenSearch hits and merged, and the requirement that the direct-CLI and
  daemon-backed paths behave identically. Labeling of kapa hits stays owned by `knowledge-labels`.
- `rest-api-kapa`: the REST surface for kapa source-group discovery, following the precedent of
  `rest-api-gdrive-import` giving an external-service integration its own capability.

### Modified Capabilities

- `rest-api-answer`: a posted batch manifest SHALL carry `kapa_source_groups`, and the daemon SHALL
  run batches with kapa retrieval active when the manifest selects groups and credentials are
  present. Currently the daemon is specified and implemented to run without it.
- `rest-api-chat`: a daemon-backed chat session SHALL accept and apply kapa source-group selection,
  so in-session `/use-kapa` behaves the same as in direct mode.
- `local-ui-app`: the answer-batch screen SHALL offer a kapa source-group picker, and manifests read
  or written by the UI SHALL preserve `kapa_source_groups` — extending the existing requirements
  covering manifest reading, routing carriage, and UI-written manifests.

## Impact

**External services.** Adds kapa.ai (`api.kapa.ai`) as a fourth external service the snap talks to,
alongside OpenSearch, the inference server, and Tika. It is reached over the existing `network` plug
from both the `rag` CLI app and the `ragd` daemon. No change to how OpenSearch, the inference
server, or Tika are used; kapa results are merged alongside OpenSearch hits in the existing
retrieval path.

**Config keys.** `kapa.enabled` and `kapa.project.id` remain `package`-scoped (user-overridable, as
the precedence layers allow); both are additionally seeded in `post-refresh`. `kapa.api.key` is
removed — **BREAKING**. Environment variables: `KAPA_API_KEY` becomes the only source for the
secret; `KAPA_PROJECT_ID` continues to override the config key.

**User-facing surfaces.** `/use-kapa` in the chat REPL starts working against the daemon; the
`kapa_source_groups` manifest field becomes usable through every path; the browser UI gains a
source-group picker on the answer-batch screen; a new REST endpoint appears. No new Cobra command,
so the fixed root command order is unaffected.

**Documentation that must change.** `docs/usage.md` (the `kapa_source_groups` manifest field and
`/use-kapa`), `INSTALL.md` and `docs/local-ui.md` (the secrets recipe, adding `KAPA_API_KEY` to the
systemd drop-in and noting the removed config key), `docs/rest-api.md` and `rest-api.yaml` (the new
endpoint and the extended batch manifest body).

**Code.** `cmd/cli/basic/common.go` (`buildKapaClient`), `cmd/cli/basic/answer_api.go`,
`internal/api/handlers_answer.go`, `internal/api/config.go`, the daemon chat handler and
`internal/api/server.go` (route registration), `snap/hooks/install`, `snap/hooks/post-refresh`,
`ui/lib/manifest.ts`, `ui/lib/api/`, and the answer-batch UI components. Test coverage follows the
existing round-trip pattern in `cmd/cli/basic/rfp/manifest_roundtrip_test.go` and
`cmd/cli/basic/answer_api_test.go`.

**Dependency note.** The `add-knowledge-labels` change is implemented in the tree (`LabelKapa`
exists) but its `knowledge-labels` spec is not yet synced into `openspec/specs/`. This change
depends on that labeling contract and does not modify it.
