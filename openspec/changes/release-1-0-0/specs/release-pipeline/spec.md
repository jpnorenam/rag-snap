## ADDED Requirements

### Requirement: Checks gate publishing
The publish workflow SHALL run `go vet ./...`, `go test ./...` and the browser UI build (`npm ci` and `npm run build` in `ui/`) before building the snap, and SHALL NOT build or publish the snap when any of them fails.

#### Scenario: Failing test blocks publishing
- **WHEN** a commit whose `go test ./...` fails is pushed to `main`
- **THEN** the workflow fails before the snap build step
- **AND** nothing is published to any channel

#### Scenario: Passing checks publish to edge
- **WHEN** a commit whose checks all pass is pushed to `main`
- **THEN** the workflow builds the snap and publishes it to `latest/edge`

### Requirement: Channel per trigger
The workflow SHALL publish pushes to `main` to `latest/edge`, SHALL publish published GitHub releases to `latest/stable`, and SHALL build without publishing on manual dispatch.

#### Scenario: Release publishes to stable
- **WHEN** a GitHub release for tag `v1.0.0` is published and its checks pass
- **THEN** the snap built from that tag, with version `1.0.0`, is published to `latest/stable`

#### Scenario: Manual dispatch builds only
- **WHEN** the workflow is started by manual dispatch
- **THEN** the snap is built and uploaded as a workflow artifact
- **AND** nothing is published to the Store

### Requirement: Tags visible to the build
The workflow's checkout SHALL include the repository's tags, so the version derived during the snap build matches the tag of the commit being built.

#### Scenario: Release build sees its tag
- **WHEN** the workflow runs for the release of tag `v1.0.0`
- **THEN** the snap build log reports the derived version `1.0.0`, not a `0.0.0+git` version

### Requirement: Supported Go toolchain
The snap build SHALL compile the Go binaries with a Go release that is within upstream support at the time of release.

#### Scenario: Build toolchain
- **WHEN** the 1.0.0 snap is built
- **THEN** the `go-cli` part uses the `go/1.26/stable` build-snap or a newer supported release
