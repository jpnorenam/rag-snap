# Local web UI

`ragd` can serve a local **browser UI** for chatting with your knowledge bases. The UI is a
static single-page application embedded into the `ragd` binary and served from a
**loopback-only** HTTP listener, same-origin with the REST API. A browser cannot connect to
the daemon's unix socket, so this opt-in TCP listener is what bridges the browser to the API.

Remote/HTTPS exposure is intentionally **not** part of this surface: the listener binds
`127.0.0.1` only and refuses any non-loopback address.

- [Quick start: from install to a first answer](#quick-start-from-install-to-a-first-answer)
- [Navigating the UI](#navigating-the-ui)
- [Enabling the listener](#enabling-the-listener)
- [Configuring the chat backend and API key](#configuring-the-chat-backend-and-api-key)
- [Launching with `rag ui`](#launching-with-rag-ui)
- [Trust model](#trust-model)
- [Troubleshooting](#troubleshooting)

---

## Quick start: from install to a first answer

For the full path from a fresh install to a working UI — installing the snap, configuring
backends, secrets, and enabling the loopback listener — see
**[INSTALL.md](../INSTALL.md#enable-the-browser-ui)**.

Once configured, opening the UI is just:

```bash
rag-cli.rag ui
```

In the browser, type a question and press **Enter**. The UI opens a websocket to the daemon,
which streams the model's answer back token by token. If you keep knowledge bases, select them
from the chips at the top of the page to ground answers in your documents.

> For obtaining a Bedrock API key step by step, see the [Bedrock guide](bedrock_guide.md).

---

## Navigating the UI

The UI is a multi-page application with a persistent dark navigation rail on the left. The
rail lists the app's sections; **Chat**, **Prompts**, and **Status** are shipped today, and the
remaining entries (Knowledge bases, Search, Answer RFPs) show a **Soon** badge until their features
land. The active section is marked with an orange left-border indicator, and the browser tab title
tracks the section you are on. On narrow windows the rail collapses to an icon-only strip; hover a
icon for its label.

### Background operations

Long-running work the daemon performs on your behalf — ingesting documents, running an answer
batch, exporting a knowledge base — runs as an **operation**. An **operations indicator** in
the top bar (right-hand side, next to the chat connection status) makes these visible from any
page:

- The indicator appears once the session has seen at least one operation and shows a **count of
  running operations**, with a spinner while anything is in flight.
- Clicking it opens a panel listing the session's operations, newest first. Each row shows a
  status dot (running, succeeded, failed, or cancelled), the operation's description, and a
  relative timestamp (hover for the exact time). Operations that report progress render a thin
  progress bar; failed operations show their error message inline.
- While an operation is running and cancellable, the row offers a **Cancel** action. Cancelling
  asks for confirmation, then requests cooperative cancellation from the daemon
  (`DELETE /1.0/operations/{id}`); the row moves to the cancelled state once the daemon reports
  it. A cancelled operation is shown distinctly from a failed one.
- Terminal rows can be dismissed with the × to de-clutter the list. Dismissal is local — if the
  daemon still lists the operation, it reappears after a reload.

The panel is seeded from `GET /1.0/operations` on load, so **reloading the page does not lose a
running operation**. Live updates arrive over the `GET /1.0/events` websocket; if that socket is
unavailable the indicator silently falls back to polling, so it keeps working with no error
banner as long as the REST API is reachable. This mirrors the CLI, where the same operations are
driven from commands like `rag-cli.rag k ingest …`.

### Prompts

The **Prompts** page is the browser equivalent of `rag-cli.rag prompt init`. It shows the three
templates that drive generation — the **chat system prompt**, the **answer system prompt** (batch
answering), and the **source rules** (the grounding block appended to a batch manifest's custom
prompt) — each as a card marked **Default** or **Customized**.

- **Edit** expands a card into a monospace editor pre-filled with the prompt in effect. While
  editing, *View default prompt* shows the built-in default read-only, so you can compare against
  it (or copy from it) without leaving the editor. **Save** stays disabled until you actually
  change something.
- **Reset to default** appears only on a customized prompt and asks for confirmation before
  replacing your text with the built-in default.
- Only one card is open at a time, and unsaved edits are protected: switching cards, navigating
  away, or reloading the page asks before discarding them. A failed save keeps your text.

Prompts are held by the **daemon**, so what you save here is exactly what `rag-cli.rag chat`,
`answer batch`, and the REST API use. The daemon resolves prompts when a chat session or batch run
*starts*, which is why the confirmation reads "New chats and batch runs will use it" — work
already in flight keeps the prompts it began with.

### Status

The **Status** page (pinned to the bottom of the rail) is the browser equivalent of
`rag-cli.rag status` and `rag-cli.rag get`/`set`. It has two zones.

**Services** shows one card per service — OpenSearch, the inference server, Tika, and the ragd
daemon — each with its state (**Running**, **Unreachable**, or **Not configured** — the word is
always shown, never colour alone), its endpoint, and its own details:

- **OpenSearch** lists the configured embedding and rerank **model IDs** (copyable, as `knowledge
  init` prints them) *and* the models OpenSearch actually has **deployed**. If a configured model
  is not deployed, the card says so — that combination breaks retrieval, and without this you would
  not find out until a search failed. Fix it with `rag-cli.rag knowledge init`.
- **Inference** shows the LLM it serves, **Tika** its version, and **ragd** its API version and
  listeners.

The daemon probes the services when you ask, not on a timer: the page checks on load and on
**Refresh**, and shows when it last checked. An unreachable service degrades on its own card,
with the CLI command to try next — it never takes the page down.

**Configuration** lists the effective configuration. Each key shows its value and a **layer** chip:
`package` (shipped with the snap) or `user` (your override). Filter the keys with the search box,
edit a value in place with the pencil, and **Revert** a `user` value back to the packaged one
(the confirmation shows both values). Saving writes the **user** layer, exactly as
`sudo rag-cli.rag set <key>=<value>` does, and the same rules apply: you can override existing
keys but not invent new ones. Changing a key that feeds a service connection prompts you to
re-check Status above.

Secret values are never shown. The service credentials are environment variables, not
configuration, and the one config key that *is* a secret (`gdrive.client.secret`) is redacted by
the daemon — it renders as `••••` and can be written but never read back.

---

## Enabling the listener

The loopback listener is opt-in and **off by default** (the unix socket remains the only
default surface). Enable it and restart the daemon:

```bash
# Turn on the loopback listener (serves the API and the UI on 127.0.0.1)
sudo rag-cli.rag set api.loopback.enabled=true

# Restart ragd so it opens the listener
sudo snap restart rag-cli.ragd
```

Two config keys control the listener:

| Key                    | Default        | Meaning                                                                 |
| ---------------------- | -------------- | ----------------------------------------------------------------------- |
| `api.loopback.enabled` | `false`        | Whether `ragd` opens the loopback listener and serves the UI.           |
| `api.loopback.address` | `127.0.0.1:0`  | Loopback bind address. `:0` picks an OS-assigned port. **Must be loopback** — a non-loopback address is refused at startup. |

The resolved URL (with the OS-assigned port) is written to the daemon log and reported by
`GET /1.0` under `config.loopback`:

```bash
sudo snap logs rag-cli.ragd | grep 'serving loopback API'
# serving loopback API on 127.0.0.1:43210
```

The UI is then reachable at `http://127.0.0.1:43210/ui/` on that resolved port. Prefer
`rag-cli.rag ui`, which discovers the port and token for you.

---

## Configuring the chat backend and API key

The UI talks to the daemon, and the **daemon** — not your shell — makes the call to the
inference backend. Backend secrets are passed to `ragd` through environment variables
(`OPENSEARCH_USERNAME`, `OPENSEARCH_PASSWORD`, `CHAT_API_KEY`), never through config.

This matters for the chat API key. When you run `rag-cli.rag chat` interactively, the CLI
inherits `CHAT_API_KEY` from your shell, so a plain `export CHAT_API_KEY=…` is enough. But
the UI is served by the **`ragd` systemd service**, which has its own environment and does
**not** see your shell exports. Without the key, the daemon calls the backend with no
`Authorization` header and the backend replies `401 Unauthorized` (e.g. Bedrock:
`"Authorization header is missing"`).

Give the daemon its secrets — the chat key, and your real OpenSearch credentials if your
cluster doesn't use the `admin`/`admin` default — with a **root-only systemd drop-in** (the
snap's auto-generated unit is regenerated on every restart and must not be edited directly).
The same recipe is in [the REST API guide](rest-api.md):

```bash
sudo mkdir -p /etc/systemd/system/snap.rag-cli.ragd.service.d
printf '[Service]\nEnvironment=CHAT_API_KEY=%s\nEnvironment=OPENSEARCH_USERNAME=%s\nEnvironment=OPENSEARCH_PASSWORD=%s\n' \
  "$YOUR_CHAT_KEY" "$YOUR_OPENSEARCH_USER" "$YOUR_OPENSEARCH_PASSWORD" | \
  sudo tee /etc/systemd/system/snap.rag-cli.ragd.service.d/10-secrets.conf >/dev/null
sudo chmod 600 /etc/systemd/system/snap.rag-cli.ragd.service.d/10-secrets.conf
sudo systemctl daemon-reload
sudo snap restart rag-cli.ragd
```

The drop-in is `root:root 0600`, so the secrets are never world-readable and never pass
through the `snapctl` config store or the `GET /1.0` config summary.

Confirm the running daemon actually has them (checks the live process, not just the unit):

```bash
sudo tr '\0' '\n' < /proc/$(pgrep -x ragd)/environ | grep -E 'CHAT_API_KEY|OPENSEARCH_USERNAME|OPENSEARCH_PASSWORD'
```

The drop-in directory survives `snap restart` and `snap install --dangerous` of the same
build. A full `snap remove` clears it, so re-apply the drop-in after a clean reinstall.

> **Note:** the snap deliberately declares no `environment:` values for `CHAT_API_KEY`,
> `OPENSEARCH_USERNAME`, or `OPENSEARCH_PASSWORD` in its own metadata. Hardcoding any of them
> (even as an empty string) would make snapd apply that value over whatever the systemd unit
> provides, so the drop-in could never take effect — this is what lets a non-default OpenSearch
> username/password work the same way as the chat key.

---

## Launching with `rag ui`

The simplest way in is the `rag ui` command. It contacts the daemon over the trusted unix
socket, discovers the loopback URL and token, and opens your browser with the token applied:

```bash
rag-cli.rag ui

# Print the URL instead of opening a browser (e.g. on a headless host)
rag-cli.rag ui --no-browser
```

When the listener is disabled, `rag ui` explains how to enable it (via
`api.loopback.enabled`) rather than failing silently. You must be a member of the API access
group (default `rag`) to reach the daemon over the unix socket and launch the UI.

---

## Trust model

The unix socket authenticates peers by their kernel credentials (`SO_PEERCRED`). Those
credentials do not exist for TCP connections, so the loopback listener authenticates with a
**localhost bearer token** instead:

- On first enable, the daemon generates a high-entropy token and stores it **owner-only
  (`0600`)** under `$SNAP_COMMON` (`ragd/ui.token`). Under strict confinement the daemon
  cannot chown the file to the API access group, so it does **not** try to; clients obtain the
  token value over the **peercred-gated `GET /1.0`** instead of reading the file. Any user who
  can reach the unix socket (root or a member of the API access group, default `rag`) can
  therefore retrieve it — the same trust boundary as the socket. The token is reused across
  restarts.
- Requests to `/1.0/...` over the loopback listener must present the token (as a
  `Bearer` header or the `rag_ui_token` cookie). Requests without a valid token are rejected.
- **Static UI assets under `/ui/` load without the token** so the page shell can render;
  only the `/1.0/...` API is gated.
- `rag ui` performs the handoff by opening `/ui/login?token=…`, which sets an `HttpOnly`
  cookie scoped to the loopback origin and redirects into the app. The token therefore never
  enters the page's JavaScript or the address-bar history, and same-origin API calls and the
  chat websocket carry it automatically.
- The token is **per-installation** and is **never** baked into the embedded UI assets.

> **⚠️ A loopback port is reachable by any local user.** As with the unix socket, treat
> membership in the API access group — and possession of the token — as equivalent to full
> access over the RAG stack. The token is the local trust boundary, and the seam where TLS
> client certs / OIDC attach if the surface is ever exposed remotely (a separate, deferred
> decision).

---

## Troubleshooting

**`unknown command "ui"` from `rag-cli.rag ui`.** The installed snap predates the UI command.
Confirm the version with `snap list rag-cli` and reinstall the latest build, naming the file
explicitly (e.g. `sudo snap install --dangerous ./rag-cli_<version>_amd64.snap`, using the exact
filename from `ls rag-cli_*.snap`) — a `rag-cli_*.snap` glob can match an older snap left in the
directory.

**The old UI URL no longer loads after a restart.** Expected. With the default
`api.loopback.address=127.0.0.1:0` the OS assigns a fresh port on every start, so a bookmarked
link goes stale. Always reopen with `rag-cli.rag ui` rather than reusing a previous URL.

**`401 Unauthorized` / `"Authorization header is missing"` when sending a message.** The
daemon has no chat API key. See
[Configuring the chat backend and API key](#configuring-the-chat-backend-and-api-key) — set it
via the systemd drop-in, not a shell `export`.

**`chat operation did not return a websocket URL/secret`.** The UI bundle is older than the
daemon. Rebuild the snap so the embedded UI matches (`make ui` then `snapcraft`), reinstall,
restart the daemon, and hard-reload the browser (Ctrl+Shift+R) to bypass the cached bundle.

**A knowledge base fails to load, or search/ingest errors with `opensearch not available`, even
though the CLI works fine against the same cluster.** The daemon doesn't have your OpenSearch
credentials. Give it `OPENSEARCH_USERNAME`/`OPENSEARCH_PASSWORD` the same way as `CHAT_API_KEY` —
see [Configuring the chat backend and API key](#configuring-the-chat-backend-and-api-key).