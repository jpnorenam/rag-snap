# Releasing rag-cli

How a version of the `rag-cli` snap gets from `main` to `latest/stable`. This is for maintainers;
users only need [INSTALL.md](../INSTALL.md).

- [How versions and channels work](#how-versions-and-channels-work)
- [Before tagging](#before-tagging)
- [Tag and publish](#tag-and-publish)
- [Clean-VM end-to-end pass](#clean-vm-end-to-end-pass)
- [Rollback](#rollback)
- [Known issues](#known-issues)

## How versions and channels work

- The snap version comes from the nearest `v*` git tag, computed at build time by
  [`snap/local/derive-version.sh`](../snap/local/derive-version.sh):

  | Built from | Version |
  |---|---|
  | The commit tagged `v1.0.0` | `1.0.0` |
  | 3 commits after `v1.0.0` | `1.0.0+git3.abc1234` |
  | A tree with no `v*` tag | `0.0.0+git.abc1234` |
  | Any of these with uncommitted changes | the same, plus `-dirty` |

  Preview what a build would produce with `sh snap/local/derive-version.sh .`.
- [`.github/workflows/snap-publish.yml`](../.github/workflows/snap-publish.yml) runs `go vet`,
  `go test` and the UI build first, and builds nothing if any of them fails. Then:
  - a push to `main` publishes to `latest/edge`;
  - a **published GitHub release** publishes to `latest/stable`;
  - a manual dispatch builds an artifact and publishes nothing.
- `golangci-lint` is not part of the CI gate. Run `make all` locally before tagging.

## Before tagging

- [ ] Store listing decisions are recorded: the publisher account, public or private listing, and
      the `license:` field in `snap/snapcraft.yaml`. For 1.0.0 these are pending a decision by the
      repository owner (JP); the placeholder in `snapcraft.yaml` says so. Don't tag until they're
      settled.
- [ ] `make all` passes locally (no new lint issues in files touched since the last release).
- [ ] `main` builds green in CI, and the `edge` revision it published passed the
      [clean-VM end-to-end pass](#clean-vm-end-to-end-pass).
- [ ] User-facing changes since the last release are reflected in `INSTALL.md` and `docs/`.

## Tag and publish

1. Tag the commit that passed the end-to-end pass, and push the tag:

   ```bash
   git tag -a v1.0.0 -m "rag-cli 1.0.0"
   git push origin v1.0.0
   ```

2. Publish a GitHub release for the tag (`gh release create v1.0.0 --generate-notes`, or through
   the web UI). Publishing it, not just creating a draft, starts the `stable` build.
3. When the workflow finishes, check the Store:

   ```bash
   snap info rag-cli | grep -E 'latest/(stable|edge)'
   ```

   `latest/stable` must show version `1.0.0`. If it shows `0.0.0+git…`, the build didn't see the
   tag (check the checkout's `fetch-depth`). Close `stable` and fix it before anyone refreshes.
4. On the test VM, `sudo snap refresh rag-cli --channel stable` and confirm
   `rag-cli.rag --version` prints `1.0.0`.

## Clean-VM end-to-end pass

Run on a fresh Ubuntu 24.04 VM (for example `multipass launch 24.04 --cpus 4 --memory 16G --disk 40G`)
with the candidate build installed from `edge`, or with `--dangerous` from a local `snapcraft -v`.
Every config path needs `snapctl`, so none of this can be checked with `go run`.

**Fresh install**

- [ ] `snap services rag-cli`: `rag-cli.tika-server` is `enabled active`, `rag-cli.ragd` is
      `disabled inactive`.
- [ ] `rag-cli.rag get tika.http` and `rag-cli.rag get knowledge.http` show the defaults
      (`127.0.0.1:9998/tika`, `127.0.0.1:9200` with TLS).
- [ ] `sudo rag-cli.rag set chat.http.host=example.com` succeeds without `--package` (then revert
      it by setting the package value instead).
- [ ] With the chat backend unset, a chat command fails with `config key "chat.http.host" is not set`.
- [ ] From another machine, port 9998 on the VM is refused.

**Refresh from the previous release**

- [ ] Install the previous revision (for 1.0.0: `edge` revision 42, version `0.0.9`), set
      `knowledge.http.host` with `--package`, a user-layer `chat.model`, and
      `api.socket.group=rag`, then refresh to the candidate. All three values and both service
      states are unchanged.

**End to end, following INSTALL.md**

- [ ] OpenSearch, Claude on Bedrock, `knowledge init`, `k create`, `k ingest` of a PDF (through
      Tika), `chat`.
- [ ] Secrets drop-in, `sudo snap start --enable rag-cli.ragd`, `rag-cli.rag ui`; as a non-root
      member of `sudo`, the CLI reaches the daemon without `sudo`.
- [ ] `answer batch` on a small manifest, through the daemon and with `ragd` stopped.

**Deferred in-snap validation from open changes**

The same pass covers checks that open OpenSpec changes left for an installed snap. Tick them in
their own `tasks.md` as you go; a failure becomes a [known issue](#known-issues) rather than a
release blocker.

| Change | Tasks |
|---|---|
| `fix-engine-init-model-reporting` | 4.3, 4.4 |
| `add-engine-model-pruning` | 4.2 – 4.5 |
| `add-knowledge-labels` | 6.1 – 6.3 |
| `add-kapa-retrieval-wiring` | 9.3 – 9.6 |
| `add-kapa-source-group-selector` | 9.2 – 9.6 |
| `add-ui-answer-batch` | 11.4 |
| `add-answer-build-column-selection` | 10.5 |
| `ui-repo-ingestion` | 5.4 |

## Rollback

- **One machine:** `sudo snap revert rag-cli` returns to the previous revision and its data.
- **Everyone on `stable`:** close the channel so nobody else refreshes into the bad revision, then
  release a fixed build. Closing `stable` makes it follow `candidate`, `beta` and `edge` in turn, so
  release a known-good revision to `stable` instead if one exists:

  ```bash
  snapcraft release rag-cli <good-revision> stable
  # or, with no good revision to fall back to:
  snapcraft close rag-cli stable
  ```

Config written by a newer revision stays after a rollback. That's harmless: older revisions read
the same key names.

## Known issues

Record here what the end-to-end pass found that didn't block the release.

- _None recorded yet for 1.0.0._
