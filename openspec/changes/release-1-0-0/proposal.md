## Why

rag-cli is about to be released as 1.0.0 on `latest/stable` for internal use by Canonical Field
Engineers answering RFPs, but the snap is still packaged as a development build (`grade: devel`,
a hardcoded `0.0.9` version, no test gate before publishing). A new install also needs about 35
manual commands, roughly ten of which set values the snap already knows (the bundled Tika's
address, the local OpenSearch defaults, service startup, a socket group that exists). This change
makes the snap releasable and removes those steps, as the base for the guided `setup` command
that follows in later changes.

## What Changes

- **Release packaging:** `grade: stable`. The snap version comes from the git tag the build runs
  on (`v1.0.0` → `1.0.0`), with a commit-based development version for untagged edge builds,
  replacing the hardcoded `0.0.9`.
- **Store metadata:** a refreshed summary and description that cover Bedrock/Anthropic, the
  browser UI and RFP answering. **Pending a decision by the repo owner (JP):** publisher account,
  public vs. private listing, and the `license` field. This change leaves placeholders for them
  and does not decide them.
- **CI gate:** the publish workflow runs `go vet ./...` and `go test ./...` and builds the UI, and
  publishes only when they pass. The GitHub release that publishes to `stable` is the one built
  from the `v1.0.0` tag.
- **Toolchain:** the Go build-snap moves from `go/1.24/stable` (end of life) to a supported
  release.
- **Zero-config package defaults** (registered by the shared install/post-refresh script, never
  overwriting an existing value):
  - `tika.http.host=127.0.0.1`, `tika.http.port=9998`, `tika.http.path=tika`: the bundled server.
  - `knowledge.http.host=127.0.0.1`, `knowledge.http.port=9200`, `knowledge.http.tls=true`: a
    local OpenSearch snap.
  - `chat.http.{host,port,path,tls}`, `chat.model`, `knowledge.model.{embedding,rerank}`
    registered with empty values, so a plain user-layer `rag-cli.rag set <key>=<value>` works
    without `--package`.
  - `api.socket.group` default changes from `rag` (a group that normally doesn't exist) to
    `sudo`. **BREAKING** for new installs only: members of `sudo`, rather than of a `rag` group,
    can reach `ragd` without `sudo`. Existing installs keep their stored value.
- **Service startup:** `tika-server` is enabled and started at install (it needs no secrets).
  `ragd` stays disabled at install on purpose: started without secrets, it would serve every CLI
  command and fail them with `401`. A later `setup` change will enable it once secrets are stored.
- **Tika binds to loopback:** the bundled Tika server listens on `127.0.0.1` instead of `0.0.0.0`.
  Since it now starts automatically, it must not be reachable from the network on a laptop.
- **Help text:** `status` no longer describes itself as "the status of the inference snap".
- **Docs:** INSTALL.md drops the steps that are now defaults and uses user-layer `set`. A new
  release checklist (`docs/release.md`) covers tagging, promotion and a clean-VM end-to-end pass
  that also closes the deferred in-snap validation tasks of open changes.

Out of scope: a `ragd` secrets file, `setup --profile` and `doctor`, a team OpenSearch cluster,
Node 20 end of life (elasticdump), and snap size.

## Capabilities

### New Capabilities
- `snap-packaging`: the snap's release grade and version derivation, the package-layer defaults
  registered by the install and post-refresh hooks, service startup on install, the bundled Tika
  listener address, and the Store metadata.
- `release-pipeline`: the CI gate that must pass before publishing, and which events publish to
  `edge` and `stable`.

### Modified Capabilities
None. `rest-api-server` specifies authentication by `api.socket.group` membership but not the
group's default value, so the default changes in `snap-packaging` without a delta to that spec.

## Impact

- **External services:** Tika only. Its listener moves to loopback and it starts at install. The
  OpenSearch and inference clients are unchanged: they now find package defaults instead of
  unset keys, and setting the keys explicitly behaves exactly as before.
- **Config keys:** no new key names. Newly registered at the **package** layer: `tika.http.*`,
  `knowledge.http.*`, `chat.http.*`, `chat.model`, `knowledge.model.*`. `api.socket.group`'s
  package default changes. Existing values are never overwritten, so a refresh keeps every
  operator choice, including `--package` values set by the AWS setup script.
- **User-facing surfaces:** `rag-cli.rag status` help text; `rag-cli.rag set <key>=…` now works
  for backend keys without `--package` (both forms keep working); `tika-server` runs after
  install. Docs to change: `INSTALL.md`, `README.md`, `docs/usage.md` (services and config
  sections), `docs/local-ui.md` and `docs/rest-api.md` (socket group default), the new
  `docs/release.md`.
- **Code and packaging:** `snap/snapcraft.yaml` (grade, `adopt-info`/version, Go build-snap,
  `tika-server` install mode, metadata), `apps/package-config.sh`, `apps/tika-start.sh`,
  `snap/hooks/install`, `internal/api/config.go` (`defaultSocketGroup`),
  `cmd/cli/basic/status.go`, `.github/workflows/snap-publish.yml`, `go.mod` if the `go`
  directive moves.
