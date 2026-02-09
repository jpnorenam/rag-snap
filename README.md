# RAG Snap

A CLI based client that 

## Quick start

### Prerequisites
Before starting, the RAG snap depends on OpenSearch and Inference snaps:

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

Ensure you are using the right engine for better performance:
```bash
sudo <inference-snap> show-engine
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

sudo rag set --package chat.http.host="127.0.0.1"
sudo rag set --package chat.http.port="8324"
sudo rag set --package chat.http.path="v1"
sudo rag set --package knowledge.http.host="127.0.0.1"
sudo rag set --package knowledge.http.port="9200"
sudo rag set --package knowledge.http.tls="true"
sudo rag set --package tika.http.path="tika"
sudo rag set --package tika.http.port="9998"
sudo rag set --package tika.http.host="127.0.0.1"

export OPENSEARCH_USERNAME="admin"
export OPENSEARCH_PASSWORD="admin"

sudo snap start rag.tika-server
```

---
**Note:** Instead of the local Inference snap, it is also possible to use an external inference API.

*Warning:* If you are using a third-party inference API be aware of not sending confidential information as part of the provided context or prompt.

For example, using the AWS Bedrock inference service:
```bash
sudo rag set --package chat.http.host="bedrock-runtime.us-east-2.amazonaws.com"
sudo rag set --package chat.http.port="443"
sudo rag set --package chat.http.tls="true"
sudo rag set --package chat.http.path="openai/v1"

export CHAT_API_KEY="bedrock-api-key-****"

rag chat mistral.mistral-large-3-675b-instruct
```

## Usage