#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
source "$SCRIPT_DIR/common.sh"

declare -A CFG=()
MASKED_KEYS=(OPENSEARCH_ADMIN_PASSWORD TLS_ROOT_PASS TLS_ADMIN_PASS TLS_NODE_PASS CHAT_API_KEY GOOGLE_DRIVE_CLIENT_SECRET)

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

assert_equals() {
	local expected=$1
	local actual=$2
	local desc=$3
	TESTS_RUN=$((TESTS_RUN + 1))
	if [ "$expected" = "$actual" ]; then
		echo "✓ $desc"
		TESTS_PASSED=$((TESTS_PASSED + 1))
	else
		echo "✗ $desc"
		echo "  Expected: $expected"
		echo "  Actual:   $actual"
		TESTS_FAILED=$((TESTS_FAILED + 1))
	fi
}

assert_matches() {
	local pattern=$1
	local actual=$2
	local desc=$3
	TESTS_RUN=$((TESTS_RUN + 1))
	if [[ $actual =~ $pattern ]]; then
		echo "✓ $desc"
		TESTS_PASSED=$((TESTS_PASSED + 1))
	else
		echo "✗ $desc"
		echo "  Pattern:  $pattern"
		echo "  Actual:   $actual"
		TESTS_FAILED=$((TESTS_FAILED + 1))
	fi
}

test_mask() {
	echo "==> Testing mask()"
	
	CFG[OPENSEARCH_ADMIN_PASSWORD]="secret123"
	CFG[CHAT_API_KEY]="key456"
	
	local input="Setting OPENSEARCH_ADMIN_PASSWORD=secret123 and CHAT_API_KEY=key456"
	local expected="Setting OPENSEARCH_ADMIN_PASSWORD=*** and CHAT_API_KEY=***"
	local actual
	actual=$(echo "$input" | mask)
	
	assert_equals "$expected" "$actual" "mask() should replace secret values with ***"
	
	local multiline="Line 1: secret123
Line 2: key456
Line 3: no secrets"
	local expected_ml="Line 1: ***
Line 2: ***
Line 3: no secrets"
	local actual_ml
	actual_ml=$(echo "$multiline" | mask)
	
	assert_equals "$expected_ml" "$actual_ml" "mask() should work on multiple lines"
}

test_has() {
	echo "==> Testing has()"
	
	CFG[KEY1]="value1"
	CFG[KEY2]=""
	
	if has KEY1; then
		echo "✓ has() returns true for set key"
		TESTS_RUN=$((TESTS_RUN + 1))
		TESTS_PASSED=$((TESTS_PASSED + 1))
	else
		echo "✗ has() should return true for KEY1"
		TESTS_RUN=$((TESTS_RUN + 1))
		TESTS_FAILED=$((TESTS_FAILED + 1))
	fi
	
	if has KEY2; then
		echo "✓ has() returns true for empty value"
		TESTS_RUN=$((TESTS_RUN + 1))
		TESTS_PASSED=$((TESTS_PASSED + 1))
	else
		echo "✗ has() should return true for KEY2 (empty value)"
		TESTS_RUN=$((TESTS_RUN + 1))
		TESTS_FAILED=$((TESTS_FAILED + 1))
	fi
	
	if ! has KEY3; then
		echo "✓ has() returns false for unset key"
		TESTS_RUN=$((TESTS_RUN + 1))
		TESTS_PASSED=$((TESTS_PASSED + 1))
	else
		echo "✗ has() should return false for KEY3"
		TESTS_RUN=$((TESTS_RUN + 1))
		TESTS_FAILED=$((TESTS_FAILED + 1))
	fi
}

test_gen_secret() {
	echo "==> Testing gen_secret()"
	
	local secret
	secret=$(gen_secret)
	
	assert_equals "32" "${#secret}" "gen_secret() should generate 32 characters"
	assert_matches "^[A-Za-z0-9]+$" "$secret" "gen_secret() should only contain alphanumeric characters"
	
	local secret2
	secret2=$(gen_secret)
	
	if [ "$secret" != "$secret2" ]; then
		echo "✓ gen_secret() generates different values"
		TESTS_RUN=$((TESTS_RUN + 1))
		TESTS_PASSED=$((TESTS_PASSED + 1))
	else
		echo "✗ gen_secret() generated the same value twice"
		TESTS_RUN=$((TESTS_RUN + 1))
		TESTS_FAILED=$((TESTS_FAILED + 1))
	fi
}

test_load_save_env() {
	echo "==> Testing load_env() and save_env()"
	
	local tmpdir
	tmpdir=$(mktemp -d)
	
	local testfile="$tmpdir/test.env"
	
	CFG[TEST_KEY1]="value1"
	CFG[TEST_KEY2]="value with spaces"
	CFG[TEST_KEY3]='value"with"quotes'
	CFG[TEST_KEY4]='value\with\backslashes'
	
	save_env "$testfile" TEST_KEY1 TEST_KEY2 TEST_KEY3 TEST_KEY4
	
	if [ -f "$testfile" ]; then
		echo "✓ save_env() creates file"
		TESTS_RUN=$((TESTS_RUN + 1))
		TESTS_PASSED=$((TESTS_PASSED + 1))
	else
		echo "✗ save_env() should create file"
		TESTS_RUN=$((TESTS_RUN + 1))
		TESTS_FAILED=$((TESTS_FAILED + 1))
		rm -rf "$tmpdir"
		return
	fi
	
	unset CFG
	declare -gA CFG=()
	
	load_env "$testfile" TEST_KEY1 TEST_KEY2 TEST_KEY3 TEST_KEY4
	
	assert_equals "value1" "${CFG[TEST_KEY1]}" "load_env() should restore simple value"
	assert_equals "value with spaces" "${CFG[TEST_KEY2]}" "load_env() should restore value with spaces"
	assert_equals 'value"with"quotes' "${CFG[TEST_KEY3]}" "load_env() should restore value with quotes"
	assert_equals 'value\with\backslashes' "${CFG[TEST_KEY4]}" "load_env() should restore value with backslashes"
	
	rm -rf "$tmpdir"
}

test_model_id() {
	echo "==> Testing model_id()"
	
	local tmpdir
	tmpdir=$(mktemp -d)
	
	local logfile="$tmpdir/knowledge-init.log"
	
	cat > "$logfile" << 'EOF'
Initializing knowledge base...
Loading models...
Embedding model ID: embedding-model-v1
Processing...
Rerank model ID: rerank-model-v2
Complete.
EOF
	
	local emb
	emb=$(model_id Embedding "$logfile")
	assert_equals "embedding-model-v1" "$emb" "model_id() should extract Embedding model ID"
	
	local rer
	rer=$(model_id Rerank "$logfile")
	assert_equals "rerank-model-v2" "$rer" "model_id() should extract Rerank model ID"
	
	rm -rf "$tmpdir"
}

# write_credentials_json is the shared writer both callers use. Pass '{}' to
# replace the file, or the current object to keep unrelated keys; the API key is
# only replaced when a new one is supplied.
test_write_credentials_json() {
	echo "==> Testing write_credentials_json()"
	local d f
	d=$(mktemp -d)
	f=$d/credentials.json

	# Replace policy ('{}' base): only the OpenSearch keys and the supplied key.
	write_credentials_json "$f" "pw1" "key1" '{}'
	assert_equals "admin" "$(jq -r .OPENSEARCH_USERNAME "$f")" "writes the admin username"
	assert_equals "pw1" "$(jq -r .OPENSEARCH_PASSWORD "$f")" "writes the password"
	assert_equals "key1" "$(jq -r .CHAT_API_KEY "$f")" "writes a supplied API key"
	assert_equals "600" "$(stat -c %a "$f")" "writes mode 0600"

	# Merge policy: an unrelated key and the existing API key are kept.
	printf '{"OTHER":"keep","CHAT_API_KEY":"oldkey","OPENSEARCH_USERNAME":"admin","OPENSEARCH_PASSWORD":"old"}' >"$f"
	write_credentials_json "$f" "pw2" "" "$(cat "$f")"
	assert_equals "keep" "$(jq -r .OTHER "$f")" "keeps unrelated keys"
	assert_equals "oldkey" "$(jq -r .CHAT_API_KEY "$f")" "keeps the API key when none is supplied"
	assert_equals "pw2" "$(jq -r .OPENSEARCH_PASSWORD "$f")" "updates the password"

	# A supplied key replaces the old one.
	write_credentials_json "$f" "pw3" "newkey" "$(cat "$f")"
	assert_equals "newkey" "$(jq -r .CHAT_API_KEY "$f")" "replaces the API key when one is supplied"
	rm -rf "$d"
}

# knowledge_init_and_extract is the shared model-init both callers use: it runs
# `knowledge init`, takes both model IDs from the same log and configures them.
test_knowledge_init_and_extract() {
	echo "==> Testing knowledge_init_and_extract()"
	local d log
	d=$(mktemp -d)
	RAG=$d/rag
	cat >"$RAG" <<'EOF'
#!/usr/bin/env bash
echo "Deploying models"
echo "Embedding model ID: emb-model-1"
echo "Rerank model ID: rer-model-2"
EOF
	chmod +x "$RAG"
	rag_set() { printf '%s=%s\n' "$1" "$2" >>"$d/config"; }

	log=$d/init.log
	knowledge_init_and_extract "$log"
	assert_equals "emb-model-1" "$EMB_MODEL_ID" "extracts the embedding model ID"
	assert_equals "rer-model-2" "$RER_MODEL_ID" "extracts the rerank model ID"
	assert_matches "knowledge.model.embedding=emb-model-1" "$(cat "$d/config")" "configures the embedding model"
	assert_matches "knowledge.model.rerank=rer-model-2" "$(cat "$d/config")" "configures the rerank model"
	assert_matches "Embedding model ID: emb-model-1" "$(cat "$log")" "keeps the init output in the log"
	rm -rf "$d"
}

# drive_import_folder is the shared import both callers use: it runs the import
# for the configured folder with the saved OAuth client, reports the command's own
# exit status without claiming per-archive success, records the attempt, and does
# not fail the caller.
test_drive_import_folder() {
	echo "==> Testing drive_import_folder()"
	local d log out rc
	d=$(mktemp -d)
	RAG=$d/rag
	cat >"$RAG" <<'EOF'
#!/usr/bin/env bash
echo "creds=$GOOGLE_DRIVE_CLIENT_ID/$GOOGLE_DRIVE_CLIENT_SECRET args=$*"
exit 3
EOF
	chmod +x "$RAG"
	CFG[DRIVE_IMPORT]=yes
	CFG[DRIVE_FOLDER_URL]="https://example.invalid/folder"
	CFG[GOOGLE_DRIVE_CLIENT_ID]=client-id
	CFG[GOOGLE_DRIVE_CLIENT_SECRET]=client-secret
	unset 'CFG[DRIVE_IMPORT_LAST_ATTEMPT]'

	log=$d/import.log
	out=$d/out
	rc=0
	drive_import_folder "$log" >"$out" 2>&1 || rc=$?
	assert_equals "3" "$rc" "returns the import command's own exit status"
	assert_matches "creds=client-id/\\*\\*\\* args=knowledge import --url https://example.invalid/folder --all" \
		"$(cat "$out")" "imports the folder with the saved OAuth client, client ID intact"
	assert_matches "^[0-9]{4}-" "${CFG[DRIVE_IMPORT_LAST_ATTEMPT]}" "records the attempt timestamp"
	assert_matches "creds=" "$(cat "$log")" "keeps the import output in its own log"

	cat >"$RAG" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
	unset 'CFG[DRIVE_IMPORT_LAST_ATTEMPT]'
	rc=0
	drive_import_folder "$d/import2.log" >"$out" 2>&1 || rc=$?
	assert_equals "0" "$rc" "a successful import returns 0"
	rm -rf "$d"
}

# Finishing the Google sign-in must stay possible after masking: the client ID is
# a public identifier inside the authorization URL, and only real secrets are masked.
test_mask_keeps_oauth_url() {
	echo "==> Testing mask() keeps the OAuth authorization URL usable"
	CFG[GOOGLE_DRIVE_CLIENT_ID]="1234-example.apps.googleusercontent.com"
	CFG[GOOGLE_DRIVE_CLIENT_SECRET]="top-secret-value"
	local url="https://accounts.google.com/o/oauth2/v2/auth?client_id=1234-example.apps.googleusercontent.com&state=xyz"
	assert_matches "client_id=1234-example.apps.googleusercontent.com" "$(printf '%s\n' "$url" | mask)" \
		"the OAuth client ID and authorization URL are not masked"
	assert_matches "secret=\\*\\*\\*" "$(printf 'secret=%s\n' "${CFG[GOOGLE_DRIVE_CLIENT_SECRET]}" | mask)" \
		"a real secret value is still masked"
}

main() {
	echo "Running common.sh tests"
	echo "======================="
	
	test_mask
	test_has
	test_gen_secret
	test_load_save_env
	test_model_id
	test_knowledge_init_and_extract
	test_write_credentials_json
	test_drive_import_folder
	test_mask_keeps_oauth_url
	
	echo ""
	echo "======================="
	echo "Results: $TESTS_PASSED/$TESTS_RUN passed"
	
	if [ "$TESTS_FAILED" -gt 0 ]; then
		echo "FAILED: $TESTS_FAILED test(s) failed"
		exit 1
	else
		echo "SUCCESS: All tests passed"
		exit 0
	fi
}

main
