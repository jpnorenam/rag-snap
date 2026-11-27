#!/usr/bin/env bash
# Guest bootstrap for the rag-cli OpenSearch node. Run as root by opensearch-on-aws.sh:
#   sudo bash bootstrap-opensearch.sh --node-name rag0 --channel 2/stable --secrets-file <file>
# Callers supply policy: --heap <GiB> overrides the RAM calculation, --bind <addr>
# limits the listening addresses, --skip-sysctl leaves host sysctl settings alone.
# The secrets file (KEY=value, mode 0600) is deleted when this script exits.
# Every stage checks the node's actual state first, so reruns resume safely and
# never regenerate certificates or re-initialize an initialized security index.
set -euo pipefail
umask 077

SNAP_DATA_DIR=/var/snap/opensearch/current
CONF=$SNAP_DATA_DIR/etc/opensearch
CERTS=$CONF/certificates
PLUGINS=$SNAP_DATA_DIR/usr/share/opensearch/plugins
JDK=/snap/opensearch/current/usr/lib/jvm/java-21-openjdk-amd64
SNAP_LOGS=/var/snap/opensearch/common/ops/snap/logs
CLUSTER_LOG=/var/snap/opensearch/common/var/log/opensearch/opensearch-cluster.log
STATE_DIR=/var/lib/rag-bootstrap
MARKER=$STATE_DIR/daemon-started
SYSCTL_FILE=/etc/sysctl.d/60-rag-opensearch.conf
URL=https://localhost:9200
REQUIRED_PLUGS=(process-control log-observe mount-observe sys-fs-cgroup-service system-observe)
# Bounded waits are tries x interval; overridable so tests do not sleep for minutes.
WAIT_TRIES=${RAG_BOOTSTRAP_TRIES:-36}
WAIT_INTERVAL=${RAG_BOOTSTRAP_INTERVAL:-5}

NODE=rag0
CHANNEL=2/stable
SECRETS_FILE=
BIND=
CONFIG_CHANGED=no
STAGE=start
declare -A S=()

mask() {
	local line k
	while IFS= read -r line || [ -n "$line" ]; do
		for k in "${!S[@]}"; do [ -n "${S[$k]}" ] && line=${line//"${S[$k]}"/***}; done
		printf '%s\n' "$line"
	done
}

stage() { STAGE=$1; echo "==> $1"; }

fail() {
	echo "bootstrap failed at stage '$STAGE': $*" >&2
	if systemctl list-units --all --no-legend 'snap.opensearch.daemon.service' | grep -q .; then
		echo "--- journalctl -u snap.opensearch.daemon (last 40 lines)" >&2
		journalctl -u snap.opensearch.daemon -n 40 --no-pager 2>&1 | mask >&2 || true
	fi
	if [ -f "$CLUSTER_LOG" ]; then
		echo "--- $CLUSTER_LOG (last 40 lines)" >&2
		tail -n 40 "$CLUSTER_LOG" | mask >&2
	fi
	exit 1
}

# Run a command with its output masked; on failure show the snap's own step log.
run_masked() { # run_masked <snap-log-name> cmd...
	local log=$1 rc=0
	shift
	"$@" 2>&1 | mask || rc=$?
	if [ "$rc" != 0 ]; then
		[ -f "$SNAP_LOGS/$log.log" ] && { echo "--- $SNAP_LOGS/$log.log (last 40 lines)" >&2; tail -n 40 "$SNAP_LOGS/$log.log" | mask >&2; }
		fail "command failed: ${1} ${2:-} ${3:-}"
	fi
}

load_secrets() {
	local line
	[ -f "$SECRETS_FILE" ] || fail "secrets file $SECRETS_FILE not found"
	while IFS= read -r line || [ -n "$line" ]; do
		[[ $line =~ ^(OPENSEARCH_ADMIN_PASSWORD|TLS_ROOT_PASS|TLS_ADMIN_PASS|TLS_NODE_PASS)=(.*)$ ]] || fail "unexpected line in secrets file"
		S[${BASH_REMATCH[1]}]=${BASH_REMATCH[2]}
	done <"$SECRETS_FILE"
	rm -f "$SECRETS_FILE"
	local k
	for k in OPENSEARCH_ADMIN_PASSWORD TLS_ROOT_PASS TLS_ADMIN_PASS TLS_NODE_PASS; do
		[ -n "${S[$k]:-}" ] || fail "$k missing from secrets file"
	done
}

# valid_heap <value>: a positive integer number of GiB.
valid_heap() { [[ ${1:-} =~ ^[1-9][0-9]*$ ]]; }

# heap_gib <MemTotal KiB>: 3/8 of memory, rounded to the nearest GiB, 1..31.
heap_gib() {
	local h=$((($1 * 3 / 8 + 524288) / 1048576))
	((h < 1)) && h=1
	((h > 31)) && h=31
	echo "$h"
}

# replace_admin_hash <file> <hash>: rewrite only the hash line of the top-level admin block.
replace_admin_hash() {
	NEW_HASH=$2 awk '
		/^[^[:space:]#][^:]*:/ { in_admin = ($0 ~ /^admin:[[:space:]]*$/) }
		in_admin && !done && /^[[:space:]]+hash:/ { sub(/hash:.*/, "hash: \"" ENVIRON["NEW_HASH"] "\""); done = 1 }
		{ print }' "$1"
}

# curl as admin with the saved password; the credential is read from stdin, not argv.
os_curl() { # os_curl <password> <path> [curl args...]
	local pw=$1 path=$2
	shift 2
	printf 'user = "admin:%s"\n' "$pw" | curl -sk --max-time 10 -K - "$@" "$URL$path"
}

# security_state: ready | uninitialized | rejected, after a bounded wait for a
# definitive answer. No HTTP response is transient and never means "uninitialized".
security_state() {
	local body code
	body=$(mktemp)
	for _ in $(seq 1 "$WAIT_TRIES"); do
		code=$(os_curl "${S[OPENSEARCH_ADMIN_PASSWORD]}" /_plugins/_security/authinfo -o "$body" -w '%{http_code}' || true)
		case "$code" in
		200) rm -f "$body"; echo ready; return ;;
		401) rm -f "$body"; echo rejected; return ;;
		503) grep -q 'Security not initialized' "$body" && { rm -f "$body"; echo uninitialized; return; } ;;
		esac
		sleep "$WAIT_INTERVAL"
	done
	rm -f "$body"
	fail "no definitive security state from $URL after $((WAIT_TRIES * WAIT_INTERVAL)) s (last HTTP status: ${code:-none})"
}

# await_ready waits (bounded) for the node to accept the saved admin password after
# security initialization: the security index can lag the command that creates it, so a
# 503 (including "not initialized") or a connection failure is pending, never a reason to
# re-run security-init. A 401 is a definite credential rejection; running out of tries is a
# readiness timeout. The two failures are reported differently.
await_ready() {
	local body code
	body=$(mktemp)
	for _ in $(seq 1 "$WAIT_TRIES"); do
		code=$(os_curl "${S[OPENSEARCH_ADMIN_PASSWORD]}" /_plugins/_security/authinfo -o "$body" -w '%{http_code}' || true)
		case "$code" in
		200) rm -f "$body"; return 0 ;;
		401)
			rm -f "$body"
			fail "the saved OpenSearch admin credentials are rejected after security initialization (HTTP 401); not re-running security-init"
			;;
		esac
		sleep "$WAIT_INTERVAL"
	done
	rm -f "$body"
	fail "OpenSearch did not accept the saved admin password within $((WAIT_TRIES * WAIT_INTERVAL)) s after security-init (last HTTP status: ${code:-none}); the security index may still be initializing"
}

# check_roles <comma-separated node.roles>: each required role must be a complete token.
check_roles() {
	local list=$1 r
	for r in cluster_manager data ingest ml; do
		[[ ",$list," == *",$r,"* ]] || fail "node roles '$list' do not include $r"
	done
}

daemon_active() { [ "$(snap services opensearch.daemon 2>/dev/null | awk 'NR==2{print $3}')" = active ]; }

tls_complete() {
	local f
	for f in root-ca.pem admin.pem admin-key.pem "node-$NODE.pem" "node-$NODE-key.pem"; do
		[ -f "$CERTS/$f" ] || return 1
	done
	grep -Eq "^node\.name: *\"?$NODE\"?\$" "$CONF/opensearch.yml"
}

# install_like <src> <dest> <reference>: move src over dest with the owner/mode of reference.
install_like() {
	chown --reference="$3" "$1"
	chmod --reference="$3" "$1"
	mv -f "$1" "$2"
}

do_sysctl() {
	stage "host sysctl settings"
	printf '%s\n' 'vm.max_map_count=262144' 'vm.swappiness=0' 'net.ipv4.tcp_retries2=5' >"$SYSCTL_FILE.tmp"
	chmod 0644 "$SYSCTL_FILE.tmp"
	mv -f "$SYSCTL_FILE.tmp" "$SYSCTL_FILE"
	sysctl -p "$SYSCTL_FILE" >/dev/null || fail "sysctl -p $SYSCTL_FILE failed"
	[ "$(sysctl -n vm.max_map_count)" = 262144 ] && [ "$(sysctl -n vm.swappiness)" = 0 ] &&
		[ "$(sysctl -n net.ipv4.tcp_retries2)" = 5 ] || fail "sysctl values did not take effect"
}

do_snap() {
	stage "OpenSearch snap and interfaces"
	if snap list opensearch >/dev/null 2>&1; then
		local tracking
		tracking=$(snap list opensearch | awk 'NR==2{print $4}')
		[ "$tracking" = "$CHANNEL" ] || echo "note: installed opensearch tracks $tracking (requested $CHANNEL); reusing it"
	else
		snap install opensearch --channel="$CHANNEL" || fail "snap install opensearch --channel=$CHANNEL failed"
	fi
	# The host sysctl file above is authoritative; keep the snap's own handler off.
	snap set opensearch set-sysctl-props=no || fail "snap set opensearch set-sysctl-props=no failed"
	[ "$(snap get opensearch set-sysctl-props)" = no ] || fail "could not set opensearch set-sysctl-props=no"
	local plug
	for plug in "${REQUIRED_PLUGS[@]}"; do
		plug_connected "$plug" || snap connect "opensearch:$plug" || true
		plug_connected "$plug" || fail "opensearch:$plug is not connected; run: sudo snap connect opensearch:$plug"
	done
	snap list opensearch | awk 'NR==2{print "opensearch " $2 " rev " $3 " tracking " $4}'
}

plug_connected() {
	snap connections opensearch | awk -v p="opensearch:$1" '$2 == p && $3 != "-" { f = 1 } END { exit !f }'
}

do_tls() {
	stage "TLS setup"
	if tls_complete; then
		echo "certificates for $NODE already present; not regenerating"
		return
	fi
	[ ! -f "$MARKER" ] || fail "certificates are incomplete but the daemon was already started; refusing to regenerate them"
	run_masked setup snap run opensearch.setup \
		--node-name "$NODE" \
		--node-roles cluster_manager,data,ingest,ml \
		--tls-priv-key-root-pass "${S[TLS_ROOT_PASS]}" \
		--tls-priv-key-admin-pass "${S[TLS_ADMIN_PASS]}" \
		--tls-priv-key-node-pass "${S[TLS_NODE_PASS]}" \
		--tls-init-setup yes
	tls_complete || fail "opensearch.setup finished but the expected certificates are missing"
}

do_heap() {
	stage "JVM heap"
	local gib f="$CONF/jvm.options.d/heap.options" tmp
	[ -d "$CONF/jvm.options.d" ] || fail "$CONF/jvm.options.d not found"
	
	if [ -n "${HEAP_GIB:-}" ]; then
		valid_heap "$HEAP_GIB" || fail "invalid heap value: '$HEAP_GIB' (expected a positive integer number of GiB)"
		gib=$HEAP_GIB
	else
		gib=$(heap_gib "$(awk '/^MemTotal:/{print $2}' /proc/meminfo)")
	fi
	
	if [ -f "$f" ] && [ "$(cat "$f")" = "$(printf -- '-Xms%sg\n-Xmx%sg' "$gib" "$gib")" ]; then
		echo "heap already ${gib} GiB"
		return
	fi
	tmp=$(mktemp "$f.XXXXXX")
	printf -- '-Xms%sg\n-Xmx%sg\n' "$gib" "$gib" >"$tmp"
	install_like "$tmp" "$f" "$CONF/jvm.options"
	echo "heap set to ${gib} GiB"
	CONFIG_CHANGED=yes
}

# bind_ok <address>: the requested address, or the IPv6 loopback that a
# loopback-bound node also listens on (nodes info reports both forms).
bind_ok() {
	[ "$1" = "$BIND" ] && return 0
	case $1:$BIND in
	127.*:127.* | 127.*:::1 | ::1:127.* | ::1:::1) return 0 ;;
	esac
	return 1
}

# do_bind: apply the caller's binding policy, if any. opensearch.setup rewrites
# network.host every time it runs (scripts/wrappers/setup.sh sets it from
# --node-host, defaulting to [_local_, _site_]), so this must run after do_tls,
# and the http/transport overrides that would win over it go with it.
do_bind() {
	[ -n "${BIND:-}" ] || return 0
	stage "network binding"
	local f=$CONF/opensearch.yml tmp
	[ -f "$f" ] || fail "$f not found"
	# A bare network/http/transport mapping would need a YAML parser to edit
	# safely; refuse it rather than corrupt it.
	! grep -Eq '^[[:space:]]*"?(network|http|transport)"?[[:space:]]*:[[:space:]]*(#.*)?$' "$f" ||
		fail "$f expresses network settings as a nested block; set network.host, http.host and transport.host to $BIND as single-line values and rerun"
	tmp=$(mktemp "$f.XXXXXX")
	# Drop every host key and whatever opensearch.setup wrote under it. That
	# command writes network.host as a block list, so the key's following list
	# items have to go with the key, then the requested single-line values are
	# appended.
	BIND=$BIND awk '
		skip { if ($0 ~ /^[[:space:]]/) next; skip = 0 }
		/^[[:space:]]*"?(network|http|transport)\.(host|bind_host)"?[[:space:]]*:/ {
			if ($0 ~ /:[[:space:]]*(#.*)?$/) skip = 1
			next
		}
		{ print }
		END { printf "network.host: %s\nhttp.host: %s\ntransport.host: %s\n", ENVIRON["BIND"], ENVIRON["BIND"], ENVIRON["BIND"] }
	' "$f" >"$tmp"
	if cmp -s "$tmp" "$f"; then
		rm -f "$tmp"
		echo "network binding already limited to $BIND"
		return
	fi
	install_like "$tmp" "$f" "$f"
	echo "network binding set to $BIND"
	CONFIG_CHANGED=yes
}

# verify_bind: the node's own bound addresses, for HTTP and for transport. A
# successful request to localhost does not show whether other interfaces are
# listening too, and one of the two lists being absent proves nothing.
verify_bind() {
	[ -n "${BIND:-}" ] || return 0
	# Only reached when a caller supplied --bind, and that caller requires jq.
	command -v jq >/dev/null 2>&1 || fail "jq is needed to verify the effective binding"
	local json kind addrs err a host bad='' checked=0
	json=$(os_curl "${S[OPENSEARCH_ADMIN_PASSWORD]}" \
		'/_nodes/_local/http,transport?filter_path=nodes.*.http.bound_address,nodes.*.transport.bound_address' || true)
	for kind in http transport; do
		# A non-empty array of strings is the only acceptable answer: a wrong
		# type, an empty list or one mixed entry is reported, never dropped.
		addrs=$(printf '%s' "$json" | jq -r --arg k "$kind" \
			'.nodes[]? | .[$k].bound_address
			 | if . == null then "ERR:missing"
			   elif type != "array" then "ERR:not a list"
			   elif length == 0 then "ERR:an empty list"
			   elif (map(type) | any(. != "string")) then "ERR:a list with entries that are not strings"
			   else .[] end' 2>/dev/null || true)
		err=$(printf '%s\n' "$addrs" | grep -m1 '^ERR:' || true)
		[ -z "$err" ] ||
			fail "the node reported the $kind bound_address as ${err#ERR:} at $URL; cannot confirm the binding is limited to $BIND (response: ${json:-none})"
		[ -n "$addrs" ] ||
			fail "the node reported no $kind bound_address at $URL; cannot confirm the binding is limited to $BIND (response: ${json:-none})"
		for a in $addrs; do
			checked=$((checked + 1))
			[[ $a =~ ^(\[[0-9A-Fa-f:]+\]|[0-9]+(\.[0-9]+){3}):[0-9]+$ ]] || { bad="$bad $kind:$a(unreadable)"; continue; }
			host=${a%:*}
			host=${host#[}
			host=${host%]}
			bind_ok "$host" || bad="$bad $kind:$a"
		done
	done
	[ -z "$bad" ] || fail "OpenSearch is listening beyond $BIND on:$bad; REST and transport must both be limited to $BIND"
	echo "bound addresses limited to $BIND ($checked checked across http and transport)"
}

# do_config: write the caller's heap and binding policy, then restart once if
# either changed and the daemon is already running. Restarting between the two
# would bring the node back up with the binding the other stage still has to fix.
do_config() {
	CONFIG_CHANGED=no
	do_heap
	do_bind
	if [ "$CONFIG_CHANGED" = yes ] && daemon_active; then
		stage "restart for the new configuration"
		snap restart opensearch.daemon
	fi
}

do_admin_hash() {
	stage "admin password hash"
	local f="$CONF/opensearch-security/internal_users.yml" hash tmp changed
	[ -f "$f" ] || fail "$f not found"
	[ -f "$f.rag-orig" ] || install -m 0600 -o root -g root "$f" "$f.rag-orig"
	# hash.sh is packaged with mode 0644: run it through bash with the snap's JDK;
	# the password is passed only through the environment.
	hash=$(OPENSEARCH_JAVA_HOME=$JDK OS_ADMIN_PW=${S[OPENSEARCH_ADMIN_PASSWORD]} \
		bash "$PLUGINS/opensearch-security/tools/hash.sh" -env OS_ADMIN_PW | tail -n 1) ||
		fail "hash.sh failed"
	[[ $hash =~ ^\$2[aby]\$ ]] || fail "hash.sh did not print a bcrypt hash"
	tmp=$(mktemp "$f.XXXXXX")
	replace_admin_hash "$f" "$hash" >"$tmp"
	changed=$({ diff "$f" "$tmp" || true; } | grep -c '^>' || true)
	[ "$changed" = 1 ] || { rm -f "$tmp"; fail "expected exactly one changed line in internal_users.yml, got $changed"; }
	install_like "$tmp" "$f" "$f"
}

do_start() {
	stage "start daemon"
	mkdir -p "$STATE_DIR"
	touch "$MARKER"
	snap start --enable opensearch.daemon
	local code
	for _ in $(seq 1 36); do
		code=$(curl -sk --max-time 5 -o /dev/null -w '%{http_code}' "$URL/" || true)
		[ "$code" != 000 ] && { echo "endpoint answered (HTTP $code)"; return; }
		sleep 5
	done
	fail "no HTTP response from $URL after 180 s"
}

do_security_init() {
	stage "security initialization"
	run_masked security-config snap run opensearch.security-init --tls-priv-key-admin-pass="${S[TLS_ADMIN_PASS]}"
	# The command can report success before the security index is usable, so wait for the
	# saved password to authenticate instead of re-running security-init.
	await_ready
}

diagnose_rejected() {
	if [ "$(printf 'user = "admin:admin"\n' | curl -sk --max-time 10 -K - -o /dev/null -w '%{http_code}' "$URL/" || true)" = 200 ]; then
		fail "security was initialized from the unmodified internal_users.yml (admin:admin still works); the saved password was never applied. Not resetting security."
	fi
	fail "the saved OpenSearch admin credentials are rejected (HTTP 401). Not re-running setup or security-init."
}

do_ready() {
	stage "readiness"
	local health roles
	await_ready
	health=$(os_curl "${S[OPENSEARCH_ADMIN_PASSWORD]}" '/_cluster/health?wait_for_status=yellow&timeout=60s' --max-time 70 || true)
	[[ $health =~ \"status\":\"(green|yellow)\" ]] || fail "cluster health is not yellow or green: ${health:-no response}"
	# node.roles is the full role names (node.role is the abbreviated form, e.g. "dim").
	roles=$(os_curl "${S[OPENSEARCH_ADMIN_PASSWORD]}" '/_cat/nodes?h=node.roles,heap.max' || true)
	check_roles "${roles%% *}"
	echo "cluster ${BASH_REMATCH[1]}; node roles/heap: $roles"
	verify_bind
}

main() {
	SKIP_SYSCTL=no
	HEAP_GIB=
	BIND=
	while [ $# -gt 0 ]; do
		case $1 in
		--node-name) NODE=$2; shift 2 ;;
		--channel) CHANNEL=$2; shift 2 ;;
		--secrets-file) SECRETS_FILE=$2; shift 2 ;;
		--skip-sysctl) SKIP_SYSCTL=yes; shift ;;
		--bind)
			[ $# -ge 2 ] && [[ $2 != --* ]] && [ -n "$2" ] ||
				{ echo "--bind requires a value: the address OpenSearch must listen on" >&2; exit 2; }
			BIND=$2
			shift 2
			;;
		--heap)
			[ $# -ge 2 ] && [[ $2 != --* ]] ||
				{ echo "--heap requires a value: a positive integer number of GiB" >&2; exit 2; }
			valid_heap "$2" ||
				{ echo "invalid --heap value: '$2' (expected a positive integer number of GiB)" >&2; exit 2; }
			HEAP_GIB=$2
			shift 2
			;;
		*) echo "unknown argument: $1" >&2; exit 2 ;;
		esac
	done
	[ "$(id -u)" = 0 ] || { echo "run as root" >&2; exit 2; }
	trap '[ -n "$SECRETS_FILE" ] && rm -f "$SECRETS_FILE"' EXIT
	load_secrets

	[ "$SKIP_SYSCTL" = no ] && do_sysctl
	do_snap
	local state=
	if [ -f "$MARKER" ]; then
		# The requested heap must reach disk before anything starts or waits for
		# the node: a saved heap larger than available memory stops it from
		# starting at all, and the rerun that lowers it has to repair that.
		do_config
		daemon_active || snap start --enable opensearch.daemon
		state=$(security_state)
	fi
	case $state in
	rejected) diagnose_rejected ;;
	ready)
		echo "security already initialized with the saved password"
		;;
	*)
		do_tls
		do_config
		do_admin_hash
		if [ ! -f "$MARKER" ] || ! daemon_active; then do_start; fi
		state=$(security_state)
		case $state in
		uninitialized) do_security_init ;;
		rejected) diagnose_rejected ;;
		esac
		;;
	esac
	do_ready
	echo "bootstrap complete"
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
	main "$@"
fi
