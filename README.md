# RAG Snap

A CLI based client that 

## Quick start

### Prerequisites
Before starting, the RAG snap depends on OpenSearch and Inference snaps:

1. Install a [Inference snap](https://github.com/canonical/inference-snaps) of your selection.

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

2. Install and setup the [OpenSearch snap](https://github.com/canonical/opensearch-snap).

Test your Inference snap installation:
```bash
curl -u admin:admin -k https://localhost:9200/_cluster/health?pretty
```

### Installation

```bash
sudo snap rag
```

