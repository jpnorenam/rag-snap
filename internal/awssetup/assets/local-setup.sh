#!/usr/bin/env bash
# Local OpenSearch setup for rag-cli
# Usage: ./local-setup.sh [setup|start|stop|destroy]
set -euo pipefail
umask 077

# Show usage without context check
if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ] || [ "${1:-}" = "help" ]; then
	cat <<-EOF
	Usage: $0 [MODE]
	
	Local OpenSearch setup for rag-cli
	
	Modes:
	  setup    - Install and configure OpenSearch, initialize knowledge base (default)
	  start    - Start OpenSearch and Tika services (enable boot startup)
	  stop     - Stop OpenSearch and Tika services (disable boot startup)
	  destroy  - Remove managed installation after confirmation
	  
	The setup mode installs OpenSearch locally, configures it for loopback-only access,
	and initializes the knowledge base with ML models.
	EOF
	exit 0
fi

# Reject sudo execution
if [ "${EUID:-$(id -u)}" = 0 ] || [ -n "${SUDO_USER:-}" ]; then
	echo "error: do not run this script with sudo; it will call sudo itself where needed" >&2
	exit 1
fi

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)

# Source shared code
if [ ! -f "$SCRIPT_DIR/common.sh" ]; then
	echo "error: common.sh not found in $SCRIPT_DIR" >&2
	exit 1
fi
source "$SCRIPT_DIR/common.sh"

# Bootstrap script path
BOOTSTRAP=$SCRIPT_DIR/bootstrap-opensearch.sh

# Load snap context
CONTEXT=$SCRIPT_DIR/snap-context.env
declare -A CFG=()
load_env "$CONTEXT" RAG_SNAP_INSTANCE RAG_SNAP_USER_COMMON 2>/dev/null || {
	echo "error: cannot load $CONTEXT" >&2
	echo "This file should have been exported by 'rag-cli.rag prepare-script local'" >&2
	exit 1
}
RAG=${CFG[RAG_SNAP_INSTANCE]}.rag

# Configuration files
SETTINGS=$SCRIPT_DIR/settings.env
SECRETS=$SCRIPT_DIR/secrets.env
STATE=$SCRIPT_DIR/state.env

# Allowed keys
SETTINGS_KEYS=(OPENSEARCH_CHANNEL OPENSEARCH_HEAP_GIB CHAT_HOST CHAT_PORT CHAT_PATH CHAT_TLS CHAT_MODEL DRIVE_IMPORT DRIVE_FOLDER_URL)
SECRET_KEYS=(TLS_ROOT_PASS TLS_ADMIN_PASS TLS_NODE_PASS OPENSEARCH_ADMIN_PASSWORD CHAT_API_KEY GOOGLE_DRIVE_CLIENT_ID GOOGLE_DRIVE_CLIENT_SECRET)
STATE_KEYS=(OPENSEARCH_INSTALLED_BY_US OPENSEARCH_CHANNEL_INSTALLED OPENSEARCH_SETUP_COMPLETED RAG_CLI_CONFIGURED KNOWLEDGE_INIT_COMPLETED EMBEDDING_MODEL_ID RERANK_MODEL_ID SYSCTL_FILE_CREATED SYSCTL_FILE_CONTENT DRIVE_IMPORT_LAST_ATTEMPT)
MASKED_KEYS=(OPENSEARCH_ADMIN_PASSWORD TLS_ROOT_PASS TLS_ADMIN_PASS TLS_NODE_PASS CHAT_API_KEY GOOGLE_DRIVE_CLIENT_SECRET)

# OpenSearch paths. The shared bootstrap records on the host that it started
# this node; that marker outlives the snap and has to be cleared with it.
SYSCTL_CONF=/etc/sysctl.d/60-rag-local-opensearch.conf
BOOTSTRAP_STATE_MARKER=/var/lib/rag-bootstrap/daemon-started
BIND_ADDRESS=127.0.0.1
URL=https://$BIND_ADDRESS:9200

PHASE=""

die() {
	echo "error${PHASE:+ in phase '$PHASE'}: $*" >&2
	exit 1
}

# Load configurations
load_settings() {
	load_env "$SETTINGS" "${SETTINGS_KEYS[@]}"
	load_env "$SECRETS" "${SECRET_KEYS[@]}"
	load_env "$STATE" "${STATE_KEYS[@]}"
}

# Save to specific env file
save_settings() { save_env "$SETTINGS" "${SETTINGS_KEYS[@]}"; }
save_secrets() { save_env "$SECRETS" "${SECRET_KEYS[@]}"; }
save_state() { save_env "$STATE" "${STATE_KEYS[@]}"; }

# Validation
valid_heap() { [[ $1 =~ ^[1-9][0-9]*$ ]]; }
valid_channel() { [[ $1 =~ ^[0-9]+/(stable|candidate|beta|edge)$ ]]; }
valid_port() { [[ $1 =~ ^[0-9]+$ ]] && ((10#$1 >= 1 && 10#$1 <= 65535)); }
valid_bool() { [[ $1 == true || $1 == false ]]; }
valid_yesno() { [[ $1 == yes || $1 == no ]]; }

# Gate the effective heap, whether just entered or loaded from settings.env,
# before anything is generated, installed, configured or started.
validate_heap() {
	PHASE="settings"
	has OPENSEARCH_HEAP_GIB || CFG[OPENSEARCH_HEAP_GIB]=6
	valid_heap "${CFG[OPENSEARCH_HEAP_GIB]}" ||
		die "OPENSEARCH_HEAP_GIB must be a positive integer number of GiB (the local default is 6); got '${CFG[OPENSEARCH_HEAP_GIB]}'"
}

# Prompt for settings
ask() {
	local key=$1 prompt=$2 default=${3:-} secret=${4:-}
	
	if has "$key"; then
		return 0
	fi
	
	local answer
	local display_prompt="$prompt"
	[ -n "$default" ] && display_prompt="$prompt [${default}]"
	
	if [ -n "$secret" ]; then
		read -rsp "${display_prompt}: " answer
		echo
	else
		read -rp "${display_prompt}: " answer
	fi
	
	answer=${answer:-$default}
	CFG[$key]=$answer
}

# Preflight checks
preflight() {
	PHASE="preflight"
	for cmd in bash curl sudo snap jq; do
		command -v "$cmd" >/dev/null || die "required command not found: $cmd"
	done
	snap list "${CFG[RAG_SNAP_INSTANCE]}" >/dev/null 2>&1 || die "${CFG[RAG_SNAP_INSTANCE]} snap is not installed"
	if systemctl is-active --quiet snap."${CFG[RAG_SNAP_INSTANCE]}".ragd.service 2>/dev/null; then
		die "ragd is running; please stop it before running setup (sudo snap stop ${CFG[RAG_SNAP_INSTANCE]}.ragd)"
	fi
}

# Collection settings
collect_settings() {
	PHASE="settings"
	say "Collecting deployment settings..."
	
	ask OPENSEARCH_CHANNEL "OpenSearch snap channel" "2/stable"
	valid_channel "${CFG[OPENSEARCH_CHANNEL]}" ||
		die "OPENSEARCH_CHANNEL is '${CFG[OPENSEARCH_CHANNEL]}'; expected a channel such as 2/stable (N/stable|candidate|beta|edge)"
	
	ask OPENSEARCH_HEAP_GIB "OpenSearch heap size in GiB" "6"
	validate_heap
	
	ask CHAT_HOST "Inference API host" "localhost"
	ask CHAT_PORT "Inference API port" "8000"
	valid_port "${CFG[CHAT_PORT]}" || die "CHAT_PORT is '${CFG[CHAT_PORT]}'; expected a port between 1 and 65535"
	ask CHAT_PATH "Inference API path" "/v1/chat/completions"
	ask CHAT_TLS "Inference API uses TLS?" "false"
	valid_bool "${CFG[CHAT_TLS]}" || die "CHAT_TLS is '${CFG[CHAT_TLS]}'; expected true or false"
	ask CHAT_MODEL "Model identifier" "default"
	
	ask CHAT_API_KEY "Inference API key (leave empty to skip)" "" secret
	
	ask DRIVE_IMPORT "Import from Google Drive?" "no"
	valid_yesno "${CFG[DRIVE_IMPORT]}" || die "DRIVE_IMPORT is '${CFG[DRIVE_IMPORT]}'; expected yes or no"
	
	if [ "${CFG[DRIVE_IMPORT]}" = yes ]; then
		ask DRIVE_FOLDER_URL "Google Drive folder URL" ""
		ask GOOGLE_DRIVE_CLIENT_ID "Google OAuth client ID" ""
		ask GOOGLE_DRIVE_CLIENT_SECRET "Google OAuth client secret" "" secret
		local key
		for key in DRIVE_FOLDER_URL GOOGLE_DRIVE_CLIENT_ID GOOGLE_DRIVE_CLIENT_SECRET; do
			[ -n "${CFG[$key]:-}" ] || die "$key is required when DRIVE_IMPORT is yes"
		done
	fi
	
	save_settings
	save_secrets
	say "Settings saved"
}

# Generate deployment secrets
generate_secrets() {
	[ -n "${CFG[TLS_ROOT_PASS]:-}" ] && return 0
	PHASE="secrets"
	say "Generating deployment secrets..."
	CFG[TLS_ROOT_PASS]=$(gen_secret)
	CFG[TLS_ADMIN_PASS]=$(gen_secret)
	CFG[TLS_NODE_PASS]=$(gen_secret)
	CFG[OPENSEARCH_ADMIN_PASSWORD]=$(gen_secret)
	save_secrets
}

# Check/install OpenSearch snap
install_opensearch() {
	PHASE="opensearch-install"
	
	if snap list opensearch >/dev/null 2>&1; then
		# OpenSearch already installed - check ownership
		if [ "${CFG[OPENSEARCH_INSTALLED_BY_US]:-no}" != "yes" ]; then
			die "OpenSearch snap is installed but not managed by this setup.
This script will not modify an unmanaged OpenSearch installation.

Options:
1. Use a different machine for local rag-cli deployment
2. Remove OpenSearch: sudo snap remove opensearch --purge
3. Configure rag-cli manually (see INSTALL.md)"
		fi
		say "OpenSearch snap already installed (managed)"
		return 0
	fi
	
	# Install OpenSearch
	say "Installing OpenSearch snap from ${CFG[OPENSEARCH_CHANNEL]}..."
	sudo snap install opensearch --channel="${CFG[OPENSEARCH_CHANNEL]}" || die "snap install failed"
	
	CFG[OPENSEARCH_INSTALLED_BY_US]=yes
	CFG[OPENSEARCH_CHANNEL_INSTALLED]=${CFG[OPENSEARCH_CHANNEL]}
	save_state
	say "OpenSearch installed"
}

# Raise vm.max_map_count only when the effective value is below the minimum, and
# never lower a higher one. The snap's own sysctl handler is disabled and verified
# by the shared bootstrap before the daemon starts.
configure_sysctl() {
	PHASE="sysctl"
	local target=262144 current
	current=$(sysctl -n vm.max_map_count 2>/dev/null || echo 0)
	
	if [ "$current" -lt "$target" ]; then
		say "Raising vm.max_map_count from $current to $target..."
		sudo sysctl -w vm.max_map_count="$target" >/dev/null ||
			die "could not set vm.max_map_count to $target (effective value is $current). In a container this is a host setting: run 'sudo sysctl -w vm.max_map_count=$target' on the host and rerun, or run setup on the host."
		current=$(sysctl -n vm.max_map_count 2>/dev/null || echo 0)
		[ "$current" -ge "$target" ] ||
			die "vm.max_map_count still reads $current, below the required $target, after the write was accepted. In a container this value comes from the host: run 'sudo sysctl -w vm.max_map_count=$target' on the host and rerun."
		say "vm.max_map_count is now $current"
	else
		say "vm.max_map_count already $current (>= $target); leaving it unchanged"
	fi
	
	persist_sysctl "$current"
}

# persist_sysctl <value>: own a dedicated file, never an existing one.
persist_sysctl() {
	local content="vm.max_map_count=$1" tmp
	
	if [ -f "$SYSCTL_CONF" ]; then
		say "$SYSCTL_CONF already exists; leaving it unchanged"
		return 0
	fi
	
	tmp=$(mktemp /tmp/rag-sysctl.XXXXXX)
	printf '%s\n' "$content" >"$tmp"
	sudo install -m 0644 -o root -g root "$tmp" "$SYSCTL_CONF" || {
		rm -f "$tmp"
		die "could not create $SYSCTL_CONF; create it with '$content' or rerun with write access to $(dirname "$SYSCTL_CONF")"
	}
	rm -f "$tmp"
	
	CFG[SYSCTL_FILE_CREATED]=yes
	CFG[SYSCTL_FILE_CONTENT]=$content
	save_state
	say "Persisted $content to $SYSCTL_CONF"
}

# Bootstrap OpenSearch using shared script
bootstrap_opensearch() {
	PHASE="opensearch-bootstrap"
	
	say "Bootstrapping OpenSearch..."
	
	configure_sysctl
	
	# Create secrets file for bootstrap
	local secrets_file
	secrets_file=$(mktemp /tmp/opensearch-secrets.XXXXXX)
	chmod 600 "$secrets_file"
	cat >"$secrets_file" <<-EOF
	OPENSEARCH_ADMIN_PASSWORD=${CFG[OPENSEARCH_ADMIN_PASSWORD]}
	TLS_ROOT_PASS=${CFG[TLS_ROOT_PASS]}
	TLS_ADMIN_PASS=${CFG[TLS_ADMIN_PASS]}
	TLS_NODE_PASS=${CFG[TLS_NODE_PASS]}
	EOF
	
	# Run bootstrap - it handles fresh setup and reruns
	sudo bash "$BOOTSTRAP" --node-name rag-local --channel "${CFG[OPENSEARCH_CHANNEL]}" --secrets-file "$secrets_file" --skip-sysctl --heap "${CFG[OPENSEARCH_HEAP_GIB]}" --bind "$BIND_ADDRESS" || {
		rm -f "$secrets_file"
		die "bootstrap failed"
	}
	
	CFG[OPENSEARCH_SETUP_COMPLETED]=$(date -Iseconds)
	save_state
	say "OpenSearch bootstrap complete"
}

# Check security state - returns 0 only for authenticated readiness
check_security_state() {
	local code body
	body=$(mktemp)
	code=$(printf 'user = "admin:%s"\n' "${CFG[OPENSEARCH_ADMIN_PASSWORD]}" | \
		curl -sk --max-time 10 -K - -o "$body" -w '%{http_code}' "$URL/_plugins/_security/authinfo" 2>/dev/null || echo "000")
	rm -f "$body"
	
	case "$code" in
		200) echo "ready"; return 0 ;;
		401) echo "rejected"; return 1 ;;
		503) echo "uninitialized"; return 1 ;;
		*) echo "unreachable"; return 1 ;;
	esac
}

# Update credentials.json, keeping every key this setup does not manage. An API
# key is only replaced when a new one was supplied.
create_credentials() {
	PHASE="credentials"
	local creds="${CFG[RAG_SNAP_USER_COMMON]}/credentials.json" base='{}'
	say "Updating $creds..."
	
	# Merge policy: keep every key this setup does not manage; an API key is only
	# replaced when a new one was supplied.
	if [ -f "$creds" ]; then
		jq -e 'type == "object"' "$creds" >/dev/null 2>&1 ||
			die "$creds is not a JSON object; it was left unchanged. Fix or move it, then rerun"
		base=$(cat "$creds")
	fi
	
	write_credentials_json "$creds" "${CFG[OPENSEARCH_ADMIN_PASSWORD]}" "${CFG[CHAT_API_KEY]:-}" "$base"
}

# Configure rag-cli
configure_ragcli() {
	PHASE="configure"
	say "Configuring rag-cli..."
	
	create_credentials
	configure_rag_cli "$BIND_ADDRESS"
	
	say "Starting Tika..."
	start_tika_and_wait
	
	CFG[RAG_CLI_CONFIGURED]=$(date -Iseconds)
	save_state
}

# Initialize knowledge base
# model_deployed <model-id>: true when the model is in the DEPLOYED state. Dies
# on an authentication, network or API failure so a request failure is never
# mistaken for a missing model.
model_deployed() {
	local id=$1 body code state
	body=$(mktemp)
	code=$(printf 'user = "admin:%s"\n' "${CFG[OPENSEARCH_ADMIN_PASSWORD]}" |
		curl -sk --max-time 10 -K - -o "$body" -w '%{http_code}' "$URL/_plugins/_ml/models/$id") || {
		rm -f "$body"
		die "could not reach $URL to check model $id; not treating a request failure as a missing model"
	}
	state=$(jq -r '.model_state // empty' "$body" 2>/dev/null)
	rm -f "$body"
	case $code in
	200)
		[ -n "$state" ] || die "could not parse the model state for $id from $URL"
		[ "$state" = DEPLOYED ]
		;;
	404) return 1 ;;
	401 | 403) die "$URL rejected the admin credentials while checking model $id" ;;
	*) die "unexpected HTTP $code from $URL while checking model $id" ;;
	esac
}

init_knowledge() {
	PHASE="knowledge-init"
	
	# Reuse the configured models when they are already deployed. A saved timestamp
	# alone is not proof of readiness, and initialising again with healthy models
	# registers duplicates.
	local emb rer id ready=yes
	emb=$("$RAG" get knowledge.model.embedding 2>/dev/null || echo "")
	rer=$("$RAG" get knowledge.model.rerank 2>/dev/null || echo "")
	# Check every configured model. Both checks always run (no short-circuit), and
	# any request failure stops setup before init.
	for id in "$emb" "$rer"; do
		[ -n "$id" ] || continue
		model_deployed "$id" || ready=no
	done
	if [ -n "$emb" ] && [ -n "$rer" ] && [ "$ready" = yes ]; then
		say "Models already deployed ($emb, $rer); reusing them"
		CFG[KNOWLEDGE_INIT_COMPLETED]=$(date -Iseconds)
		save_state
		return 0
	fi
	
	say "Initializing knowledge base (this may take a few minutes)..."
	local log
	log=$(mktemp /tmp/knowledge-init.XXXXXX)
	knowledge_init_and_extract "$log"
	rm -f "$log"
	
	CFG[EMBEDDING_MODEL_ID]=$EMB_MODEL_ID
	CFG[RERANK_MODEL_ID]=$RER_MODEL_ID
	CFG[KNOWLEDGE_INIT_COMPLETED]=$(date -Iseconds)
	save_state
	
	say "Knowledge base initialized"
}

# Google Drive import
drive_import() {
	[ "${CFG[DRIVE_IMPORT]:-no}" = yes ] || return 0
	
	PHASE="drive-import"
	
	local log rc=0
	log=$(mktemp /tmp/drive-import.XXXXXX)
	drive_import_folder "$log" || rc=$?
	rm -f "$log"
	
	save_state
	
	# Local policy: an import is best effort. Report the command's own status
	# without claiming that every archive imported.
	if [ "$rc" = 0 ]; then
		say "Import attempt finished; check the output above for per-archive results"
	else
		say "Warning: the import command exited $rc; see the output above"
	fi
}

# Main setup mode
mode_setup() {
	say "==> Local OpenSearch Setup for rag-cli"
	
	load_settings
	[ -f "$SETTINGS" ] && say "Using saved settings from $SETTINGS"
	
	# ask() keeps answered questions, so this asks only what is still missing
	# and validates what was saved, before anything on the host is touched.
	collect_settings
	
	validate_heap
	preflight
	generate_secrets
	install_opensearch
	
	bootstrap_opensearch
	configure_ragcli
	init_knowledge
	drive_import
	
	say ""
	say "==> Setup complete!"
	say ""
	say "OpenSearch is running on https://127.0.0.1:9200 (loopback-bound)"
	say "Tika is running on http://127.0.0.1:9998/tika"
	say "Knowledge base initialized with embedding and rerank models"
	say ""
	say "You can now use: ${CFG[RAG_SNAP_INSTANCE]}.rag chat"
	say ""
	say "To start/stop services: $0 start|stop"
	say "To remove this deployment: $0 destroy"
}

# Start mode
mode_start() {
	PHASE="start"
	load_settings
	
	[ "${CFG[OPENSEARCH_INSTALLED_BY_US]:-no}" = "yes" ] || die "OpenSearch not installed by this setup"
	
	say "Starting services..."
	sudo snap start --enable opensearch.daemon
	sudo snap start --enable "${CFG[RAG_SNAP_INSTANCE]}.tika-server"
	
	# Wait for authenticated readiness
	say "Waiting for OpenSearch..."
	local i state
	for i in $(seq 1 36); do
		state=$(check_security_state 2>/dev/null || echo "failed")
		if [ "$state" = "ready" ]; then
			say "OpenSearch is ready"
			say "Services started (enabled for boot startup)"
			return 0
		fi
		[ "$i" -lt 36 ] && sleep 5
	done
	
	die "OpenSearch did not reach authenticated readiness (last state: $state, waited 180s)"
}

# Stop mode
mode_stop() {
	PHASE="stop"
	load_settings
	
	[ "${CFG[OPENSEARCH_INSTALLED_BY_US]:-no}" = "yes" ] || die "OpenSearch not installed by this setup"
	
	say "Stopping services..."
	sudo snap stop --disable opensearch.daemon
	sudo snap stop --disable "${CFG[RAG_SNAP_INSTANCE]}.tika-server"
	
	say "Services stopped (disabled from boot startup)"
}

# Destroy mode
mode_destroy() {
	PHASE="destroy"
	load_settings
	
	# Check ownership first
	if [ "${CFG[OPENSEARCH_INSTALLED_BY_US]:-no}" != "yes" ]; then
		die "OpenSearch was not installed by this setup; not removing"
	fi
	
	# Confirm
	confirm "This will stop services and remove the managed OpenSearch installation. Continue?" || {
		say "Cancelled"
		exit 0
	}
	
	say "Destroying managed deployment..."
	
	# Stop services
	sudo snap stop --disable opensearch.daemon 2>/dev/null || true
	sudo snap stop --disable "${CFG[RAG_SNAP_INSTANCE]}.tika-server" 2>/dev/null || true
	
	# Remove OpenSearch
	sudo snap remove opensearch --purge || die "failed to remove OpenSearch snap"
	
	# Clear the shared bootstrap's marker: it described a node that no longer
	# exists, and a stale one makes the next setup skip TLS setup and leave
	# OpenSearch unable to start.
	sudo rm -f "$BOOTSTRAP_STATE_MARKER" 2>/dev/null || true
	
	# Clean credentials.json
	local creds="${CFG[RAG_SNAP_USER_COMMON]}/credentials.json"
	if [ -f "$creds" ]; then
		local current_pw
		current_pw=$(jq -r '.OPENSEARCH_PASSWORD // empty' "$creds" 2>/dev/null || echo "")
		if [ -n "$current_pw" ] && [ "$current_pw" = "${CFG[OPENSEARCH_ADMIN_PASSWORD]:-}" ]; then
			local tmp
			tmp=$(mktemp)
			jq 'del(.OPENSEARCH_USERNAME, .OPENSEARCH_PASSWORD)' "$creds" > "$tmp" && mv "$tmp" "$creds"
		fi
	fi
	
	# Clean sysctl: remove the file only if this deployment created it and it
	# still holds exactly what was created. The runtime value is left alone.
	if [ -f "$SYSCTL_CONF" ]; then
		if [ "${CFG[SYSCTL_FILE_CREATED]:-no}" != "yes" ]; then
			say "Keeping $SYSCTL_CONF: this setup did not create it"
		elif [ -z "${CFG[SYSCTL_FILE_CONTENT]:-}" ]; then
			say "Keeping $SYSCTL_CONF: no record of the content this setup created"
		elif [ "$(cat "$SYSCTL_CONF")" = "${CFG[SYSCTL_FILE_CONTENT]}" ]; then
			sudo rm -f "$SYSCTL_CONF" || die "could not remove $SYSCTL_CONF"
			say "Removed $SYSCTL_CONF"
		else
			say "Keeping $SYSCTL_CONF: it was edited after this setup created it"
		fi
	fi
	
	# Clear deployment secrets
	for key in TLS_ROOT_PASS TLS_ADMIN_PASS TLS_NODE_PASS OPENSEARCH_ADMIN_PASSWORD; do
		unset CFG[$key]
	done
	save_secrets
	
	# Clear obsolete state
	for key in OPENSEARCH_SETUP_COMPLETED KNOWLEDGE_INIT_COMPLETED EMBEDDING_MODEL_ID RERANK_MODEL_ID SYSCTL_FILE_CREATED SYSCTL_FILE_CONTENT OPENSEARCH_INSTALLED_BY_US DRIVE_IMPORT_LAST_ATTEMPT; do
		unset CFG[$key]
	done
	save_state
	
	say "Deployment destroyed"
	say "Reusable credentials and settings preserved"
}

# Main
main() {
	local mode=${1:-setup}
	case "$mode" in
		setup) mode_setup ;;
		start) mode_start ;;
		stop) mode_stop ;;
		destroy) mode_destroy ;;
		*) echo "error: unknown mode '$mode'" >&2; echo "Run $0 --help for usage" >&2; exit 1 ;;
	esac
}

# Act only when executed; sourcing the script just loads its functions.
if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
	main "$@"
fi
