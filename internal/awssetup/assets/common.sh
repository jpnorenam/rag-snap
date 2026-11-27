#!/usr/bin/env bash
# common.sh - shared functions for local and AWS RAG automation
# Sourced by both local-setup.sh and opensearch-on-aws.sh

# --- output masking -----------------------------------------------------------

mask() {
	local line k
	while IFS= read -r line || [ -n "$line" ]; do
		for k in "${MASKED_KEYS[@]}"; do
			[ -n "${CFG[$k]:-}" ] && line=${line//"${CFG[$k]}"/***}
		done
		printf '%s\n' "$line"
	done
}

# --- shared output helpers ----------------------------------------------------

say() { printf '%s\n' "$*"; }

confirm() { # confirm <question>; true on y/yes
	local a
	read -rp "$1 [y/N]: " a
	[[ $a =~ ^[Yy]([Ee][Ss])?$ ]]
}

# --- settings/secrets/state I/O (data only, never sourced) --------------------

has() { [ "${CFG[$1]+set}" = set ]; }

# Format: KEY="value" with \" and \\ as the only escapes (what save_env writes).
# Hand-written KEY=value (unquoted) and KEY='value' are taken literally.
load_env() { # load_env <file> <allowed keys...>
	local file=$1 n=0 line key val dq='^"(([^"\\]|\\["\\])*)"$' sq="^'([^']*)'\$"
	shift
	local -A allowed=()
	for key in "$@"; do allowed[$key]=1; done
	[ -f "$file" ] || return 0
	while IFS= read -r line || [ -n "$line" ]; do
		n=$((n + 1))
		[[ $line =~ ^[[:space:]]*(#|$) ]] && continue
		[[ $line =~ ^([A-Z][A-Z0-9_]*)=(.*)$ ]] || die "$file line $n: expected KEY=value"
		key=${BASH_REMATCH[1]} val=${BASH_REMATCH[2]}
		[ -n "${allowed[$key]:-}" ] || die "$file line $n: unknown key $key"
		if [[ $val =~ $dq ]]; then
			val=${BASH_REMATCH[1]}
			val=${val//\\\\/$'\001'}
			val=${val//\\\"/\"}
			val=${val//$'\001'/\\}
		elif [[ $val =~ $sq ]]; then
			val=${BASH_REMATCH[1]}
		elif [[ $val == [\"\']* ]]; then
			die "$file line $n: unbalanced quotes in the value of $key"
		fi
		CFG[$key]=$val
	done <"$file"
}

save_env() { # save_env <file> <keys...>
	local file=$1 tmp key
	shift
	tmp=$(mktemp "$file.XXXXXX")
	local v
	for key in "$@"; do
		has "$key" || continue
		v=${CFG[$key]//\\/\\\\}
		printf '%s="%s"\n' "$key" "${v//\"/\\\"}" >>"$tmp"
	done
	mv -f "$tmp" "$file"
}

# --- secret generation --------------------------------------------------------

gen_secret() {
	local s
	s=$(head -c 512 /dev/urandom | LC_ALL=C tr -dc 'A-Za-z0-9')
	echo "${s:0:32}"
}

# --- rag-cli configuration with verification ----------------------------------

rag_set() { # rag_set <key> <value>: package-scope set, then verify the effective value
	sudo "$RAG" set --package "$1=$2" || die "sudo $RAG set --package $1=... failed"
	local got
	got=$("$RAG" get "$1" 2>/dev/null) || got=
	[ "$got" = "$2" ] || die "rag-cli $1 is '$got' after setting '$2': a user-layer value overrides it; run 'sudo $RAG set $1=$2' and rerun"
}

# --- rag-cli endpoint configuration -------------------------------------------

# configure_rag_cli <opensearch_host>: point rag-cli at the OpenSearch endpoint
# and set the Tika and chat settings. The endpoint host differs between
# deployments and is passed in; the remaining values are the same for both.
configure_rag_cli() {
	local host=$1
	rag_set knowledge.http.host "$host"
	rag_set knowledge.http.port 9200
	rag_set knowledge.http.tls true
	rag_set tika.http.host 127.0.0.1
	rag_set tika.http.port 9998
	rag_set tika.http.path tika
	rag_set chat.http.host "${CFG[CHAT_HOST]}"
	rag_set chat.http.port "${CFG[CHAT_PORT]}"
	rag_set chat.http.path "${CFG[CHAT_PATH]}"
	rag_set chat.http.tls "${CFG[CHAT_TLS]}"
	rag_set chat.model "${CFG[CHAT_MODEL]}"
}

# start_tika_and_wait: enable and start the Tika server, then wait (bounded) for
# it to answer on http://127.0.0.1:9998/tika. Fails the run if it cannot be
# started or does not become ready, so a failed Tika never looks like success.
start_tika_and_wait() {
	sudo snap start --enable "${CFG[RAG_SNAP_INSTANCE]}.tika-server" >/dev/null ||
		die "could not start ${CFG[RAG_SNAP_INSTANCE]}.tika-server (see: sudo snap logs ${CFG[RAG_SNAP_INSTANCE]}.tika-server)"
	# Bounded wait, overridable so tests do not sleep for two minutes.
	local _ tries=${RAG_TIKA_TRIES:-24} interval=${RAG_TIKA_INTERVAL:-5}
	for _ in $(seq 1 "$tries"); do
		curl -fsS --max-time 5 -o /dev/null http://127.0.0.1:9998/tika && { say "Tika is ready"; return 0; }
		sleep "$interval"
	done
	die "Tika did not answer on http://127.0.0.1:9998/tika within $((tries * interval)) s (see: sudo snap logs ${CFG[RAG_SNAP_INSTANCE]}.tika-server)"
}

# --- credentials.json ---------------------------------------------------------

# write_credentials_json <file> <password> <api_key> <base-object>: write the
# OpenSearch credentials to <file> atomically (mode 0600): OPENSEARCH_USERNAME,
# OPENSEARCH_PASSWORD and, when <api_key> is non-empty, CHAT_API_KEY, layered on
# the JSON object in <base-object>. Pass '{}' to replace the file, or the
# current contents to keep the keys it already holds. Never prints a secret.
write_credentials_json() {
	local f=$1 pass=$2 key=$3 base=$4 tmp
	tmp=$(mktemp "$f.XXXXXX")
	chmod 600 "$tmp"
	printf '%s' "$base" | jq \
		--arg pass "$pass" --arg key "$key" \
		'. + {OPENSEARCH_USERNAME: "admin", OPENSEARCH_PASSWORD: $pass}
		 + (if $key != "" then {CHAT_API_KEY: $key} else {} end)' >"$tmp" || {
		rm -f "$tmp"
		die "could not write $f"
	}
	mv -f "$tmp" "$f"
}

# --- model initialization -----------------------------------------------------

# model_id <Embedding|Rerank> <file>: extract the ID printed by `knowledge init`.
model_id() {
	tr -d '\r' <"$2" | sed 's/\x1b\[[0-9;?]*[A-Za-z]//g' |
		sed -nE "s/.*$1 model ID: ([A-Za-z0-9_-]+)[[:space:]]*$/\1/p" | tail -n 1
}

# knowledge_init_and_extract <logfile>: run `knowledge init`, keep its output in
# <logfile> and show it live, configure the model IDs it reports and expose them
# as EMB_MODEL_ID / RER_MODEL_ID for the caller to record. Dies if either ID is
# missing, so a failed or partial init never looks successful.
knowledge_init_and_extract() {
	local log=$1
	"$RAG" knowledge init 2>&1 | mask | tee "$log" || die "knowledge init failed"
	EMB_MODEL_ID=$(model_id Embedding "$log")
	RER_MODEL_ID=$(model_id Rerank "$log")
	[ -n "$EMB_MODEL_ID" ] && [ -n "$RER_MODEL_ID" ] ||
		die "knowledge init did not report both model IDs"
	rag_set knowledge.model.embedding "$EMB_MODEL_ID"
	rag_set knowledge.model.rerank "$RER_MODEL_ID"
}

# --- Google Drive import -------------------------------------------------------

# drive_import_guidance: how to finish the Google sign-in the import command starts.
drive_import_guidance() {
	cat <<-'EOF'
		Google sign-in uses a local callback on 127.0.0.1. If no browser opens here:
		  a) on the machine with the browser, first run: ssh -L <port>:127.0.0.1:<port> <you>@<this-host>
		     (<port> is in the redirect_uri of the printed URL), then open the URL there; or
		  b) open the URL anywhere, finish consent, copy the failed
		     http://127.0.0.1:<port>/?... page and run: curl '<that address>' on this machine
		     within 5 minutes.
	EOF
}

# drive_import_folder <logfile>: confirm a repeat run, show the sign-in guidance,
# run the import with the saved OAuth client and cached token (output masked into
# <logfile> and shown), and record the attempt in CFG[DRIVE_IMPORT_LAST_ATTEMPT].
# Returns the import command's own exit status; 0 also means the repeat run was
# declined. Whether a non-zero status fails the caller is the caller's policy.
drive_import_folder() {
	local log=$1 rc=0
	if [ -n "${CFG[DRIVE_IMPORT_LAST_ATTEMPT]:-}" ]; then
		confirm "A previous import attempt finished at ${CFG[DRIVE_IMPORT_LAST_ATTEMPT]}. Run the import again?" ||
			return 0
	fi
	say "Importing from Google Drive..."
	drive_import_guidance
	GOOGLE_DRIVE_CLIENT_ID=${CFG[GOOGLE_DRIVE_CLIENT_ID]} \
	GOOGLE_DRIVE_CLIENT_SECRET=${CFG[GOOGLE_DRIVE_CLIENT_SECRET]} \
		"$RAG" knowledge import --url "${CFG[DRIVE_FOLDER_URL]}" --all 2>&1 |
		mask | tee "$log" || rc=${PIPESTATUS[0]}
	CFG[DRIVE_IMPORT_LAST_ATTEMPT]=$(date -Iseconds)
	return "$rc"
}
