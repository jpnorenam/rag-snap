#!/usr/bin/env bash
# Test heap configuration behavior in the shared bootstrap's heap writer.
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
TEST_BASE=$(mktemp -d)
trap 'rm -rf "$TEST_BASE"' EXIT

# Stub directory
STUB_DIR="$TEST_BASE/stubs"
mkdir -p "$STUB_DIR"
export PATH="$STUB_DIR:$PATH"

# Test state
CALL_LOG="$TEST_BASE/calls.log"
CONF_DIR="$TEST_BASE/conf"
HEAP_FILE="$CONF_DIR/jvm.options.d/heap.options"
MARKER="$CONF_DIR/marker"
mkdir -p "$CONF_DIR/jvm.options.d"
# install_like copies owner/mode from jvm.options, so the reference must exist.
printf -- '-Xss1m\n' > "$CONF_DIR/jvm.options"
> "$CALL_LOG"

# Source bootstrap functions (the script's main guard keeps it from running)
source "$SCRIPT_DIR/bootstrap-opensearch.sh"

# Stub commands
cat > "$STUB_DIR/snap" << 'EOF'
#!/bin/bash
echo "snap $*" >> "$CALL_LOG"
case "$1 $2" in
  "services opensearch.daemon")
    if [ -f "$DAEMON_ACTIVE" ]; then
      echo "Service                  Startup  Current  Notes"
      echo "opensearch.daemon        enabled  active   -"
    else
      echo "Service                  Startup  Current  Notes"
      echo "opensearch.daemon        disabled  inactive   -"
    fi
    ;;
  "restart opensearch.daemon")
    echo "restarted" > "$TEST_BASE/restarted"
    ;;
esac
EOF
chmod +x "$STUB_DIR/snap"

export CONF="$CONF_DIR"
export DAEMON_ACTIVE="$TEST_BASE/daemon_active"
export CALL_LOG
export TEST_BASE

# Test counters
TESTS_RUN=0
TESTS_PASSED=0

pass() {
  echo "✓ $1"
  TESTS_PASSED=$((TESTS_PASSED + 1))
  TESTS_RUN=$((TESTS_RUN + 1))
}

# Named "no" so it cannot shadow the bootstrap's own fail(), which must keep
# exiting for the invalid-value cases below.
no() {
  echo "✗ $1: $2"
  TESTS_RUN=$((TESTS_RUN + 1))
}

# do_heap is expected to exit for rejected values, so run it in a subprocess.
heap_exits() { ( do_heap ) >/dev/null 2>&1; }

echo "==> Testing heap configuration"
echo ""

# Test 1: Explicit heap via HEAP_GIB
echo "Test 1: Bootstrap with --heap parameter"
HEAP_GIB=8
rc=0
do_heap >/dev/null 2>&1 || rc=$?
if [ "$rc" = 0 ] && grep -q "^-Xms8g$" "$HEAP_FILE" && grep -q "^-Xmx8g$" "$HEAP_FILE"; then
  pass "Explicit heap 8 GiB applied"
else
  no "Explicit heap" "rc=$rc, expected Xms8g/Xmx8g, got: $(cat "$HEAP_FILE" 2>/dev/null || echo 'file missing')"
fi

# Test 2: Auto-calculation when HEAP_GIB unset
echo "Test 2: Auto-calculate heap from RAM"
rm -f "$HEAP_FILE"
unset HEAP_GIB

# Create proc/meminfo stub
mkdir -p "$TEST_BASE/proc"
echo "MemTotal:       16777216 kB" > "$TEST_BASE/proc/meminfo"

# Temporarily override awk with full path fallback
awk_orig=$(which awk)
cat > "$STUB_DIR/awk" << EOF
#!/bin/bash
if [[ "\$*" == */proc/meminfo* ]]; then
  $awk_orig '/^MemTotal:/{print \$2}' "$TEST_BASE/proc/meminfo"
else
  $awk_orig "\$@"
fi
EOF
chmod +x "$STUB_DIR/awk"

rc=0
do_heap >/dev/null 2>&1 || rc=$?
if [ "$rc" = 0 ] && grep -q "^-Xms6g$" "$HEAP_FILE" && grep -q "^-Xmx6g$" "$HEAP_FILE"; then
  pass "Auto-calculated heap 6 GiB from 16 GiB RAM"
else
  no "Auto-calculate" "rc=$rc, expected Xms6g/Xmx6g, got: $(cat "$HEAP_FILE" 2>/dev/null || echo 'file missing')"
fi

# Test 3: Heap already set (idempotent)
echo "Test 3: Heap already set (skip rewrite)"
> "$CALL_LOG"
rc=0
do_config >/dev/null 2>&1 || rc=$?
if [ "$rc" = 0 ] && ! grep -q "restart" "$CALL_LOG"; then
  pass "Idempotent - no restart when heap unchanged"
else
  no "Idempotent" "rc=$rc, unexpected restart"
fi

# Test 4: Changed heap triggers restart (daemon active)
# do_config writes the heap and the binding, then restarts once; do_heap itself
# only records that a restart is due.
echo "Test 4: Changed heap restarts active daemon"
touch "$DAEMON_ACTIVE"
rm -f "$TEST_BASE/restarted"
HEAP_GIB=10
rc=0
do_config >/dev/null 2>&1 || rc=$?
if [ "$rc" = 0 ] && [ -f "$TEST_BASE/restarted" ] && grep -q "^-Xms10g$" "$HEAP_FILE" && grep -q "^-Xmx10g$" "$HEAP_FILE"; then
  pass "Changed heap to 10 GiB and restarted daemon"
else
  no "Changed heap restart" "rc=$rc, restart: $([ -f "$TEST_BASE/restarted" ] && echo yes || echo no), file: $(cat "$HEAP_FILE" 2>/dev/null)"
fi

# Test 5: Changed heap without restart (daemon inactive)
echo "Test 5: Changed heap without restart (inactive daemon)"
rm -f "$DAEMON_ACTIVE" "$TEST_BASE/restarted"
HEAP_GIB=12
rc=0
do_config >/dev/null 2>&1 || rc=$?
if [ "$rc" = 0 ] && [ ! -f "$TEST_BASE/restarted" ] && grep -q "^-Xms12g$" "$HEAP_FILE" && grep -q "^-Xmx12g$" "$HEAP_FILE"; then
  pass "Changed heap to 12 GiB without restarting inactive daemon"
else
  no "Changed heap no restart" "rc=$rc, restart: $([ -f "$TEST_BASE/restarted" ] && echo yes || echo no)"
fi

# Test 6: Bounds checking (1-31 GiB)
echo "Test 6: heap_gib calculation bounds"
# Small RAM
result=$(heap_gib 1048576)  # 1 GiB
if [ "$result" = "1" ]; then
  pass "Minimum heap 1 GiB enforced"
else
  no "Min heap" "Expected 1, got $result"
fi

# Large RAM
result=$(heap_gib 100000000)  # ~95 GiB
if [ "$result" = "31" ]; then
  pass "Maximum heap 31 GiB enforced"
else
  no "Max heap" "Expected 31, got $result"
fi

# 16 GiB RAM
result=$(heap_gib 16777216)
if [ "$result" = "6" ]; then
  pass "16 GiB RAM -> 6 GiB heap (3/8 formula)"
else
  no "16G RAM" "Expected 6, got $result"
fi

# Test 7: daemon_active exact matching
echo "Test 7: daemon_active service state detection"
# Create mock that will be called by daemon_active
cat > "$STUB_DIR/snap" << 'SNAPEOF'
#!/bin/bash
case "$1 $2" in
  "services opensearch.daemon")
    if [ -f "$DAEMON_ACTIVE" ]; then
      printf "Service                  Startup  Current  Notes\n"
      printf "opensearch.daemon        enabled  active   -\n"
    else
      printf "Service                  Startup  Current  Notes\n"
      printf "opensearch.daemon        disabled inactive  -\n"
    fi
    ;;
esac
SNAPEOF
chmod +x "$STUB_DIR/snap"

touch "$DAEMON_ACTIVE"
if daemon_active; then
  pass "daemon_active detects active state"
else
  no "daemon_active" "Did not detect active"
fi

rm -f "$DAEMON_ACTIVE"
if ! daemon_active; then
  pass "daemon_active correctly rejects inactive state"
else
  no "daemon_active inactive" "False positive on inactive"
fi

# Test 8: Invalid heap validation
echo "Test 8: Heap validation rejects invalid values"
HEAP_SHA=$(sha256sum "$HEAP_FILE" | awk '{print $1}')

export HEAP_GIB="invalid"
if ! heap_exits; then
  pass "Rejects invalid heap value"
else
  no "Invalid heap" "Should have failed"
fi

export HEAP_GIB="0"
if ! heap_exits; then
  pass "Rejects zero heap"
else
  no "Zero heap" "Should have failed"
fi

export HEAP_GIB="-3"
if ! heap_exits; then
  pass "Rejects negative heap"
else
  no "Negative heap" "Should have failed"
fi

if [ "$(sha256sum "$HEAP_FILE" | awk '{print $1}')" = "$HEAP_SHA" ]; then
  pass "Rejected values leave the existing heap file untouched"
else
  no "Rejected values mutated heap.options" "$(cat "$HEAP_FILE")"
fi

# Test 9: the local caller's own heap validation, loaded from production.
# End-to-end application of the validated value is covered by
# test-local-bootstrap-flow.sh; these cases cover the predicate and the gate.
echo "Test 9: local caller heap validation"
mkdir -p "$TEST_BASE/assets"
cp "$SCRIPT_DIR/local-setup.sh" "$SCRIPT_DIR/common.sh" "$TEST_BASE/assets/"
cat > "$TEST_BASE/assets/snap-context.env" <<EOF
RAG_SNAP_INSTANCE="rag-cli"
RAG_SNAP_USER_COMMON="$TEST_BASE/usercommon"
EOF
LOCAL_SETUP_COPY=$TEST_BASE/assets/local-setup.sh

# local-setup.sh reads its snap context at load time, so source the copy that has
# one. Its main guard keeps sourcing from running setup, and each case runs as its
# own process because the gate exits on rejection.
local_valid_heap() { LS="$LOCAL_SETUP_COPY" bash -c 'source "$LS"; valid_heap "$1"' _ "$1"; }

# effective_heap <value|unset>: what the gate leaves for the shared bootstrap.
effective_heap() {
  LS="$LOCAL_SETUP_COPY" SETVAL="$1" bash -c '
    source "$LS"
    [ "$SETVAL" = unset ] || CFG[OPENSEARCH_HEAP_GIB]=$SETVAL
    validate_heap
    printf "%s" "${CFG[OPENSEARCH_HEAP_GIB]}"'
}

for v in 6 1 12 31; do
  if local_valid_heap "$v"; then
    pass "valid_heap accepts $v"
  else
    no "valid_heap $v" "Should accept $v"
  fi
done

for v in 0 -5 abc 3.5 ""; do
  if ! local_valid_heap "$v"; then
    pass "valid_heap rejects '$v'"
  else
    no "valid_heap '$v'" "Should reject '$v'"
  fi
done

if [ "$(effective_heap unset 2>/dev/null)" = 6 ]; then
  pass "validate_heap applies the local default of 6 GiB when unset"
else
  no "Default heap" "Expected 6, got '$(effective_heap unset 2>&1)'"
fi

if [ "$(effective_heap 10 2>/dev/null)" = 10 ]; then
  pass "validate_heap keeps an explicit override"
else
  no "Explicit heap" "Expected 10, got '$(effective_heap 10 2>&1)'"
fi

gate_output=$(effective_heap abc 2>&1 || true)
if ! effective_heap abc >/dev/null 2>&1 &&
  grep -q 'OPENSEARCH_HEAP_GIB' <<<"$gate_output" &&
  grep -q "'abc'" <<<"$gate_output" &&
  grep -qi 'positive integer' <<<"$gate_output"; then
  pass "validate_heap rejects an invalid saved value with a specific diagnostic"
else
  no "Gate diagnostic" "Got: $gate_output"
fi

echo ""
echo "Results: $TESTS_PASSED/$TESTS_RUN passed"
[ "$TESTS_PASSED" -eq "$TESTS_RUN" ]
