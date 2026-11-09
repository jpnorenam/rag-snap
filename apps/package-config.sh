# Package-layer config registration, sourced by the install and post-refresh
# hooks so the two cannot drift. A user-layer `rag set <key>=...` rejects keys
# that do not exist at the package layer, so every key users may configure must
# be registered here; registering only on install left refreshed installations
# unable to configure keys added later (such as kapa.*).
#
# Registration is idempotent and never overwrites a value that is already set:
# an operator may have changed a package value with `rag set --package`.

# register_package_key <key> <default>
register_package_key() {
  if [ -z "$(snapctl get "config.package.$1")" ]; then
    snapctl set "config.package.$1=$2"
  fi
}

# Chat backend wire protocol:
#   sudo rag set chat.api=anthropic
# openai    — OpenAI Chat Completions: a local Inference snap, or a third party's
#             OpenAI-compatible route (e.g. Bedrock bedrock-runtime …/openai/v1). Default.
# anthropic — Anthropic Messages API, e.g. Claude on Bedrock: chat.http.host=
#             bedrock-runtime.<region>.amazonaws.com, chat.http.path=anthropic,
#             chat.model=global.anthropic.claude-sonnet-5-5. chat.model is required (no model
#             listing) and sampling temperature is not sent. CHAT_API_KEY is the bearer.
register_package_key chat.api "openai"

# Chat backend endpoint and model. Registered empty: there is no default backend,
# and an empty value reads exactly like an unset key ("config key ... is not set").
# Registering them lets a plain user-layer `rag set chat.http.host=...` work
# without --package. Examples (Claude on Bedrock):
#   sudo rag set chat.http.host=bedrock-runtime.us-east-2.amazonaws.com
#   sudo rag set chat.http.port=443
#   sudo rag set chat.http.tls=true
#   sudo rag set chat.http.path=anthropic
#   sudo rag set chat.model=global.anthropic.claude-sonnet-5-5
register_package_key chat.http.host ""
register_package_key chat.http.port ""
register_package_key chat.http.path ""
register_package_key chat.http.tls ""
register_package_key chat.model ""

# Knowledge store (OpenSearch). Defaults to a local OpenSearch snap; point it at a
# remote cluster with, e.g.:
#   sudo rag set knowledge.http.host=10.0.0.5
register_package_key knowledge.http.host "127.0.0.1"
register_package_key knowledge.http.port "9200"
register_package_key knowledge.http.tls "true"

# Embedding and rerank model IDs, written by `rag knowledge init` (through ragd) or
# set by hand from its output. Registered empty until then.
register_package_key knowledge.model.embedding ""
register_package_key knowledge.model.rerank ""

# Bundled Tika server (the tika-server daemon). Fixed: tika-start.sh listens on
# 127.0.0.1:9998, so these only change if that script does.
register_package_key tika.http.host "127.0.0.1"
register_package_key tika.http.port "9998"
register_package_key tika.http.path "tika"

# Google Drive OAuth2 credentials:
#   sudo rag set gdrive.client.id=<client-id>
#   sudo rag set gdrive.client.secret=<client-secret>
register_package_key gdrive.client.id ""
register_package_key gdrive.client.secret ""

# kapa.ai:
#   sudo rag set kapa.enabled=false
#   sudo rag set kapa.project.id=<id>
# The API key is a secret and is never config: it comes from KAPA_API_KEY in the
# environment (a systemd drop-in for ragd, the shell or credentials.json for the CLI).
register_package_key kapa.enabled "true"
register_package_key kapa.project.id ""

# RFP analysis. Sends RFP documents directly to Google's Gemini API and generates
# a complete answer-batch manifest, so it is OFF by default: it sends document
# content to a third party. There is no gateway and no configurable endpoint; the
# destination is fixed in code.
#   sudo rag set analysis.enabled=true
#   sudo rag set analysis.model=<gemini-model-id>
#
# analysis.model has no default on purpose. Gemini model ids are versioned (the Pro
# tier currently ships only under a preview identifier), so baking one in would age
# badly and fail as an opaque 404. Ids are unprefixed, not google/... as on a
# gateway. Verified values:
#   gemini-3.1-pro-preview   Pro tier, preview (deprecated with >= 2 weeks' notice)
#   gemini-3.8-flash         newest stable model
#
# The API key is NEVER config: it comes from GEMINI_API_KEY only, with no fallback
# to CHAT_API_KEY (that key belongs to the chat backend, never to Google). The key
# MUST belong to a billing-enabled (paid-tier) Google project: on the unpaid tier,
# Google may use submitted content to improve its products and human reviewers may
# read it. See the `ragd` app's environment comment in snapcraft.yaml.
register_package_key analysis.enabled "false"
register_package_key analysis.model ""
# Question extraction defaults to medium reasoning: at low, a 36-row compliance matrix
# whose cells pack several requirements came back nearly unsplit (53 questions where a
# person wrote 164), and medium cleared it for a few thousand reasoning tokens.
# Registration never overwrites, so an install that already holds "low" keeps it.
register_package_key analysis.reasoning.questions "medium"
register_package_key analysis.reasoning.strategy "high"
register_package_key analysis.answer.words "100"
register_package_key analysis.answer.limit "soft"

# Answer judge. Every `answer batch` run is judged by Gemini: each answer is
# checked against the context it was written from and given a confidence score,
# rating and comment. It is ON by default (the Gemini agreement covers sending
# answers and knowledge-base chunks) and needs GEMINI_API_KEY (same rules as
# analysis above) plus a model. judge.model empty falls back to analysis.model.
#   sudo rag set judge.enabled=false
# judge.concurrency bounds the Gemini calls in flight; raise it on a higher quota tier.
register_package_key judge.enabled "true"
register_package_key judge.model ""
register_package_key judge.reasoning "medium"
register_package_key judge.concurrency "4"

# REST API daemon (ragd) socket. Members of api.socket.group (plus root) may use
# the local unix socket; access is enforced by the daemon's SO_PEERCRED check, not
# by the socket's file ownership (under strict confinement the daemon cannot chown
# the socket to an arbitrary group). api.socket.mode defaults to 0666 so non-root
# group members can reach it; the peercred check is the gate.
#   sudo rag set api.socket.group=<group>
#   sudo rag set api.socket.mode=<mode>
# The default is `sudo`: on Ubuntu the installing user is in it, and its members
# can already act as root, so the default grants nobody new access. (It was `rag`,
# a group that normally does not exist; installs that stored it keep it.)
register_package_key api.socket.group "sudo"
register_package_key api.socket.mode "0666"

# Opt-in loopback (local REST API + browser UI) listener, OFF by default. The bind
# is loopback-only; a non-loopback address is refused by the daemon.
#   sudo rag set api.loopback.enabled=true
#   sudo rag set api.loopback.address=127.0.0.1:0   # :0 = OS-assigned port
register_package_key api.loopback.enabled "false"
register_package_key api.loopback.address "127.0.0.1:0"
