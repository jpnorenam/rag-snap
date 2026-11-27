#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

echo "================================================================"
echo "Running all local-setup automation tests"
echo "================================================================"
echo ""

TOTAL_PASSED=0
TOTAL_FAILED=0

run_test() {
	local test_script=$1
	local test_name=$(basename "$test_script" .sh)
	
	echo "Running: $test_name"
	echo "----------------------------------------"
	
	if "$test_script"; then
		echo "✓ $test_name PASSED"
		TOTAL_PASSED=$((TOTAL_PASSED + 1))
	else
		echo "✗ $test_name FAILED"
		TOTAL_FAILED=$((TOTAL_FAILED + 1))
	fi
	echo ""
}

run_test "$SCRIPT_DIR/test-common.sh"
run_test "$SCRIPT_DIR/test-aws-refactoring.sh"
run_test "$SCRIPT_DIR/test-local-behavior.sh"
run_test "$SCRIPT_DIR/test-heap.sh"
run_test "$SCRIPT_DIR/test-readiness.sh"
run_test "$SCRIPT_DIR/test-local-bootstrap-flow.sh"

echo "================================================================"
echo "Test Summary"
echo "================================================================"
echo "Passed: $TOTAL_PASSED"
echo "Failed: $TOTAL_FAILED"
echo ""

if [ "$TOTAL_FAILED" -gt 0 ]; then
	echo "OVERALL: FAILED"
	exit 1
else
	echo "OVERALL: SUCCESS"
	exit 0
fi
