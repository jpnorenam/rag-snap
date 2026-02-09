# RAG Snap

A CLI based RAG implementation to locally manage knowledge bases and chat with them.

## Quick start

### Prerequisites
Before starting, the RAG snap depends on the OpenSearch snap, and optionally, one of the Inference snaps:

#### Install and setup the [OpenSearch snap](https://github.com/canonical/opensearch-snap).

During the [creation of the certificates](https://github.com/canonical/opensearch-snap?tab=readme-ov-file#creating-certificates), ensure that the `ingest` and `ml` roles are set in the node.

``` bash
sudo snap run opensearch.setup                  \
    --node-name vdb0                            \
    --node-roles cluster_manager,data,ingest,ml \
    --tls-priv-key-root-pass root1234           \
    --tls-priv-key-admin-pass admin1234         \
    --tls-priv-key-node-pass node1234           \
    --tls-init-setup yes
```

Validate your OpenSearch snap node roles:
```bash
curl -k -u admin:admin https://localhost:9200/_cat/nodes?v
```

Increase the JVM heap size to fit the sentence embedding and cross-encoder models.
```bash
# Todo: Which is the right way to this?
echo '-Xms4g' | sudo tee /var/snap/opensearch/current/etc/opensearch/jvm.options.d/heap.options
echo '-Xmx8g' | sudo tee -a /var/snap/opensearch/current/etc/opensearch/jvm.options.d/heap.options

sudo snap restart opensearch
```

#### (Recommended) Install a [Inference snap](https://github.com/canonical/inference-snaps) of your selection.

Ensure you are using the right engine avilable in your machine for better performance:
```bash
sudo <inference-snap-name> show-engine
```

Test your Inference snap installation:
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

### Installation

```bash
## Once published into Snapstore
# sudo snap rag

sudo snap install --dangerous ./rag_*.snap
```

### Package setup

The package supports a set of configurations for it's different features:
```bash
sudo rag set --package chat.http.host="127.0.0.1"
sudo rag set --package chat.http.port="8324"
sudo rag set --package chat.http.path="v1"
sudo rag set --package knowledge.http.host="127.0.0.1"
sudo rag set --package knowledge.http.port="9200"
sudo rag set --package knowledge.http.tls="true"
sudo rag set --package tika.http.path="tika"
sudo rag set --package tika.http.port="9998"
sudo rag set --package tika.http.host="127.0.0.1"
```

The optional secrets like `OPENSEARCH_USERNAME`, `OPENSEARCH_PASSWORD`, and `CHAT_API_KEY` are provided via enviroment variables:
```bash
export OPENSEARCH_USERNAME="admin"
export OPENSEARCH_PASSWORD="admin"
```

The snap manages the tika-server service. To start it run:
```bash
sudo snap start rag.tika-server
```

The status of the snap can be checked with `rag status`.


## Usage

### Intialize your Knowledge Base, Pipelines and Models.

```bash
rag knowledge init
```

It will print the the models id so you can add them to the package config
```bash
sudo rag set --package knowledge.model.embedding=<embedding-model-id>
sudo rag set --package knowledge.model.rerank=<rerank-model-id>
```

### Manage your Knowledge Bases

The initialization creates a `default` knowledge base, however your knowledge bases can be separated into contexts that later can be activated.

Create an `example` knowledge base:
```bash
rag k create example
```

List the knowledge bases with `rag k list`.

Ingest files into the `example` and `default`:
```bash
rag k ingest example <path-to-local-file-a> <source-id-file-a>

rag k ingest default <path-to-local-file-b> <source-id-file-b>
```

Currently, only supports local files. Urls and GSuite docs are planned.

List the added sources with `rag k list -s`.


### Chat with your Knowledge Bases

Start a new conversation:
```bash
rag chat
```

Activate the relevant knowledge bases for your conversation and ask questions:
```bash
Using inference server at http://127.0.0.1:8324/v1
Using the `default` knowledge base at https://127.0.0.1:9200
	> Use `/use-knowledge` to see other available knowledge bases

Type your prompt, then ENTER to submit. CTRL-C to quit.
» /use-knowledge 
┃ Select active knowledge bases
┃   • default (27 docs, 671.1kb)
┃ > ✓ example (102 docs, 1.2mb)
x toggle • ↑ up • ↓ down • / filter • enter submit • ctrl+a select all

» This a relevant question that can be answered from the example knowledge base ... ?
```

---
**Note:** Additionally to the Inference snap, it is also possible to use an external OpenAI-compatible inference API.
For example, using the AWS Bedrock inference service:
```bash
sudo rag set --package chat.http.host="bedrock-runtime.us-east-2.amazonaws.com"
sudo rag set --package chat.http.port="443"
sudo rag set --package chat.http.tls="true"
sudo rag set --package chat.http.path="openai/v1"

export CHAT_API_KEY="bedrock-api-key-****"

rag chat mistral.mistral-large-3-675b-instruct
```

*Warning:* If you are using a third-party inference API be aware of not sending confidential information as part of the provided context or prompt.

