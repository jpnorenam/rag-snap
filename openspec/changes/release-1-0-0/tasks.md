## 1. Version and grade

- [x] 1.1 In `snap/snapcraft.yaml`, replace `version: &snap-version "0.0.9"` with `adopt-info: go-cli` and set `grade: stable`
- [x] 1.2 In the `go-cli` part's `override-build`, derive the version from `git describe --tags --match 'v[0-9]*'` through the existing `project_git` helper (exact tag → `X.Y.Z`; later commit → `X.Y.Z+git<n>.<sha>`; no tag → `0.0.0+git.<sha>`; reuse the `-dirty` check), validate it against snapd's rules (≤ 32 chars, `[A-Za-z0-9.+~-]`, alphanumeric at both ends), fail the build on an invalid result, and call `craftctl set version=…`
- [x] 1.3 Check the derivation locally on a scratch clone: no tag, a lightweight `v1.0.0` tag on HEAD, one commit past the tag, and a dirty tree; record the four versions in the PR description

## 2. Toolchain

- [x] 2.1 Change the `go-cli` build-snap from `go/1.24/stable` to `go/1.26/stable`
- [x] 2.2 Run `go mod tidy`, `go vet ./...` and `go test ./...` under Go 1.26; keep `go 1.24.0` in `go.mod` unless tidy requires a change, and note the outcome in the PR

## 3. Package defaults and services

- [x] 3.1 In `apps/package-config.sh`, register `tika.http.{host,port,path}` (`127.0.0.1`, `9998`, `tika`), `knowledge.http.{host,port,tls}` (`127.0.0.1`, `9200`, `true`), and `chat.http.{host,port,path,tls}`, `chat.model`, `knowledge.model.{embedding,rerank}` as empty, with comments in the file's existing style
- [x] 3.2 Change the `api.socket.group` default to `sudo` in `apps/package-config.sh` and `defaultSocketGroup` in `internal/api/config.go`; update any test that asserts `rag`
- [x] 3.3 Remove the commented-out `set --package` block and the stale `snap start --enable` comments from `snap/hooks/install`, replacing them with a pointer to `package-config.sh`; update the `post-refresh` comment about `tika-server`
- [x] 3.4 Remove `install-mode: disable` from the `tika-server` app (keep it on `ragd`) and update the `ragd` comment to say it will be enabled by `setup` once secrets are stored
- [x] 3.5 Change `tika-start.sh` to bind `-h 127.0.0.1`

## 4. Help text and Store metadata

- [x] 4.1 Change `status`'s `Short`/`Long` in `cmd/cli/basic/status.go` to describe rag-cli and its backends
- [x] 4.2 Rewrite `summary` and `description` and add `title`, `website`, `source-code` and `issues` in `snap/snapcraft.yaml`; add a commented `license:` placeholder that names the pending decision by JP (publisher, visibility, license)

## 5. CI gate

- [x] 5.1 Add a `checks` job to `.github/workflows/snap-publish.yml`: `actions/setup-go` (Go 1.26), `go vet ./...`, `go test ./...`, then `actions/setup-node` and `npm ci && npm run build` in `ui/`
- [x] 5.2 Make `build-and-publish` `needs: checks`, and set `fetch-depth: 0` on its checkout so tags reach the snap build
- [ ] 5.3 Run the workflow by manual dispatch on the branch: confirm checks run, the snap builds with a `0.0.0+git…` or `1.0.0+git…` version, and nothing is published

## 6. Documentation

- [x] 6.1 `INSTALL.md`: drop the Tika `set` and `snap start` steps, the knowledge-host step for local OpenSearch (keep it as the remote-cluster override), and the `api.socket.group` step; keep `set --package` for the backend configuration (a later `setup --profile` writes team defaults to the package layer, and user-layer values would silently shadow them), and document plain `set` as the way to make a personal override
- [x] 6.2 `INSTALL.md` and `docs/usage.md` (services section): Tika starts at install and listens on loopback only; `ragd` stays opt-in; how to disable Tika
- [x] 6.3 `docs/local-ui.md` and `docs/rest-api.md`: the default access group is `sudo`, existing installs keep their stored group, and what that means for the trust model
- [x] 6.4 `README.md`: install from `stable` instead of `--channel edge`; link `docs/release.md`
- [x] 6.5 Add `docs/release.md`: pre-release gates (owner decisions recorded, `make all`), tagging `vX.Y.Z` and publishing a GitHub release, checking the uploaded revision's version in the Store, the clean-VM end-to-end pass (task group 7), and rollback (`snap refresh --revision`, closing `stable`)

## 7. Validation

- [x] 7.1 Run `make all` (the lint step with `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run --new-from-rev=origin/main` if the installed linter panics); no new issues in touched files
- [ ] 7.2 Build and install the snap on a clean Ubuntu 24.04 VM: `tika-server` is `enabled active`, `ragd` is `disabled inactive`, `rag-cli.rag get tika.http` / `get knowledge.http` show the defaults, and `sudo rag-cli.rag set chat.http.host=…` succeeds without `--package`
- [ ] 7.3 On the VM, with chat unset, confirm commands report `config key "chat.http.host" is not set`; from another host, confirm port 9998 is refused
- [ ] 7.4 Refresh test: install edge revision 42, set `knowledge.http.host` with `--package`, a user-layer `chat.model` and `api.socket.group=rag`, refresh to the new build, and confirm all three values and both service states are unchanged
- [ ] 7.5 End-to-end on the VM, following the updated INSTALL.md: OpenSearch, Bedrock Claude chat, `k init`, `k create`, `k ingest` (PDF through Tika), `chat`, then `ragd` with the drop-in and `rag-cli.rag ui`; as a non-root `sudo` member, confirm the socket admits the user
- [ ] 7.6 During the same pass, run and tick the deferred in-snap validation tasks of the open changes it covers (`fix-engine-init-model-reporting` 4.3–4.4, `add-engine-model-pruning` 4.2–4.5, `add-knowledge-labels` 6.1–6.3, `add-kapa-retrieval-wiring` 9.3–9.6, `add-kapa-source-group-selector` 9.2–9.6, `add-ui-answer-batch` 11.4, `add-answer-build-column-selection` 10.5, `ui-repo-ingestion` 5.4); list any that fail as 1.0.0 known issues in `docs/release.md` rather than blocking the release

## 8. Release

- [ ] 8.1 Record JP's decisions on publisher, visibility and license, and fill the `license:` placeholder (blocking for 8.2)
- [ ] 8.2 Merge, tag `v1.0.0`, publish the GitHub release, confirm `latest/stable` shows version `1.0.0`, and install it from `stable` on the VM
