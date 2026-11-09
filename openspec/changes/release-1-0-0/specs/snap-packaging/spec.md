## ADDED Requirements

### Requirement: Release grade and tag-derived version
The snap SHALL be built with `grade: stable`, and its version SHALL be derived from the git tag of the commit being built rather than written in `snapcraft.yaml`.

Only tags matching `v[0-9]*` count. A build of a commit that carries such a tag SHALL have that
tag's version without the leading `v`. A build of a later commit SHALL have the nearest tag's
version, followed by `+git<count>.<short-commit>`. A build with no such tag reachable SHALL have
version `0.0.0+git.<short-commit>`. A build with uncommitted changes to built paths SHALL append
`-dirty`. A build whose derived version does not satisfy snapd's version rules SHALL fail.

#### Scenario: Tagged release build
- **WHEN** the snap is built from the commit tagged `v1.0.0` with a clean tree
- **THEN** the snap's version is `1.0.0`
- **AND** `SNAP_VERSION` inside the installed snap is `1.0.0`

#### Scenario: Build after a release tag
- **WHEN** the snap is built from a commit 3 commits after `v1.0.0` whose short hash is `abc1234`
- **THEN** the snap's version is `1.0.0+git3.abc1234`

#### Scenario: Build with no release tag
- **WHEN** the snap is built from a checkout with no reachable `v*` tag
- **THEN** the snap's version starts with `0.0.0+git.`

#### Scenario: Invalid derived version
- **WHEN** the derived version would exceed 32 characters or contain a character snapd rejects
- **THEN** the build fails with a message naming the derived version

### Requirement: Package-layer registration of backend keys
The install and post-refresh hooks SHALL register, at the package layer, every backend key a user may configure, so that a user-layer `rag-cli.rag set <key>=<value>` is accepted for them without `--package`.

The keys and their registered defaults are:

| Key | Default |
|---|---|
| `tika.http.host` | `127.0.0.1` |
| `tika.http.port` | `9998` |
| `tika.http.path` | `tika` |
| `knowledge.http.host` | `127.0.0.1` |
| `knowledge.http.port` | `9200` |
| `knowledge.http.tls` | `true` |
| `chat.http.host`, `chat.http.port`, `chat.http.path`, `chat.http.tls`, `chat.model` | empty |
| `knowledge.model.embedding`, `knowledge.model.rerank` | empty |

A key registered with an empty value SHALL behave at runtime exactly as an unset key does.

#### Scenario: Fresh install has Tika and local OpenSearch defaults
- **WHEN** the snap is installed on a machine that never had it
- **THEN** `rag-cli.rag get tika.http` reports host `127.0.0.1`, port `9998` and path `tika`
- **AND** `rag-cli.rag get knowledge.http` reports host `127.0.0.1`, port `9200` and tls `true`

#### Scenario: User-layer set of a chat key
- **WHEN** a user runs `sudo rag-cli.rag set chat.http.host=bedrock-runtime.us-east-2.amazonaws.com` on a fresh install
- **THEN** the value is stored in the user layer and the command succeeds

#### Scenario: Empty chat key reports as not set
- **WHEN** a command that needs the chat backend runs and `chat.http.host` holds its registered empty value
- **THEN** the command fails with the same "config key \"chat.http.host\" is not set" error as when the key is absent

### Requirement: Registration never overwrites stored values
The package-layer registration SHALL write a key only when it has no non-empty stored value at the package layer, and SHALL never modify the user layer.

#### Scenario: Refresh keeps an operator's package value
- **WHEN** an install has `knowledge.http.host=10.0.0.5` set with `--package` and the snap is refreshed
- **THEN** `knowledge.http.host` is still `10.0.0.5` after the refresh

#### Scenario: Refresh keeps a user override
- **WHEN** an install has a user-layer `chat.model` and the snap is refreshed
- **THEN** the user-layer value is unchanged and still takes precedence

### Requirement: Default socket access group
The package-layer default for `api.socket.group` SHALL be `sudo`, and `ragd` SHALL use `sudo` when the key is unset or empty.

An install that already stores a value for `api.socket.group` SHALL keep it across refreshes.

#### Scenario: Fresh install grants sudo members access
- **WHEN** the snap is freshly installed, `ragd` is started, and a non-root member of the `sudo` group runs `rag-cli.rag status`
- **THEN** the daemon accepts the connection over the unix socket

#### Scenario: Existing install keeps its group
- **WHEN** an install that stores `api.socket.group=rag` is refreshed
- **THEN** `api.socket.group` is still `rag`

### Requirement: Service startup on install
The `tika-server` service SHALL be enabled and started when the snap is installed, and the `ragd` service SHALL remain installed but disabled and stopped until an operator or a later setup step enables it.

A refresh SHALL leave each service's enabled and active state as it was before the refresh.

#### Scenario: Fresh install
- **WHEN** the snap is freshly installed
- **THEN** `snap services rag-cli` shows `rag-cli.tika-server` as `enabled active`
- **AND** shows `rag-cli.ragd` as `disabled inactive`

#### Scenario: Refresh of an install whose Tika was never enabled
- **WHEN** an install whose `tika-server` is `disabled inactive` is refreshed
- **THEN** `tika-server` is still `disabled inactive` after the refresh

### Requirement: Tika listens on loopback only
The bundled Tika server SHALL bind to `127.0.0.1` and SHALL NOT accept connections on non-loopback addresses.

#### Scenario: Local extraction works
- **WHEN** `tika-server` is running with the default `tika.http.*` values and a document is ingested
- **THEN** text extraction succeeds through `http://127.0.0.1:9998/tika`

#### Scenario: Network access refused
- **WHEN** another machine connects to port 9998 on the host's network address
- **THEN** the connection is refused

### Requirement: Status command description
The `status` command's help text SHALL describe the status of rag-cli and its backends, and SHALL NOT describe the command as the status of the inference snap.

#### Scenario: Help text
- **WHEN** a user runs `rag-cli.rag status --help`
- **THEN** the description refers to rag-cli and its backends (knowledge store, chat backend, Tika)
- **AND** does not contain "inference snap"
