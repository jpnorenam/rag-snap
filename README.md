# RAG Snap

A CLI based client that 

## Quick start

### Prerequisites
Before starting, the RAG snap depends on OpenSearch and Inference snaps:

#### Install a [Inference snap](https://github.com/canonical/inference-snaps) of your selection.

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

### Installation

```bash
## Once uploaded into snapstore
# sudo snap rag

sudo snap install --dangerous ./rag_*.snap
```
