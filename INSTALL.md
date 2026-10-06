# Installing and configuring rag-cli

This is the full path from nothing to a working install: prerequisites, installing the snap,
configuring it against your backends, verifying the CLI works, and (optionally) enabling the
browser UI.

- [Prerequisites](#prerequisites)
- [Installation](#install-the-snap)
- [Knowledge Initialization](#knowledge-initialization)
- [Enable the browser UI](#enable-the-browser-ui)
- [Where to go next](#where-to-go-next)


---
## Node Prerequisites

### -op1) local lxd

prepare an lxd envirionment
```
sudo snap install lxd --channel 6/stable
sudo lxd init
```

adjust specs based on your host resources (check cpu `nproc` and memroy `free -h` to adjust accordingly)

```
sudo lxc launch ubuntu:24.04 rag-snap --vm -c limits.cpu=6 -c limits.memory=40GiB -d root,size=32GiB
lxc shell rag-snap
```

## Prerequisites

`rag-cli` is a thin orchestrator over three services. Set these up first (or point at existing
ones — nothing below requires them to be on `127.0.0.1`).

### 1. OpenSearch (the `knowledge` store)

The [Official OpenSearch product](https://opensearch.org/) which we will install via [OpenSearch snap](https://github.com/canonical/opensearch-snap) for an easier set up
```
sudo snap install opensearch
```

During [certificate creation](https://github.com/canonical/opensearch-snap?tab=readme-ov-file#creating-certificates),
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

### 2. An inference backend (the `chat` backend)

Pick one:

#### -op1) (Recommended) [AWS Bedrock](docs/bedrock_guide.md)

a third-party OpenAI-compatible API.

> **Warning:** your prompts and retrieved context are sent to an external service. Do not
> ingest or ask about confidential information in this configuration.

#### -op2) (Alternative) Canonical Inference Snap

An [Inference snap](https://github.com/canonical/inference-snaps) running locally. 

```
sudo snap install gemma3

```
Pick the engine appropriate for your hardware 

``` bash
# sudo gemma3 use-engine --auto
Evaluating engines for optimal hardware compatibility:
✘ amd-gpu: not compatible
✔ cpu: compatible, score=10
✔ intel-cpu: compatible, score=16
✘ intel-gpu: not compatible
✘ nvidia-gpu: not compatible
Selected engine: intel-cpu

# gemma3 status
engine: intel-cpu
services:
    server: active
    server-webui: active
entrypoints:
    kserve:
        url: http://127.0.0.1:8328/v2
    openai:
        url: http://127.0.0.1:8328/v3
    tensorflow-serving:
        url: http://127.0.0.1:8328/v1
    webui:
        url: http://127.0.0.1:8329
model:
    name: gemma3-4b-ov
```

and confirm it responds through the `openai` entrypoint, using the `model` name

```bash
curl http://localhost:8328/v3/chat/completions \
-H 'Content-Type: application/json'          \
-d '{
  "model": "gemma3-4b-ov" 
  "messages": [
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "Hello!"}
  ]
}'
```

### 3. Tika (the `input metadata/text extraction` service)
[Official Apache Tika product](https://tika.apache.org/), for input metadata and text extraction
Bundled with the snap — nothing to install separately. It's started in
[Configure the backends](#configure-the-backends) below.


---

## Install the snap


### 1. Snap Install
From the Snap store:

```bash
sudo snap install rag-cli --channel edge
```

Or [build locally](./CONTRIBUTING.md):

```bash
snapcraft -v
sudo snap install --dangerous ./rag-cli_*.snap
```


### 2. Configure the backends

Set these with `sudo rag-cli.rag set --package <key>=<value>`. Substitute your real hosts —
`127.0.0.1` below is just the common case

Tested options to pick from are following:

#### -op1) via Bedrock

```bash
sudo rag-cli.rag set --package chat.http.host="bedrock-runtime.us-east-2.amazonaws.com"
sudo rag-cli.rag set --package chat.http.port="443"
sudo rag-cli.rag set --package chat.http.tls="true"
sudo rag-cli.rag set --package chat.http.path="openai/v1"
sudo rag-cli.rag set --package chat.model="mistral.mistral-large-3-675b-instruct"
```

#### -op2) via local inference snap

```bash
sudo rag-cli.rag set --package chat.http.host="127.0.0.1"
sudo rag-cli.rag set --package chat.http.port="8324"
sudo rag-cli.rag set --package chat.http.path="v1"
```

### 3. Knowledge (OpenSearch)

use your cluster's real host. a remote/external OpenSearch cluster works the same
way, just use its real host/port.

```bash
sudo rag-cli.rag set --package knowledge.http.host="127.0.0.1"   # or a remote host, e.g. a cluster IP
sudo rag-cli.rag set --package knowledge.http.port="9200"
sudo rag-cli.rag set --package knowledge.http.tls="true"
```

### 4. Tika (bundled, always local)

```bash
sudo rag-cli.rag set --package tika.http.host="127.0.0.1"
sudo rag-cli.rag set --package tika.http.port="9998"
sudo rag-cli.rag set --package tika.http.path="tika"
sudo snap start rag-cli.tika-server
```

### 5. Validate Status

Check service are active and endpoints configured.

```bash
$ sudo rag-cli.rag status
models:
    embedding: huggingface/sentence-transformers/msmarco-distilbert-base-tas-b (j8_tr6ABrqI30v_eRcuP)
    llm: gemma-3-4b-it-ov-int4-fq
    reranker: huggingface/cross-encoders/ms-marco-MiniLM-L-12-v2 (ks_tr6ABrqI30v_ec8sr)
services:
    ragd: active
    tika-server: active
endpoints:
    openai: http://127.0.0.1:8328/v3
    opensearch: https://127.0.0.1:9200
    tika: http://127.0.0.1:9998/tika
```

You can get a dump of all options with `rag-cli.rag get`


### 6. Secrets

Secrets are never stored in config — they're environment variables.

**For the CLI** (`rag-cli.rag chat`, `k create`, `knowledge init`, etc.), export them in your
shell before running commands:

```bash
export OPENSEARCH_USERNAME="admin"
export OPENSEARCH_PASSWORD="admin"              # or your cluster's real password
export CHAT_API_KEY="bedrock-api-key-****"      # only for bedrock
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
  "$CHAT_API_KEY" "$OPENSEARCH_USERNAME" "$OPENSEARCH_PASSWORD" | \
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

### 7. Survive  restarts

Some service daemons (opensearch, rag-cli tika-server and ragd) are declared `install-mode: disabled`, so on every reboot it stops the service, permantently, until someone enables them.

```
sudo snap start --enable opensearch
sudo snap start --enable rag-cli.tika-server
sudo snap start --enable rag-cli.ragd           # if you use web UI / REST API
```
Running `snap enable $service` will not work due to the declared install mode.

---


## Knowledge Initialization

### 1. Model setup

This prints the embedding and rerank model IDs it resolved. 

```bash
rag-cli.rag knowledge init
```

With the `ragd` daemon running, the daemon writes them to the package configuration itself and the command says so. 
Otherwise set them yourself, using the IDs printed above:

```bash
sudo rag-cli.rag set --package knowledge.model.embedding=<embedding-model-id>
sudo rag-cli.rag set --package knowledge.model.rerank=<rerank-model-id>
```

Check what the engine will use with `rag-cli.rag get knowledge.model`.


### 2. Create a knowledge base

```bash
rag-cli.rag k create default                                            # just once
rag-cli.rag k ingest default <source-id> --file <path-to-local-file>    # as many as needed
```

Now you can chat!
```bash
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
