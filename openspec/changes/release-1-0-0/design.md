## Context

rag-cli is published from `main` to `latest/edge` as `0.0.9` (`grade: devel`), with no `stable`
release and no git tags in the repository. Its 1.0.0 audience is Canonical Field Engineers, each
running the snap on their own Ubuntu laptop, mostly against Claude on Bedrock and a local
OpenSearch snap. A central OpenSearch on AWS is being built on a separate branch.

Relevant current state:

- **Config** is snapctl-only, in two layers: `package` (seeded by `apps/package-config.sh`,
  sourced by the install and post-refresh hooks) and `user`. A user-layer `set` is rejected
  unless the key already exists in a layer. Backend keys (`chat.http.*`, `knowledge.http.*`,
  `tika.http.*`, `knowledge.model.*`) are not registered, so INSTALL.md and the AWS setup script
  use `sudo rag-cli.rag set --package`.
- **Empty values are already "unset"** in both consumers: `getConfigString` in
  `cmd/cli/basic/common.go` and `requireString` in `internal/api/config.go` reject `""` with
  "config key … is not set", the bool helpers fall back on `""`, and model-ID lookups treat `""` as
  not configured. `config.Get` reads the whole `config` JSON object, so a key holding `""` counts
  as existing for the user-layer check. Registering empty keys therefore changes which `set`
  forms work, but not runtime behaviour.
- **Services:** `tika-server` and `ragd` both declare `install-mode: disable`. `tika-start.sh`
  binds Tika to `0.0.0.0:9998`, while every consumer reaches it through `tika.http.host`, which
  INSTALL.md always sets to `127.0.0.1`.
- **Version:** the hardcoded `version: "0.0.9"` reaches the UI through `SNAP_VERSION`
  (`internal/buildinfo.Version`). The commit is already derived from git in the `go-cli` part's
  `override-build`, through `git -C "${CRAFT_PROJECT_DIR}"`.
- **CI:** `.github/workflows/snap-publish.yml` builds with `snapcore/action-build@v1`, publishes
  to `edge` on a push to `main` and to `stable` on a published release, and runs no tests.
  `go vet ./...` and `go test ./...` pass on the current tree.
- **Toolchain:** `go/1.24/stable` build-snap (out of upstream support), `go 1.24.0` in `go.mod`.
  Supported Go releases are 1.26 and 1.27.

## Goals / Non-Goals

**Goals:**
- The snap is stable-grade, with a version that comes from the release tag.
- Nothing is published unless vet, tests and the UI build pass.
- A fresh install needs no configuration for Tika and none for a local OpenSearch, and backend
  keys can be set in the user layer.
- Refreshing an existing install changes none of its stored values or service states.

**Non-Goals:**
- Storing secrets for `ragd`, or a guided `setup`/`--profile`/`doctor`: later changes. This
  change only stops blocking them.
- Deciding the Store publisher, listing visibility or license (pending JP, the repo owner).
- Node 20 end of life in the elasticdump part, snap size, or multi-architecture builds.
- Team OpenSearch cluster defaults.

## Decisions

### 1. Version from the git tag, through `adopt-info`

Replace `version:` with `adopt-info: go-cli`, and have the `go-cli` part's `override-build` call
`craftctl set version=…`, using the same `project_git` helper that already derives the commit:

| Build | `git describe` result | Snap version |
|---|---|---|
| On tag `v1.0.0` | exact match | `1.0.0` |
| 3 commits after `v1.0.0` | `v1.0.0-3-gabc1234` | `1.0.0+git3.abc1234` |
| No `v*` tag reachable | none | `0.0.0+git.abc1234` |
| Any of these with uncommitted changes | | suffix `-dirty`, reusing the existing check |

Only tags matching `v[0-9]*` count. The result must fit snapd's version rules (at most 32
characters, `[A-Za-z0-9.+~-]`, alphanumeric first and last), and the build fails if it does not,
rather than publishing an odd version. CI's checkout needs `fetch-depth: 0` (or an explicit tag
fetch) so `git describe` can see tags.

*Alternatives:* bumping `version:` by hand for each release (forgettable, and the reason edge
still says `0.0.9`); setting the version from a CI env var (local `snapcraft` builds would then
differ from CI builds).

### 2. Register backend keys at the package layer, defaults only where the value is fixed

`apps/package-config.sh` gains:

```
tika.http.host=127.0.0.1   tika.http.port=9998   tika.http.path=tika        # bundled, fixed
knowledge.http.host=127.0.0.1  knowledge.http.port=9200  knowledge.http.tls=true  # local OpenSearch snap
chat.http.host=""  chat.http.port=""  chat.http.path=""  chat.http.tls=""  chat.model=""
knowledge.model.embedding=""  knowledge.model.rerank=""
```

`register_package_key` already writes only when the stored value is empty, so it never
overwrites an operator's value, and post-refresh registration covers existing installs. The
chat keys get no defaults: the team's Bedrock endpoint and model belong to the upcoming
`--profile`, and an empty `chat.*` keeps today's clear "config key … is not set" error.

*Alternatives:* seeding the Bedrock/Claude endpoint here (it would bake the team's choices into a
public snap, and age with every model change); registering knowledge keys empty (a local
OpenSearch is the default for 1.0.0 users, and anyone else sets the host).

### 3. Default `api.socket.group` to `sudo`

Both the package default and `defaultSocketGroup` in `internal/api/config.go` become `sudo`. On
Ubuntu the installing user is in `sudo` on desktop and cloud images alike, and members of `sudo`
can already act as root, so the effective set of users does not grow. INSTALL.md already tells
every user to make this change by hand.

Existing installs are **not migrated**: registration never overwrites, so an install that stored
`rag` keeps it. A post-refresh migration would have to guess whether `rag` was a deliberate
choice, and the hook cannot create host groups to make `rag` real.

*Alternatives:* `adm` (not on every image and semantically about logs); the installing user's
primary group (the install hook runs as root and has no invoking user); keeping `rag` (it
normally doesn't exist, so only root can use the socket).

### 4. Start `tika-server` at install and bind it to loopback; keep `ragd` disabled

Drop `install-mode: disable` from `tika-server` so snapd enables and starts it on install. It
needs no secrets or config. On refresh, snapd keeps each service's existing enabled/disabled
state, so an install where it was never enabled stays that way (documented, not migrated).

Because Tika now runs by default on laptops, `tika-start.sh` binds `127.0.0.1` instead of
`0.0.0.0`. No supported configuration reaches Tika other than through `tika.http.host`, whose
default is `127.0.0.1`.

`ragd` keeps `install-mode: disable`. When running, it serves every CLI command, and right after
install it has no secrets, so the user's first command would fail with `401` even with a correct
`credentials.json`. The later `setup` change enables it after storing the secrets.

*Alternatives:* starting both at install (the first-command failure above); keeping Tika
disabled (one more step for every user, with no safety benefit).

### 5. CI gate in the same workflow

Add a `checks` job (Go from `go.mod`'s toolchain, Node for the UI): `go vet ./...`,
`go test ./...`, and `npm ci && npm run build` in `ui/`. Make `build-and-publish` depend on it
(`needs: checks`). `golangci-lint` stays out of the gate for 1.0.0: the pinned 2.12.2 panics on
Go 1.27 and the repository has a pre-existing lint baseline. `-race` stays out too, because of a
known race in the loopback test helper. Both are listed as post-1.0 follow-ups.

### 6. Go 1.26 build-snap

Use `go/1.26/stable`: supported, and not the newest release, which keeps current tools working
(the local lint issue is specific to 1.27). `go.mod`'s `go 1.24.0` directive is a minimum
language version, not the compiler, so it stays unless `go mod tidy` under 1.26 requires
otherwise. The security fixes come from the toolchain.

### 7. Store metadata in `snapcraft.yaml`, with placeholders for owner decisions

Rewrite `summary` and `description` (RAG over local knowledge bases, Bedrock/Anthropic and local
inference, browser UI, RFP answer batches), and add `title`, `website`, `source-code` and
`issues`, pointing at the current repository. `license`, the publisher and listing visibility are
left as commented placeholders that name JP's pending decision. The release checklist has a gate
item for them.

## Risks / Trade-offs

- [Tika starts at install and uses up to a 2 GiB heap on every laptop, used or not] → 1.0.0
  users ingest documents routinely. `sudo snap stop --disable rag-cli.tika-server` is documented.
- [A setup that reached Tika over the LAN breaks with the loopback bind] → No documented setup
  does this. Release notes call it out.
- [Changing `api.socket.group` to `sudo` lets `sudo` members use `ragd` without a password
  prompt] → They can already become root; the daemon exposes only its API. Documented in the
  local UI trust model.
- [A shallow CI checkout hides the tags, so a release builds as `0.0.0+git…`] → `fetch-depth: 0`,
  plus a checklist step that checks the uploaded revision's version before promoting it.
- [Edge builds change from `0.0.9` to `0.0.0+git…` until `v1.0.0` is tagged] → Accepted. Tag
  right after merge.
- [Knowledge defaults make `status` report "connection refused" on a laptop without OpenSearch,
  rather than "not set"] → That's more accurate. INSTALL.md's OpenSearch step comes before
  `status`.

## Migration Plan

1. Merge, then tag `v1.0.0` on the merge commit and publish a GitHub release. CI builds the
   tagged revision and publishes it to `stable`. The push to `main` has already published a
   `0.0.0+git…` build to `edge`.
2. Follow `docs/release.md`: confirm the revision's version, then run the clean-VM end-to-end
   pass (fresh install from `stable`, and a refresh from the current `edge` revision 42).
3. Refreshing existing installs keeps every stored value and service state. New keys appear with
   their defaults.

Rollback: `snap refresh --revision=<previous>` per machine, or close `stable` in the Store.
Package defaults written by a newer revision remain after a rollback; they're inert for older
code, which reads the same key names.

## Open Questions

- Publisher account, public vs. private listing, and `license` (JP).
- Should `edge` keep publishing every push to `main` after 1.0.0, or move to `candidate` with a
  promotion step? This design keeps the current behaviour.
