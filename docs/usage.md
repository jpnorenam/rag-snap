# RAG Snap Usage Guide

## Knowledge base management

The `knowledge` command (alias `k`) manages the OpenSearch-backed knowledge bases used for
Retrieval-Augmented Generation. A **knowledge base** is an OpenSearch index that holds chunked,
vector-embedded documents. You can maintain multiple independent bases and search across them.

### Prerequisites

The OpenSearch snap must be running and reachable. Run `rag status` to verify connectivity before
using any `knowledge` sub-command.

---

### Sub-commands at a glance

| Command | Description |
|---|---|
| `knowledge init` | Create ingest/search pipelines and the shared index template |
| `knowledge list` | List knowledge bases (indexes) |
| `knowledge list --sources` | List ingested source documents |
| `knowledge create <name>` | Create a new knowledge base |
| `knowledge ingest <name> <source-id>` | Ingest a document into a knowledge base |
| `knowledge search <query>` | Semantic + lexical search across one or more bases |
| `knowledge metadata <name> <source-id>` | Show metadata for an ingested source |
| `knowledge forget <name> <source-id>` | Remove a source and all its chunks |
| `knowledge delete <name>` | Delete an entire knowledge base |

---

### Typical workflow

```
1. knowledge init          # once per OpenSearch cluster
2. knowledge create <name> # once per topic / project
3. knowledge ingest …      # repeat for each document
4. knowledge search …      # ad-hoc or used by chat
5. knowledge forget …      # when a source is outdated
6. knowledge delete …      # when a whole base is no longer needed
```

---

### `knowledge init`

Registers the ML models and creates the ingest and search pipelines that every knowledge base
relies on. Run this **once** after the snap is installed or after OpenSearch is reset.

```
rag knowledge init [--sentence-transformer <model>] [--cross-encoder <model>]
```

| Flag | Short | Default | Description |
|---|---|---|---|
| `--sentence-transformer` | `-s` | _(cluster default)_ | Sentence transformer model name |
| `--cross-encoder` | `-c` | _(cluster default)_ | Cross-encoder re-ranking model name |

**Example**

```bash
rag knowledge init
```

---

### `knowledge list`

List all knowledge bases, or list the source documents within a base.

```
rag knowledge list [index_name] [--sources]
```

| Flag | Short | Default | Description |
|---|---|---|---|
| `--sources` | `-s` | `false` | List ingested sources instead of indexes |

**Example — list all knowledge bases**

```bash
$ rag knowledge list

KNOWLEDGE BASE                 HEALTH     STATUS     DOCS         SIZE
docs                           green      open       142          4.3mb
wiki-rag                       green      open       318          9.1mb
```

**Example — list sources within a specific base**

```bash
$ rag knowledge list docs --sources

SOURCE ID                                          KNOWLEDGE BASE                 STATUS       CHUNKS   INGESTED AT
snap-docs                                          docs                           completed    142      2025-06-01T10:00:00Z
```

**Example — list sources across all bases**

```bash
$ rag knowledge list --sources

SOURCE ID                                          KNOWLEDGE BASE                 STATUS       CHUNKS   INGESTED AT
snap-docs                                          docs                           completed    142      2025-06-01T10:00:00Z
wiki-rag                                           wiki-rag                       completed    318      2025-06-02T14:22:10Z
```

---

### `knowledge create`

Create a new, empty knowledge base index.

```
rag knowledge create <knowledge_base_name>
```

The name must be a short identifier (letters, numbers, hyphens). It is used as a suffix for the
underlying OpenSearch index name.

**Example**

```bash
$ rag knowledge create docs
Knowledge base 'docs' created successfully.
```

---

### `knowledge ingest`

Ingest a document into a knowledge base. The document is parsed, converted to Markdown, split into
overlapping chunks, embedded, and stored in OpenSearch. Provide the document either as a local file
or a URL.

```
rag knowledge ingest <knowledge_base_name> <source_id> (--file <path> | --url <url>)
```

| Flag | Short | Required | Description |
|---|---|---|---|
| `--file` | `-f` | one of the two | Local file path (PDF, HTML, plain text, …) |
| `--url` | `-u` | one of the two | URL of a static HTML page to fetch and extract |

`<source_id>` is a human-readable identifier you choose (e.g. `snap-docs`, `rag-wiki`). It is used
to reference the source in `metadata`, `forget`, and search results. It must be unique within the
cluster.

**Example — ingest a local PDF**

```bash
$ rag knowledge ingest docs snap-docs --file ~/Downloads/snapcraft-docs.pdf
Ingested 89 chunks into index 'rag-kb-docs'
```

**Example — ingest a web page**

```bash
$ rag knowledge ingest wiki-rag rag-wiki \
    --url https://en.wikipedia.org/wiki/Retrieval-augmented_generation
Ingested 37 chunks into index 'rag-kb-wiki-rag'
```

> **Note on JavaScript-heavy pages:** `--url` fetches and extracts static HTML. Pages that render
> their content entirely in JavaScript (SPAs) will produce an error with a suggestion to save the
> rendered page locally and use `--file` instead.

---

### `knowledge search`

Run a hybrid semantic + lexical search across one or more knowledge bases.

```
rag knowledge search <query> [--bases <name,...>] [--top <k>]
```

| Flag | Short | Default | Description |
|---|---|---|---|
| `--bases` | `-b` | `default` | Comma-separated list of knowledge base names to search |
| `--top` | `-k` | `10` | Maximum number of results returned per index |

**Example — search the default base**

```bash
$ rag knowledge search "how does vector search work"

--- Result 1 (score: 0.9821, index: rag-kb-default) ---
  Source: rag-wiki
  Date:   2025-06-02T14:22:10Z
  Vector search (also called semantic search) finds documents by comparing high-dimensional …

Total: 10 results
```

**Example — search across multiple bases**

```bash
$ rag knowledge search "snap confinement" --bases docs,wiki-rag --top 5
```

---

### `knowledge metadata`

Show the stored metadata record for a specific ingested source.

```
rag knowledge metadata <knowledge_base_name> <source_id>
```

**Example**

```bash
$ rag knowledge metadata docs snap-docs

Source ID:      snap-docs
Knowledge base: docs
Status:         completed
File name:      snapcraft-docs.pdf
File path:      /home/user/Downloads/snapcraft-docs.pdf
Content type:   application/pdf
Content length: 1048576 bytes
Checksum:       a3f1c2…
Chunks:         89 (size=512, overlap=64)
Ingested at:    2025-06-01T10:00:00Z
Updated at:     2025-06-01T10:00:42Z
Title:          Snapcraft Documentation
Author:         Canonical
Language:       en
```

---

### `knowledge forget`

Remove a single source document and all its chunks from a knowledge base. The source metadata
record is also deleted. Use this to replace outdated content: forget the old source, then ingest
the updated file under the same `source_id`.

```
rag knowledge forget <knowledge_base_name> <source_id>
```

**Example**

```bash
$ rag knowledge forget docs snap-docs
Deleted 89 chunks and metadata for source 'snap-docs' from index 'rag-kb-docs'
```

**Refresh a source with updated content**

```bash
rag knowledge forget docs snap-docs
rag knowledge ingest docs snap-docs --file ~/Downloads/snapcraft-docs-v2.pdf
```

---

### `knowledge delete`

Delete an entire knowledge base index and all associated source metadata. This operation is
**irreversible**. You will be prompted to type the knowledge base name to confirm.

```
rag knowledge delete <knowledge_base_name>
```

**Example**

```bash
$ rag knowledge delete docs

The following 2 source(s) will be permanently deleted:

  SOURCE ID                                          STATUS       CHUNKS   INGESTED AT
  snap-docs                                          completed    89       2025-06-01T10:00:00Z
  contributing-guide                                 completed    12       2025-06-03T09:15:00Z

This will permanently delete the index 'rag-kb-docs' and all its data.
Type the knowledge base name to confirm: docs
Deleted index 'rag-kb-docs' and 2 source metadata record(s).
```

---

### End-to-end example

```bash
# 1. Initialise pipelines (once)
rag knowledge init

# 2. Create a knowledge base for project documentation
rag knowledge create project-docs

# 3. Ingest documents
rag knowledge ingest project-docs design-doc   --file ~/docs/design.pdf
rag knowledge ingest project-docs api-ref      --file ~/docs/api-reference.html
rag knowledge ingest project-docs release-blog \
    --url https://example.com/blog/v2-release

# 4. Verify what was ingested
rag knowledge list project-docs --sources

# 5. Search
rag knowledge search "authentication flow" --bases project-docs

# 6. Update a document that has changed
rag knowledge forget project-docs design-doc
rag knowledge ingest project-docs design-doc --file ~/docs/design-v2.pdf

# 7. Clean up when the project is archived
rag knowledge delete project-docs
```

---

## Chat

The `chat` command (alias `c`) opens an interactive REPL that sends your prompts to the inference
server and, when a knowledge base is available, automatically retrieves relevant context before
each answer (RAG).

### Starting a session

```
rag chat [model_name]
```

| Argument | Required | Description |
|---|---|---|
| `model_name` | No | LLM model identifier. Auto-detected from the inference server when omitted. |

**Example — auto-detect model**

```bash
rag chat
```

**Example — choose a specific model**

```bash
rag chat deepseek-r1:8b
```

If the inference server requires authentication, set the `CHAT_API_KEY` environment variable before
starting:

```bash
CHAT_API_KEY=sk-… rag chat
```

The client waits up to 60 seconds for the model to finish loading before giving up.

---

### The REPL

```
Using inference server at http://localhost:8324
Using the `default` knowledge base at http://localhost:9200
  > Use `/use-knowledge` to see other available knowledge bases

Type your prompt, then ENTER to submit. CTRL-C to quit.
»
```

| Action | Effect |
|---|---|
| Type a prompt, press Enter | Send to the LLM (with RAG context if available) |
| `Tab` | Autocomplete slash commands |
| `Ctrl-C` (empty line) | Exit the session |
| `Ctrl-C` (mid-prompt) | Cancel current input, stay in session |
| Type `exit`, press Enter | Exit the session |

Input history is available within the session via the Up/Down arrow keys.

---

### Slash commands

Slash commands are processed locally and never sent to the LLM. Start typing `/` and use Tab to
autocomplete.

#### `/use-knowledge`

Opens an interactive multi-select menu to choose which knowledge bases are active for the rest of
the session. Changes take effect on the very next prompt.

```
» /use-knowledge

  Select active knowledge bases
  > [x] docs (142 docs, 4.3mb)
    [x] wiki-rag (318 docs, 9.1mb)
    [ ] scratch (5 docs, 0.1mb)
```

Use Space to toggle, Enter to confirm, Esc/Ctrl-C to keep the current selection unchanged.

---

### How RAG works in chat

Each time you send a prompt, the following happens automatically:

```
1. Query rewriting
   The last 3 turns of conversation are summarised into search keywords so
   that follow-up questions ("what about the storage layer?") retrieve the
   right chunks even without repeating context.

2. Hybrid search
   The rewritten keywords are used for a BM25 (lexical) search; the original
   prompt is used for a semantic (vector) search. Results are merged and
   ranked by relevance.

3. Context injection
   The top-10 chunks are prepended to your prompt before it is sent to the
   LLM. The model is instructed to cite sources and to explicitly say when
   the context is insufficient rather than fabricate an answer.

4. History
   Only your original prompt (not the augmented one) is saved in the
   conversation history, keeping the history clean for subsequent rewrites.
```

When no knowledge base is reachable, the client falls back to plain chat with no retrieval step.

#### Reasoning models (DeepSeek R1, QwQ, …)

Models that emit `<think>…</think>` reasoning blocks before their answer are fully supported.
Reasoning text is rendered in blue so you can distinguish thinking from the final response.
Think blocks are automatically stripped when building the search query rewrite context so they
do not pollute subsequent retrieval queries.

---

### Best practices

**Match knowledge bases to topics.** Searching across many unrelated bases dilutes relevance
scores. Create one base per project or domain and use `/use-knowledge` to activate only the
relevant ones before each conversation.

```bash
# ingest project docs into a dedicated base
rag knowledge ingest project-x api-ref --file ~/docs/api.pdf

# start chat, then switch to it
rag chat
» /use-knowledge   # select project-x
» How does the authentication flow work?
```

**Ask specific questions first.** The hybrid search works best with concrete nouns and technical
terms. Opening with "explain the whole system" pulls broad, low-scoring chunks. Narrow questions
("what ports does the snap use by default?") retrieve sharper context.

**Use follow-up questions freely.** The query rewriter carries conversation context forward, so
short follow-ups like "what about the fallback path?" correctly resolve to the topic already
established in the session. You do not need to repeat yourself.

**Trust the model's "I don't know".** The RAG prompt explicitly instructs the model to state when
the retrieved context is insufficient rather than guess. If you get that response, the relevant
content is likely not ingested yet — add it with `knowledge ingest` and try again.

**Refresh stale sources before long sessions.** Outdated chunks can lower answer quality. Before
a working session on a topic, check ingestion dates and refresh changed documents:

```bash
rag knowledge list my-base --sources   # check dates
rag knowledge forget my-base old-doc
rag knowledge ingest my-base old-doc --file ~/docs/updated.pdf
```

**Debug with `--verbose`.** Pass `-v` before the subcommand to see which model is chosen, how many
RAG chunks were retrieved, and the rewritten search keywords:

```bash
rag -v chat
```

Sample verbose output during a turn:

```
Extracting lexical keywords
Search keywords: snap confinement interfaces plugs slots
Retrieved 8 results from knowledge base
```

---

### Example session

```
$ rag chat

Using inference server at http://localhost:8324
Using the `default` knowledge base at http://localhost:9200
  > Use `/use-knowledge` to see other available knowledge bases

Type your prompt, then ENTER to submit. CTRL-C to quit.

» /use-knowledge
  [x] snap-docs  (89 docs, 2.1mb)

» What interfaces are required for network access in a strict snap?

  Based on the snapcraft documentation, a strictly confined snap needs the
  `network` plug to make outbound connections and `network-bind` to open
  listening sockets. Declare them under `plugs:` in snapcraft.yaml:

    plugs:
      network:
      network-bind:

  (source: snap-docs, score: 0.9743)

» And for hardware serial ports?

  For serial port access the snap needs the `serial-port` interface, which
  requires manual connection by the user:

    sudo snap connect mysnap:serial-port

  (source: snap-docs, score: 0.9512)

» exit
Closing chat
```

---