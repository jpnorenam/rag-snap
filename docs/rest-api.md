# REST API (`ragd`)

`ragd` is an optional long-running daemon that exposes the same knowledge-management,
search, chat, and batch-answering capabilities over a **versioned REST API on a local unix
socket**. It owns the long-lived OpenSearch, inference, and Tika clients and holds the
backend secrets, so any local program — including the `rag` CLI itself — can drive the RAG
stack without rebuilding clients or handling credentials.

The surface is deliberately small: a unix socket (access gated by socket group membership),
an **opt-in loopback TCP listener** for local clients that cannot dial a unix socket (gated by
a per-installation bearer token), async operations with a progress events websocket, and an
auto-generated OpenAPI specification. Remote HTTPS access is intentionally **not** part of this
surface — the loopback listener binds `127.0.0.1` only and refuses any non-loopback address.

- [Security model](#security-model)
- [Service management](#service-management)
- [Socket configuration](#socket-configuration)
- [Loopback (local REST API over TCP)](#loopback-local-rest-api-over-tcp)
- [CLI integration](#cli-integration)
- [Quick start over the socket](#quick-start-over-the-socket)
- [Response envelope](#response-envelope)
- [Async operations and events](#async-operations-and-events)
- [Endpoint reference](#endpoint-reference)

---

## Security model

> **⚠️ Group membership is root-equivalent for the RAG stack.**
> A connection over the socket is granted **full, unscoped access** to every endpoint —
> there is no per-endpoint authorization. Any user in the configured access group (default
> `rag`) can create and delete knowledge bases, ingest and export data, run chat and batch
> answering, and read the daemon's effective configuration. Treat membership in this group
> as equivalent to root access over the RAG stack and grant it accordingly.

Access is decided per connection from the peer's operating-system credentials
(`SO_PEERCRED`). A peer is admitted only if its effective user is `root` or a member of the
configured access group. These credentials are stamped by the kernel and cannot be spoofed.

---

## Service management

The daemon is opt-in. Like `tika-server`, it is installed but **disabled by default** and
must be started explicitly:

```bash
# Start the daemon (and enable it across reboots)
sudo snap start --enable rag-cli.ragd

# Check status
snap services rag-cli

# Stop / restart
sudo snap stop rag-cli.ragd
sudo snap restart rag-cli.ragd

# Follow logs
sudo snap logs -f rag-cli.ragd
```

Backend secrets (`OPENSEARCH_USERNAME`, `OPENSEARCH_PASSWORD`, `CHAT_API_KEY`, and
`GEMINI_API_KEY` for RFP analysis) are set on the **daemon's** service environment, not on each CLI invocation and never in `snapctl`
config. The daemon reads the rest of its configuration (`chat.*`, `knowledge.*`, `tika.*`,
`api.socket.*`) from `snapctl` at startup.

To give the daemon an inference API key (e.g. for AWS Bedrock) and/or your real OpenSearch
credentials, inject them via a root-only systemd drop-in, then restart the daemon:

```bash
sudo mkdir -p /etc/systemd/system/snap.rag-cli.ragd.service.d
printf '[Service]\nEnvironment=CHAT_API_KEY=%s\nEnvironment=OPENSEARCH_USERNAME=%s\nEnvironment=OPENSEARCH_PASSWORD=%s\n' \
  "$YOUR_CHAT_KEY" "$YOUR_OPENSEARCH_USER" "$YOUR_OPENSEARCH_PASSWORD" | \
  sudo tee /etc/systemd/system/snap.rag-cli.ragd.service.d/10-secrets.conf >/dev/null
sudo chmod 600 /etc/systemd/system/snap.rag-cli.ragd.service.d/10-secrets.conf
sudo systemctl daemon-reload
sudo snap restart rag-cli.ragd
```

To enable RFP analysis on the daemon, add `Environment=GEMINI_API_KEY=<key>` to the same drop-in.
It is the only variable analysis reads (there is no fallback to `CHAT_API_KEY`), and the key must
belong to a billing-enabled Google project — see [`answer batch --analyze`](usage.md#configuration).

The drop-in file is `root:root 0600`, so the secrets are never world-readable and never pass
through the `snapctl` config store or the `GET /1.0` config summary. `ragd`'s app definition
declares no hardcoded `environment:` values for any of these three keys — a hardcoded value
would be reapplied by `snap run` after systemd and silently override the drop-in, so leaving
them undeclared is what lets this recipe work for a non-default OpenSearch username/password
too, not just the chat key.

> **Applying config changes:** the daemon snapshots config at startup. After changing config
> with `rag set ...`, reload the daemon to pick up the new values — either `sudo snap restart
> rag-cli.ragd`, or send it `SIGHUP` for an in-place reload (re-reads config and rebuilds the
> backend clients without dropping the socket). In-flight operations and chat sessions do not
> survive a full restart.

---

## Socket configuration

Two package-scoped config keys (user-overridable) control the socket:

| Key | Default | Purpose |
|---|---|---|
| `api.socket.group` | `rag` | Host group whose members (plus `root`) are granted access. Enforced by the daemon's `SO_PEERCRED` check, **not** by the socket's file ownership. |
| `api.socket.mode` | `0666` | Octal file mode for the socket. Defaults to world-connectable; the peercred check is the access gate. |

The daemon creates the socket at `$SNAP_COMMON/ragd/unix.socket`
(`/var/snap/rag-cli/common/ragd/unix.socket`) and `chmod`s it to `api.socket.mode`. It does
**not** change the socket's group owner: under strict confinement the snap's seccomp profile
forbids `chown`ing the socket to an arbitrary group, so the socket stays `root`-owned and
world-connectable, and **access is gated entirely by the peer-credential check** — a
connecting process is admitted only if its effective user is `root` or a member of
`api.socket.group` (resolved from the host's passwd/group databases at connect time).

> **Note:** because there is no file-permission (DAC) backstop, any local process can `connect()`
> to the socket — but an unauthenticated peer can do nothing beyond receive an immediate `403`.
> The peer credentials are stamped by the kernel and cannot be spoofed.

To grant a user access, add them to the group and start a fresh login session (or use `newgrp`)
so the new membership is visible to the peercred check:

```bash
# Override the access group if desired (must already exist as a package key)
sudo rag set api.socket.group=rag

# Add a user to the group, then start a new session so the membership takes effect.
# No daemon restart is needed — the peercred check reads group membership live.
sudo usermod -aG rag "$USER"
newgrp rag   # or log out and back in
```

A connection from a user outside the group and not `root` is rejected with HTTP `403` and a
message naming the group to join.

### Using the CLI without `sudo`

The default group, `rag`, usually doesn't exist on a fresh host, so until you act only `root`
can reach the daemon. Running the CLI as `sudo` is **not** a workaround: the snap's `home`
interface doesn't let `root` read files in another user's home directory, so any command that
takes a file from your home fails:

```
$ sudo rag-cli.rag answer batch ./prompt.yaml
Error: loading batch manifest: reading manifest file: open ./prompt.yaml: permission denied
```

Run without `sudo` and give your own user access instead. The quickest way is to point
`api.socket.group` at a group you're already in. The daemon reads this key at startup, so
restart it after the change:

```bash
sudo rag-cli.rag set --package api.socket.group=sudo   # any existing group you belong to
sudo snap restart rag-cli.ragd
rag-cli.rag -v answer batch ./prompt.yaml              # no sudo
```

To keep access narrower than `sudo`, create the dedicated group instead:
`sudo groupadd rag && sudo usermod -aG rag "$USER"`, then start a new login session.

> **Symptom of missing access:** a CLI user who can't reach the daemon falls back to **direct
> mode** without a warning. Direct mode talks to OpenSearch and the inference server itself,
> using `OPENSEARCH_USERNAME`/`OPENSEARCH_PASSWORD`/`CHAT_API_KEY` from your shell, not the
> daemon's drop-in. If those aren't exported, `chat` may work under `sudo` (via `ragd`) while
> `answer batch` without `sudo` can't reach the knowledge base. Older builds then answered every
> question with *"The provided context does not contain enough information to answer this
> question."* without calling the model. Behind a proxy you'd see only the keyword-extraction
> request and no answer request. Current builds stop with a `knowledge base not available in
> direct mode` error instead. Exporting the credentials also makes direct mode work, but routing
> through the daemon keeps the secrets out of your shell and runs the same path as chat and the UI.

---

## Loopback (local REST API over TCP)

The unix socket is unreachable by a browser and awkward for non-CLI local clients (scripts,
another process). `ragd` can **optionally** serve the exact same `/1.0` API over a loopback TCP
listener bound to `127.0.0.1`. This is off by default and controlled by two package-scoped,
user-overridable config keys:

| Key | Default | Purpose |
|---|---|---|
| `api.loopback.enabled` | `false` | Whether the daemon opens the loopback listener at all. |
| `api.loopback.address` | `127.0.0.1:0` | Loopback bind address. `:0` means an OS-assigned port, discovered at runtime. A non-loopback host (e.g. `0.0.0.0:8080` or an empty host `:8080`) is **refused** — the daemon fails to start rather than exposing the API on a LAN. |

```bash
# Enable the loopback listener and restart the daemon to pick it up.
sudo rag set api.loopback.enabled=true
sudo snap restart rag-cli.ragd
```

### Token model

Because `SO_PEERCRED` is unavailable for TCP peers, loopback requests authenticate with a
**per-installation bearer token** rather than by group membership. On first enable the daemon
generates a high-entropy (256-bit) token, persists it owner-only (`0600`) under
`$SNAP_COMMON/ragd/ui.token` so it survives restarts, and reuses it thereafter. The comparison
is constant-time.

The token value is **never read off disk by clients**. Instead the daemon returns it in the
`GET /1.0` config summary, which is already peercred-gated to `root` and access-group members
— exactly the principals the token is scoped to grant. (This mirrors why the token file is not
made group-readable: under strict confinement the daemon cannot `chown` it to a group without
crashing, so delivery over the trusted socket is used instead.)

### Discovering the port and token over the socket

A trusted unix-socket client reads both the resolved port and the token from `GET /1.0` under
`config.loopback`:

```bash
SOCK=/var/snap/rag-cli/common/ragd/unix.socket

# The config summary's loopback section carries enabled/address/url/token/token_path.
curl --unix-socket "$SOCK" http://ragd/1.0 | jq '.metadata.config.loopback'
# {
#   "enabled": true,
#   "address": "127.0.0.1:38283",
#   "url": "http://127.0.0.1:38283",
#   "token": "…64 hex chars…",
#   "token_path": "/var/snap/rag-cli/common/ragd/ui.token"
# }
```

### Calling the API over loopback

Present the token as an `Authorization: Bearer` header (a browser websocket upgrade that
cannot set headers may instead carry it as the `rag_ui_token` cookie):

```bash
SOCK=/var/snap/rag-cli/common/ragd/unix.socket
URL=$(curl -s --unix-socket "$SOCK" http://ragd/1.0 | jq -r '.metadata.config.loopback.url')
TOKEN=$(curl -s --unix-socket "$SOCK" http://ragd/1.0 | jq -r '.metadata.config.loopback.token')

# Authenticated request over the loopback port.
curl -H "Authorization: Bearer $TOKEN" "$URL/1.0"

# The same request without the token is rejected with 403.
curl -i "$URL/1.0"
```

The unix socket and its `SO_PEERCRED` gate are unchanged when the loopback listener is enabled;
the two transports share one handler set and never diverge. To turn the listener back off, `sudo
rag set api.loopback.enabled=false` and restart the daemon; the owner-only token file is inert
while disabled.

> **Inference API key:** the loopback listener serves the same RAG stack, so giving the daemon
> a chat/inference key works exactly as for the socket — via the root-only systemd drop-in in
> [Service management](#service-management), never through config or the token.

---

## CLI integration

The `rag` CLI **detects a running `ragd` automatically**: if the socket exists and the caller
is trusted, knowledge, search, chat, and `answer batch` commands route through the daemon;
otherwise they fall back to constructing backend clients directly (the original behaviour).
No flags or extra steps are required. Detection is skipped under `--debug`, which forces the
direct/offline path.

---

## Quick start over the socket

Any HTTP client that can speak to a unix socket works. With `curl`:

```bash
SOCK=/var/snap/rag-cli/common/ragd/unix.socket

# Discover the API: version, auth state, and feature extensions
curl --unix-socket "$SOCK" http://ragd/

# Server info: effective (secret-free) config summary and backend readiness
curl --unix-socket "$SOCK" http://ragd/1.0

# List knowledge bases (sync)
curl --unix-socket "$SOCK" http://ragd/1.0/knowledge

# Hybrid search (sync)
curl --unix-socket "$SOCK" -X POST http://ragd/1.0/search \
  -H 'Content-Type: application/json' \
  -d '{"bases":["project-docs"],"query":"how do I rotate credentials?"}'
```

A trusted root response reports `"auth":"trusted"`; an untrusted caller sees `"untrusted"`.

---

## Response envelope

Every JSON response is one of three shapes. Clients switch on the numeric
`status_code` / `error_code`, not the text status.

```jsonc
// sync — HTTP 200
{ "type": "sync", "status": "Success", "status_code": 200, "metadata": { ... } }

// async — HTTP 202, with Location: /1.0/operations/<uuid>
{ "type": "async", "status": "Operation created", "status_code": 100,
  "operation": "/1.0/operations/<uuid>", "metadata": { /* operation object */ } }

// error — HTTP 4xx/5xx
{ "type": "error", "error_code": 404, "error": "knowledge base not found" }
```

---

## Async operations and events

Long-running work — knowledge-engine init, ingest, export/import, and batch answering —
returns `202` with an operation URL. Poll, long-poll, or cancel it:

```bash
SOCK=/var/snap/rag-cli/common/ragd/unix.socket

# Poll an operation
curl --unix-socket "$SOCK" http://ragd/1.0/operations/<uuid>

# Long-poll until it reaches a terminal state (or N seconds elapse)
curl --unix-socket "$SOCK" "http://ragd/1.0/operations/<uuid>/wait?timeout=30"

# Cancel a cancellable operation
curl --unix-socket "$SOCK" -X DELETE http://ragd/1.0/operations/<uuid>
```

For live progress, subscribe to the events websocket (`GET /1.0/events?type=operation,logging`)
**before** launching the operation to avoid a poll race. Chat is an interactive
websocket-class operation: `POST /1.0/chat` returns an operation whose metadata carries the
websocket URL to dial for streamed tokens and `<think>` blocks.

---

## Endpoint reference

| Method & path | Kind | Purpose |
|---|---|---|
| `GET /` | sync | API discovery: version, auth state, `api_extensions` |
| `GET /1.0` | sync | Server info: config summary + backend readiness |
| `GET /1.0/operations`, `GET /1.0/operations/{id}` | sync | List / inspect operations |
| `GET /1.0/operations/{id}/wait` | sync | Long-poll an operation to completion |
| `DELETE /1.0/operations/{id}` | sync | Cancel a cancellable operation |
| `GET /1.0/operations/{id}/websocket` | ws | Chat session stream |
| `GET /1.0/events` | ws | Operation/logging event stream |
| `GET`/`POST /1.0/knowledge` | sync | List / create knowledge bases |
| `GET`/`DELETE /1.0/knowledge/{name}` | sync | Inspect / delete a base |
| `PATCH /1.0/knowledge/{name}` | sync / async | Set a base's default label (async when backfilling existing data) |
| `GET`/`DELETE /1.0/knowledge/{name}/sources[/{id}]` | sync | List / inspect / forget sources |
| `POST /1.0/knowledge/{name}/sources` | async | Ingest (file/URL/batch) |
| `POST /1.0/knowledge/{name}/export` | async | Export a base |
| `POST /1.0/knowledge/import` | async | Import a base |
| `POST /1.0/knowledge-engine` | async | Initialise models/pipelines/indexes |
| `GET /1.0/knowledge-engine/models` | sync | List registered models with state, size, and role |
| `DELETE /1.0/knowledge-engine/models/{id}` | sync | Undeploy and delete a model (`?force=true` if in use) |
| `POST /1.0/search` | sync | Hybrid search |
| `POST /1.0/chat` | async (ws) | Start an interactive chat session |
| `POST /1.0/answer/batch` | async | Run a prepared batch manifest |
| `POST /1.0/answer/build` | async | Extract questions from a document (spreadsheets return a column choice) |
| `POST /1.0/answer/build/extract` | async | Extract questions from a chosen spreadsheet column |
| `POST /1.0/answer/analyze` | async | Triage RFP documents for manifest generation |
| `POST /1.0/answer/analyze/generate` | async | Generate a complete manifest from a triage token |
| `POST /1.0/answer/check` | sync | Check a manifest without running it |
| `GET /1.0/prompts`, `GET /1.0/prompts/{name}` | sync | Read the prompt templates |
| `PUT /1.0/prompts/{name}` | sync | Customize a prompt template |
| `DELETE /1.0/prompts/{name}` | sync | Reset a prompt to its built-in default |
| `GET /1.0/status` | sync | Live service health, endpoints, and deployed models |
| `GET /1.0/config` | sync | Read the configuration with layer provenance |
| `PUT /1.0/config/{key}` | sync | Set a value in the user layer |
| `DELETE /1.0/config/{key}` | sync | Revert a key to its package value |

The full, authoritative contract is the generated [`rest-api.yaml`](../rest-api.yaml)
OpenAPI specification at the repository root.

---

## Prompt templates

Three templates drive generation: `chat_system_prompt` (interactive chat),
`answer_system_prompt` (batch answering), and `source_rules` (the grounding block appended to a
batch manifest's custom prompt). The daemon owns them, stores customizations in
`$SNAP_COMMON/ragd/prompts.json`, and seeds every chat session and batch run it starts from that
store — so a prompt saved here is what `rag-cli.rag chat`, `answer batch`, and the web UI all use.

Each prompt is returned with its effective `value`, the built-in `default` it falls back to, and a
`customized` flag, so a client can show the default for comparison and offer a meaningful reset
without a second request.

```bash
# Read all three (in the order the CLI presents them).
sudo curl -s --unix-socket /var/snap/rag-cli/common/ragd/unix.socket \
  http://localhost/1.0/prompts | jq '.metadata[] | {name, customized}'

# Customize one.
sudo curl -s --unix-socket /var/snap/rag-cli/common/ragd/unix.socket -X PUT \
  -H 'Content-Type: application/json' \
  -d '{"value": "You are a terse assistant. Answer in one sentence."}' \
  http://localhost/1.0/prompts/chat_system_prompt

# Reset it to the built-in default (idempotent).
sudo curl -s --unix-socket /var/snap/rag-cli/common/ragd/unix.socket -X DELETE \
  http://localhost/1.0/prompts/chat_system_prompt
```

Semantics worth knowing:

- **Prompts are resolved when work starts.** A chat session or batch operation keeps the prompts
  it began with; an edit applies to the *next* session or run, never to one already in flight.
- **Empty values are rejected** (400). Resetting is an explicit `DELETE`, so clearing an editor
  cannot silently discard a customization.
- **A value equal to the default clears the override**, so `customized` never lies and a future
  release's improved default is not shadowed by a stale identical copy.
- **Only overrides are stored.** A prompt you never customized always resolves to the built-in
  default of the installed release.
- Prompts are machine-global, like knowledge bases: any caller authorized to reach the API can
  change them.

---

## Status

`GET /1.0/status` probes the three external services **at request time** and reports the daemon
itself. It is the API behind the web UI's Status page and the analogue of `rag-cli.rag status`.

Each service reports a `state` of `running`, `unreachable`, or `not configured` (no endpoint
resolved — nothing is broken, nothing is configured), plus its resolved `endpoint`. Services
degrade **independently**: an unreachable OpenSearch never fails the request or hides Tika's
version. Probes run concurrently under a short timeout, so a fully-down stack answers in seconds.

```bash
sudo curl -s --unix-socket /var/snap/rag-cli/common/ragd/unix.socket \
  http://localhost/1.0/status | jq '.metadata'
```

Per-service detail:

- **`opensearch`** — the configured embedding and rerank model IDs (`models`), and the models
  OpenSearch actually has deployed (`deployed_models`, from `_plugins/_ml/models/_search` filtered
  to `model_state: DEPLOYED`). Each configured model carries a `deployed` flag. **A configured
  model ID that is not deployed breaks retrieval, and nothing else reports it until a search
  fails** — this flag is how you see it. Fix it with `rag-cli.rag knowledge init`.
- **`inference`** — the LLM model name the chat backend serves (`llm_model`). When the backend
  cannot list models (AWS Bedrock's OpenAI-compatible route, or any `chat.api=anthropic` setup),
  this is the configured `chat.model`, and with `chat.api=anthropic` health is a connection check
  only, so no tokens are spent. With no `chat.model` configured there, the entry is `unreachable`
  with guidance. Under `chat.api=anthropic`, the optional `temperature` in chat and
  answer-batch request bodies is accepted but ignored.
- **`tika`** — the Tika server version (`version`).
- **`ragd`** — the API version and the enabled listeners (unix socket, and the loopback address
  when the loopback listener is on). The localhost token is never included.

## Configuration

`GET /1.0/config` returns the effective configuration, sorted by key, each entry naming the
`layer` it resolves from: `user` when an override exists, otherwise `package`. Deprecated keys are
hidden, exactly as `rag-cli.rag get` hides them. An overridden entry also carries the
`package_value` a revert would restore.

Writes go to the **user** layer — the same override `sudo rag-cli.rag set <key>=<value>` writes —
and follow the same rules: a key that does not already exist is rejected (the API overrides keys,
it never creates them), and deprecated keys are read-only.

```bash
# Read the configuration with its layer provenance.
sudo curl -s --unix-socket /var/snap/rag-cli/common/ragd/unix.socket \
  http://localhost/1.0/config | jq '.metadata.keys[] | {key, value, layer}'

# Override a key (user layer).
sudo curl -s --unix-socket /var/snap/rag-cli/common/ragd/unix.socket -X PUT \
  -H 'Content-Type: application/json' -d '{"value": "9000"}' \
  http://localhost/1.0/config/chat.http.port

# Revert it to the packaged value.
sudo curl -s --unix-socket /var/snap/rag-cli/common/ragd/unix.socket -X DELETE \
  http://localhost/1.0/config/chat.http.port
```

**Secrets never come back out.** The service credentials (`OPENSEARCH_USERNAME`,
`OPENSEARCH_PASSWORD`, `CHAT_API_KEY`) are environment variables and are not config keys at all.
The config keys that *are* secrets — any key whose last segment is `secret`, `password`, or
`token`, today `gdrive.client.secret` — are redacted on read: the key is listed and stays
writable, but its value is replaced with `<redacted>`. They are write-only through the API.
### `POST /1.0/answer/build`

Derives a batch manifest from an uploaded RFP/RFI document. Accepts
`multipart/form-data` with a `file` field (PDF, DOCX, XLSX, or CSV) and an
optional `refine` field (`"false"` to skip the LLM refinement pass; refinement
is on by default). Runs as an async operation whose completion metadata depends
on the document kind:

- **Free-text (PDF, DOCX):** questions are extracted in one pass and published
  under the `questions` key (each `{id, question, source?}`), for interactive
  client-side review.
- **Spreadsheet/CSV (XLSX, CSV):** the question column cannot be reliably
  guessed, so the operation does **not** extract on this pass. It parses the
  document into tables and completes with a discriminated response —
  `needs_column: true`, an opaque `build_token`, the parsed `tables` (each with
  its `header` row and, per column, `sample` cells and `avg_len`), and a
  heuristic `suggested` `{table_index, column_index}`. The client picks a column
  and calls `POST /1.0/answer/build/extract`.

This endpoint does **not** persist a manifest or run the batch. Tika parses XLSX
(and PDF/DOCX); CSV is parsed directly. Advertised via the `answer_build` entry
in `api_extensions`; the two-pass spreadsheet flow via `answer_build_columns`.

### `POST /1.0/answer/build/extract`

Extracts questions from one column of a table parsed and staged by a prior
`POST /1.0/answer/build` call — no re-upload, no re-parse. JSON body:
`{build_token, table_index, column_index, id_column_index?, min_length?, refine?}`
(`id_column_index` defaults to `-1` = auto-number; `min_length` defaults to 20).
Runs as an async operation and publishes the questions under the `questions` key
— the same shape a free-text build produces. An unknown or expired `build_token`
returns `400` (re-upload the document); a column that yields no questions after
the `min_length` filter fails the operation with a column-naming message so the
client can pick a different column or lower the minimum without starting a batch
run.

### `POST /1.0/answer/analyze`

First of the two RFP-analysis passes. `multipart/form-data`: `file` is the RFP, and any
number of repeated `context` fields are supporting documents. An optional `instructions`
field carries the operator's free-text instructions (granularity, scope, which questions to
include). They are applied to every analysis call for the session, including generation,
without being sent again. Over 4,000 characters is a `400` before any operation starts.
Runs as an async operation and publishes `analysis_token`, `expires_at`, `model`, `triage`,
`report` and `instructions` (the instructions the session is working to, empty when none).

`triage` is what the model found without generating anything: the RFP's identity, its
section structure with the id prefix each section uses, an estimated question count,
`openstack_in_scope`, and `multi_field_answers`. `report` says per document whether it was
read `native`ly (a PDF, as bytes) or `converted` — no model API accepts a spreadsheet or a
word-processor document, so those become table-preserving Markdown first, and a client
should show which happened.

Returns `400` when analysis is not configured or no API key is present in the environment;
the key never appears in any response. An upload larger than the staging bound is rejected
before anything is conveyed, naming the limit and the measured value.

The staged entry holds the conveyed document **content**, not a reference to it — base64
PDF bytes have no server-side handle to point at — so the session is bounded by bytes as
well as by count, and its TTL is short. `expires_at` is published so a client can tell the
operator their session has a lifetime.

### `POST /1.0/answer/analyze/generate`

Second pass. JSON body: `{analysis_token, answer_words, answer_limit, openstack?, accept_shortfall?, retry_questions?}` —
`answer_limit` is `"hard"` or `"soft"`, and `openstack` (`"sunbeam"` or `"charmed"`) is
meaningful only when the triage reported OpenStack in scope. The documents are **not**
re-uploaded; generation runs against the set the token refers to.

Publishes `manifest` (in the shape `POST /1.0/answer/batch` accepts, so a client can post
it back unchanged), `check` (see below) and `model`. An unknown or expired token returns
`400` stating that the session expired and the documents must be uploaded again — a
recoverable error, so a client should return to its upload step rather than dead-ending.

The metadata also carries the prompt's two halves: `prompt_generated`, the part the model
wrote, and `prompt_appended`, the rules rag-cli adds. `prompt_generated` (trimmed) +
`"\n\n"` + `prompt_appended` is exactly `manifest.prompt`, so a client can let the first be
edited and re-join it without being able to touch the rules. Both are omitted together when
the prompt does not end with the exact rules block; show the prompt whole and read-only
then.

A truncated response, or an **empty** response at the model's output ceiling, fails the
operation rather than completing with a short manifest, and **cannot be accepted**. On a
reasoning model the output ceiling covers the model's own reasoning, so an empty response is
a budget failure and must never be read as "the model found nothing".

A question count materially short of the triage estimate, from an extraction that finished
normally, also fails the operation — but as a decision for the operator, not a dead end. The
failed operation's metadata carries a `shortfall` object; read it rather than parsing the
error text:

```json
{
  "expected": 220, "produced": 53,
  "finish_reason": "STOP", "answer_tokens": 9214, "ceiling": 65536,
  "effort": "low", "next_effort": "medium",
  "sections": [
    {"name": "A.1 Functional Requirements", "id_prefix": "FR_", "estimated": 95, "produced": 31},
    {"name": "A.2 Non-Functional Requirements", "id_prefix": "NFR_", "estimated": 115, "produced": 22}
  ]
}
```

A section's `produced` is `-1` when it has no id prefix to attribute questions by. The
produced questions stay on the session (they are not sent to the client). Post the same token
and interview answers again with **one** of:

- `"accept_shortfall": true` — continue from the retained questions without extracting them
  again. The completed metadata carries `shortfall_accepted: {expected, produced}`, which a
  client should record with the manifest.
- `"retry_questions": true` — re-run only the extraction, at `next_effort`. Refused when
  `next_effort` is empty (already at the highest effort).

Either flag on a session holding no shortfall, or both together, is a `400`. The session's
lifetime is unchanged, so an expired token is still the expired-session `400`.

### `POST /1.0/answer/check`

Checks a manifest without running it. Accepts the same JSON body as
`POST /1.0/answer/batch`; answers no question, calls no model, and stores nothing.
Synchronous.

Returns the question and domain counts, `domain_matches` (how many questions each pattern
would route, resolved with the run's own routing so the check cannot disagree with a run),
`prompt` assertions (`no_context_rule`, `directness_rule`, `domain_text_rule`,
`length_stated`, `length_hard`), and `findings` at two severities. An entry in
`domain_matches` routing exactly one question also carries `only_question: {id, text}` and
`context_preview`, so a client can show the question beside the context attached to it —
informational only, never a finding:

- `defect` — would fail a run, or names a retrieval target that contributes nothing.
- `warning` — will run, but may not be intended. A pattern reserved for a section with no
  questions yet is the canonical example, so a zero-match pattern is never a defect.

`references_checked` and `kapa_checked` are reported **separately**, and each is `false`
when that check did not run: the knowledge store may be unreachable, and this daemon does
not wire kapa retrieval at all, so it cannot report whether a source group exists. A client
must present a check that did not run as such rather than implying it passed.
