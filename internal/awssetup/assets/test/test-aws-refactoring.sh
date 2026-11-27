#!/usr/bin/env bash
# Verify opensearch-on-aws.sh keeps shared behaviour in common.sh and calls it.
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
AWS_SCRIPT="$SCRIPT_DIR/opensearch-on-aws.sh"

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

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
rec="$tmp/calls"
: >"$rec"

echo "Testing opensearch-on-aws.sh shared behaviour"
echo "=============================================="

# The shared functions live in common.sh and must not be redefined here.
duplicated=
for fn in mask has load_env save_env gen_secret rag_set model_id say confirm \
	knowledge_init_and_extract drive_import_guidance drive_import_folder \
	configure_rag_cli start_tika_and_wait write_credentials_json; do
	grep -q "^${fn}()" "$AWS_SCRIPT" && duplicated="$duplicated $fn"
done
if [ -z "$duplicated" ]; then
	pass "does not redefine any shared function"
else
	fail "redefines shared functions" "also defined in common.sh:$duplicated"
fi

# Run the two phases that share code with the local caller, with the shared
# functions replaced by recorders, and check they are the ones called.
cat >"$tmp/exec.sh" <<'DRIVER'
#!/usr/bin/env bash
set -euo pipefail
source "$1"
rec=$2
CFG[DRIVE_IMPORT]=yes
work=$(mktemp -d)
phase() { PHASE_LOG=$work/phase.log; : >"$PHASE_LOG"; }
save_all() { :; }
knowledge_init_and_extract() { printf 'model-init:%s\n' "$1" >>"$rec"; }
drive_import_folder() { printf 'drive-import:%s\n' "$1" >>"$rec"; }
write_credentials() { printf 'credentials\n' >>"$rec"; }
configure_rag_cli() { printf 'endpoint:%s\n' "$1" >>"$rec"; }
start_tika_and_wait() { printf 'tika\n' >>"$rec"; }
IP=10.0.0.9
init_models
drive_import
configure_local
DRIVER

if bash "$tmp/exec.sh" "$AWS_SCRIPT" "$rec"; then
	pass "init_models and drive_import run against the shared functions"
else
	fail "AWS phases did not run" "see the error above"
fi

if grep -q '^model-init:' "$rec"; then
	pass "init_models calls the shared knowledge_init_and_extract"
else
	fail "init_models does not call the shared model init" "$(tr '\n' '|' <"$rec")"
fi

if grep -q '^drive-import:' "$rec"; then
	pass "drive_import calls the shared drive_import_folder"
else
	fail "drive_import does not call the shared drive import" "$(tr '\n' '|' <"$rec")"
fi

if grep -q '^endpoint:10.0.0.9$' "$rec" && grep -q '^tika$' "$rec" && grep -q '^credentials$' "$rec"; then
	pass "configure_local passes the endpoint to the shared configuration and Tika start"
else
	fail "configure_local does not delegate the shared operations" "$(tr '\n' '|' <"$rec")"
fi

# AWS records a new import attempt only when the command succeeds, so a failed
# first attempt does not make the next run offer to retry.
cat >"$tmp/import.sh" <<'DRIVER'
#!/usr/bin/env bash
set -euo pipefail
source "$1"
rec=$2
rcwant=$3
CFG[DRIVE_IMPORT]=yes
phase() { PHASE_LOG=$rec.phaselog; : >"$PHASE_LOG"; }
save_all() { printf 'saved\n' >>"$rec"; }
drive_import_folder() { return "$rcwant"; }
drive_import
DRIVER

rec2=$tmp/import-rec
: >"$rec2"
rc=0
bash "$tmp/import.sh" "$AWS_SCRIPT" "$rec2" 3 >/dev/null 2>&1 || rc=$?
if [ "$rc" != 0 ] && ! grep -q '^saved' "$rec2"; then
	pass "a failed import fails the run and records no new attempt"
else
	fail "a failed import recorded an attempt" "rc=$rc, calls: $(tr '\n' '|' <"$rec2")"
fi

: >"$rec2"
rc=0
bash "$tmp/import.sh" "$AWS_SCRIPT" "$rec2" 0 >/dev/null 2>&1 || rc=$?
if [ "$rc" = 0 ] && grep -q '^saved' "$rec2"; then
	pass "a successful import records the attempt"
else
	fail "a successful import did not record the attempt" "rc=$rc, calls: $(tr '\n' '|' <"$rec2")"
fi

echo ""
echo "Results: $TESTS_PASSED/$TESTS_RUN passed"
[ "$TESTS_PASSED" -eq "$TESTS_RUN" ]
