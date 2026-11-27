#!/usr/bin/env bash
# Test bootstrap rerun and readiness behavior
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
TEST_BASE=$(mktemp -d)
trap 'rm -rf "$TEST_BASE"' EXIT

# Test counters
TESTS_RUN=0
TESTS_PASSED=0

pass() {
  echo "✓ $1"
  TESTS_PASSED=$((TESTS_PASSED + 1))
  TESTS_RUN=$((TESTS_RUN + 1))
}

# Named "no" so it cannot shadow the bootstrap's own fail(), which must keep its
# production behavior (diagnostics, then exit) in the cases below.
no() {
  echo "✗ $1: $2"
  TESTS_RUN=$((TESTS_RUN + 1))
}

echo "==> Testing readiness and bootstrap rerun"
echo ""

# Source the real functions first: every override below has to be applied after
# this line, or the production definitions replace it.
source "$SCRIPT_DIR/bootstrap-opensearch.sh"

STUB_DIR="$TEST_BASE/stubs"
mkdir -p "$STUB_DIR" "$TEST_BASE/conf"
printf '#!/bin/bash\nexit 0\n' > "$STUB_DIR/systemctl"
printf '#!/bin/bash\nexit 0\n' > "$STUB_DIR/journalctl"
chmod +x "$STUB_DIR"/*
export PATH="$STUB_DIR:$PATH"

CONF="$TEST_BASE/conf"
CLUSTER_LOG="$TEST_BASE/cluster.log"
WAIT_TRIES=3
WAIT_INTERVAL=1
URL="https://opensearch.invalid:9200"
S[OPENSEARCH_ADMIN_PASSWORD]="testpass"

# Stub os_curl: the only boundary to a real service.
os_curl() {
  local pass="$1"; shift
  local endpoint="$1"; shift
  local body_arg=""
  local write_arg=""

  while [ $# -gt 0 ]; do
    case "$1" in
      -o) body_arg="$2"; shift 2 ;;
      -w) write_arg="$2"; shift 2 ;;
      *) shift ;;
    esac
  done

  local state_file="$TEST_BASE/endpoint_state"
  local state=$(cat "$state_file" 2>/dev/null || echo "503_uninit")

  case "$state" in
    503_uninit)
      if [ -n "$body_arg" ]; then
        echo '{"error":"Security not initialized"}' > "$body_arg"
      fi
      [ -n "$write_arg" ] && echo "503"
      return 1
      ;;
    200_ready)
      if [ -n "$body_arg" ]; then
        echo '{"user_name":"admin"}' > "$body_arg"
      fi
      [ -n "$write_arg" ] && echo "200"
      return 0
      ;;
    401_rejected)
      if [ -n "$body_arg" ]; then
        echo '{"error":"Unauthorized"}' > "$body_arg"
      fi
      [ -n "$write_arg" ] && echo "401"
      return 1
      ;;
    503_other)
      if [ -n "$body_arg" ]; then
        echo '{"error":"Service unavailable"}' > "$body_arg"
      fi
      [ -n "$write_arg" ] && echo "503"
      return 1
      ;;
    timeout)
      sleep 5
      return 1
      ;;
  esac
}

# Test 1: security_state detects uninitialized
echo "Test 1: security_state detects 503 'Security not initialized'"
echo "503_uninit" > "$TEST_BASE/endpoint_state"
state=$(security_state 2>/dev/null)
if [ "$state" = "uninitialized" ]; then
  pass "503 with 'Security not initialized' detected as uninitialized"
else
  no "Uninitialized detection" "Expected 'uninitialized', got '$state'"
fi

# Test 2: security_state detects ready
echo "Test 2: security_state detects 200 ready"
echo "200_ready" > "$TEST_BASE/endpoint_state"
state=$(security_state 2>/dev/null)
if [ "$state" = "ready" ]; then
  pass "200 response detected as ready"
else
  no "Ready detection" "Expected 'ready', got '$state'"
fi

# Test 3: security_state detects rejected
echo "Test 3: security_state detects 401 rejected"
echo "401_rejected" > "$TEST_BASE/endpoint_state"
state=$(security_state 2>/dev/null)
if [ "$state" = "rejected" ]; then
  pass "401 response detected as rejected"
else
  no "Rejected detection" "Expected 'rejected', got '$state'"
fi

# Test 4: security_state retries and times out on persistent 503 (not "uninitialized")
echo "Test 4: security_state times out on non-uninitialized 503"
echo "503_other" > "$TEST_BASE/endpoint_state"
# The timeout path calls the production fail(), which exits: run it in a subshell.
if ! state=$( (security_state) 2>/dev/null ); then
  pass "Persistent non-uninitialized 503 times out correctly"
else
  no "503 timeout" "Should have failed, got state '$state'"
fi

# Test 5: await_ready waits for 200
echo "Test 5: await_ready accepts 200"
echo "200_ready" > "$TEST_BASE/endpoint_state"
if await_ready 2>/dev/null; then
  pass "await_ready succeeds on 200"
else
  no "await_ready 200" "Should have succeeded"
fi

# Test 6: await_ready fails on 401
echo "Test 6: await_ready fails on 401 (rejected credentials)"
echo "401_rejected" > "$TEST_BASE/endpoint_state"
if ! ( await_ready ) >/dev/null 2>&1; then
  pass "await_ready fails on 401 as expected"
else
  no "await_ready 401" "Should have failed on rejected credentials"
fi

# Test 7: await_ready retries on 503 then succeeds
echo "Test 7: await_ready retries on 503 then succeeds when ready"
echo "503_uninit" > "$TEST_BASE/endpoint_state"
(
  sleep 1
  echo "200_ready" > "$TEST_BASE/endpoint_state"
) &
if await_ready 2>/dev/null; then
  pass "await_ready retried 503 and succeeded when ready"
else
  no "await_ready retry" "Should have succeeded after retry"
fi
wait

echo ""
echo "Results: $TESTS_PASSED/$TESTS_RUN passed"
[ "$TESTS_PASSED" -eq "$TESTS_RUN" ]
