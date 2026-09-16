## Context

The kapa.ai integration exists in full on the direct-CLI path — `knowledge.KapaClient` with
`Search` and `ListSourceGroups`, a `/use-kapa` slash command, parallel retrieval merged in
`retrieveContext`, and a `kapa_source_groups` manifest field — and is compiled into the released
snap. It cannot fire in normal operation for three separate reasons, and fixing any one alone leaves
it broken.

1. **Transport.** `answer batch` prefers the daemon whenever `ragd` is running. Neither
   `batchManifestJSON` (`cmd/cli/basic/answer_api.go`) nor `batchManifestRequest`
   (`internal/api/handlers_answer.go`) carries `kapa_source_groups`, so the field is dropped twice
   in transit. This is the third occurrence of the same defect class, after `target_kb` and
   `questions[].source`; the comment above `batchManifestJSON` already warns about it.
2. **Client.** `handlers_answer.go` passes a literal `nil` kapa client to `chat.RunBatch`, with a
   comment recording it as unwired. `internal/api` has no kapa configuration at all.
3. **Configuration.** The `kapa.*` keys are seeded only by the one-shot `install` hook.
   `snap/hooks/post-refresh` seeds nothing (its only substantive line is commented out), so any
   installation that arrived at its current revision by `snap refresh` has no kapa keys — and because
   a `user` set rejects a key absent from the `package` layer, the integration cannot even be
   configured. On the maintainer's own machine all three keys report `no value set`.

Constraints that shape the design:

- **Config is snapctl-only.** `pkg/storage/snapctl_storage.go` is the only wired backend, so any code
  path reading kapa config works only inside the snap. Unit tests must therefore exercise
  credential resolution through a pure function that takes values, not through config lookups.
- **Two precedence layers.** `package` (install/post-refresh hook, maintainer) then `user`
  (overrides). A key must exist at the `package` layer before a `user` set is accepted.
- **Secrets never live in config.** `OPENSEARCH_USERNAME`, `OPENSEARCH_PASSWORD`, and `CHAT_API_KEY`
  are environment-only; the daemon receives them from a root-only systemd drop-in because
  `snapcraft.yaml` deliberately declares no `environment:` stanza for them (anything hardcoded there
  is applied by `snap run` *after* systemd and would override the drop-in). `kapa.api.key` violates
  this rule today and is removed by this change.

## Goals / Non-Goals

**Goals:**

- A manifest selecting `kapa_source_groups` grounds against kapa identically whether it runs in
  direct mode or through the daemon.
- One place defines when a kapa client exists, shared by the CLI and the daemon, unit-testable
  without snapctl.
- Source groups are discoverable by name over the API, so no one types opaque group IDs.
- A refreshed installation can configure the integration.
- The API key is environment-only, and any previously stored value stops lingering in snapd state.
- A selected-but-unusable integration is visible rather than silently downgraded.

**Non-Goals:**

- A kapa health card on the status page. Deferred; `rest-api-status` is untouched.
- Any change to how kapa hits are labeled or how prompts prioritise labels — `knowledge-labels`
  owns `kapa-canonical` and stays as is.
- Ingesting kapa content into a local knowledge base. Kapa stays a live retrieval source, never
  indexed.
- Reranking across the two sources. The existing merge order (local first, kapa after) is preserved
  deliberately; changing relevance behaviour is a separate concern.
- Per-question source-group selection. Selection stays run-wide, matching `knowledge_bases`.

## Decisions

### D1. One pure resolution function, called by both paths

Add `knowledge.ResolveKapaClient(enabled bool, projectID, apiKey string) *KapaClient` to
`cmd/cli/basic/knowledge` — beside `KapaClient` itself, taking plain values and touching neither
config nor environment. `buildKapaClient` in `cmd/cli/basic/common.go` keeps reading config and
environment and delegates the rule; the daemon reads its own config and environment and calls the
same function.

*Why:* the enabled/project/key rule is exactly what regressed, and it must not exist twice.
Keeping it value-in/value-out makes it testable under `go test` despite the snapctl-only backend.

*Alternative rejected:* having `internal/api` call `buildKapaClient` directly. That would make the
daemon import `cmd/cli/basic`, which imports `internal/apiclient` — a layering inversion (the daemon
depending on the CLI's API *client*) that invites an import cycle as both grow.

### D2. Daemon resolves kapa once at startup, in `internal/api/config.go`

Kapa configuration joins the other backend settings resolved when the server is constructed:
`kapa.enabled` and `kapa.project.id` from config, `KAPA_API_KEY` and `KAPA_PROJECT_ID` from the
process environment.

*Why:* consistency with every other backend. `config.go` already builds the OpenSearch, inference,
and Tika URLs once at construction, and `CHAT_API_KEY` is likewise read from the daemon's process
environment, which only systemd can change. Resolving kapa per request would make it the only
setting with different reload semantics.

*Consequence, to be documented:* changing kapa config or the drop-in requires
`sudo snap restart rag-cli.ragd`. This matches the existing behaviour for all other config and
secrets and is not a new limitation, but it is not obvious and belongs in the docs.

### D3. `GET /1.0/kapa/source-groups` at the top level, not under `/1.0/knowledge/`

*Why:* `/1.0/knowledge/{name}/...` is a wildcard route, so every literal segment placed beside it
permanently shadows a knowledge base of that name. `POST /1.0/knowledge/gdrive/import` already
shadows a base named `gdrive`; adding `/1.0/knowledge/kapa/...` would extend that latent hazard to
`kapa`. Kapa is also not a local knowledge base — it is never ingested, never indexed, has no
sources of ours — so nesting it under the knowledge namespace misdescribes it.

*Alternative rejected:* following the `rest-api-gdrive-import` precedent of nesting under
`/1.0/knowledge/`. Consistency is real but is outweighed here: gdrive genuinely imports *into*
knowledge, whereas kapa is a peer retrieval source, and the shadowing hazard is avoidable.

Synchronous (`getSync`) rather than an operation: it is a remote read taking well under a second,
and `rest-api-knowledge` already reserves operations for model deploy, ingest, export, and import.

### D4. Carry the field, and add a structural guard against the next drop

`kapa_source_groups` is added to `batchManifestJSON` and `batchManifestRequest`, plus a round-trip
test in the style of `cmd/cli/basic/rfp/manifest_roundtrip_test.go` asserting that a manifest with
every optional field set survives YAML → CLI struct → JSON body → daemon struct → run manifest.

*Why:* three fields have now been dropped in this exact seam. A per-field test only catches the
field someone remembered to test. The round-trip test is written to enumerate the manifest's fields
so that adding a field to `chat.BatchManifest` without threading it through fails the test rather
than shipping silently.

### D5. Reporting an unusable or failing integration

Two distinct paths:

- **Pre-flight (selected but unconfigured)** is known before any question runs, so the handler
  publishes it in the operation's metadata and the direct path prints it to stderr. The run
  continues against local knowledge bases.
- **Per-question retrieval failure** happens inside `retrieveContext`, which today only prints under
  `--verbose`. Add an optional warning sink to `Session` (nil keeps today's verbose-only behaviour);
  the daemon wires it to operation metadata, the CLI to stderr.

*Why:* silence is what made this bug survive a release — a batch that answered without kapa looked
identical to one with no kapa configured. Failures must not abort the turn either: a kapa outage
degrading an RFP run to local-only grounding is far better than failing 200 questions.

### D6. Removing `kapa.api.key`, including from snapd state

The `install` hook stops registering the key, `buildKapaClient` stops reading it, and
`post-refresh` additionally runs `snapctl unset` for it at both layers.

*Why:* leaving support removed but the value resident in snapd state keeps a credential in the one
place the project's own rule says it must never be. Refresh is the only hook that runs on an
already-configured machine, so it is the only place the cleanup can happen.

*Trade-off accepted:* the unset is irreversible from the snap's side. An operator downgrading to a
revision that still reads the config key must re-supply the value. Documented in the migration
notes.

### D7. `post-refresh` becomes a real seeding hook

The kapa keys must be registered on refresh, and `post-refresh` currently seeds nothing. Factor the
`package`-key registration shared by `install` and `post-refresh` into a sourced shell fragment
staged into the snap, so the two hooks cannot drift.

*Why:* copy-pasting `snapctl set` blocks between two hooks is how the keys came to exist in one and
not the other. Registration must be idempotent and must never overwrite an operator's value — the
same "set only if absent" discipline the existing hook needs.

*Note:* this fixes the kapa keys specifically. Whether the other `package` keys have the same
install-only exposure is a broader audit deliberately left out of this change.

### D8. UI: chips, a dedicated API module, four view states

Following `.claude/skills/ui-conventions/SKILL.md`:

- **Selection** uses toggle chips (`p-chip` / `p-chip--positive` + `p-chip__value`), the same
  vocabulary as the KB selector in `ChatScreen.tsx`. No new visual pattern, so the skill needs no
  update.
- **API access** is a new `ui/lib/api/kapa.ts` going through `envelope.ts` (`getSync`), exporting a
  typed interface mirroring the daemon view and normalising a null array to `[]`. No component
  fetches directly.
- **All four states** are implemented: loading (spinner + text), empty (icon, headline, guidance
  naming the CLI equivalent), loaded, error (negative notification + retry). The unconfigured
  condition renders as guidance — credentials required, with the `set --package` and drop-in hints —
  not as an error and not as an empty picker, which is why D9 keeps the two distinguishable.
- **Styles** go in `ui/app/globals.scss` under a `// --- kapa ---` group, colours from `--vf-*`
  tokens only, verified in both themes via the `is-dark` toggle.
- `ApiError.code === 0` renders the standard daemon-unreachable message, not the raw error.

### D9. Unconfigured is a distinct response condition, not an empty list

The endpoint distinguishes "not configured" from "configured, no groups" and from an upstream
failure.

*Why:* the UI must render three different things — a configuration hint, a genuine empty state, and
an error with retry. Collapsing them into an empty array is precisely the silent-degradation
pattern this change exists to remove.

### D10. Daemon chat selection mirrors `/use-knowledge`

Source-group selection on a daemon chat session travels as its own control message, parallel to the
existing active-knowledge-bases control message, and is independent of it.

*Why:* the two selections are orthogonal — one names local indexes, the other a remote project's
groups — and coupling them would make "add kapa" silently clear the user's active bases. Sessions
start with nothing selected so a session never opens by querying an entire kapa project.

### Snap packaging impact

No new plugs, interfaces, or bundled binaries. Both the `rag` app and the `ragd` daemon already hold
`network`, which is what reaching `api.kapa.ai` over HTTPS requires under strict confinement. Changes
are limited to `snap/hooks/install`, `snap/hooks/post-refresh`, and the shared seeding fragment
staged by `snapcraft.yaml`. **No `environment:` stanza is added for `KAPA_API_KEY`** — doing so would
be applied after systemd and would override the operator's drop-in, the trap already documented in
`snapcraft.yaml`.

## Risks / Trade-offs

- **Kapa becomes a per-question network dependency for large RFP batches.** A 200-question manifest
  makes 200 upstream calls; rate limiting or latency could slow or partially degrade a run. →
  Retrieval already runs concurrently with local search, so it adds latency only when slower than
  OpenSearch; per-question failures are reported and never abort the batch, so a mid-run throttle
  degrades that question to local grounding instead of losing the run.
- **Startup-time credential resolution surprises operators.** Editing `kapa.project.id` or the
  drop-in appears to do nothing until restart. → Same semantics as every other backend and secret;
  addressed by documenting the restart in `INSTALL.md` and `docs/local-ui.md` rather than by adding
  a reload path this change does not own.
- **`snapctl unset` of `kapa.api.key` is irreversible.** → Documented in the migration notes; the
  value is a credential the operator holds independently, and a downgrade requires re-supplying it.
- **Removing a config key is breaking for anyone who set it.** → The failure mode is safe and
  legible: with no `KAPA_API_KEY` in the environment, no client is constructed, and D5 makes a
  manifest that selects source groups say so explicitly instead of quietly answering local-only.
- **The `post-refresh` seeding change touches every future refresh.** A hook error there fails the
  refresh. → Keep it to idempotent `snapctl set`/`unset`, mirror the existing `install` structure,
  and verify by refreshing an installation that predates the keys.
- **The UI picker depends on the daemon reaching kapa.** In a restricted network the picker cannot
  populate. → A manifest's existing selection is preserved rather than stripped when listing fails
  (specified), so a locked-down environment can still run manifests authored elsewhere. Whether to
  add manual ID entry is left open below.

## Migration Plan

1. **Transport and client** (D1, D2, D4) — the field, the shared resolver, daemon config. Unblocks
   daemon-backed batches; verifiable with `go test ./...`.
2. **Reporting** (D5) — pre-flight and per-question surfacing.
3. **Endpoint** (D3, D9) — route, handler, `rest-api.yaml`, `docs/rest-api.md`.
4. **Hooks and key removal** (D6, D7) — install/post-refresh seeding fragment, drop
   `kapa.api.key`.
5. **UI** (D8) — API module, chips, view states, manifest round-trip in `ui/lib/manifest.ts`.
6. **Docs** — `docs/usage.md`, `INSTALL.md`, `docs/local-ui.md`.

Steps 1–3 are independently shippable and are what unblocks RFP runs; 5 is the largest and depends
only on 3.

**Verification.** `make all` locally (CI has no test/lint gate), then `snapcraft` and
`snap install --dangerous`, because every config path needs snapctl. End-to-end check: refresh an
installation predating the keys and confirm they appear; set `kapa.project.id`; supply
`KAPA_API_KEY` via the drop-in; run the same manifest in direct mode and through the daemon and
confirm `--verbose` reports non-zero kapa hits on both.

**Rollback.** Reverting the code restores the previous behaviour, with one asymmetry: the
`post-refresh` unset of `kapa.api.key` has already happened, so a rollback to a revision reading that
key needs the value re-supplied. Nothing else is destructive — the seeding is additive and does not
overwrite operator values.

## Open Questions

- Should the UI offer manual source-group ID entry when the listing cannot be retrieved? It rescues
  restricted-network use at the cost of exposing opaque IDs the picker exists to hide.
- Should the daemon cache the source-group listing for a short TTL? Every answer-batch screen load
  currently means an upstream call, and the list changes rarely.
- Should direct-mode `/use-kapa` be advertised even when unconfigured, explaining what to set? It is
  currently hidden entirely when no client exists, which makes the integration undiscoverable.
- Do the other `package` keys share the install-only exposure that hid this bug? Out of scope here,
  but worth a follow-up audit.
