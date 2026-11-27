#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
LOCAL_SETUP="$SCRIPT_DIR/local-setup.sh"
BOOTSTRAP_SRC="$SCRIPT_DIR/bootstrap-opensearch.sh"

TEST_DIR=$(mktemp -d)
trap 'rm -rf "$TEST_DIR"' EXIT

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

pass() {
	echo "✓ $1"
	TESTS_PASSED=$((TESTS_PASSED + 1))
	TESTS_RUN=$((TESTS_RUN + 1))
}

fail() {
	echo "✗ $1"
	echo "  $2"
	TESTS_FAILED=$((TESTS_FAILED + 1))
	TESTS_RUN=$((TESTS_RUN + 1))
}

echo "==> Testing local-setup.sh behavior"
echo ""

# A copy with the snap context the script reads at load time, so its functions
# can be sourced and executed. The main guard keeps sourcing from running setup.
mkdir -p "$TEST_DIR/assets" "$TEST_DIR/usercommon" "$TEST_DIR/bin"
cp "$LOCAL_SETUP" "$SCRIPT_DIR/common.sh" "$TEST_DIR/assets/"
cat > "$TEST_DIR/assets/snap-context.env" << EOF
RAG_SNAP_INSTANCE="rag-cli"
RAG_SNAP_USER_COMMON="$TEST_DIR/usercommon"
EOF
LOCAL_COPY=$TEST_DIR/assets/local-setup.sh

# Records any rag-cli invocation so a test can assert none happened.
cat > "$TEST_DIR/bin/rag-cli.rag" << EOF
#!/usr/bin/env bash
echo "rag-cli.rag \$*" >> "$TEST_DIR/rag-calls.log"
EOF
chmod +x "$TEST_DIR/bin/rag-cli.rag"

echo "Test: Script rejects sudo invocation"
output=$(sudo bash "$LOCAL_SETUP" setup 2>&1 || true)
if echo "$output" | grep -q "do not run this script with sudo"; then
	pass "Rejects sudo invocation with helpful message"
else
	fail "Should reject sudo invocation" "Output: $output"
fi

echo ""
echo "Test: Heap and binding are delegated to the shared bootstrap"
# Responsibility split: the local caller resolves and validates its policy, the
# shared bootstrap owns the single writer for each. End-to-end application on
# fresh, rerun and recovery paths is covered by test-local-bootstrap-flow.sh.
if ! grep -qE 'Xms|Xmx|heap\.options' "$LOCAL_SETUP" &&
	grep -q -- '--heap' "$LOCAL_SETUP" &&
	grep -qE '^\s+printf -- .-Xms%sg' "$BOOTSTRAP_SRC"; then
	pass "Local caller writes no heap file and passes --heap to the shared writer"
else
	fail "Heap writer is in the wrong place" \
		"local-setup.sh must not write heap.options; bootstrap-opensearch.sh must hold the writer"
fi

# Matches a YAML host assignment or any reference to opensearch.yml, not
# rag-cli's own chat.http.host/tika.http.host configuration keys.
if ! grep -qE '(network|http|transport)\.(host|bind_host): ' "$LOCAL_SETUP" &&
	! grep -q 'opensearch\.yml' "$LOCAL_SETUP" &&
	grep -q -- '--bind "\$BIND_ADDRESS"' "$LOCAL_SETUP" &&
	grep -q '^BIND_ADDRESS=127.0.0.1$' "$LOCAL_SETUP" &&
	grep -q 'network.host: %s' "$BOOTSTRAP_SRC"; then
	pass "Local caller declares the loopback policy and passes --bind to the shared writer"
else
	fail "Binding writer is in the wrong place" \
		"local-setup.sh must not write network/http/transport host keys; it must pass --bind"
fi

echo ""
echo "Test: Settings file structure"
cd "$TEST_DIR"
source "$SCRIPT_DIR/common.sh"
declare -A CFG=()

cat > test-settings.env << 'EOF'
OPENSEARCH_CHANNEL="2/stable"
OPENSEARCH_HEAP_GIB="8"
CHAT_HOST="bedrock-runtime.us-east-1.amazonaws.com"
CHAT_PORT="443"
CHAT_PATH="/api"
CHAT_TLS="true"
CHAT_MODEL="claude-3-5-sonnet"
EOF

load_env test-settings.env OPENSEARCH_CHANNEL OPENSEARCH_HEAP_GIB CHAT_HOST CHAT_PORT CHAT_PATH CHAT_TLS CHAT_MODEL

if [ "${CFG[OPENSEARCH_HEAP_GIB]}" = "8" ] && [ "${CFG[CHAT_TLS]}" = "true" ]; then
	pass "Settings file loads correctly via common.sh"
else
	fail "Settings not loaded correctly" "HEAP=${CFG[OPENSEARCH_HEAP_GIB]}, TLS=${CFG[CHAT_TLS]}"
fi

echo ""
echo "Test: State tracking structure"
unset CFG
declare -A CFG=()

cat > test-state.env << 'EOF'
OPENSEARCH_INSTALLED_BY_US="yes"
OPENSEARCH_SETUP_COMPLETED="2026-10-09T04:00:00+00:00"
KNOWLEDGE_INIT_COMPLETED="2026-10-09T04:10:00+00:00"
EMBEDDING_MODEL_ID="model-embed-v1"
RERANK_MODEL_ID="model-rerank-v2"
SYSCTL_FILE_CREATED="yes"
EOF

load_env test-state.env OPENSEARCH_INSTALLED_BY_US OPENSEARCH_SETUP_COMPLETED KNOWLEDGE_INIT_COMPLETED EMBEDDING_MODEL_ID RERANK_MODEL_ID SYSCTL_FILE_CREATED

if [ "${CFG[OPENSEARCH_INSTALLED_BY_US]}" = "yes" ] && \
   [ -n "${CFG[EMBEDDING_MODEL_ID]}" ] && \
   [ "${CFG[SYSCTL_FILE_CREATED]}" = "yes" ]; then
	pass "State file structure matches implementation"
else
	fail "State file mismatch" "Check state keys"
fi

echo ""
echo "Test: Google Drive import is a supported opt-in feature"
cat > "$TEST_DIR/drive-settings.env" << 'EOF'
OPENSEARCH_CHANNEL="2/stable"
OPENSEARCH_HEAP_GIB="6"
DRIVE_IMPORT="yes"
DRIVE_FOLDER_URL="https://drive.google.com/drive/folders/abc123"
EOF
cat > "$TEST_DIR/drive-secrets.env" << 'EOF'
GOOGLE_DRIVE_CLIENT_ID="client-id"
GOOGLE_DRIVE_CLIENT_SECRET="client-secret"
EOF

# The production key lists must accept the Drive keys (load_env rejects unknown
# keys), and the masking list must cover the client secret.
drive_load=$(LS="$LOCAL_COPY" S1="$TEST_DIR/drive-settings.env" S2="$TEST_DIR/drive-secrets.env" bash -c '
	source "$LS"
	load_env "$S1" "${SETTINGS_KEYS[@]}"
	load_env "$S2" "${SECRET_KEYS[@]}"
	printf "%s/%s/%s" "${CFG[DRIVE_IMPORT]}" "${CFG[DRIVE_FOLDER_URL]}" "${CFG[GOOGLE_DRIVE_CLIENT_ID]}"
	printf "%s" " masked=$(echo "secret is ${CFG[GOOGLE_DRIVE_CLIENT_SECRET]}" | mask)"' 2>&1) ||
	drive_load="FAILED: $drive_load"

if [ "$drive_load" = "yes/https://drive.google.com/drive/folders/abc123/client-id masked=secret is ***" ]; then
	pass "Drive settings and OAuth secrets are declared, loadable and masked"
else
	fail "Drive settings not supported" "Got: $drive_load"
fi

# Opt-in: with DRIVE_IMPORT unset or no, the import must not run.
: > "$TEST_DIR/rag-calls.log"
drive_rc=0
PATH="$TEST_DIR/bin:$PATH" LS="$LOCAL_COPY" bash -c '
	source "$LS"
	CFG[DRIVE_IMPORT]=no
	drive_import' >/dev/null 2>&1 || drive_rc=$?

if [ "$drive_rc" = 0 ] && [ ! -s "$TEST_DIR/rag-calls.log" ]; then
	pass "Import is skipped without invoking rag-cli when DRIVE_IMPORT is not yes"
else
	fail "Drive import not opt-in" "rc=$drive_rc, calls: $(cat "$TEST_DIR/rag-calls.log")"
fi

echo ""
echo "Test: Usage message"
if bash "$LOCAL_SETUP" --help 2>&1 | grep -q "setup.*Install and configure" && \
   bash "$LOCAL_SETUP" --help 2>&1 | grep -q "start.*Start OpenSearch" && \
   bash "$LOCAL_SETUP" --help 2>&1 | grep -q "stop.*Stop OpenSearch" && \
   bash "$LOCAL_SETUP" --help 2>&1 | grep -q "destroy.*Remove managed"; then
	pass "Usage shows all modes with descriptions"
else
	fail "Usage incomplete" "Should show: setup, start, stop, destroy with descriptions"
fi

echo ""
echo "==========================="
echo "Results: $TESTS_PASSED/$TESTS_RUN passed"

if [ "$TESTS_FAILED" -gt 0 ]; then
	echo "FAILED: $TESTS_FAILED test(s) failed"
	exit 1
else
	echo "SUCCESS: All behavioral tests passed"
	exit 0
fi
