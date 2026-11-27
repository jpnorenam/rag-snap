# Local OpenSearch Setup

Automate the installation and configuration of a local (loopback-only) OpenSearch instance for rag-cli development and testing.

## Overview

`local-setup.sh` installs OpenSearch as a snap, bootstraps TLS certificates and security, and configures rag-cli to use it — all on a single machine with loopback-only binding (no network exposure).

**Use cases:**
- Development and testing with a dedicated OpenSearch instance
- CI/CD environments where you control the machine
- Learning and experimentation without AWS costs

**Not for:**
- Production or multi-user workloads
- Multi-user or network-accessible deployments
- Machines you don't have sudo access to

## Prerequisites

- Ubuntu (tested on 24.04)
- Sudo access
- Enough free memory for the default 6 GiB OpenSearch heap, plus headroom for OpenSearch's own overhead, Tika, the operating system and anything else running on the machine. 4 GB of RAM is not sufficient for the default heap.
- At least 20 GB free disk space

## Quick Start

```bash
# 1. Export the setup script
rag-cli.rag prepare-script local --output ~/rag-local

# 2. Run setup (outside the snap)
cd ~/rag-local
./local-setup.sh setup

# 3. Start using rag-cli
rag-cli.rag chat
```

## Setup Process

The `setup` mode:

1. **Preflight checks** - Checks required commands (`bash`, `curl`, `sudo`, `snap`, `jq`), that the rag-cli snap is installed, and that `ragd` is not running
2. **Settings collection** - Prompts for the settings it does not already have, with defaults, and validates them
3. **OpenSearch installation** - Installs the `opensearch` snap
4. **Bootstrap** - Applies the heap and loopback binding, sets the sysctl minimum, generates TLS certificates, creates the admin account and starts the daemon
5. **rag-cli configuration** - Sets knowledge, tika, and chat endpoints; stores credentials
6. **Knowledge initialization** - Runs `knowledge init` to set up pipelines and ML models

**State tracking:** Setup is idempotent. If interrupted, rerun `./local-setup.sh setup` to resume. Completed phases are tracked in `state.env` and skipped on reruns.

## Operations

### Start Services

```bash
./local-setup.sh start
```

Starts OpenSearch and Tika services with `--enable` (auto-start on boot).

### Stop Services

```bash
./local-setup.sh stop
```

Stops OpenSearch and Tika services with `--disable` (prevents auto-start).

### Destroy

```bash
./local-setup.sh destroy
```

**Destructive operation.** Prompts for confirmation, then:
- Stops and removes the OpenSearch snap
- Removes sysctl configuration (if we created it)
- Clears state markers
- **Preserves:** credentials, secrets (for reuse)

**Ownership check:** Refuses to destroy an OpenSearch snap not installed by this script. This prevents accidental removal of OpenSearch installations managed by other tools or users.

## Configuration

### Settings (`settings.env`)

Hand-editable file with deployment answers:

```bash
OPENSEARCH_HEAP_GIB=6
OPENSEARCH_CHANNEL=2/stable
CHAT_HOST=bedrock-runtime.us-east-1.amazonaws.com
CHAT_PORT=443
CHAT_PATH=openai/v1
CHAT_TLS=true
CHAT_MODEL=mistral.mistral-large-3-675b-instruct
```

Every value is read literally after the `=`: put a comment on its own line, not
after a value.

### Secrets (`secrets.env`)

Never edit directly (`***` placeholders). Managed by the script:

```bash
OPENSEARCH_ADMIN_PASSWORD=***
TLS_ROOT_PASS=***
TLS_ADMIN_PASS=***
TLS_NODE_PASS=***
CHAT_API_KEY=***
GOOGLE_DRIVE_CLIENT_ID=***
GOOGLE_DRIVE_CLIENT_SECRET=***
```

### State (`state.env`)

Tracks completed phases. Do not edit.

```bash
OPENSEARCH_SETUP_COMPLETED="2026-01-01T00:00:00+00:00"
KNOWLEDGE_INIT_COMPLETED="2026-01-01T00:05:00+00:00"
RAG_CLI_CONFIGURED="2026-01-01T00:04:00+00:00"
EMBEDDING_MODEL_ID=<model ID reported by knowledge init>
RERANK_MODEL_ID=<model ID reported by knowledge init>
OPENSEARCH_INSTALLED_BY_US="yes"
OPENSEARCH_CHANNEL_INSTALLED="2/stable"
SYSCTL_FILE_CREATED="yes"
SYSCTL_FILE_CONTENT="vm.max_map_count=262144"
DRIVE_IMPORT_LAST_ATTEMPT="2026-01-01T00:10:00+00:00"
```

### rag-cli Configuration

Written to snap config (package scope):

```bash
knowledge.http.host=127.0.0.1
knowledge.http.port=9200
knowledge.http.tls=true
tika.http.host=127.0.0.1
tika.http.port=9998
chat.http.host=<CHAT_HOST>
chat.model=<CHAT_MODEL>
knowledge.model.embedding=<extracted from knowledge init>
knowledge.model.rerank=<extracted from knowledge init>
```

Credentials are stored in `$SNAP_USER_COMMON/credentials.json` (the snap's user-common directory):

```json
{
  "OPENSEARCH_USERNAME": "admin",
  "OPENSEARCH_PASSWORD": "<OPENSEARCH_ADMIN_PASSWORD>",
  "CHAT_API_KEY": "<CHAT_API_KEY, present only when supplied>"
}
```

Setup merges into this file: it updates the OpenSearch username and password, replaces
`CHAT_API_KEY` only when a new value is supplied, and leaves every other key untouched.

## Sysctl Configuration

OpenSearch requires `vm.max_map_count >= 262144`. The script:

1. Reads the effective value: `sysctl -n vm.max_map_count`
2. Raises it with `sudo sysctl -w vm.max_map_count=262144` **only if it is lower**, never lowering a higher one, then reads it back. If the write is rejected, or the value still reads too low — which is what happens in a container, where this setting belongs to the host — setup stops and tells you the command to run on the host.
3. Persists it to `/etc/sysctl.d/60-rag-local-opensearch.conf`, mode 0644, **only if that file does not already exist**. An existing file is left untouched and is not claimed.
4. Records `SYSCTL_FILE_CREATED=yes` and the exact content in `SYSCTL_FILE_CONTENT`.

`vm.swappiness` and `net.ipv4.tcp_retries2` are left alone. The snap sets those itself when its `set-sysctl-props` handler is enabled, so setup disables that handler and verifies the saved value before the daemon starts.

On destroy, the file is removed **only if** `SYSCTL_FILE_CREATED=yes` **and** its content still matches `SYSCTL_FILE_CONTENT`. A file that was edited afterwards, or that this setup did not create, is kept and the reason is printed. The runtime value is never restored.

## Heap Configuration

OpenSearch heap is configured via `OPENSEARCH_HEAP_GIB` in `settings.env`.

**Default:** 6 GiB

**Validation:** Must be a positive integer.

**Changing heap:** Edit `settings.env` and rerun `./local-setup.sh setup`. The new value is applied even if setup was previously completed - no reinstall required. If the daemon is running, it will be restarted with the new heap size.

## Troubleshooting

### Setup hangs at "Waiting for OpenSearch"

Check logs:

```bash
sudo snap logs opensearch -n 100
```

Common causes:
- Insufficient heap (try 4g minimum)
- Out of memory (check `dmesg | grep -i oom`)
- Port 9200 already in use: `sudo lsof -i :9200`

### Setup stops with a Tika error

Setup fails rather than reporting success when the Tika server cannot start or does
not answer. Check:

```bash
sudo snap logs rag-cli.tika-server -n 100
```

### `knowledge init` fails

Check:
- OpenSearch is running: `curl -k https://127.0.0.1:9200`
- Credentials are correct: check `credentials.json`
- Model download succeeded: see `knowledge init` output

### Destroy refuses to remove OpenSearch

**Expected behavior.** The script refuses to destroy OpenSearch installations it didn't create. Check `state.env`:

- If `OPENSEARCH_INSTALLED_BY_US=yes` is absent, the script didn't install it
- Manually uninstall: `sudo snap remove opensearch`

### Sysctl changes not persisting

Check:
- `/etc/sysctl.d/60-rag-local-opensearch.conf` exists
- It contains `vm.max_map_count=<value>`
- Owner is root: `ls -l /etc/sysctl.d/60-rag-local-opensearch.conf`

Manually apply:

```bash
sudo sysctl -p /etc/sysctl.d/60-rag-local-opensearch.conf
```

## Differences from AWS Setup

| Feature | Local Setup | AWS Setup |
|---------|-------------|-----------|
| **Network binding** | Loopback only (127.0.0.1) | Public IP with CIDR restriction |
| **TLS** | Self-signed certificates | Self-signed certificates |
| **Persistence** | Local disk | EBS volume |
| **Cost** | Free (uses your machine) | EC2 + EBS charges |
| **Scalability** | Single machine only | Dedicated instance |

## Security Notes

- **Loopback only:** `network.host`, `http.host` and `transport.host` are set to `127.0.0.1`, and any conflicting `http.*`/`transport.*` override is removed, so neither REST nor transport is reachable from the network. This is applied after `opensearch.setup` runs — that command rewrites `network.host` itself, defaulting it to `[_local_, _site_]` — and before the daemon starts, on first setup and on every rerun. Before reporting success, setup reads the node's own `http.bound_address` and `transport.bound_address`, requires a non-empty list for each, and fails if anything other than loopback is listening; a working `localhost` request on its own would not show that. The block list `opensearch.setup` writes for `network.host` is replaced by the single-line values, with its list items removed. A bare `network`, `http` or `transport` mapping is refused, naming the keys to set by hand.
- **Self-signed TLS:** rag-cli connects over TLS with certificate validation disabled
- **Credentials:** Stored in `$SNAP_USER_COMMON/credentials.json` (mode 0600, snap-confined). Setup merges into that file: it sets the OpenSearch username and password, replaces `CHAT_API_KEY` only when you supply a new one, and leaves every other key alone. A file whose contents are not a JSON object is reported and left untouched.
- **Secrets on disk:** `secrets.env` is mode 0600 and resides in the deployment directory (mode 0700)

**Do not expose this setup to untrusted networks.** Use AWS setup for remote or multi-user access.

## File Layout

```
~/rag-local/
├── local-setup.sh          # Main orchestrator
├── common.sh               # Shared functions
├── bootstrap-opensearch.sh # OpenSearch bootstrap script
├── snap-context.env        # Snap instance name, user common dir
├── settings.env            # Deployment answers (hand-editable)
├── secrets.env             # Secrets (script-managed, masked)
└── state.env               # Completion markers (do not edit)
```

OpenSearch's own logs stay in the snap (`sudo snap logs opensearch`).

## See Also

- [docs/opensearch-on-aws.md](opensearch-on-aws.md) - AWS deployment guide
- [docs/usage.md](usage.md) - rag-cli usage reference
- [INSTALL.md](../INSTALL.md) - Installation and configuration walkthrough
