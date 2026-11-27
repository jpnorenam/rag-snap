#!/usr/bin/env bash
# Caller-flow regression: local-setup.sh -> bootstrap-opensearch.sh.
#
# This test executes the real local caller and the real shared bootstrap control
# flow. Nothing is reimplemented: local-setup.sh and bootstrap-opensearch.sh are
# copied verbatim into a temporary sandbox (the copies are checksum-compared with
# the originals), sourced so their functions load without running setup, and then
# their own entry points are called. Only two things are substituted:
#
#   1. The hard-coded system paths, reassigned AFTER the production scripts have
#      initialized, including every derived path (CONF, CERTS, MARKER, ...).
#   2. External commands (snap, sudo, curl, systemctl, sysctl, awk, id, install
#      and the rag-cli binary), stubbed on PATH. The stubs refuse any absolute
#      path outside the sandbox and the stub curl never opens a socket, so the
#      test cannot mutate host configuration or reach a real service.
#
# Scenarios:
#   A. Initialized but stopped installation whose saved heap is too large to
#      start. Setting a smaller OPENSEARCH_HEAP_GIB and rerunning setup must
#      write the heap before the daemon is started or the endpoint is probed,
#      and must then reach authenticated readiness.
#   B. Healthy installation already running the requested heap: no rewrite, no
#      restart.
#   C. Fresh (uninitialized) installation: TLS setup, heap, admin hash,
#      daemon start and security initialization still run, in that order.
#   D. Invalid saved OPENSEARCH_HEAP_GIB: setup must fail before it generates
#      secrets, installs the snap, touches sysctl or starts anything.
#   E. The shared bootstrap's --heap argument: every invalid and missing form
#      must fail before the secrets file is consumed and before any mutation,
#      and a valid value must be written as both -Xms and -Xmx.
#   F. --heap omitted (the AWS invocation): the RAM-based calculation is kept.
set -euo pipefail

ASSETS=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

TESTS_RUN=0
TESTS_FAILED=0

SAVED_PW=savedadminpasswordsavedadminpw4

ok() {
	printf '  \xe2\x9c\x93 %s\n' "$1"
	TESTS_RUN=$((TESTS_RUN + 1))
}

no() {
	printf '  \xe2\x9c\x97 %s\n      %s\n' "$1" "$2"
	TESTS_RUN=$((TESTS_RUN + 1))
	TESTS_FAILED=$((TESTS_FAILED + 1))
}

sha() { sha256sum "$1" | awk '{print $1}'; }

# Heap written for a given size, exactly as the shared writer produces it.
heap_text() { printf -- '-Xms%sg\n-Xmx%sg\n' "$1" "$1"; }

# ---------------------------------------------------------------------------
# Sandbox construction
# ---------------------------------------------------------------------------

# make_sandbox <name>: build a self-contained sandbox and echo its root.
# Scenario shape is read from these variables:
#   SC_SETTINGS_HEAP  heap requested through settings.env (local default is 6)
#   SC_SAVED_HEAP     heap already on disk, empty for none
#   SC_MARKER         yes|no   daemon-started marker (installation initialized)
#   SC_CERTS          yes|no   TLS material present
#   SC_SECINIT        yes|no   security index initialized
#   SC_DAEMON         yes|no   daemon currently active
#   SC_INSTALLED      yes|no   OpenSearch snap installed
#   SC_SECRETS        yes|no   secrets.env already holds deployment secrets
#   SC_STATE          yes|no   state.env exists
#   SC_MEMINFO        MemTotal in kB for the RAM calculation, empty for none
#   SC_MEM_LIMIT_GIB  largest heap the stubbed daemon can start with
# Optional knobs, defaulted by reset_scenario:
#   SC_YML_EXTRA      extra opensearch.yml lines (binding overrides to correct)
#   SC_YML_BOUND      yes|no   already carries the canonical loopback binding
#   SC_SETUP_REWRITES yes|no   opensearch.setup regenerates opensearch.yml
#   SC_BOUND          address the node reports as bound, empty for the real one
#   SC_BOUND_SHAPE    full|http-only|empty|malformed|no-nodes|transport-wildcard
#   SC_MAXMAP         value the sysctl stub reports for vm.max_map_count
#   SC_SYSCTL_W       ok|fail|ineffective  behavior of `sysctl -w`
#   SC_SYSCTL_FILE    pre-existing content of the local sysctl file, empty for none
#   SC_SNAP_SET       ok|fail              behavior of `snap set set-sysctl-props`
#   SC_SNAP_GET       no|yes               value `snap get set-sysctl-props` reports
#   SC_STATE_EXTRA    extra state.env lines
#   SC_SETTINGS_SHAPE full|partial  partial omits every CHAT_* and DRIVE_IMPORT
#   SC_SETTINGS_EXTRA extra settings.env lines (overrides, for invalid values)
#   SC_API_KEY        CHAT_API_KEY saved in secrets.env, empty for none
#   SC_CREDS          pre-existing credentials.json content, empty for none
#   SC_IMPORT_RC      exit status of the stubbed `rag-cli.rag knowledge import`

# reset_scenario: defaults for the optional knobs, so each scenario sets only
# what it is about.
reset_scenario() {
	SC_YML_EXTRA=
	SC_YML_BOUND=no
	SC_SETUP_REWRITES=no
	SC_BOUND=127.0.0.1
	SC_BOUND_SHAPE=full
	SC_MAXMAP=262144
	SC_SYSCTL_W=ok
	SC_SYSCTL_FILE=
	SC_SNAP_SET=ok
	SC_SNAP_GET=no
	SC_STATE_EXTRA=
	SC_SETTINGS_SHAPE=full
	SC_SETTINGS_EXTRA=
	SC_API_KEY=
	SC_CREDS=
	SC_IMPORT_RC=0
	SC_MODEL_STATE=deployed
	SC_MODEL_STATES=
	SC_TIKA=ok
}
reset_scenario

make_sandbox() {
	local sb=$WORK/$1
	local sysroot=$sb/sysroot
	local conf=$sysroot/var/snap/opensearch/current/etc/opensearch

	mkdir -p "$sb"/{assets,bin,drivers,usercommon,ragcfg} \
		"$conf"/{jvm.options.d,opensearch-security,certificates} \
		"$sysroot"/var/snap/opensearch/current/usr/share/opensearch/plugins/opensearch-security/tools \
		"$sysroot"/var/lib/rag-bootstrap "$sysroot"/etc/sysctl.d "$sysroot"/snap-logs

	# Production code, copied verbatim (verified below).
	cp "$ASSETS/local-setup.sh" "$ASSETS/common.sh" "$ASSETS/bootstrap-opensearch.sh" "$sb/assets/"

	# Snap context normally exported by `rag-cli.rag prepare-script local`.
	cat >"$sb/assets/snap-context.env" <<-EOF
		RAG_SNAP_INSTANCE="rag-cli"
		RAG_SNAP_USER_COMMON="$sb/usercommon"
	EOF

	{
		echo 'OPENSEARCH_CHANNEL="2/stable"'
		echo "OPENSEARCH_HEAP_GIB=\"$SC_SETTINGS_HEAP\""
		if [ "$SC_SETTINGS_SHAPE" = full ]; then
			echo 'CHAT_HOST="localhost"'
			echo 'CHAT_PORT="8000"'
			echo 'CHAT_PATH="/v1/chat/completions"'
			echo 'CHAT_TLS="false"'
			echo 'CHAT_MODEL="default"'
			echo 'DRIVE_IMPORT="no"'
		fi
		[ -n "$SC_SETTINGS_EXTRA" ] && printf '%s\n' "$SC_SETTINGS_EXTRA"
	} >"$sb/assets/settings.env"

	# Saved deployment secrets: these must be reused, never regenerated.
	if [ "$SC_SECRETS" = yes ]; then
		{
			echo 'TLS_ROOT_PASS="rootpassrootpassrootpassrootpas1"'
			echo 'TLS_ADMIN_PASS="adminpassadminpassadminpassadmi2"'
			echo 'TLS_NODE_PASS="nodepassnodepassnodepassnodepas3"'
			echo "OPENSEARCH_ADMIN_PASSWORD=\"$SAVED_PW\""
			echo "CHAT_API_KEY=\"$SC_API_KEY\""
		} >"$sb/assets/secrets.env"
	fi
	[ -n "$SC_CREDS" ] && printf '%s' "$SC_CREDS" >"$sb/usercommon/credentials.json"

	if [ "$SC_STATE" = yes ]; then
		{
			echo 'OPENSEARCH_INSTALLED_BY_US="yes"'
			echo 'OPENSEARCH_CHANNEL_INSTALLED="2/stable"'
			echo 'RAG_CLI_CONFIGURED="2026-10-01T00:00:00+00:00"'
			echo 'KNOWLEDGE_INIT_COMPLETED="2026-10-01T00:05:00+00:00"'
			echo 'EMBEDDING_MODEL_ID="emb-saved"'
			echo 'RERANK_MODEL_ID="rer-saved"'
			if [ "$SC_MARKER" = yes ]; then
				echo 'OPENSEARCH_SETUP_COMPLETED="2026-10-01T00:02:00+00:00"'
			fi
			[ -n "$SC_STATE_EXTRA" ] && printf '%s\n' "$SC_STATE_EXTRA"
		} >"$sb/assets/state.env"
	fi

	# Installed-snap configuration tree.
	printf -- '-Xss1m\n' >"$conf/jvm.options"
	chmod 660 "$conf/jvm.options"
	[ -n "$SC_SAVED_HEAP" ] && heap_text "$SC_SAVED_HEAP" >"$conf/jvm.options.d/heap.options"
	{
		echo 'cluster.name: rag'
		echo 'path.data: /var/snap/opensearch/common/var/lib/opensearch'
		[ -n "$SC_YML_EXTRA" ] && printf '%s\n' "$SC_YML_EXTRA"
		[ "$SC_CERTS" = yes ] && echo 'node.name: "rag-local"'
		# The canonical form the shared bootstrap writes, for scenarios that
		# start from an already correctly bound installation.
		[ "$SC_YML_BOUND" = yes ] &&
			printf 'network.host: 127.0.0.1\nhttp.host: 127.0.0.1\ntransport.host: 127.0.0.1\n'
	} >"$conf/opensearch.yml"
	chmod 660 "$conf/opensearch.yml"

	cat >"$conf/opensearch-security/internal_users.yml" <<-'EOF'
		_meta:
		  type: "internalusers"
		  config_version: 2

		admin:
		  hash: "$2y$12$oldoldoldoldoldoldoldoldoldoldoldoldoldoldoldoldoldol"
		  reserved: true
		  backend_roles:
		  - "admin"
		  description: "admin user"
	EOF

	if [ "$SC_CERTS" = yes ]; then
		local f
		for f in root-ca.pem admin.pem admin-key.pem node-rag-local.pem node-rag-local-key.pem; do
			echo "saved certificate $f" >"$conf/certificates/$f"
		done
		# An initialized node already has its hash applied and the original kept.
		cp "$conf/opensearch-security/internal_users.yml" \
			"$conf/opensearch-security/internal_users.yml.rag-orig"
	fi

	# The security plugin's hash tool, invoked as `bash <path>` by the bootstrap.
	cat >"$sysroot/var/snap/opensearch/current/usr/share/opensearch/plugins/opensearch-security/tools/hash.sh" <<-'EOF'
		#!/usr/bin/env bash
		echo 'Rehashing...'
		echo '$2y$12$newnewnewnewnewnewnewnewnewnewnewnewnewnewnewnewnewne'
	EOF

	[ "$SC_MARKER" = yes ] && touch "$sysroot/var/lib/rag-bootstrap/daemon-started"
	# An initialized installation already has its models configured and deployed.
	printf '%s' "$SC_MODEL_STATE" >"$sb/model-state"
	: >"$sb/model-states"
	for _m in $SC_MODEL_STATES; do printf '%s %s\n' "${_m%%=*}" "${_m#*=}" >>"$sb/model-states"; done
	printf '%s' "$SC_TIKA" >"$sb/tika"
	if [ "$SC_MARKER" = yes ]; then
		printf 'emb-saved' >"$sb/ragcfg/knowledge.model.embedding"
		printf 'rer-saved' >"$sb/ragcfg/knowledge.model.rerank"
	fi
	[ "$SC_DAEMON" = yes ] && echo active >"$sb/daemon.state"
	[ "$SC_SECINIT" = yes ] && touch "$sb/security.initialized"
	[ "$SC_INSTALLED" = yes ] && touch "$sb/snap.installed"
	[ -n "$SC_MEMINFO" ] && printf 'MemTotal:       %s kB\n' "$SC_MEMINFO" >"$sb/meminfo"
	[ -n "$SC_SYSCTL_FILE" ] && printf '%s\n' "$SC_SYSCTL_FILE" >"$sysroot/etc/sysctl.d/60-rag-local-opensearch.conf"
	printf '%s' "$SC_MAXMAP" >"$sb/maxmap"
	printf '%s' "$SC_BOUND" >"$sb/bound"
	printf '%s' "$SC_BOUND_SHAPE" >"$sb/bound-shape"
	printf '%s' "$SC_SYSCTL_W" >"$sb/sysctl-w"
	printf '%s' "$SC_SNAP_SET" >"$sb/snap-set"
	printf '%s' "$SC_SNAP_GET" >"$sb/snap-get"
	printf '%s' "$SC_SETUP_REWRITES" >"$sb/setup-rewrites"
	printf '%s' "$SC_IMPORT_RC" >"$sb/import-rc"

	: >"$sb/events.log"

	write_stubs "$sb"
	write_drivers "$sb"
	echo "$sb"
}

write_drivers() {
	local sb=$1

	# Loads the real local caller, then re-points its system paths at the
	# sandbox. The overrides run after the script has initialized, so they are
	# not overwritten by the production assignments.
	cat >"$sb/drivers/local-driver.sh" <<-'EOF'
		#!/usr/bin/env bash
		set -euo pipefail
		source "$SB/assets/local-setup.sh"
		SYSCTL_CONF=$SYSROOT/etc/sysctl.d/60-rag-local-opensearch.conf
		BOOTSTRAP_STATE_MARKER=$SYSROOT/var/lib/rag-bootstrap/daemon-started
		URL=$TEST_URL
		# Also redirected for a caller that still holds the config paths itself,
		# so this test can be run against an older revision for comparison.
		SNAP_DATA=$SYSROOT/var/snap/opensearch/current
		OPENSEARCH_CONF=$SNAP_DATA/etc/opensearch
		OPENSEARCH_YML=$OPENSEARCH_CONF/opensearch.yml
		main "$@"
	EOF

	# The local caller runs the bootstrap through `sudo bash <bootstrap>`; the
	# sudo stub redirects that to this driver, which loads the real bootstrap and
	# re-points its paths the same way before calling its own main. Scenarios E
	# and F call this driver directly, the way opensearch-on-aws.sh does.
	cat >"$sb/drivers/bootstrap-driver.sh" <<-'EOF'
		#!/usr/bin/env bash
		set -euo pipefail
		source "$SB/assets/bootstrap-opensearch.sh"
		SNAP_DATA_DIR=$SYSROOT/var/snap/opensearch/current
		CONF=$SNAP_DATA_DIR/etc/opensearch
		CERTS=$CONF/certificates
		PLUGINS=$SNAP_DATA_DIR/usr/share/opensearch/plugins
		JDK=$SYSROOT/jdk
		SNAP_LOGS=$SYSROOT/snap-logs
		CLUSTER_LOG=$SYSROOT/cluster.log
		STATE_DIR=$SYSROOT/var/lib/rag-bootstrap
		MARKER=$STATE_DIR/daemon-started
		SYSCTL_FILE=$SYSROOT/etc/sysctl.d/60-rag-opensearch.conf
		URL=$TEST_URL
		main "$@"
	EOF
	chmod +x "$sb/drivers"/*.sh
}

write_stubs() {
	local d=$1/bin

	# Shared stub preamble: event log with a heap snapshot, sandbox path guard.
	cat >"$d/_stub-common" <<-'EOF'
		heap_now() {
			if [ -f "$HEAP_FILE" ]; then tr '\n' ' ' <"$HEAP_FILE" | sed 's/ *$//'; else printf 'absent'; fi
		}
		bind_now() {
			local f=$SYSROOT/var/snap/opensearch/current/etc/opensearch/opensearch.yml
			sed -nE 's/^[[:space:]]*"?network\.host"?[[:space:]]*:[[:space:]]*(.*)$/\1/p' "$f" 2>/dev/null |
				tail -n 1 | tr -d ' ' | grep . || printf 'absent'
		}
		event() { printf '%s heap=[%s]\n' "$1" "$(heap_now)" >>"$EVENTS"; }
		forbid() {
			printf 'FORBIDDEN %s heap=[%s]\n' "$1" "$(heap_now)" >>"$EVENTS"
			echo "stub: refusing $1" >&2
			exit 99
		}
		# Any absolute path must stay inside the sandbox. The two /tmp templates
		# are created by the scripts under test themselves.
		guard_path() {
			case $1 in
			"$SB"/*) return 0 ;;
			/tmp/opensearch-secrets.* | /tmp/knowledge-init.* | /tmp/rag-sysctl.*) return 0 ;;
			/*) forbid "path outside sandbox: $1" ;;
			esac
		}
	EOF

	cat >"$d/sudo" <<-'EOF'
		#!/usr/bin/env bash
		set -uo pipefail
		source "$SB/bin/_stub-common"
		for a in "$@"; do guard_path "$a"; done
		if [ "${1:-}" = bash ]; then
			prog=${2:-}
			case $prog in
			*/bootstrap-opensearch.sh)
				shift 2
				event bootstrap-invoked
				exec bash "$SB/drivers/bootstrap-driver.sh" "$@"
				;;
			*) forbid "sudo bash $prog" ;;
			esac
		fi
		exec "$@"
	EOF

	cat >"$d/snap" <<-'EOF'
		#!/usr/bin/env bash
		set -uo pipefail
		source "$SB/bin/_stub-common"
		daemon_state() { cat "$SB/daemon.state" 2>/dev/null || echo inactive; }
		# Start the daemon only if the configured heap fits in memory, which is
		# what makes an oversized heap unrecoverable without rewriting the file.
		try_start() {
			printf 'bind-seen:%s\n' "$(bind_now)" >>"$EVENTS"
			local want
			want=$(sed -n 's/^-Xmx\([0-9]*\)g$/\1/p' "$HEAP_FILE" 2>/dev/null | tail -n 1)
			if [ -n "$want" ] && [ "$want" -le "$MEM_LIMIT_GIB" ]; then
				echo active >"$SB/daemon.state"
			else
				echo inactive >"$SB/daemon.state"
				event "daemon-start-refused(heap>${MEM_LIMIT_GIB}g)"
			fi
		}
		case "$*" in
		"list opensearch")
			[ -f "$SB/snap.installed" ] || { echo 'error: no matching snaps installed' >&2; exit 1; }
			printf 'Name        Version  Rev  Tracking  Publisher   Notes\n'
			printf 'opensearch  2.19.4   123  2/stable  canonical*  -\n'
			;;
		"list rag-cli") printf 'Name     Version  Rev  Tracking     Publisher  Notes\nrag-cli  0.1      7    latest/edge  -          -\n' ;;
		"install opensearch"*)
			event snap-install
			touch "$SB/snap.installed"
			;;
		"services opensearch.daemon")
			printf 'Service            Startup   Current   Notes\n'
			printf 'opensearch.daemon  enabled   %s  -\n' "$(daemon_state)"
			;;
		"connections opensearch")
			printf 'Interface  Plug  Slot  Notes\n'
			for p in process-control log-observe mount-observe sys-fs-cgroup-service system-observe; do
				printf '%s  opensearch:%s  :%s  -\n' "$p" "$p" "$p"
			done
			;;
		"get opensearch set-sysctl-props") cat "$SB/snap-get" ;;
		"set opensearch set-sysctl-props=no")
			event snap-set-sysctl-props
			[ "$(cat "$SB/snap-set")" = ok ] || { echo 'error: cannot perform the following tasks' >&2; exit 1; }
			;;
		"start --enable opensearch.daemon")
			event daemon-start
			try_start
			;;
		"restart opensearch.daemon")
			event daemon-restart
			try_start
			;;
		"start --enable rag-cli.tika-server")
			event tika-start
			[ "$(cat "$SB/tika")" != start-fail ] || exit 1
			;;
		"stop --disable opensearch.daemon")
			event daemon-stop
			echo inactive >"$SB/daemon.state"
			;;
		"stop --disable rag-cli.tika-server") event tika-stop ;;
		"remove opensearch --purge")
			event snap-remove
			rm -f "$SB/snap.installed"
			;;
		"run opensearch.setup"*)
			event opensearch-setup
			conf=$SYSROOT/var/snap/opensearch/current/etc/opensearch
			for f in root-ca.pem admin.pem admin-key.pem node-rag-local.pem node-rag-local-key.pem; do
				echo "generated certificate $f" >"$conf/certificates/$f"
			done
			if [ "$(cat "$SB/setup-rewrites")" = yes ]; then
				# Models scripts/wrappers/setup.sh from the opensearch-snap
				# repository source, which sets network.host to its
				# [_local_, _site_] default: anything written before setup
				# is replaced, and the node is no longer loopback-only.
				# Written as a whole file here, which is the worst case.
				{
					echo '# Generated by opensearch.setup'
					echo 'cluster.name: opensearch'
					echo 'path.data: /var/snap/opensearch/common/var/lib/opensearch'
					echo 'network.host: ["_local_", "_site_"]'
					echo 'node.name: "rag-local"'
					echo 'node.roles: [cluster_manager, data, ingest, ml]'
					echo 'plugins.security.ssl.transport.enabled: true'
				} >"$conf/opensearch.yml"
			else
				grep -q '^node\.name:' "$conf/opensearch.yml" || echo 'node.name: "rag-local"' >>"$conf/opensearch.yml"
			fi
			;;
		"run opensearch.security-init"*)
			event security-init
			touch "$SB/security.initialized"
			;;
		*) forbid "snap $*" ;;
		esac
	EOF

	cat >"$d/curl" <<-'EOF'
		#!/usr/bin/env bash
		set -uo pipefail
		source "$SB/bin/_stub-common"
		out= want_code= url= use_stdin=no
		while [ $# -gt 0 ]; do
			case $1 in
			-o) out=$2; shift 2 ;;
			-w) want_code=yes; shift 2 ;;
			-K) use_stdin=yes; shift 2 ;;
			http://* | https://*) url=$1; shift ;;
			*) shift ;;
			esac
		done
		creds=
		[ "$use_stdin" = yes ] && creds=$(cat)
		respond() { # respond <code> <body>
			if [ -n "$out" ]; then printf '%s' "$2" >"$out"; else printf '%s' "$2"; fi
			[ -n "$want_code" ] && printf '%s' "$1"
			exit 0
		}
		case $url in
		*9998/tika)
			event probe-tika
			[ "$(cat "$SB/tika")" != not-ready ] || exit 7
			printf 'Apache Tika\n'
			exit 0
			;;
		"$TEST_URL"*) ;;
		*) forbid "curl to $url" ;;
		esac
		path=${url#"$TEST_URL"}
		case $path in
		/_plugins/_security/authinfo) kind=authinfo ;;
		/_cluster/health*) kind=health ;;
		/_cat/nodes*) kind=nodes ;;
		/_nodes/_local/http,transport*) kind=bound ;;
		/_plugins/_ml/models/*) kind=model ;;
		/) kind=root ;;
		*) forbid "curl path $path" ;;
		esac
		if [ "$(cat "$SB/daemon.state" 2>/dev/null || echo inactive)" != active ]; then
			event "probe-$kind:down"
			[ -n "$want_code" ] && printf '000'
			exit 7
		fi
		if [ ! -f "$SB/security.initialized" ]; then
			event "probe-$kind:503"
			respond 503 '{"error":"OpenSearch Security not initialized."}'
		fi
		auth=none
		case $creds in
		*"admin:$SAVED_PW"*) auth=ok ;;
		*admin:*) auth=bad ;;
		esac
		if [ "$auth" = bad ] || { [ "$auth" = none ] && [ "$kind" != root ]; }; then
			event "probe-$kind:401:auth=$auth"
			respond 401 '{"error":"Unauthorized"}'
		fi
		event "probe-$kind:200:auth=$auth"
		case $kind in
		model)
			# The deployment-state check reads model_state per configured model; the
			event records that the check ran, so a short-circuit is visible.
			_mid=${path##*/}
			state=$(sed -n "s/^${_mid} //p" "$SB/model-states" 2>/dev/null | head -1)
			[ -n "$state" ] || state=$(cat "$SB/model-state" 2>/dev/null || echo deployed)
			event "model-check:${_mid}:${state}"
			case $state in
			missing) respond 404 '{"error":"model not found"}' ;;
			auth) respond 401 '{"error":"Unauthorized"}' ;;
			forbidden) respond 403 '{"error":"Forbidden"}' ;;
			registered) respond 200 '{"model_state":"REGISTERED"}' ;;
			*) respond 200 '{"model_state":"DEPLOYED"}' ;;
			esac
			;;
		bound)
			# What the node itself reports, which is the only evidence that
			# external interfaces are excluded.
			b=$(cat "$SB/bound")
			# A node reports IPv6 bound addresses bracketed, as [::1]:9200.
			case $b in *:*) b="[$b]" ;; esac
			case $(cat "$SB/bound-shape") in
			http-only) respond 200 "{\"nodes\":{\"n1\":{\"http\":{\"bound_address\":[\"$b:9200\"]}}}}" ;;
			empty) respond 200 "{\"nodes\":{\"n1\":{\"transport\":{\"bound_address\":[]},\"http\":{\"bound_address\":[\"$b:9200\"]}}}}" ;;
			malformed) respond 200 "{\"nodes\":{\"n1\":{\"transport\":{\"bound_address\":\"$b:9300\"},\"http\":{\"bound_address\":[\"$b:9200\"]}}}}" ;;
			no-nodes) respond 200 '{}' ;;
			mixed-null) respond 200 "{\"nodes\":{\"n1\":{\"transport\":{\"bound_address\":[\"$b:9300\",null]},\"http\":{\"bound_address\":[\"$b:9200\"]}}}}" ;;
			mixed-number) respond 200 "{\"nodes\":{\"n1\":{\"transport\":{\"bound_address\":[\"$b:9300\"]},\"http\":{\"bound_address\":[\"$b:9200\",9200]}}}}" ;;
			transport-wildcard) respond 200 "{\"nodes\":{\"n1\":{\"transport\":{\"bound_address\":[\"0.0.0.0:9300\"]},\"http\":{\"bound_address\":[\"$b:9200\"]}}}}" ;;
			*) respond 200 "{\"nodes\":{\"n1\":{\"transport\":{\"bound_address\":[\"$b:9300\"]},\"http\":{\"bound_address\":[\"$b:9200\"]}}}}" ;;
			esac
			;;
		authinfo) respond 200 '{"user_name":"admin"}' ;;
		health) respond 200 '{"cluster_name":"rag","status":"green","number_of_nodes":1}' ;;
		nodes) respond 200 'cluster_manager,data,ingest,ml 4gb' ;;
		root) [ "$auth" = ok ] && respond 200 '{"version":{"number":"2.19.4"}}' || respond 401 '{"error":"Unauthorized"}' ;;
		esac
	EOF

	cat >"$d/rag-cli.rag" <<-'EOF'
		#!/usr/bin/env bash
		set -uo pipefail
		source "$SB/bin/_stub-common"
		case "${1:-}" in
		set)
			shift
			[ "${1:-}" = --package ] && shift
			printf '%s' "${1#*=}" >"$SB/ragcfg/${1%%=*}"
			;;
		get) cat "$SB/ragcfg/$2" 2>/dev/null || exit 1 ;;
		knowledge)
			event "rag-knowledge-${2:-}"
			if [ "${2:-}" = import ]; then
				echo 'Importing from Google Drive...'
				exit "$(cat "$SB/import-rc")"
			fi
			echo 'Embedding model ID: emb-new'
			echo 'Rerank model ID: rer-new'
			;;
		*) forbid "rag-cli.rag $*" ;;
		esac
	EOF

	cat >"$d/systemctl" <<-'EOF'
		#!/usr/bin/env bash
		case "$*" in
		"is-active --quiet snap.rag-cli.ragd.service") exit 3 ;;
		*) exit 0 ;;
		esac
	EOF

	cat >"$d/journalctl" <<-'EOF'
		#!/usr/bin/env bash
		exit 0
	EOF

	cat >"$d/sysctl" <<-'EOF'
		#!/usr/bin/env bash
		set -uo pipefail
		source "$SB/bin/_stub-common"
		case "${1:-}" in
		-n)
			case ${2:-} in
			vm.max_map_count) cat "$SB/maxmap" ;;
			vm.swappiness) cat "$SB/swappiness" 2>/dev/null || echo 60 ;;
			net.ipv4.tcp_retries2) cat "$SB/tcp_retries2" 2>/dev/null || echo 15 ;;
			*) echo 0 ;;
			esac
			;;
		-w)
			event "sysctl-write:${2:-}"
			key=${2%%=*} val=${2#*=}
			case $(cat "$SB/sysctl-w") in
			fail) echo "sysctl: permission denied on key '$key'" >&2; exit 1 ;;
			ineffective) exit 0 ;;
			esac
			case $key in
			vm.max_map_count) printf '%s' "$val" >"$SB/maxmap" ;;
			vm.swappiness) printf '%s' "$val" >"$SB/swappiness" ;;
			net.ipv4.tcp_retries2) printf '%s' "$val" >"$SB/tcp_retries2" ;;
			esac
			;;
		-p)
			event sysctl-apply
			# The AWS path writes its own file and applies it; mirror that here.
			while IFS='=' read -r key val; do
				case $key in
				vm.max_map_count) printf '%s' "$val" >"$SB/maxmap" ;;
				vm.swappiness) printf '%s' "$val" >"$SB/swappiness" ;;
				net.ipv4.tcp_retries2) printf '%s' "$val" >"$SB/tcp_retries2" ;;
				esac
			done <"${2:-/dev/null}"
			;;
		esac
	EOF

	# Pass-through except the RAM read in the bootstrap's heap calculation, which
	# is answered from the sandbox when the scenario provides a MemTotal.
	cat >"$d/awk" <<-'EOF'
		#!/usr/bin/env bash
		if [ -f "$SB/meminfo" ] && [[ "$*" == */proc/meminfo* ]]; then
			exec /usr/bin/awk '/^MemTotal:/{print $2}' "$SB/meminfo"
		fi
		exec /usr/bin/awk "$@"
	EOF

	# The bootstrap requires uid 0; the sandbox makes privilege unnecessary.
	cat >"$d/id" <<-'EOF'
		#!/usr/bin/env bash
		[ "${1:-}" = -u ] && { echo 0; exit 0; }
		exec /usr/bin/id "$@"
	EOF

	# `install -o root -g root` needs privilege the test does not have.
	cat >"$d/install" <<-'EOF'
		#!/usr/bin/env bash
		args=()
		while [ $# -gt 0 ]; do
			case $1 in
			-o | -g) shift 2 ;;
			*) args+=("$1"); shift ;;
			esac
		done
		exec /usr/bin/install "${args[@]}"
	EOF

	chmod +x "$d"/*
	chmod -x "$d/_stub-common"
}

# sandbox_env <sandbox>: fill ENVA with the environment both drivers run under.
sandbox_env() {
	ENVA=(
		HOME="$1"
		PATH="$1/bin:/usr/bin:/bin"
		SB="$1"
		SYSROOT="$1/sysroot"
		EVENTS="$1/events.log"
		HEAP_FILE="$1/sysroot/var/snap/opensearch/current/etc/opensearch/jvm.options.d/heap.options"
		SAVED_PW="$SAVED_PW"
		MEM_LIMIT_GIB="$SC_MEM_LIMIT_GIB"
		TEST_URL=https://opensearch.invalid:9200
		RAG_BOOTSTRAP_TRIES=3
		RAG_BOOTSTRAP_INTERVAL=1
		RAG_TIKA_TRIES=2
		RAG_TIKA_INTERVAL=1
	)
}

# run_setup <sandbox> <mode>: run the real local caller, recording its status.
run_setup() {
	local sb=$1 mode=$2 rc=0
	shift 2
	sandbox_env "$sb"
	if [ $# -gt 0 ]; then printf '%s\n' "$@" >"$sb/stdin"; else : >"$sb/stdin"; fi
	(
		cd "$sb"
		unset SUDO_USER
		env -i "${ENVA[@]}" bash "$sb/drivers/local-driver.sh" "$mode"
	) >"$sb/run.log" 2>&1 <"$sb/stdin" || rc=$?
	echo "$rc" >"$sb/run.rc"
}

# run_bootstrap <sandbox> <extra args...>: call the shared bootstrap's own main
# the way opensearch-on-aws.sh does. The secrets file is written fresh for each
# run, so whether it survives shows if the run reached secret consumption.
run_bootstrap() {
	local sb=$1 rc=0
	shift
	cat >"$sb/secrets-file.env" <<-EOF
		OPENSEARCH_ADMIN_PASSWORD=$SAVED_PW
		TLS_ROOT_PASS=rootpassrootpassrootpassrootpas1
		TLS_ADMIN_PASS=adminpassadminpassadminpassadmi2
		TLS_NODE_PASS=nodepassnodepassnodepassnodepas3
	EOF
	sandbox_env "$sb"
	(
		cd "$sb"
		env -i "${ENVA[@]}" bash "$sb/drivers/bootstrap-driver.sh" \
			--node-name rag-local --channel 2/stable \
			--secrets-file "$sb/secrets-file.env" "$@"
	) >"$sb/run.log" 2>&1 </dev/null || rc=$?
	echo "$rc" >"$sb/run.rc"
}

# assert_stops_early <label> <sandbox> <message substring>: setup must fail with a
# usable diagnostic and must not have started or configured anything.
assert_stops_early() {
	local label=$1 sb=$2 want=$3 rc problems=()
	rc=$(cat "$sb/run.rc")
	[ "$rc" = 1 ] || problems+=("exit $rc, wanted 1")
	grep -qF -e "$want" "$sb/run.log" || problems+=("missing diagnostic '$want'")
	grep -qE '^(daemon-start|daemon-restart|probe-)' "$sb/events.log" &&
		problems+=("started or probed the service: $(first_service_event "$sb")")
	if [ ${#problems[@]} -eq 0 ]; then
		ok "$label"
	else
		no "$label" "$(printf '%s; ' "${problems[@]}")last log line: $(tail -n 1 "$sb/run.log")"
	fi
}

# first_service_event <sandbox>: first daemon start/restart or endpoint probe.
first_service_event() {
	grep -m1 -E '^(daemon-start|daemon-restart|probe-)' "$1/events.log" || true
}

# mutation_events <sandbox>: every event that changed state or touched a service.
mutation_events() {
	grep -E '^(snap-install|snap-set-sysctl-props|sysctl-|daemon-|opensearch-setup|security-init|tika-start|probe-|rag-)' \
		"$1/events.log" || true
}

echo "==> Caller flow: local-setup.sh -> bootstrap-opensearch.sh"
echo ""

# ---------------------------------------------------------------------------
echo "Scenario A: lowered heap repairs an initialized installation that cannot start"
SC_SETTINGS_HEAP=4 SC_SAVED_HEAP=64 SC_MARKER=yes SC_CERTS=yes SC_SECINIT=yes \
	SC_DAEMON=no SC_INSTALLED=yes SC_SECRETS=yes SC_STATE=yes SC_MEMINFO= \
	SC_MEM_LIMIT_GIB=8
A=$(make_sandbox scenario-a)
CONF_A=$A/sysroot/var/snap/opensearch/current/etc/opensearch
HEAP_A=$CONF_A/jvm.options.d/heap.options
SECRETS_SHA=$(sha "$A/assets/secrets.env")
USERS_SHA=$(sha "$CONF_A/opensearch-security/internal_users.yml")
CERTS_SHA=$(cat "$CONF_A"/certificates/* | sha256sum | awk '{print $1}')

for f in local-setup.sh common.sh bootstrap-opensearch.sh; do
	if [ "$(sha "$A/assets/$f")" = "$(sha "$ASSETS/$f")" ]; then
		ok "sandbox $f is the production file verbatim"
	else
		no "sandbox $f differs from production" "the test must exercise unmodified production code"
	fi
done

run_setup "$A" setup
RC_A=$(cat "$A/run.rc")

if [ "$RC_A" = 0 ]; then
	ok "local-setup.sh setup completed (exit 0)"
else
	no "local-setup.sh setup failed (exit $RC_A)" "$(tail -n 6 "$A/run.log" | tr '\n' '|')"
fi

if [ -f "$HEAP_A" ] && grep -qx -- '-Xms4g' "$HEAP_A" && grep -qx -- '-Xmx4g' "$HEAP_A"; then
	ok "requested heap written: -Xms4g and -Xmx4g"
else
	no "requested heap not written" "heap.options: $(cat "$HEAP_A" 2>&1 | tr '\n' ' ')"
fi

FIRST_A=$(first_service_event "$A")
if [ -n "$FIRST_A" ] && [ "${FIRST_A#*heap=}" = "[-Xms4g -Xmx4g]" ]; then
	ok "heap applied before the start/readiness sequence (first service event: $FIRST_A)"
else
	no "heap not applied before the start/readiness sequence" \
		"first service event: ${FIRST_A:-none}; expected heap=[-Xms4g -Xmx4g]"
fi

if grep -qE '^(daemon-start|daemon-restart|probe-).*Xmx64g' "$A/events.log"; then
	no "daemon started or endpoint probed with the stale 64 GiB heap" \
		"$(grep -E '^(daemon-start|daemon-restart|probe-)' "$A/events.log" | head -n 3 | tr '\n' '|')"
else
	ok "no start or probe used the stale 64 GiB heap"
fi

if grep -q '^probe-authinfo:200:auth=ok$' <(sed 's/ heap=.*//' "$A/events.log"); then
	ok "reached authenticated readiness with the saved admin password"
else
	no "never reached authenticated readiness" "events: $(tr '\n' '|' <"$A/events.log")"
fi

if grep -q 'bootstrap complete' "$A/run.log" && grep -q 'Setup complete' "$A/run.log"; then
	ok "bootstrap and setup both reported completion"
else
	no "flow did not report completion" "$(tail -n 4 "$A/run.log" | tr '\n' '|')"
fi

if grep -q 'TLS_ROOT_PASS="rootpassrootpassrootpassrootpas1"' "$A/assets/secrets.env" &&
	grep -q 'TLS_ADMIN_PASS="adminpassadminpassadminpassadmi2"' "$A/assets/secrets.env" &&
	grep -q 'TLS_NODE_PASS="nodepassnodepassnodepassnodepas3"' "$A/assets/secrets.env" &&
	grep -q "OPENSEARCH_ADMIN_PASSWORD=\"$SAVED_PW\"" "$A/assets/secrets.env"; then
	ok "saved secrets reused, not regenerated"
else
	no "secrets.env lost a saved value" "$(sed 's/=.*/=.../' "$A/assets/secrets.env" | tr '\n' '|')"
fi

if [ "$(jq -r '.OPENSEARCH_PASSWORD' "$A/usercommon/credentials.json")" = "$SAVED_PW" ] &&
	[ "$(jq -r '.OPENSEARCH_USERNAME' "$A/usercommon/credentials.json")" = admin ]; then
	ok "credentials.json carries the saved admin credentials"
else
	no "credentials.json does not carry the saved credentials" \
		"$(cat "$A/usercommon/credentials.json" 2>&1)"
fi

if [ "$(cat "$CONF_A"/certificates/* | sha256sum | awk '{print $1}')" = "$CERTS_SHA" ] &&
	! grep -q '^opensearch-setup' "$A/events.log"; then
	ok "certificates preserved and opensearch.setup not rerun"
else
	no "certificates were regenerated" "events: $(tr '\n' '|' <"$A/events.log")"
fi

if [ "$(sha "$CONF_A/opensearch-security/internal_users.yml")" = "$USERS_SHA" ] &&
	[ -f "$CONF_A/opensearch-security/internal_users.yml.rag-orig" ] &&
	! grep -q '^security-init' "$A/events.log"; then
	ok "security initialization and admin hash left untouched"
else
	no "security state was reinitialized" "events: $(tr '\n' '|' <"$A/events.log")"
fi

if grep -q '^FORBIDDEN' "$A/events.log"; then
	no "a stub refused an out-of-sandbox action" "$(grep '^FORBIDDEN' "$A/events.log" | tr '\n' '|')"
else
	ok "no command attempted to touch host configuration or a real service"
fi

# ---------------------------------------------------------------------------
echo ""
echo "Scenario B: healthy installation already at the requested heap and binding"
reset_scenario
SC_YML_BOUND=yes
SC_SETTINGS_HEAP=6 SC_SAVED_HEAP=6 SC_MARKER=yes SC_CERTS=yes SC_SECINIT=yes \
	SC_DAEMON=yes SC_INSTALLED=yes SC_SECRETS=yes SC_STATE=yes SC_MEMINFO= \
	SC_MEM_LIMIT_GIB=8
B=$(make_sandbox scenario-b)
HEAP_B=$B/sysroot/var/snap/opensearch/current/etc/opensearch/jvm.options.d/heap.options
HEAP_B_SHA=$(sha "$HEAP_B")

run_setup "$B" setup
RC_B=$(cat "$B/run.rc")

if [ "$RC_B" = 0 ]; then
	ok "rerun completed (exit 0)"
else
	no "rerun failed (exit $RC_B)" "$(tail -n 6 "$B/run.log" | tr '\n' '|')"
fi

if [ "$(sha "$HEAP_B")" = "$HEAP_B_SHA" ] && grep -q 'heap already 6 GiB' "$B/run.log"; then
	ok "heap file untouched and reported as already applied"
else
	no "heap file was rewritten" "$(cat "$HEAP_B")"
fi

if ! grep -qE '^(daemon-restart|daemon-start) ' "$B/events.log" &&
	grep -q 'network binding already limited to 127.0.0.1' "$B/run.log"; then
	ok "no restart and no redundant start: neither heap nor binding changed"
else
	no "daemon was restarted or restarted unnecessarily" "$(tr '\n' '|' <"$B/events.log")"
fi

if grep -q 'security already initialized' "$B/run.log" && ! grep -q '^security-init' "$B/events.log"; then
	ok "security initialization skipped on the initialized node"
else
	no "security state was touched" "$(tr '\n' '|' <"$B/events.log")"
fi

# ---------------------------------------------------------------------------
echo ""
echo "Scenario C: fresh uninitialized installation"
SC_SETTINGS_HEAP=6 SC_SAVED_HEAP= SC_MARKER=no SC_CERTS=no SC_SECINIT=no \
	SC_DAEMON=no SC_INSTALLED=yes SC_SECRETS=yes SC_STATE=yes SC_MEMINFO= \
	SC_MEM_LIMIT_GIB=8
C=$(make_sandbox scenario-c)
CONF_C=$C/sysroot/var/snap/opensearch/current/etc/opensearch
HEAP_C=$CONF_C/jvm.options.d/heap.options

run_setup "$C" setup
RC_C=$(cat "$C/run.rc")

if [ "$RC_C" = 0 ]; then
	ok "fresh setup completed (exit 0)"
else
	no "fresh setup failed (exit $RC_C)" "$(tail -n 6 "$C/run.log" | tr '\n' '|')"
fi

if [ -f "$HEAP_C" ] && grep -qx -- '-Xms6g' "$HEAP_C" && grep -qx -- '-Xmx6g' "$HEAP_C"; then
	ok "local default heap written: -Xms6g and -Xmx6g"
else
	no "local default heap not written" "heap.options: $(cat "$HEAP_C" 2>&1 | tr '\n' ' ')"
fi

SEQ_C=$(sed 's/ heap=.*//' "$C/events.log" | grep -E '^(opensearch-setup|daemon-start|security-init)$' | tr '\n' ' ')
if [ "$SEQ_C" = "opensearch-setup daemon-start security-init " ]; then
	ok "fresh order preserved: TLS setup, daemon start, security initialization"
else
	no "fresh install order changed" "got: ${SEQ_C:-none}"
fi

FIRST_C=$(first_service_event "$C")
if [ "${FIRST_C#*heap=}" = "[-Xms6g -Xmx6g]" ]; then
	ok "heap applied before the daemon start on a fresh install"
else
	no "heap not applied before the daemon start" "first service event: ${FIRST_C:-none}"
fi

if [ -f "$CONF_C/opensearch-security/internal_users.yml.rag-orig" ] &&
	grep -q '2y\$12\$new' "$CONF_C/opensearch-security/internal_users.yml"; then
	ok "admin hash applied on the fresh install"
else
	no "admin hash not applied" "$(grep hash: "$CONF_C/opensearch-security/internal_users.yml" || true)"
fi

if grep -q '^probe-authinfo:200:auth=ok$' <(sed 's/ heap=.*//' "$C/events.log"); then
	ok "fresh install reached authenticated readiness"
else
	no "fresh install never reached authenticated readiness" "$(tr '\n' '|' <"$C/events.log")"
fi

if grep -q '^FORBIDDEN' "$C/events.log"; then
	no "a stub refused an out-of-sandbox action" "$(grep '^FORBIDDEN' "$C/events.log" | tr '\n' '|')"
else
	ok "no command attempted to touch host configuration or a real service"
fi

# ---------------------------------------------------------------------------
echo ""
echo "Scenario D: invalid saved heap is rejected before any deployment change"
SC_SETTINGS_HEAP=abc SC_SAVED_HEAP= SC_MARKER=no SC_CERTS=no SC_SECINIT=no \
	SC_DAEMON=no SC_INSTALLED=no SC_SECRETS=no SC_STATE=no SC_MEMINFO= \
	SC_MEM_LIMIT_GIB=8
D=$(make_sandbox scenario-d)
CONF_D=$D/sysroot/var/snap/opensearch/current/etc/opensearch

run_setup "$D" setup
RC_D=$(cat "$D/run.rc")

if [ "$RC_D" = 1 ]; then
	ok "setup refused the invalid saved heap (exit 1)"
else
	no "setup did not refuse the invalid saved heap (exit $RC_D)" "$(tail -n 4 "$D/run.log" | tr '\n' '|')"
fi

if grep -q 'OPENSEARCH_HEAP_GIB' "$D/run.log" && grep -q "'abc'" "$D/run.log" &&
	grep -qi 'positive integer' "$D/run.log"; then
	ok "diagnostic names the setting, the rejected value and the expected form"
else
	no "diagnostic is not specific enough" "$(tr '\n' '|' <"$D/run.log")"
fi

if [ ! -f "$D/assets/secrets.env" ]; then
	ok "no deployment secrets were generated"
else
	no "secrets.env was created before validation" "$(cat "$D/assets/secrets.env")"
fi

if [ ! -f "$D/snap.installed" ] && ! grep -q '^snap-install' "$D/events.log"; then
	ok "OpenSearch snap was not installed"
else
	no "the snap was installed before validation" "$(tr '\n' '|' <"$D/events.log")"
fi

if [ ! -f "$D/assets/state.env" ]; then
	ok "no installation state was recorded"
else
	no "state.env was written before validation" "$(cat "$D/assets/state.env")"
fi

MUT_D=$(mutation_events "$D")
if [ -z "$MUT_D" ]; then
	ok "no sysctl change, service start or configuration call happened"
else
	no "deployment mutations happened before validation" "$(echo "$MUT_D" | tr '\n' '|')"
fi

if [ ! -f "$CONF_D/jvm.options.d/heap.options" ] &&
	[ ! -f "$D/usercommon/credentials.json" ] &&
	[ ! -f "$D/sysroot/etc/sysctl.d/60-rag-local-opensearch.conf" ]; then
	ok "no heap file, credentials file or sysctl file was created"
else
	no "files were created before validation" \
		"heap=$(ls "$CONF_D/jvm.options.d" 2>&1), creds=$(ls "$D/usercommon" 2>&1)"
fi

# ---------------------------------------------------------------------------
echo ""
echo "Scenario E: shared bootstrap --heap argument handling"

# assert_heap_arg <label> <expected status> <expected message> -- <bootstrap args...>
assert_heap_arg() {
	local label=$1 want_rc=$2 want_msg=$3
	shift 4
	SC_SETTINGS_HEAP=6 SC_SAVED_HEAP=6 SC_MARKER=yes SC_CERTS=yes SC_SECINIT=yes \
		SC_DAEMON=no SC_INSTALLED=yes SC_SECRETS=yes SC_STATE=yes SC_MEMINFO= \
		SC_MEM_LIMIT_GIB=8
	local sb heap before rc
	sb=$(make_sandbox "arg-$(printf '%s' "$label" | tr -c 'a-zA-Z0-9' '-')")
	heap=$sb/sysroot/var/snap/opensearch/current/etc/opensearch/jvm.options.d/heap.options
	before=$(sha "$heap")

	run_bootstrap "$sb" "$@"
	rc=$(cat "$sb/run.rc")

	local problems=()
	[ "$rc" = "$want_rc" ] || problems+=("exit $rc, wanted $want_rc")
	grep -qF -e "$want_msg" "$sb/run.log" || problems+=("missing diagnostic '$want_msg'")
	[ -f "$sb/secrets-file.env" ] || problems+=("the secrets file was consumed")
	[ -z "$(mutation_events "$sb")" ] || problems+=("mutations: $(mutation_events "$sb" | tr '\n' ' ')")
	[ "$(sha "$heap")" = "$before" ] || problems+=("heap.options rewritten to $(tr '\n' ' ' <"$heap")")

	if [ ${#problems[@]} -eq 0 ]; then
		ok "$label rejected before any mutation (exit $want_rc)"
	else
		no "$label not rejected cleanly" "$(printf '%s; ' "${problems[@]}")last log line: $(tail -n 1 "$sb/run.log")"
	fi
}

assert_heap_arg "--heap ''" 2 "invalid --heap value: ''" -- --heap ""
assert_heap_arg "--heap 0" 2 "invalid --heap value: '0'" -- --heap 0
assert_heap_arg "--heap -5" 2 "invalid --heap value: '-5'" -- --heap -5
assert_heap_arg "--heap 1.5" 2 "invalid --heap value: '1.5'" -- --heap 1.5
assert_heap_arg "--heap abc" 2 "invalid --heap value: 'abc'" -- --heap abc
assert_heap_arg "--heap with no value" 2 "--heap requires a value" -- --heap
assert_heap_arg "--heap followed by an option" 2 "--heap requires a value" -- --heap --skip-sysctl

SC_SETTINGS_HEAP=6 SC_SAVED_HEAP=6 SC_MARKER=yes SC_CERTS=yes SC_SECINIT=yes \
	SC_DAEMON=no SC_INSTALLED=yes SC_SECRETS=yes SC_STATE=yes SC_MEMINFO= \
	SC_MEM_LIMIT_GIB=8
E=$(make_sandbox scenario-e-valid)
HEAP_E=$E/sysroot/var/snap/opensearch/current/etc/opensearch/jvm.options.d/heap.options

run_bootstrap "$E" --skip-sysctl --heap 4
RC_E=$(cat "$E/run.rc")

if [ "$RC_E" = 0 ] && grep -qx -- '-Xms4g' "$HEAP_E" && grep -qx -- '-Xmx4g' "$HEAP_E"; then
	ok "valid --heap 4 accepted and written as both -Xms4g and -Xmx4g (exit 0)"
else
	no "valid --heap 4 not applied (exit $RC_E)" \
		"heap: $(tr '\n' ' ' <"$HEAP_E"); $(tail -n 3 "$E/run.log" | tr '\n' '|')"
fi

if ! grep -qE '^sysctl-' "$E/events.log" && [ ! -f "$E/secrets-file.env" ]; then
	ok "--skip-sysctl honored and the secrets file consumed on an accepted run"
else
	no "accepted run did not behave as expected" "$(tr '\n' '|' <"$E/events.log")"
fi

# ---------------------------------------------------------------------------
echo ""
echo "Scenario F: --heap omitted keeps the RAM-based calculation"
SC_SETTINGS_HEAP=6 SC_SAVED_HEAP=12 SC_MARKER=yes SC_CERTS=yes SC_SECINIT=yes \
	SC_DAEMON=no SC_INSTALLED=yes SC_SECRETS=yes SC_STATE=yes SC_MEMINFO=16777216 \
	SC_MEM_LIMIT_GIB=8
F=$(make_sandbox scenario-f)
HEAP_F=$F/sysroot/var/snap/opensearch/current/etc/opensearch/jvm.options.d/heap.options

run_bootstrap "$F"
RC_F=$(cat "$F/run.rc")

# 3/8 of 16 GiB, rounded: taken from the production formula, not restated here.
EXPECT_F=$(bash -c 'source "$1"; heap_gib 16777216' _ "$ASSETS/bootstrap-opensearch.sh")

if [ "$RC_F" = 0 ]; then
	ok "AWS-shaped run with no --heap completed (exit 0)"
else
	no "AWS-shaped run failed (exit $RC_F)" "$(tail -n 5 "$F/run.log" | tr '\n' '|')"
fi

if [ "$EXPECT_F" = 6 ] && grep -qx -- "-Xms${EXPECT_F}g" "$HEAP_F" && grep -qx -- "-Xmx${EXPECT_F}g" "$HEAP_F"; then
	ok "16 GiB of RAM auto-sized to ${EXPECT_F} GiB, replacing the saved 12 GiB"
else
	no "RAM-based calculation not applied" "expected ${EXPECT_F}g, got: $(tr '\n' ' ' <"$HEAP_F")"
fi

if grep -q '^sysctl-apply' "$F/events.log" &&
	grep -q '^probe-authinfo:200:auth=ok$' <(sed 's/ heap=.*//' "$F/events.log"); then
	ok "AWS path still applies sysctl settings and reaches authenticated readiness"
else
	no "AWS path did not complete normally" "$(tr '\n' '|' <"$F/events.log")"
fi


# ---------------------------------------------------------------------------
echo ""
echo "Scenario G: vm.max_map_count below, equal to and above the minimum"

# sysctl_case <name>: a managed, initialized, running installation.
sysctl_case() {
	SC_SETTINGS_HEAP=6 SC_SAVED_HEAP=6 SC_MARKER=yes SC_CERTS=yes SC_SECINIT=yes \
		SC_DAEMON=yes SC_INSTALLED=yes SC_SECRETS=yes SC_STATE=yes SC_MEMINFO= \
		SC_MEM_LIMIT_GIB=8
	make_sandbox "$1"
}

SYSCTL_REL=sysroot/etc/sysctl.d/60-rag-local-opensearch.conf

reset_scenario
SC_MAXMAP=65530
G1=$(sysctl_case sysctl-below)
run_setup "$G1" setup
if [ "$(cat "$G1/run.rc")" = 0 ] &&
	grep -q 'sysctl-write:vm.max_map_count=262144' "$G1/events.log" &&
	grep -q 'Raising vm.max_map_count from 65530 to 262144' "$G1/run.log" &&
	[ "$(cat "$G1/$SYSCTL_REL")" = 'vm.max_map_count=262144' ]; then
	ok "below the minimum: raised to 262144, read back and persisted"
else
	no "below the minimum not raised" \
		"rc=$(cat "$G1/run.rc"), file=$(cat "$G1/$SYSCTL_REL" 2>&1), events=$(grep -c . "$G1/events.log")"
fi

if grep -q 'SYSCTL_FILE_CREATED="yes"' "$G1/assets/state.env" &&
	grep -q 'SYSCTL_FILE_CONTENT="vm.max_map_count=262144"' "$G1/assets/state.env"; then
	ok "ownership and the exact created content are recorded in state"
else
	no "ownership not recorded" "$(cat "$G1/assets/state.env")"
fi

if [ "$(stat -c '%a' "$G1/$SYSCTL_REL")" = 644 ]; then
	ok "the created sysctl file is mode 0644"
else
	no "wrong mode on the sysctl file" "$(stat -c '%a' "$G1/$SYSCTL_REL")"
fi

reset_scenario
SC_MAXMAP=262144
G2=$(sysctl_case sysctl-equal)
run_setup "$G2" setup
if [ "$(cat "$G2/run.rc")" = 0 ] &&
	! grep -q 'sysctl-write' "$G2/events.log" &&
	[ "$(cat "$G2/$SYSCTL_REL")" = 'vm.max_map_count=262144' ]; then
	ok "equal to the minimum: no runtime write, value persisted"
else
	no "equal to the minimum caused a write" "$(grep sysctl-write "$G2/events.log" || true)"
fi

reset_scenario
SC_MAXMAP=500000
G3=$(sysctl_case sysctl-above)
run_setup "$G3" setup
if [ "$(cat "$G3/run.rc")" = 0 ] &&
	! grep -q 'sysctl-write' "$G3/events.log" &&
	[ "$(cat "$G3/maxmap")" = 500000 ] &&
	[ "$(cat "$G3/$SYSCTL_REL")" = 'vm.max_map_count=500000' ]; then
	ok "above the minimum: never lowered, the higher value is what gets persisted"
else
	no "above the minimum was lowered" \
		"effective=$(cat "$G3/maxmap"), file=$(cat "$G3/$SYSCTL_REL" 2>&1)"
fi

if ! grep -qE 'sysctl-write:(vm\.swappiness|net\.ipv4\.tcp_retries2)' "$G1/events.log" "$G2/events.log" "$G3/events.log" &&
	[ ! -f "$G1/swappiness" ] && [ ! -f "$G1/tcp_retries2" ] &&
	! grep -q 'sysctl-apply' "$G1/events.log"; then
	ok "local runs leave vm.swappiness and net.ipv4.tcp_retries2 untouched"
else
	no "local run touched unrelated sysctls" \
		"$(grep -h 'sysctl' "$G1/events.log" "$G2/events.log" "$G3/events.log" || true)"
fi

# ---------------------------------------------------------------------------
echo ""
echo "Scenario H: a required sysctl change that cannot be made"

reset_scenario
SC_MAXMAP=65530 SC_SYSCTL_W=fail
H1=$(sysctl_case sysctl-write-fails)
run_setup "$H1" setup
assert_stops_early "a rejected sysctl write stops setup with a host remedy" "$H1" \
	"could not set vm.max_map_count to 262144"

reset_scenario
SC_MAXMAP=65530 SC_SYSCTL_W=ineffective
H2=$(sysctl_case sysctl-write-ineffective)
run_setup "$H2" setup
assert_stops_early "a write that reads back too low stops setup" "$H2" \
	"still reads 65530, below the required 262144"

if grep -qi 'container' "$H2/run.log" && grep -q 'on the host' "$H2/run.log"; then
	ok "the read-back failure explains the container case and the host remedy"
else
	no "diagnostic is not actionable" "$(tail -n 2 "$H2/run.log")"
fi

# ---------------------------------------------------------------------------
echo ""
echo "Scenario I: the snap's own sysctl handler must be disabled and verified"

reset_scenario
SC_SNAP_SET=fail
I1=$(sysctl_case snap-set-fails)
run_setup "$I1" setup
assert_stops_early "a failing 'snap set set-sysctl-props=no' stops the bootstrap" "$I1" \
	"snap set opensearch set-sysctl-props=no failed"

reset_scenario
SC_SNAP_GET=yes
I2=$(sysctl_case snap-get-mismatch)
run_setup "$I2" setup
assert_stops_early "a saved value that reads back wrong stops the bootstrap" "$I2" \
	"could not set opensearch set-sysctl-props=no"

if grep -q '^snap-set-sysctl-props' "$I2/events.log"; then
	ok "the handler is disabled and verified before the daemon is started"
else
	no "the handler was never disabled" "$(tr '\n' '|' <"$I2/events.log")"
fi

# ---------------------------------------------------------------------------
echo ""
echo "Scenario J: a pre-existing sysctl file is never claimed or overwritten"

reset_scenario
SC_MAXMAP=65530
SC_SYSCTL_FILE='# tuned by the operator
vm.max_map_count=999999'
J1=$(sysctl_case sysctl-preexisting)
BEFORE_J=$(sha "$J1/$SYSCTL_REL")
run_setup "$J1" setup

if [ "$(cat "$J1/run.rc")" = 0 ] && [ "$(sha "$J1/$SYSCTL_REL")" = "$BEFORE_J" ] &&
	grep -q 'already exists; leaving it unchanged' "$J1/run.log"; then
	ok "an existing sysctl file is left byte-identical"
else
	no "an existing sysctl file was modified" "$(cat "$J1/$SYSCTL_REL")"
fi

if ! grep -q 'SYSCTL_FILE_CREATED' "$J1/assets/state.env"; then
	ok "ownership of a pre-existing file is not claimed"
else
	no "claimed a file it did not create" "$(grep SYSCTL "$J1/assets/state.env")"
fi

# ---------------------------------------------------------------------------
echo ""
echo "Scenario K: recorded ownership survives a rerun"

reset_scenario
SC_MAXMAP=262144
K1=$(sysctl_case sysctl-rerun)
run_setup "$K1" setup
STATE_K=$(sha "$K1/assets/state.env")
FILE_K=$(sha "$K1/$SYSCTL_REL")
run_setup "$K1" setup

if [ "$(cat "$K1/run.rc")" = 0 ] &&
	grep -q 'SYSCTL_FILE_CREATED="yes"' "$K1/assets/state.env" &&
	grep -q 'SYSCTL_FILE_CONTENT="vm.max_map_count=262144"' "$K1/assets/state.env" &&
	[ "$(sha "$K1/$SYSCTL_REL")" = "$FILE_K" ]; then
	ok "a rerun keeps the ownership record and the file it created"
else
	no "a rerun lost the ownership record" \
		"state: $(grep SYSCTL "$K1/assets/state.env" || echo none)"
fi

# ---------------------------------------------------------------------------
echo ""
echo "Scenario L: cleanup removes only the file this deployment still owns"

# destroy_case <name> <state extra> <file content>
destroy_case() {
	reset_scenario
	SC_STATE_EXTRA=$2
	SC_SYSCTL_FILE=$3
	SC_SETTINGS_HEAP=6 SC_SAVED_HEAP=6 SC_MARKER=yes SC_CERTS=yes SC_SECINIT=yes \
		SC_DAEMON=yes SC_INSTALLED=yes SC_SECRETS=yes SC_STATE=yes SC_MEMINFO= \
		SC_MEM_LIMIT_GIB=8
	make_sandbox "$1"
}

OWNED='SYSCTL_FILE_CREATED="yes"
SYSCTL_FILE_CONTENT="vm.max_map_count=262144"'

L1=$(destroy_case destroy-owned "$OWNED" 'vm.max_map_count=262144')
MARKER_REL=sysroot/var/lib/rag-bootstrap/daemon-started
[ -f "$L1/$MARKER_REL" ] || no "destroy fixture has no marker" "expected $L1/$MARKER_REL"
run_setup "$L1" destroy y
if [ "$(cat "$L1/run.rc")" = 0 ] && [ ! -f "$L1/$SYSCTL_REL" ] &&
	grep -q "Removed .*60-rag-local-opensearch.conf" "$L1/run.log"; then
	ok "an owned, unchanged file is removed"
else
	no "an owned file was not removed" "rc=$(cat "$L1/run.rc"), $(tail -n 4 "$L1/run.log" | tr '\n' '|')"
fi

# The marker lives outside the snap, so removing the snap alone leaves it behind
# and the next setup would skip TLS setup and fail to start OpenSearch.
if [ ! -f "$L1/$MARKER_REL" ]; then
	ok "destroy clears the shared bootstrap's daemon-started marker"
else
	no "destroy left the stale bootstrap marker" "present: $L1/$MARKER_REL"
fi

L2=$(destroy_case destroy-edited "$OWNED" 'vm.max_map_count=262144
vm.swappiness=1')
run_setup "$L2" destroy y
if [ "$(cat "$L2/run.rc")" = 0 ] && [ -f "$L2/$SYSCTL_REL" ] &&
	grep -q 'it was edited after this setup created it' "$L2/run.log"; then
	ok "a file edited after creation is kept, with the reason given"
else
	no "an edited file was removed or unexplained" "$(tail -n 4 "$L2/run.log" | tr '\n' '|')"
fi

L3=$(destroy_case destroy-unowned '' 'vm.max_map_count=262144')
run_setup "$L3" destroy y
if [ "$(cat "$L3/run.rc")" = 0 ] && [ -f "$L3/$SYSCTL_REL" ] &&
	grep -q 'this setup did not create it' "$L3/run.log"; then
	ok "a file this setup did not create is kept, with the reason given"
else
	no "an unowned file was removed or unexplained" "$(tail -n 4 "$L3/run.log" | tr '\n' '|')"
fi

if ! grep -qE 'sysctl-write' "$L1/events.log" "$L2/events.log" "$L3/events.log"; then
	ok "cleanup does not write runtime sysctl values back"
else
	no "cleanup wrote a runtime sysctl value" \
		"$(grep -h sysctl-write "$L1/events.log" "$L2/events.log" "$L3/events.log")"
fi

# ---------------------------------------------------------------------------
echo ""
echo "Scenario M: the AWS sysctl policy is unchanged"

reset_scenario
SC_MAXMAP=65530
M1=$(sysctl_case aws-sysctl)
run_bootstrap "$M1" --heap 4
AWS_SYSCTL=$M1/sysroot/etc/sysctl.d/60-rag-opensearch.conf

if [ "$(cat "$M1/run.rc")" = 0 ] && grep -q '^sysctl-apply' "$M1/events.log" &&
	[ "$(cat "$AWS_SYSCTL")" = 'vm.max_map_count=262144
vm.swappiness=0
net.ipv4.tcp_retries2=5' ]; then
	ok "AWS still writes and applies all three sysctl settings"
else
	no "AWS sysctl policy changed" "file: $(cat "$AWS_SYSCTL" 2>&1 | tr '\n' '|')"
fi

if [ "$(cat "$M1/maxmap")" = 262144 ] && [ "$(cat "$M1/swappiness")" = 0 ] &&
	[ "$(cat "$M1/tcp_retries2")" = 5 ]; then
	ok "AWS verification reads back all three effective values"
else
	no "AWS values not applied" \
		"maxmap=$(cat "$M1/maxmap"), swappiness=$(cat "$M1/swappiness" 2>&1), retries=$(cat "$M1/tcp_retries2" 2>&1)"
fi

reset_scenario

# ---------------------------------------------------------------------------
echo ""
echo "Scenario N: loopback binding on a fresh install, over what setup writes"

YML_REL=sysroot/var/snap/opensearch/current/etc/opensearch/opensearch.yml

# bind_case <name>: managed installation, scenario knobs already set.
bind_case() {
	SC_SETTINGS_HEAP=6 SC_SAVED_HEAP=6 SC_MARKER=${SC_MARKER:-yes} SC_CERTS=${SC_CERTS:-yes} \
		SC_SECINIT=${SC_SECINIT:-yes} SC_DAEMON=${SC_DAEMON:-no} SC_INSTALLED=yes \
		SC_SECRETS=yes SC_STATE=yes SC_MEMINFO= SC_MEM_LIMIT_GIB=8
	make_sandbox "$1"
}

# yml_value <file> <key>: the effective value of a flat dotted key.
yml_value() {
	sed -nE "s/^[[:space:]]*\"?$2\"?[[:space:]]*:[[:space:]]*(.*)\$/\1/p" "$1" | tail -n 1 | tr -d ' "'
}

reset_scenario
SC_SETUP_REWRITES=yes
SC_MARKER=no SC_CERTS=no SC_SECINIT=no SC_DAEMON=no
N1=$(bind_case bind-fresh)
run_setup "$N1" setup
YML_N=$N1/$YML_REL

if [ "$(cat "$N1/run.rc")" = 0 ] &&
	[ "$(yml_value "$YML_N" 'network\.host')" = 127.0.0.1 ] &&
	[ "$(yml_value "$YML_N" 'http\.host')" = 127.0.0.1 ] &&
	[ "$(yml_value "$YML_N" 'transport\.host')" = 127.0.0.1 ]; then
	ok "fresh install binds REST and transport to loopback"
else
	no "fresh install did not bind to loopback" \
		"rc=$(cat "$N1/run.rc"), yml: $(tr '\n' '|' <"$YML_N")"
fi

if ! grep -q '_site_' "$YML_N"; then
	ok "the value opensearch.setup wrote ([_local_, _site_]) does not survive"
else
	no "setup's non-loopback value survived" "$(grep -n '_site_' "$YML_N")"
fi

if [ "$(grep -c '^bind-seen:' "$N1/events.log")" -ge 1 ] &&
	[ "$(grep -m1 '^bind-seen:' "$N1/events.log")" = 'bind-seen:127.0.0.1' ]; then
	ok "the binding is already loopback when the daemon is first started"
else
	no "the daemon started before the binding was applied" \
		"$(grep '^bind-seen:' "$N1/events.log" | head -n 2 | tr '\n' '|')"
fi

if grep -q 'bound addresses limited to 127.0.0.1' "$N1/run.log"; then
	ok "effective binding is verified from the node's own bound addresses"
else
	no "effective binding was never verified" "$(tail -n 4 "$N1/run.log" | tr '\n' '|')"
fi

if grep -q '^cluster.name: opensearch$' "$YML_N" && grep -q '^node.roles:' "$YML_N" &&
	grep -q '^plugins.security.ssl.transport.enabled: true$' "$YML_N" &&
	grep -q '^node.name: "rag-local"$' "$YML_N"; then
	ok "configuration written by setup is otherwise preserved"
else
	no "unrelated configuration was lost" "$(tr '\n' '|' <"$YML_N")"
fi

# ---------------------------------------------------------------------------
echo ""
echo "Scenario O: an existing non-loopback value and conflicting overrides"

reset_scenario
SC_YML_EXTRA='network.host: 0.0.0.0
http.host: 0.0.0.0
transport.bind_host: 0.0.0.0
http.port: 9200
custom.unrelated: keep-me'
SC_MARKER=yes SC_CERTS=yes SC_SECINIT=yes SC_DAEMON=no
O1=$(bind_case bind-override)
YML_O=$O1/$YML_REL
MODE_O=$(stat -c '%a' "$YML_O")
run_setup "$O1" setup

if [ "$(cat "$O1/run.rc")" = 0 ] &&
	[ "$(yml_value "$YML_O" 'network\.host')" = 127.0.0.1 ] &&
	[ "$(yml_value "$YML_O" 'http\.host')" = 127.0.0.1 ] &&
	[ "$(yml_value "$YML_O" 'transport\.host')" = 127.0.0.1 ] &&
	! grep -q '0\.0\.0\.0' "$YML_O"; then
	ok "a non-loopback value and its http/transport overrides are all corrected"
else
	no "conflicting binding overrides survived" "$(tr '\n' '|' <"$YML_O")"
fi

if ! grep -q 'transport.bind_host' "$YML_O" && ! grep -q 'http.bind_host' "$YML_O"; then
	ok "bind_host overrides are removed, not left to win"
else
	no "a bind_host override was left in place" "$(grep bind_host "$YML_O")"
fi

if grep -q '^custom.unrelated: keep-me$' "$YML_O" && grep -q '^http.port: 9200$' "$YML_O" &&
	grep -q '^cluster.name: rag$' "$YML_O" && grep -q '^path.data:' "$YML_O"; then
	ok "unrelated settings, including http.port, are preserved"
else
	no "unrelated settings were lost" "$(tr '\n' '|' <"$YML_O")"
fi

if [ "$(stat -c '%a' "$YML_O")" = "$MODE_O" ] && [ "$MODE_O" = 660 ] &&
	[ "$(find "$(dirname "$YML_O")" -name 'opensearch.yml.*' | wc -l)" = 0 ]; then
	ok "file mode is preserved and no temporary file is left behind"
else
	no "mode or leftovers wrong" \
		"mode before=$MODE_O after=$(stat -c '%a' "$YML_O"), leftovers=$(find "$(dirname "$YML_O")" -name 'opensearch.yml.*')"
fi

# ---------------------------------------------------------------------------
echo ""
echo "Scenario P: a managed rerun corrects binding and makes it take effect"

reset_scenario
SC_YML_EXTRA='network.host: 0.0.0.0'
SC_MARKER=yes SC_CERTS=yes SC_SECINIT=yes SC_DAEMON=yes
P1=$(bind_case bind-rerun-active)
run_setup "$P1" setup
YML_P=$P1/$YML_REL

if [ "$(cat "$P1/run.rc")" = 0 ] &&
	[ "$(yml_value "$YML_P" 'network\.host')" = 127.0.0.1 ] &&
	grep -q '^daemon-restart ' "$P1/events.log"; then
	ok "a running daemon with the wrong binding is corrected and restarted"
else
	no "the corrected binding was not made effective" \
		"value=$(yml_value "$YML_P" 'network\.host'), events=$(tr '\n' '|' <"$P1/events.log")"
fi

if grep -q 'network binding set to 127.0.0.1' "$P1/run.log"; then
	ok "the change is reported"
else
	no "the change was silent" "$(tail -n 5 "$P1/run.log" | tr '\n' '|')"
fi

# ---------------------------------------------------------------------------
echo ""
echo "Scenario Q: an unchanged binding causes no restart"

reset_scenario
SC_MARKER=yes SC_CERTS=yes SC_SECINIT=yes SC_DAEMON=yes
Q1=$(bind_case bind-unchanged)
run_setup "$Q1" setup
YML_Q=$Q1/$YML_REL
SHA_Q=$(sha "$YML_Q")
: >"$Q1/events.log"
run_setup "$Q1" setup

if [ "$(cat "$Q1/run.rc")" = 0 ] && [ "$(sha "$YML_Q")" = "$SHA_Q" ] &&
	grep -q 'network binding already limited to 127.0.0.1' "$Q1/run.log" &&
	! grep -qE '^daemon-restart ' "$Q1/events.log"; then
	ok "an unchanged binding is left byte-identical with no restart"
else
	no "an unchanged binding caused work" \
		"$(grep -E 'daemon-restart' "$Q1/events.log" || true) $(grep 'network binding' "$Q1/run.log" || true)"
fi

# ---------------------------------------------------------------------------
echo ""
echo "Scenario R: verification rejects listeners beyond loopback"

reset_scenario
SC_BOUND=0.0.0.0
SC_MARKER=yes SC_CERTS=yes SC_SECINIT=yes SC_DAEMON=no
R1=$(bind_case bind-wildcard)
run_setup "$R1" setup

if [ "$(cat "$R1/run.rc")" = 1 ] &&
	grep -q 'listening beyond 127.0.0.1' "$R1/run.log" &&
	grep -q '0.0.0.0:9200' "$R1/run.log" && grep -q '0.0.0.0:9300' "$R1/run.log"; then
	ok "a wildcard listener fails setup even though localhost answered"
else
	no "a wildcard listener was accepted" "rc=$(cat "$R1/run.rc"), $(tail -n 3 "$R1/run.log" | tr '\n' '|')"
fi

if grep -q '^probe-authinfo:200:auth=ok$' <(sed 's/ heap=.*//' "$R1/events.log"); then
	ok "the wildcard case really did serve authenticated localhost requests"
else
	no "the wildcard case failed for an unrelated reason" "$(tr '\n' '|' <"$R1/events.log")"
fi

reset_scenario
SC_BOUND=10.0.0.5
SC_MARKER=yes SC_CERTS=yes SC_SECINIT=yes SC_DAEMON=no
R2=$(bind_case bind-external)
run_setup "$R2" setup
if [ "$(cat "$R2/run.rc")" = 1 ] && grep -q '10.0.0.5:9200' "$R2/run.log"; then
	ok "a non-loopback interface address fails setup"
else
	no "a non-loopback listener was accepted" "rc=$(cat "$R2/run.rc"), $(tail -n 2 "$R2/run.log" | tr '\n' '|')"
fi

reset_scenario
SC_BOUND=::1
SC_MARKER=yes SC_CERTS=yes SC_SECINIT=yes SC_DAEMON=no
R3=$(bind_case bind-ipv6-loopback)
run_setup "$R3" setup
if [ "$(cat "$R3/run.rc")" = 0 ] && grep -q 'bound addresses limited to 127.0.0.1' "$R3/run.log"; then
	ok "the IPv6 loopback a loopback-bound node also reports is accepted"
else
	no "IPv6 loopback was rejected" "rc=$(cat "$R3/run.rc"), $(tail -n 3 "$R3/run.log" | tr '\n' '|')"
fi

# ---------------------------------------------------------------------------
echo ""
echo "Scenario S: AWS binding behavior is unchanged"

reset_scenario
SC_YML_EXTRA='network.host: ["_local_", "_site_"]'
SC_MARKER=yes SC_CERTS=yes SC_SECINIT=yes SC_DAEMON=no
S1=$(bind_case bind-aws)
YML_S=$S1/$YML_REL
SHA_S=$(sha "$YML_S")
run_bootstrap "$S1" --heap 4 --skip-sysctl

if [ "$(cat "$S1/run.rc")" = 0 ] && [ "$(sha "$YML_S")" = "$SHA_S" ] &&
	! grep -q 'network binding' "$S1/run.log" &&
	! grep -q 'probe-bound' "$S1/events.log"; then
	ok "with no --bind, opensearch.yml is untouched and no binding check runs"
else
	no "AWS binding behavior changed" \
		"$(grep -E 'network binding|bound addresses' "$S1/run.log" || true); yml=$(tr '\n' '|' <"$YML_S")"
fi

# ---------------------------------------------------------------------------
echo ""
echo "Scenario T: a nested network block is refused rather than corrupted"

reset_scenario
SC_YML_EXTRA='network:
  host: 0.0.0.0'
SC_MARKER=yes SC_CERTS=yes SC_SECINIT=yes SC_DAEMON=no
T1=$(bind_case bind-nested)
YML_T=$T1/$YML_REL
SHA_T=$(sha "$YML_T")
run_setup "$T1" setup

if [ "$(cat "$T1/run.rc")" = 1 ] && grep -q 'nested block' "$T1/run.log" &&
	[ "$(sha "$YML_T")" = "$SHA_T" ]; then
	ok "a nested block stops setup with instructions, leaving the file alone"
else
	no "a nested block was mishandled" "rc=$(cat "$T1/run.rc"), $(tail -n 3 "$T1/run.log" | tr '\n' '|')"
fi

# opensearch.setup writes network.host as a block list, so the rewrite has to
# take the list items with the key. This is the form the snap produces on every
# fresh setup; refusing it would block setup entirely.
reset_scenario
SC_YML_EXTRA='network.host:
  - _local_
  - _site_'
SC_MARKER=yes SC_CERTS=yes SC_SECINIT=yes SC_DAEMON=yes
T2=$(bind_case bind-list)
YML_T2=$T2/$YML_REL
run_setup "$T2" setup

if [ "$(cat "$T2/run.rc")" = 0 ] &&
	[ "$(yml_value "$YML_T2" 'network\.host')" = 127.0.0.1 ] &&
	[ "$(yml_value "$YML_T2" 'http\.host')" = 127.0.0.1 ] &&
	[ "$(yml_value "$YML_T2" 'transport\.host')" = 127.0.0.1 ] &&
	! grep -q '  - _local_' "$YML_T2" && ! grep -q '_site_' "$YML_T2"; then
	ok "the block list opensearch.setup writes is replaced by single-line loopback values"
else
	no "the block list was mishandled" \
		"rc=$(cat "$T2/run.rc"), yml=$(tr '\n' '|' <"$YML_T2"), $(tail -n 2 "$T2/run.log" | tr '\n' '|')"
fi

if grep -q '^daemon-restart ' "$T2/events.log"; then
	ok "the corrected binding is made effective by a restart"
else
	no "the corrected binding was not made effective" "$(tr '\n' '|' <"$T2/events.log")"
fi

reset_scenario

# ---------------------------------------------------------------------------
echo ""
echo "Scenario U: heap and binding are both applied before a single restart"

reset_scenario
SC_YML_EXTRA='network.host: 0.0.0.0'
SC_MARKER=yes SC_CERTS=yes SC_SECINIT=yes SC_DAEMON=yes
SC_SETTINGS_HEAP=4 SC_SAVED_HEAP=8 SC_INSTALLED=yes SC_SECRETS=yes SC_STATE=yes \
	SC_MEMINFO= SC_MEM_LIMIT_GIB=8
U1=$(make_sandbox restart-once)
YML_U=$U1/$YML_REL
HEAP_U=$U1/sysroot/var/snap/opensearch/current/etc/opensearch/jvm.options.d/heap.options
run_setup "$U1" setup

RESTARTS_U=$(grep -c '^daemon-restart ' "$U1/events.log" || true)
if [ "$(cat "$U1/run.rc")" = 0 ] && [ "$RESTARTS_U" = 1 ]; then
	ok "two configuration changes cause exactly one restart"
else
	no "wrong number of restarts" \
		"rc=$(cat "$U1/run.rc"), restarts=$RESTARTS_U, events=$(tr '\n' '|' <"$U1/events.log")"
fi

# The snapshots the stub records at the restart show what the node came back with.
RESTART_LINE_U=$(grep -m1 '^daemon-restart ' "$U1/events.log" || true)
BIND_AT_RESTART_U=$(grep -A1 -m1 '^daemon-restart ' "$U1/events.log" | grep '^bind-seen:' || true)
if [ "${RESTART_LINE_U#*heap=}" = "[-Xms4g -Xmx4g]" ] &&
	[ "$BIND_AT_RESTART_U" = 'bind-seen:127.0.0.1' ]; then
	ok "the restart happens with the new heap and the corrected binding already written"
else
	no "the daemon restarted before both changes were applied" \
		"restart: ${RESTART_LINE_U:-none}; binding at restart: ${BIND_AT_RESTART_U:-none}"
fi

if ! grep -q 'bind-seen:0.0.0.0' "$U1/events.log"; then
	ok "the node is never brought up on the old external binding"
else
	no "the node came up bound to 0.0.0.0" "$(grep -n 'bind-seen' "$U1/events.log" | tr '\n' '|')"
fi

if grep -qx -- '-Xmx4g' "$HEAP_U" && [ "$(yml_value "$YML_U" 'network\.host')" = 127.0.0.1 ]; then
	ok "both changes are present in the files afterwards"
else
	no "a change was lost" "heap=$(tr '\n' ' ' <"$HEAP_U"), bind=$(yml_value "$YML_U" 'network\.host')"
fi

# ---------------------------------------------------------------------------
echo ""
echo "Scenario V: binding verification needs both address lists"

# assert_bind_report <label> <shape> <message substring>
assert_bind_report() {
	local label=$1 shape=$2 want=$3 sb rc problems=()
	reset_scenario
	SC_BOUND_SHAPE=$shape
	SC_MARKER=yes SC_CERTS=yes SC_SECINIT=yes SC_DAEMON=no
	sb=$(bind_case "bound-$shape")
	run_setup "$sb" setup
	rc=$(cat "$sb/run.rc")
	[ "$rc" = 1 ] || problems+=("exit $rc, wanted 1")
	grep -qF -e "$want" "$sb/run.log" || problems+=("missing diagnostic '$want'")
	if [ ${#problems[@]} -eq 0 ]; then
		ok "$label is not accepted as proof of loopback binding"
	else
		no "$label was accepted" "$(printf '%s; ' "${problems[@]}")last log line: $(tail -n 1 "$sb/run.log")"
	fi
}

assert_bind_report "an HTTP-only response" http-only "transport bound_address as missing"
assert_bind_report "an empty transport list" empty "transport bound_address as an empty list"
assert_bind_report "a transport value that is not a list" malformed "transport bound_address as not a list"
assert_bind_report "a response with no node" no-nodes "no http bound_address at"
assert_bind_report "a wildcard transport listener" transport-wildcard "listening beyond 127.0.0.1"
assert_bind_report "a loopback address next to a null entry" mixed-null \
	"transport bound_address as a list with entries that are not strings"
assert_bind_report "a loopback address next to a number" mixed-number \
	"http bound_address as a list with entries that are not strings"

reset_scenario
SC_MARKER=yes SC_CERTS=yes SC_SECINIT=yes SC_DAEMON=no
V6=$(bind_case bound-full)
run_setup "$V6" setup
if [ "$(cat "$V6/run.rc")" = 0 ] &&
	grep -q 'bound addresses limited to 127.0.0.1 (2 checked across http and transport)' "$V6/run.log"; then
	ok "a complete response passes and reports both lists as checked"
else
	no "a complete response did not pass" \
		"rc=$(cat "$V6/run.rc"), $(grep 'bound addresses' "$V6/run.log" || tail -n 2 "$V6/run.log")"
fi

reset_scenario

# ---------------------------------------------------------------------------
echo ""
echo "Scenario W: credentials.json keeps what this setup does not manage"

# managed_case <name>: a healthy managed installation, knobs already set.
managed_case() {
	SC_SETTINGS_HEAP=6 SC_SAVED_HEAP=6 SC_YML_BOUND=yes SC_MARKER=yes SC_CERTS=yes \
		SC_SECINIT=yes SC_DAEMON=yes SC_INSTALLED=yes SC_SECRETS=yes SC_STATE=yes \
		SC_MEMINFO= SC_MEM_LIMIT_GIB=8
	make_sandbox "$1"
}

EXISTING_CREDS='{"OTHER_SERVICE_TOKEN":"keep-me","CHAT_API_KEY":"old-key","OPENSEARCH_USERNAME":"admin","OPENSEARCH_PASSWORD":"stale-password","NOTE":{"nested":true}}'

reset_scenario
SC_CREDS=$EXISTING_CREDS
W1=$(managed_case creds-preserve)
CREDS_W=$W1/usercommon/credentials.json
run_setup "$W1" setup

if [ "$(cat "$W1/run.rc")" = 0 ] &&
	[ "$(jq -r '.OTHER_SERVICE_TOKEN' "$CREDS_W")" = keep-me ] &&
	[ "$(jq -r '.NOTE.nested' "$CREDS_W")" = true ] &&
	[ "$(jq -r '.CHAT_API_KEY' "$CREDS_W")" = old-key ] &&
	[ "$(jq -r '.OPENSEARCH_PASSWORD' "$CREDS_W")" = "$SAVED_PW" ] &&
	[ "$(jq -r '.OPENSEARCH_USERNAME' "$CREDS_W")" = admin ]; then
	ok "unrelated keys and an existing API key survive; OpenSearch credentials are updated"
else
	no "credentials were replaced" "rc=$(cat "$W1/run.rc"), file: $(cat "$CREDS_W" 2>&1)"
fi

if [ "$(stat -c '%a' "$CREDS_W")" = 600 ] &&
	[ "$(find "$W1/usercommon" -name 'credentials.json.*' | wc -l)" = 0 ]; then
	ok "credentials.json is mode 0600 with no temporary file left behind"
else
	no "credentials file permissions or leftovers wrong" \
		"mode=$(stat -c '%a' "$CREDS_W"), leftovers=$(find "$W1/usercommon" -name 'credentials.json.*')"
fi

if ! grep -q "$SAVED_PW" "$W1/run.log" && ! grep -q 'old-key' "$W1/run.log"; then
	ok "no secret value is printed while updating the file"
else
	no "a secret was printed" "$(grep -c "$SAVED_PW" "$W1/run.log") password mentions in the log"
fi

echo ""
echo "Scenario X: a supplied API key replaces the saved one"
reset_scenario
SC_CREDS=$EXISTING_CREDS
SC_API_KEY=new-key
X1=$(managed_case creds-replace)
CREDS_X=$X1/usercommon/credentials.json
run_setup "$X1" setup

if [ "$(cat "$X1/run.rc")" = 0 ] &&
	[ "$(jq -r '.CHAT_API_KEY' "$CREDS_X")" = new-key ] &&
	[ "$(jq -r '.OTHER_SERVICE_TOKEN' "$CREDS_X")" = keep-me ]; then
	ok "a configured API key replaces the old value and keeps unrelated keys"
else
	no "the supplied API key was not applied" "$(cat "$CREDS_X" 2>&1)"
fi

echo ""
echo "Scenario Y: invalid credentials JSON is left alone"
reset_scenario
SC_CREDS='this is not json'
Y1=$(managed_case creds-invalid)
CREDS_Y=$Y1/usercommon/credentials.json
SHA_Y=$(sha "$CREDS_Y")
run_setup "$Y1" setup

if [ "$(cat "$Y1/run.rc")" = 1 ] && [ "$(sha "$CREDS_Y")" = "$SHA_Y" ] &&
	grep -q 'is not a JSON object' "$Y1/run.log" && grep -q 'left unchanged' "$Y1/run.log"; then
	ok "invalid JSON stops setup and the file is untouched"
else
	no "invalid JSON was mishandled" \
		"rc=$(cat "$Y1/run.rc"), file=$(cat "$CREDS_Y"), $(tail -n 2 "$Y1/run.log" | tr '\n' '|')"
fi

# ---------------------------------------------------------------------------
echo ""
echo "Scenario Z: a partial settings file is completed by asking only what is missing"

reset_scenario
SC_SETTINGS_SHAPE=partial
Z1=$(managed_case settings-partial)
# The API key is unanswered here too, so the empty answer below is the one that
# has to be remembered.
sed -i '/^CHAT_API_KEY=/d' "$Z1/assets/secrets.env"
# One answer per missing question, in the order collect_settings asks them.
# A distinct value per question means a misplaced or repeated question shows up
# as the wrong saved value rather than passing unnoticed.
run_setup "$Z1" setup myhost 9100 /custom/chat true mymodel '' no

if [ "$(cat "$Z1/run.rc")" = 0 ] && ! grep -q 'unbound variable' "$Z1/run.log"; then
	ok "setup completes instead of failing on an unset setting"
else
	no "setup did not complete" "rc=$(cat "$Z1/run.rc"), $(tail -n 4 "$Z1/run.log" | tr '\n' '|')"
fi

if grep -q 'CHAT_HOST="myhost"' "$Z1/assets/settings.env" &&
	grep -q 'CHAT_PORT="9100"' "$Z1/assets/settings.env" &&
	grep -q 'CHAT_PATH="/custom/chat"' "$Z1/assets/settings.env" &&
	grep -q 'CHAT_TLS="true"' "$Z1/assets/settings.env" &&
	grep -q 'CHAT_MODEL="mymodel"' "$Z1/assets/settings.env" &&
	grep -q 'DRIVE_IMPORT="no"' "$Z1/assets/settings.env"; then
	ok "each missing answer is collected and saved under the right key"
else
	no "missing settings were not saved correctly" "$(tr '\n' '|' <"$Z1/assets/settings.env")"
fi

if grep -q 'OPENSEARCH_CHANNEL="2/stable"' "$Z1/assets/settings.env" &&
	grep -q 'OPENSEARCH_HEAP_GIB="6"' "$Z1/assets/settings.env"; then
	ok "the saved channel and heap are untouched, so neither was asked again"
else
	no "an answered question was asked again" "$(tr '\n' '|' <"$Z1/assets/settings.env")"
fi

if grep -q 'CHAT_API_KEY=""' "$Z1/assets/secrets.env" &&
	grep -q "OPENSEARCH_ADMIN_PASSWORD=\"$SAVED_PW\"" "$Z1/assets/secrets.env"; then
	ok "an intentionally empty optional answer is saved, and saved secrets survive"
else
	no "optional empty answer not retained" "$(sed 's/=.*/=.../' "$Z1/assets/secrets.env" | tr '\n' '|')"
fi

# With everything answered, a rerun must ask nothing: no input is provided, so
# any question would fail on end of input.
: >"$Z1/events.log"
run_setup "$Z1" setup
if [ "$(cat "$Z1/run.rc")" = 0 ]; then
	ok "a rerun with the completed file asks nothing at all"
else
	no "a rerun asked a question again" "rc=$(cat "$Z1/run.rc"), $(tail -n 3 "$Z1/run.log" | tr '\n' '|')"
fi

echo ""
echo "Scenario AA: an invalid saved setting fails before any host change"
reset_scenario
SC_SETTINGS_EXTRA='CHAT_PORT="99999"'
AA1=$(managed_case settings-invalid)
run_setup "$AA1" setup

if [ "$(cat "$AA1/run.rc")" = 1 ] && grep -q "CHAT_PORT is '99999'" "$AA1/run.log" &&
	grep -q 'between 1 and 65535' "$AA1/run.log" && [ -z "$(mutation_events "$AA1")" ]; then
	ok "an invalid saved port is named and rejected before any mutation"
else
	no "an invalid saved port was not caught early" \
		"rc=$(cat "$AA1/run.rc"), mutations=$(mutation_events "$AA1" | tr '\n' ' '), $(tail -n 2 "$AA1/run.log" | tr '\n' '|')"
fi

# ---------------------------------------------------------------------------
echo ""
echo "Scenario AB: Drive import logs correctly when knowledge init is skipped"

DRIVE_SETTINGS='DRIVE_IMPORT="yes"
DRIVE_FOLDER_URL="https://drive.google.com/drive/folders/abc123"'

reset_scenario
SC_SETTINGS_EXTRA=$DRIVE_SETTINGS
SC_API_KEY=
AB1=$(managed_case drive-import-ok)
printf 'GOOGLE_DRIVE_CLIENT_ID="client-id"\nGOOGLE_DRIVE_CLIENT_SECRET="client-secret"\n' >>"$AB1/assets/secrets.env"
run_setup "$AB1" setup

if [ "$(cat "$AB1/run.rc")" = 0 ] &&
	grep -q 'Models already deployed' "$AB1/run.log" &&
	grep -q '^rag-knowledge-import' "$AB1/events.log" &&
	grep -q 'Import attempt finished' "$AB1/run.log" &&
	! grep -qi 'Warning: the import command exited' "$AB1/run.log" &&
	! grep -qi 'No such file or directory' "$AB1/run.log"; then
	ok "the import runs and reports an attempt, with knowledge init skipped (models reused)"
else
	no "import logging failed with init skipped" \
		"rc=$(cat "$AB1/run.rc"), $(tail -n 5 "$AB1/run.log" | tr '\n' '|')"
fi

if grep -q 'DRIVE_IMPORT_LAST_ATTEMPT=' "$AB1/assets/state.env" &&
	! grep -q 'all archives' "$AB1/run.log"; then
	ok "the attempt is recorded without claiming archive-level success"
else
	no "attempt recording wrong" "$(grep DRIVE "$AB1/assets/state.env" || echo none)"
fi

echo ""
echo "Scenario AC: a failing import command is reported, not masked"
reset_scenario
SC_SETTINGS_EXTRA=$DRIVE_SETTINGS
SC_IMPORT_RC=3
AC1=$(managed_case drive-import-fails)
printf 'GOOGLE_DRIVE_CLIENT_ID="client-id"\nGOOGLE_DRIVE_CLIENT_SECRET="client-secret"\n' >>"$AC1/assets/secrets.env"
run_setup "$AC1" setup

if [ "$(cat "$AC1/run.rc")" = 0 ] &&
	grep -q 'Warning: the import command exited 3' "$AC1/run.log" &&
	! grep -q 'Import attempt finished' "$AC1/run.log" &&
	grep -q 'DRIVE_IMPORT_LAST_ATTEMPT=' "$AC1/assets/state.env"; then
	ok "the import command's exit status survives logging and is reported"
else
	no "a failing import was masked" \
		"rc=$(cat "$AC1/run.rc"), $(tail -n 4 "$AC1/run.log" | tr '\n' '|')"
fi

reset_scenario
# ---------------------------------------------------------------------------
# ---------------------------------------------------------------------------
echo ""
echo "Scenario AD: a Tika failure fails setup instead of reporting success"

reset_scenario
SC_TIKA=start-fail
AD=$(bind_case tika-start-fail)
run_setup "$AD" setup
if [ "$(cat "$AD/run.rc")" = 1 ] &&
	grep -q 'could not start rag-cli.tika-server' "$AD/run.log" &&
	! grep -q 'Setup complete' "$AD/run.log"; then
	ok "a Tika start failure stops setup without a success summary"
else
	no "a Tika start failure was not fatal" "rc=$(cat "$AD/run.rc"), $(tail -n 3 "$AD/run.log" | tr '\n' '|')"
fi

reset_scenario
SC_TIKA=not-ready
AE=$(bind_case tika-not-ready)
run_setup "$AE" setup
if [ "$(cat "$AE/run.rc")" = 1 ] &&
	grep -q 'Tika did not answer' "$AE/run.log" &&
	! grep -q 'Setup complete' "$AE/run.log"; then
	ok "a Tika readiness timeout stops setup without a success summary"
else
	no "a Tika readiness timeout was not fatal" "rc=$(cat "$AE/run.rc"), $(tail -n 3 "$AE/run.log" | tr '\n' '|')"
fi

# ---------------------------------------------------------------------------
echo ""
echo "Scenario AF: model readiness decides whether init runs"

reset_scenario
SC_MODEL_STATE=missing
AF=$(bind_case models-missing)
run_setup "$AF" setup
if [ "$(cat "$AF/run.rc")" = 0 ] &&
	grep -q 'Initializing knowledge base' "$AF/run.log" &&
	grep -q '^rag-knowledge-init' "$AF/events.log"; then
	ok "models that are not deployed trigger knowledge init"
else
	no "a missing model did not trigger init" "rc=$(cat "$AF/run.rc"), $(grep -iE 'model|knowledge' "$AF/run.log" | tr '\n' '|')"
fi

reset_scenario
SC_MODEL_STATE=auth
AG=$(bind_case models-auth-error)
run_setup "$AG" setup
if [ "$(cat "$AG/run.rc")" = 1 ] &&
	grep -q 'rejected the admin credentials while checking model' "$AG/run.log" &&
	! grep -q '^rag-knowledge-init' "$AG/events.log"; then
	ok "an authentication failure checking models stops setup without running init"
else
	no "a model-check auth failure was mishandled" "rc=$(cat "$AG/run.rc"), $(tail -n 3 "$AG/run.log" | tr '\n' '|')"
fi

# ---------------------------------------------------------------------------
echo ""
echo "Scenario AH: a failing model check on the reranker is not skipped"

reset_scenario
SC_MODEL_STATES="emb-saved=missing rer-saved=forbidden"
AH=$(bind_case models-mixed-failure)
run_setup "$AH" setup
if [ "$(cat "$AH/run.rc")" = 1 ] &&
	grep -q '^model-check:emb-saved:missing' "$AH/events.log" &&
	grep -q '^model-check:rer-saved:forbidden' "$AH/events.log" &&
	grep -q 'rejected the admin credentials while checking model rer-saved' "$AH/run.log" &&
	! grep -q '^rag-knowledge-init' "$AH/events.log"; then
	ok "both model checks run; the reranker failure stops setup before init"
else
	no "a reranker model-check failure was mishandled" \
		"rc=$(cat "$AH/run.rc"), checks=$(grep '^model-check:' "$AH/events.log" | tr '\n' '|'), init=$(grep -c '^rag-knowledge-init' "$AH/events.log")"
fi

echo ""
echo "Results: $((TESTS_RUN - TESTS_FAILED))/$TESTS_RUN passed"
if [ "$TESTS_FAILED" -gt 0 ]; then
	echo ""
	echo "--- scenario A run log ---"
	cat "$A/run.log"
	echo "--- scenario D run log ---"
	cat "$D/run.log"
	echo "--- scenario D events ---"
	cat "$D/events.log"
	exit 1
fi
exit 0
