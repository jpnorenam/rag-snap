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

Bundled with the snap — nothing to install separately. It's started in
[Configure the backends](#configure-the-backends) below.

---

## Install the snap

From the Snap store:

```bash
sudo snap install rag-cli --channel edge
```

Or build and install locally:

```bash
snapcraft -v
sudo snap install --dangerous ./rag-cli_*.snap
```

---

## Configure the backends

Set these with `sudo rag-cli.rag set --package <key>=<value>`. Substitute your real hosts —
`127.0.0.1` below is just the common case; a remote/external OpenSearch cluster works the same
way, just use its real host/port.

**Chat via Bedrock:**

```bash
sudo rag-cli.rag set --package chat.http.host="bedrock-runtime.us-east-2.amazonaws.com"
sudo rag-cli.rag set --package chat.http.port="443"
sudo rag-cli.rag set --package chat.http.tls="true"
sudo rag-cli.rag set --package chat.http.path="openai/v1"
sudo rag-cli.rag set --package chat.model="mistral.mistral-large-3-675b-instruct"
```

**Chat via a local Inference snap (instead of Bedrock):**

```bash
sudo rag-cli.rag set --package chat.http.host="127.0.0.1"
sudo rag-cli.rag set --package chat.http.port="8324"
sudo rag-cli.rag set --package chat.http.path="v1"
```

**Knowledge (OpenSearch) — use your cluster's real host:**

```bash
sudo rag-cli.rag set --package knowledge.http.host="127.0.0.1"   # or a remote host, e.g. a cluster IP
sudo rag-cli.rag set --package knowledge.http.port="9200"
sudo rag-cli.rag set --package knowledge.http.tls="true"
```

**Tika (bundled, always local):**

```bash
sudo rag-cli.rag set --package tika.http.host="127.0.0.1"
sudo rag-cli.rag set --package tika.http.port="9998"
sudo rag-cli.rag set --package tika.http.path="tika"
sudo snap start rag-cli.tika-server
```

Check everything with `rag-cli.rag status`.

---

## Secrets

Secrets are never stored in config — they're environment variables.

**For the CLI** (`rag-cli.rag chat`, `k create`, `knowledge init`, etc.), export them in your
shell before running commands:

```bash
export OPENSEARCH_USERNAME="admin"
export OPENSEARCH_PASSWORD="admin"      # or your cluster's real password
export CHAT_API_KEY="bedrock-api-key-****"
```

The CLI inherits these directly from your shell, so this is enough for every `rag-cli.rag ...`
command.

**For the browser UI / REST API**, the daemon (`ragd`) runs as a separate systemd service with
its own environment — your shell's `export` is invisible to it. Give it all three secrets with a
root-only systemd drop-in (the auto-generated unit is regenerated on every restart, so don't edit
it directly):

```bash
sudo mkdir -p /etc/systemd/system/snap.rag-cli.ragd.service.d
printf '[Service]\nEnvironment=CHAT_API_KEY=%s\nEnvironment=OPENSEARCH_USERNAME=%s\nEnvironment=OPENSEARCH_PASSWORD=%s\n' \
  "$YOUR_CHAT_KEY" "$YOUR_OPENSEARCH_USER" "$YOUR_OPENSEARCH_PASSWORD" | \
  sudo tee /etc/systemd/system/snap.rag-cli.ragd.service.d/10-secrets.conf >/dev/null
sudo chmod 600 /etc/systemd/system/snap.rag-cli.ragd.service.d/10-secrets.conf
sudo systemctl daemon-reload
sudo snap restart rag-cli.ragd
```

Confirm the running daemon actually has the keys:

```bash
sudo sh -c "tr '\0' '\n' < /proc/\$(pgrep -x ragd)/environ" | grep -cE '^(CHAT_API_KEY|OPENSEARCH_USERNAME|OPENSEARCH_PASSWORD)='
# should print 3
```

> `ragd`'s app definition in `snap/snapcraft.yaml` declares no hardcoded `environment:` values
> for these — a hardcoded value there would be reapplied by `snap run` *after* systemd and
> silently override anything set via a drop-in. Because none are hardcoded, all three secrets
> above take effect the same way, including a non-default OpenSearch username/password.

---

## Initialize pipelines and models

```bash
rag-cli.rag knowledge init
```

This prints embedding/rerank model IDs — set them:

```bash
sudo rag-cli.rag set --package knowledge.model.embedding=<embedding-model-id>
sudo rag-cli.rag set --package knowledge.model.rerank=<rerank-model-id>
```

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

You must be `root` or a member of the daemon's access group (default `rag`) to reach it. See
[docs/local-ui.md](docs/local-ui.md) for navigating the UI, the trust model, and troubleshooting.

---

## Where to go next

- [docs/usage.md](docs/usage.md) — full CLI reference
- [docs/local-ui.md](docs/local-ui.md) — browser UI reference (navigation, trust model, troubleshooting)
- [docs/rest-api.md](docs/rest-api.md) — REST API (`ragd`) reference
- [docs/bedrock_guide.md](docs/bedrock_guide.md) — step-by-step Bedrock API key walkthrough
