# Installing and configuring rag-cli

This is the full path from nothing to a working install: prerequisites, installing the snap,
configuring it against your backends, verifying the CLI works, and (optionally) enabling the
browser UI.

- [Prerequisites](#prerequisites)
- [Install the snap](#install-the-snap)
- [Configure the backends](#configure-the-backends)
- [Secrets](#secrets)
- [Initialize pipelines and models](#initialize-pipelines-and-models)
- [Verify: create a knowledge base and chat](#verify-create-a-knowledge-base-and-chat)
- [Enable the browser UI](#enable-the-browser-ui)
- [Where to go next](#where-to-go-next)

---

## Prerequisites

`rag-cli` is a thin orchestrator over three services. Set these up first (or point at existing
ones — nothing below requires them to be on `127.0.0.1`).

### OpenSearch (the `knowledge` store)

> To have `rag-cli` create a dedicated OpenSearch node on AWS and configure everything below for
> you, see **[docs/opensearch-on-aws.md](docs/opensearch-on-aws.md)** (`rag-cli.rag prepare-script aws`).

Install and set up the [OpenSearch snap](https://github.com/canonical/opensearch-snap). During
[certificate creation](https://github.com/canonical/opensearch-snap?tab=readme-ov-file#creating-certificates),
make sure the `ingest` and `ml` roles are set on the node:

```bash
sudo snap run opensearch.setup                  \
    --node-name vdb0                            \
    --node-roles cluster_manager,data,ingest,ml \
    --tls-priv-key-root-pass root1234           \
    --tls-priv-key-admin-pass admin1234         \
    --tls-priv-key-node-pass node1234           \
    --tls-init-setup yes
```

Increase the JVM heap size to fit the sentence-transformer and cross-encoder models (at least
6 GB is recommended; adjust to your machine's available RAM):

```bash
echo '-Xms6g' | sudo tee /var/snap/opensearch/current/etc/opensearch/jvm.options.d/heap.options
echo '-Xmx6g' | sudo tee -a /var/snap/opensearch/current/etc/opensearch/jvm.options.d/heap.options
sudo snap restart opensearch
```

Validate the node roles:

```bash
curl -k -u admin:admin https://localhost:9200/_cat/nodes?v
```

You can also point `rag-cli` at an existing/remote OpenSearch cluster you already manage —
see [Configure the backends](#configure-the-backends) below; just substitute its host, port, and
credentials.

### An inference backend (the `chat` backend)

Pick one:

- **(Recommended) [AWS Bedrock](docs/bedrock_guide.md)** — a third-party OpenAI-compatible API.
  > **Warning:** your prompts and retrieved context are sent to an external service. Do not
  > ingest or ask about confidential information in this configuration.
- **(Alternative) An [Inference snap](https://github.com/canonical/inference-snaps)** running
  locally. Pick the engine appropriate for your hardware (`sudo <inference-snap-name>
  show-engine`), and confirm it responds:
  ```bash
  curl http://localhost:8324/v1/chat/completions \
    -H 'Content-Type: application/json'          \
    -d '{
      "messages": [
        {"role": "system", "content": "You are a helpful assistant."},
        {"role": "user", "content": "Hello!"}
      ]
    }'
  ```

### Tika (the `tika` service)

Bundled with the snap — nothing to install or configure. It starts when the snap is installed.

---

## Install the snap

From the Snap store:

```bash
sudo snap install rag-cli
```

This installs the `stable` release. For the latest build of `main`, add `--channel edge`.

Installing starts the bundled Tika server (`rag-cli.tika-server`) and registers working defaults
for it and for a local OpenSearch, so neither needs configuring unless yours is elsewhere. The
`ragd` daemon stays off until you enable it (see [Enable the browser UI](#enable-the-browser-ui)).

Or build and install locally:

```bash
snapcraft -v
sudo snap install --dangerous ./rag-cli_*.snap
```

---

## Configure the backends

Only the chat backend has no default, so it is the one thing you must configure. Set it with
`sudo rag-cli.rag set --package <key>=<value>`, as below. A plain `rag-cli.rag set <key>=<value>`
(without `--package`) is also accepted for these keys, but it stores an override that takes
precedence over every package value from then on, including any team defaults applied later. Use
`--package` unless you specifically want an override. `rag-cli.rag get <key>` shows the effective
value.

`chat.api` selects the wire protocol the chat backend speaks: `openai` (OpenAI Chat
Completions, the default) or `anthropic` (the Anthropic Messages API).

**Chat via Bedrock: Claude Sonnet 5.5 (Anthropic Messages API):**

```bash
sudo rag-cli.rag set --package chat.api="anthropic"
sudo rag-cli.rag set --package chat.http.host="bedrock-runtime.us-east-2.amazonaws.com"
sudo rag-cli.rag set --package chat.http.port="443"
sudo rag-cli.rag set --package chat.http.tls="true"
sudo rag-cli.rag set --package chat.http.path="anthropic"
sudo rag-cli.rag set --package chat.model="global.anthropic.claude-sonnet-5-5"
```

This is the same `bedrock-runtime` host as Mistral, but a different API. Claude Sonnet 5.5
supports the Anthropic Messages API there (path `anthropic`), not Chat Completions
(`openai/v1`), so changing `chat.model` alone does not work. The model must be an inference
profile: `global.anthropic.claude-sonnet-5-5` (routes worldwide, no regional premium) or
`us.anthropic.claude-sonnet-5-5` (stays in US regions). The bare `anthropic.claude-sonnet-5-5`
is not callable in `us-east-2`. With `chat.api=anthropic`:

- `chat.model` is **required**. The Messages endpoint has no model listing.
- `--temperature`, and the `temperature` field of REST requests, are **ignored**. Current Claude
  models reject sampling parameters.
- The AWS account must have model access to Claude Sonnet 5.5, and your Bedrock API key
  (`CHAT_API_KEY`) must be allowed to invoke it (`bedrock:InvokeModel` on the inference
  profile). See
  [docs/bedrock_guide.md](docs/bedrock_guide.md).

**Chat via Bedrock: OpenAI-compatible models (e.g. Mistral Large 3):**

```bash
sudo rag-cli.rag set --package chat.api="openai"
sudo rag-cli.rag set --package chat.http.host="bedrock-runtime.us-east-2.amazonaws.com"
sudo rag-cli.rag set --package chat.http.port="443"
sudo rag-cli.rag set --package chat.http.tls="true"
sudo rag-cli.rag set --package chat.http.path="openai/v1"
sudo rag-cli.rag set --package chat.model="mistral.mistral-large-3-675b-instruct"
```

**Chat via a local Inference snap (instead of Bedrock):**

```bash
sudo rag-cli.rag set --package chat.api="openai"
sudo rag-cli.rag set --package chat.http.host="127.0.0.1"
sudo rag-cli.rag set --package chat.http.port="8324"
sudo rag-cli.rag set --package chat.http.path="v1"
```

**Knowledge (OpenSearch):** the default is a local OpenSearch snap at `127.0.0.1:9200` over TLS.
To use a remote or external cluster, set its real host (and port, if not 9200):

```bash
sudo rag-cli.rag set --package knowledge.http.host="10.0.0.5"   # your cluster's host
sudo rag-cli.rag set --package knowledge.http.port="9200"
```

**Tika:** bundled and preconfigured (`127.0.0.1:9998`, path `tika`). It starts at install and
listens on loopback only. If you never ingest documents, you can turn it off with
`sudo snap stop --disable rag-cli.tika-server`.

Check everything with `rag-cli.rag status`.

Enable OpenSearch so it survives reboots. `--enable` matters: plain `snap start` would run it only
until the next reboot. The same applies to `ragd`, which you enable later in
[Enable the browser UI](#enable-the-browser-ui), once its secrets are in place:

```bash
sudo snap start --enable opensearch
```

Confirm the services read `enabled active` with `snap services | grep -E 'opensearch|rag-cli'`
(`rag-cli.ragd` stays `disabled inactive` until you enable it). See
[Services](docs/usage.md#services) for the reasoning and for recovery after an unclean shutdown.

> **Upgrading from an install older than 1.0.0:** a refresh keeps every value and service state you
> already had. If you never enabled Tika, run `sudo snap start --enable rag-cli.tika-server` once.

---

## Secrets

Secrets are never stored in config — they're environment variables.

**For the CLI** (`rag-cli.rag chat`, `k create`, `knowledge init`, etc.), export them in your
shell before running commands:

```bash
export OPENSEARCH_USERNAME="admin"
export OPENSEARCH_PASSWORD="admin"      # or your cluster's real password
export CHAT_API_KEY="bedrock-api-key-****"
export KAPA_API_KEY="kapa-api-key-****"  # optional: only if you use kapa.ai (see below)
```

The CLI inherits these directly from your shell, so this is enough for every `rag-cli.rag ...`
command.

#### Credentials file

Instead of exporting them in every shell, the CLI can read `OPENSEARCH_USERNAME`,
`OPENSEARCH_PASSWORD`, `CHAT_API_KEY` and `KAPA_API_KEY` from `~/snap/rag-cli/common/credentials.json`
(`$SNAP_USER_COMMON/credentials.json`). The [AWS setup](docs/opensearch-on-aws.md) writes this file for
you; you can also create it yourself:

```bash
umask 077
printf '%s\n' '{"OPENSEARCH_USERNAME": "admin", "OPENSEARCH_PASSWORD": "...", "CHAT_API_KEY": "..."}' \
  > ~/snap/rag-cli/common/credentials.json
```

- An exported variable always wins, even when it is set to an empty string
  (`export CHAT_API_KEY=` means "no API key").
- The file is read only when a credential the command needs is not exported. It must be a
  regular file owned by you with mode 0600, and must contain only those four keys, with string
  values. Otherwise the command stops and says what to fix, without printing any value.
- Only the CLI reads this file. `ragd` still takes its secrets from its service environment, as
  described below.

**For the browser UI / REST API**, the daemon (`ragd`) runs as a separate systemd service with
its own environment — your shell's `export` is invisible to it. Give it the secrets with a
root-only systemd drop-in (the auto-generated unit is regenerated on every restart, so don't edit
it directly):

```bash
sudo mkdir -p /etc/systemd/system/snap.rag-cli.ragd.service.d
printf '[Service]\nEnvironment=CHAT_API_KEY=%s\nEnvironment=OPENSEARCH_USERNAME=%s\nEnvironment=OPENSEARCH_PASSWORD=%s\nEnvironment=KAPA_API_KEY=%s\n' \
  "$CHAT_API_KEY" "$OPENSEARCH_USERNAME" "$OPENSEARCH_PASSWORD" "$KAPA_API_KEY" | \
  sudo tee /etc/systemd/system/snap.rag-cli.ragd.service.d/10-secrets.conf >/dev/null
sudo chmod 600 /etc/systemd/system/snap.rag-cli.ragd.service.d/10-secrets.conf
sudo systemctl daemon-reload
sudo snap restart rag-cli.ragd
```

Confirm the running daemon actually has the keys:

```bash
sudo sh -c "tr '\0' '\n' < /proc/\$(pgrep -x ragd)/environ" | grep -cE '^(CHAT_API_KEY|OPENSEARCH_USERNAME|OPENSEARCH_PASSWORD|KAPA_API_KEY)='
# should print 4 (3 if you don't use kapa.ai and left KAPA_API_KEY empty)
```

> `ragd`'s app definition in `snap/snapcraft.yaml` declares no hardcoded `environment:` values
> for these — a hardcoded value there would be reapplied by `snap run` *after* systemd and
> silently override anything set via a drop-in. Because none are hardcoded, all three secrets
> above take effect the same way, including a non-default OpenSearch username/password.

`ragd` reads its secrets and config once at startup: after changing the drop-in or any config key
(for example `kapa.project.id`), run `sudo snap restart rag-cli.ragd`.

#### kapa.ai (optional)

kapa.ai adds remote documentation search alongside your knowledge bases (`/use-kapa` in chat,
`kapa_source_groups` in batch manifests). It needs the project id in config and the API key as the
`KAPA_API_KEY` secret above:

```bash
sudo rag-cli.rag set kapa.project.id=<project-id>
```

> **Upgrading:** the API key used to be a config key, `kapa.api.key`. It is no longer read, and
> refreshing the snap removes any stored value. Move the key to `KAPA_API_KEY`: export it (or add it
> to `credentials.json`) for the CLI, and add it to the `ragd` drop-in for the browser UI and REST
> API. Downgrading to a release that read `kapa.api.key` means setting it again.

---

## Initialize pipelines and models

```bash
rag-cli.rag knowledge init
```

This prints the embedding and rerank model IDs it resolved. With the `ragd` daemon running, the
daemon writes them to the package configuration itself and the command says so. Otherwise set them
yourself, using the IDs printed above:

```bash
sudo rag-cli.rag set --package knowledge.model.embedding=<embedding-model-id>
sudo rag-cli.rag set --package knowledge.model.rerank=<rerank-model-id>
```

Check what the engine will use with `rag-cli.rag get knowledge.model`.

---

## Verify: create a knowledge base and chat

```bash
rag-cli.rag k create default
rag-cli.rag k ingest default <source-id> --file <path-to-local-file>
rag-cli.rag chat
```

In the chat REPL, `/use-knowledge` selects which bases ground your answers. See
[docs/usage.md](docs/usage.md) for the full CLI reference (ingest formats, batch jobs, export/import,
Google Drive import, `answer batch`, etc.)

---

## Enable the browser UI

The loopback listener is off by default. Enable it and start the daemon:

```bash
sudo rag-cli.rag set api.loopback.enabled=true
sudo snap start --enable rag-cli.ragd
```

(If you already started `ragd` before enabling the listener, restart it instead:
`sudo snap restart rag-cli.ragd`.) Make sure you've completed [Secrets](#secrets) above so the
daemon has `CHAT_API_KEY` and your real OpenSearch credentials — otherwise chat requests fail
with `401 Unauthorized`, and knowledge-base features fail with `opensearch not available`.

Open the UI:

```bash
rag-cli.rag ui
# or, on a headless host:
rag-cli.rag ui --no-browser
```

You must be `root` or a member of the daemon's access group to reach it. The default group is
`sudo`, which the installing user on Ubuntu is already in, so run CLI commands as yourself rather
than with `sudo`: under `sudo`, the snap can't read files in your home directory, such as a batch
manifest. To use another group, run `sudo rag-cli.rag set --package api.socket.group=<group>` and
then `sudo snap restart rag-cli.ragd`. Installs older than 1.0.0 keep the group they stored (often
`rag`, which usually doesn't exist); switch them to `sudo` the same way. See
[Using the CLI without `sudo`](docs/rest-api.md#using-the-cli-without-sudo) and
[docs/local-ui.md](docs/local-ui.md) for navigating the UI, the trust model, and troubleshooting.

---

## Where to go next

- [docs/usage.md](docs/usage.md) — full CLI reference
- [docs/local-ui.md](docs/local-ui.md) — browser UI reference (navigation, trust model, troubleshooting)
- [docs/rest-api.md](docs/rest-api.md) — REST API (`ragd`) reference
- [docs/bedrock_guide.md](docs/bedrock_guide.md) — step-by-step Bedrock API key walkthrough
