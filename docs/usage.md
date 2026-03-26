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
| `knowledge ingest --batch <config.yaml>` | Ingest multiple documents from a YAML config file |
| `knowledge search <query>` | Semantic + lexical search across one or more bases |
| `knowledge metadata <name> <source-id>` | Show metadata for an ingested source |
| `knowledge forget <name> <source-id>` | Remove a source and all its chunks |
| `knowledge delete <name>` | Delete an entire knowledge base |
| `knowledge export <name>` | Back up a knowledge base to a directory or `.tar.gz` archive |
| `knowledge import [name]` | Restore a knowledge base from an export directory or archive |

---

### Typical workflow

```
1. knowledge init          # once per OpenSearch cluster
2. knowledge create <name> # once per topic / project
3. knowledge ingest …          # repeat for each document
4. knowledge ingest --batch <yaml>  # or ingest many at once from a YAML file
5. knowledge search …      # ad-hoc or used by chat
6. knowledge forget …      # when a source is outdated
7. knowledge delete …      # when a whole base is no longer needed
8. knowledge export …      # back up a base before migration or deletion
9. knowledge import …      # restore a base from a backup
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
| `--file` | `-f` | one of three | Local file path (PDF, HTML, plain text, …) |
| `--url` | `-u` | one of three | URL of a static HTML page to fetch and extract |
| `--batch` | `-B` | one of three | YAML batch config file — ingest multiple documents at once |
| `--force` | | No | Re-ingest the source even if it is already recorded as `completed` |

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

### `knowledge ingest --batch`

Ingest multiple documents in a single command using a YAML configuration file. Each job is
processed sequentially; a failure on one job is reported and skipped — the remaining jobs
continue.

Supported job types: local files, static web pages, GitHub repositories, and Gitea (Opendev)
repositories. Repository jobs walk the entire tree and ingest every file that matches the
configured extensions and optional path filter.

```
rag knowledge ingest --batch <config.yaml> [--force]
```

| Flag | Default | Description |
|---|---|---|
| `--force` | `false` | Re-ingest sources that are already present in the knowledge base. By default, any source whose `source_id` is already recorded with status `completed` is skipped silently. Pass `--force` to override this and re-ingest regardless. |

> **Default deduplication behaviour:** Each source is identified by its `source_id` (the file path,
> URL, or repository file path). On every run the system checks whether that ID is already marked
> as `completed` in the metadata index. If it is, the source is skipped and a message is printed.
> This makes repeated runs of the same batch file safe — only new or previously-failed sources are
> ingested. Use `--force` when you want to refresh content that has changed since the last ingest.

#### YAML schema

```yaml
version: "1.0"
jobs:
  - type: file | url | github-repo | gitea-repo
    source: <path, URL, or repo identifier>
    name: <source_id>         # optional; defaults to filename or path within the repo
    target_kb: <name>         # optional; defaults to "default"
    branch: <branch-name>     # github-repo / gitea-repo only — defaults to the repo's default branch
    path: <subdir>            # github-repo / gitea-repo only — restrict to a subdirectory
    extensions:               # github-repo / gitea-repo only — file extensions to include
      - .md
      - .txt
```

| Field | Applies to | Required | Description |
|---|---|---|---|
| `type` | all | Yes | Job type: `file`, `url`, `github-repo`, or `gitea-repo` |
| `source` | all | Yes | For `file`: absolute or relative path. For `url`: `https://` URL. For `github-repo`: `"owner/repo"` or `"https://github.com/owner/repo"`. For `gitea-repo`: full URL `"https://{host}/{owner}/{repo}"`. |
| `name` | all | No | Source identifier used in `metadata`, `forget`, and search results. Defaults to the filename (for `file`/`url`) or file path within the repo (for repository jobs). Must be unique across the cluster. |
| `target_kb` | all | No | Knowledge base name. Defaults to `default`. The base must already exist (`knowledge create`). |
| `branch` | repo types | No | Branch to read from. Defaults to the repository's default branch. |
| `path` | repo types | No | Restrict ingestion to files under this subdirectory (e.g. `docs/`). Omit to process the entire repository. |
| `extensions` | repo types | Yes* | List of file extensions to ingest (e.g. `.md`, `.rst`, `.txt`). At least one extension is required — files that do not match are skipped. |

**Example config — all four job types**

```yaml
version: "1.0"
jobs:
  # Local file
  - type: file
    source: "/home/user/docs/api-reference.pdf"
    name: "api-reference"
    target_kb: "project-docs"

  # Static web page
  - type: url
    source: "https://example.com/blog/release-notes"
    name: "release-notes"
    target_kb: "project-docs"

  # GitHub repository — ingest only Markdown files under the docs/ subdirectory
  # from a specific branch; requires GITHUB_TOKEN for private repos
  - type: github-repo
    source: "https://github.com/canonical/snapcraft"
    branch: "main"
    path: "docs/"
    extensions:
      - .md
    target_kb: "project-docs"

  # Gitea / Opendev repository — ingest reStructuredText and Markdown files
  # from the entire default branch; requires GITEA_TOKEN for private instances
  - type: gitea-repo
    source: "https://opendev.org/openstack/nova"
    path: "doc/source/"
    extensions:
      - .rst
      - .md
    target_kb: "openstack-docs"
```

**Example — run a batch**

```bash
$ rag knowledge ingest --batch ~/docs/batch.yaml

Found 4 jobs in batch file version 1.0
[1/4] Processing: /home/user/docs/api-reference.pdf
✅ Success: /home/user/docs/api-reference.pdf
[2/4] Processing: https://example.com/blog/release-notes
✅ Success: https://example.com/blog/release-notes
[3/4] Processing: https://github.com/canonical/snapcraft
Found 42 files in canonical/snapcraft
  [1/42] docs/explanation/bases.md
  [2/42] docs/explanation/architectures.md
  …
✅ Success: https://github.com/canonical/snapcraft
[4/4] Processing: https://opendev.org/openstack/nova
Found 118 files in openstack/nova
  [1/118] doc/source/install/index.rst
  …
✅ Success: https://opendev.org/openstack/nova
```

> **Note on errors:** A failed job prints the reason and moves on to the next job. Run
> `knowledge list --sources` after the batch to verify which sources were successfully indexed.

> **Note on authentication:** Set `GITHUB_TOKEN` before running a batch that includes private
> GitHub repositories. For private Gitea instances set `GITEA_TOKEN` instead. Public repositories
> do not require a token but setting one raises the API rate limit.

> **Note on URLs:** The same restriction as `knowledge ingest --url` applies — pages that require
> JavaScript to render will fail. Save the rendered HTML locally and use `type: file` instead.

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

### `knowledge export`

Back up a knowledge base — all document chunks (with their pre-computed embeddings), the index
mapping, and source metadata — to a local directory or a compressed `.tar.gz` archive.
The export uses [elasticdump](https://github.com/ElasticTools/elasticdump) bundled in the snap and
preserves the embeddings so that an import requires no re-embedding.

```
rag knowledge export <knowledge_base_name> [--output <path>] [--compress]
```

| Flag | Short | Default | Description |
|---|---|---|---|
| `--output` | `-o` | `./<name>-export` | Output directory (or archive base name when used with `--compress`) |
| `--compress` | `-c` | `false` | Produce a `.tar.gz` archive and remove the intermediate directory |

The output directory (or archive) contains four files:

| File | Contents |
|---|---|
| `data.json` | All document chunks with embeddings (NDJSON, elasticdump format) |
| `mapping.json` | Index mapping (NDJSON, elasticdump format) |
| `sources.json` | Source metadata records (NDJSON, elasticdump format) |
| `manifest.json` | Export summary: knowledge base name, index name, timestamp, source count, chunk count |

A missing `manifest.json` indicates an incomplete export. `knowledge import` will reject it.

**Example — export to a directory**

```bash
$ rag knowledge export project-docs
Exporting document data to ./project-docs-export/data.json...
Exporting mapping to ./project-docs-export/mapping.json...
Exporting source metadata to ./project-docs-export/sources.json...

Export complete.
  Sources:  3
  Chunks:   228
  Location: ./project-docs-export
```

**Example — export to a compressed archive**

```bash
$ rag knowledge export project-docs --compress
Exporting document data to ./project-docs-export/data.json...
Exporting mapping to ./project-docs-export/mapping.json...
Exporting source metadata to ./project-docs-export/sources.json...
Compressing to ./project-docs-export.tar.gz...

Export complete.
  Sources:  3
  Chunks:   228
  Location: ./project-docs-export.tar.gz
```

**Example — custom output path**

```bash
rag knowledge export project-docs --output /mnt/backups/project-docs --compress
# → /mnt/backups/project-docs.tar.gz
```

---

### `knowledge import`

Restore a knowledge base from an export directory or a `.tar.gz` archive produced by
`knowledge export`. Pre-computed embeddings are imported as-is, so no re-embedding step is
needed and no ML models need to be running during import.

If a knowledge base name is omitted, the name stored in the export manifest is used automatically.
Provide a name to restore under a different name (e.g. to clone or migrate a base).

```
rag knowledge import [knowledge_base_name] --input <path> [--force]
```

| Flag | Short | Required | Description |
|---|---|---|---|
| `--input` | `-i` | Yes | Path to the export directory or `.tar.gz` archive |
| `--force` | | No | Overwrite even if the target index already contains documents |

The input format is detected automatically:

- **Directory** — used directly as the export root.
- **`.tar.gz` file** — extracted into a temporary directory, imported, then cleaned up.

**Example — restore using the original name (from manifest)**

```bash
$ rag knowledge import --input ./project-docs-export.tar.gz
Extracting ./project-docs-export.tar.gz...
Using knowledge base name from manifest: "project-docs"
Importing mapping...
Importing document data...
Importing source metadata...

Import complete.
  Sources imported: 3
  Chunks expected:  228 (from manifest)
```

**Example — restore under a different name**

```bash
rag knowledge import project-docs-staging --input ./project-docs-export.tar.gz
```

**Example — overwrite an existing index**

```bash
rag knowledge import project-docs --input ./project-docs-export --force
```

> **Note on infrastructure:** `knowledge import` automatically ensures the index template and
> sources metadata index exist before importing. You do not need to run `knowledge init` first,
> but the ML models and pipelines set up by `init` are required if you later ingest new documents
> into the restored base.

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

# 3. Ingest documents (one at a time, or all at once with a batch file)
rag knowledge ingest project-docs design-doc   --file ~/docs/design.pdf
rag knowledge ingest project-docs api-ref      --file ~/docs/api-reference.html
rag knowledge ingest project-docs release-blog \
    --url https://example.com/blog/v2-release
# alternatively:
rag knowledge ingest --batch ~/docs/project-docs-batch.yaml

# 4. Verify what was ingested
rag knowledge list project-docs --sources

# 5. Search
rag knowledge search "authentication flow" --bases project-docs

# 6. Update a document that has changed
rag knowledge forget project-docs design-doc
rag knowledge ingest project-docs design-doc --file ~/docs/design-v2.pdf

# 7. Back up before migration or deletion
rag knowledge export project-docs --compress
# → ./project-docs-export.tar.gz

# 8. Restore on another machine (or under a new name)
rag knowledge import --input ./project-docs-export.tar.gz
rag knowledge import project-docs-v2 --input ./project-docs-export.tar.gz

# 9. Clean up when the project is archived
rag knowledge delete project-docs
```

---

## Chat

The `chat` command (alias `c`) opens an interactive REPL that sends your prompts to the inference
server and, when a knowledge base is available, automatically retrieves relevant context before
each answer (RAG).

### Sub-commands at a glance

| Command | Description |
|---|---|
| `chat [model]` | Open an interactive RAG chat session |

---

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

## Answer

The `answer` command (alias `a`) runs questions through the RAG+LLM pipeline non-interactively
and exports the results to a JSON file. Use it for batch Q&A workflows such as RFP responses,
compliance questionnaires, and documentation audits.

### Sub-commands at a glance

| Command | Description |
|---|---|
| `answer batch <manifest.yaml>` | Run questions from a YAML file and export answers to JSON |

---

### `answer batch`

Run a list of questions from a YAML manifest through the RAG+LLM pipeline non-interactively.
Each question is answered in sequence, printed to the terminal, and the full set of results is
written to a timestamped JSON file in the current working directory.

```
rag answer batch <manifest.yaml>
```

#### YAML schema

```yaml
version: "1.0"
model: <model_id>             # optional; inherits from config or auto-detected from server
knowledge_bases:              # optional; defaults to the default knowledge base
  - <name>
prompt: <system_prompt>       # optional; overrides the default RAG system prompt for the whole batch
questions:
  - id: <identifier>          # optional; included in the output file for traceability
    question: <text>
```

| Field | Required | Description |
|---|---|---|
| `version` | Yes | Schema version. Use `"1.0"`. |
| `model` | No | LLM model identifier. Falls back to the `chat.model` config value, then to server auto-detection. |
| `knowledge_bases` | No | List of knowledge base names to search for context. Defaults to the `default` base. |
| `prompt` | No | Custom system prompt for the entire batch. Overrides the built-in RAG answer prompt. Useful for adding domain-specific context or tone instructions. |
| `questions[].id` | No | Identifier for the question, used in the output JSON for traceability. |
| `questions[].question` | Yes | The question text sent to the LLM. |

**Example manifest**

```yaml
version: "1.0"
knowledge_bases:
  - project-docs
prompt: |
  You are a compliance expert. Answer each question concisely and cite the relevant policy section.
questions:
  - id: "security-policy"
    question: "What is the data retention policy?"

  - id: "sla"
    question: "What are the service level objectives?"

  - id: "compliance"
    question: "Which compliance certifications does the product hold?"
```

**Example — run a batch**

```bash
$ rag answer batch ~/rfp/questions.yaml

Found 3 questions in batch manifest version 1.0
[1/3] Question: What is the data retention policy?
Answer: Data is retained for 90 days by default, configurable up to 7 years for compliance tiers.
---
[2/3] Question: What are the service level objectives?
Answer: The product targets 99.9% uptime with a 4-hour RTO and 1-hour RPO for business-critical tiers.
---
[3/3] Question: Which compliance certifications does the product hold?
Answer: The product holds SOC 2 Type II, ISO 27001, and FedRAMP Moderate certifications.
---

Results saved to batch-results-20250225-143022.json
```

**Output file format**

Results are written to `batch-results-YYYYMMDD-HHMMSS.json` in the current working directory:

```json
{
  "generated_at": "2025-02-25T14:30:22Z",
  "model": "mistral.mistral-large-3-675b-instruct",
  "results": [
    {
      "id": "security-policy",
      "question": "What is the data retention policy?",
      "answer": "Data is retained for 90 days by default, configurable up to 7 years for compliance tiers."
    }
  ]
}
```

> **Note on errors:** A question that fails (e.g. the inference server is unreachable mid-run)
> prints the error and moves on. All answers collected before the failure are still written to the
> output file.

> **Note on model selection:** If the inference server does not support model auto-detection
> (e.g. AWS Bedrock), set the model explicitly in the manifest or configure a default with
> `sudo rag set --package chat.model="<model-id>"` once after installation.

---
