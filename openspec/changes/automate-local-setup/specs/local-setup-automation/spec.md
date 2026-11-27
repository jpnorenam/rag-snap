# Spec Delta

## Purpose

Provides automated provisioning and configuration of a local rag-cli environment where OpenSearch runs on the same machine as rag-cli, using shared implementation extracted from AWS automation with explicit local adaptations.

## ADDED Requirements

### Requirement: Script export and invocation

The `prepare-script` command SHALL accept `local` as a backend option and export all required setup assets. The script SHALL reject execution with sudo.

#### Scenario: Export local setup assets
- **WHEN** user runs `rag-cli.rag prepare-script local --output ~/rag-local`
- **THEN** system creates directory `~/rag-local/` and exports: `local-setup.sh`, `common.sh`, `bootstrap-opensearch.sh`, `snap-context.env`

#### Scenario: Reject sudo invocation
- **WHEN** user runs `sudo ./local-setup.sh`
- **THEN** script exits with error message and instructs user to run without sudo

#### Scenario: Script runs as regular user
- **WHEN** user runs `./local-setup.sh` as non-root user
- **THEN** script executes and uses `sudo` internally for system operations

#### Scenario: Complete setup creates working system
- **WHEN** setup completes successfully
- **THEN** OpenSearch running on localhost:9200 (loopback-bound), Tika running on localhost:9998, rag-cli configured with credentials, knowledge base initialized with deployed models, ready for `rag-cli.rag chat`

### Requirement: Interactive setup

The script SHALL collect deployment settings interactively or load from saved `settings.env` file, validate values before applying, and provision all required services.

#### Scenario: First-time setup with prompts
- **WHEN** user runs `./local-setup.sh` and no `settings.env` exists
- **THEN** script prompts for: OpenSearch channel (default 2/stable), heap size in GiB (default 6), inference endpoint details, optional inference API key, optional Google Drive import settings

#### Scenario: Rerun with saved settings
- **WHEN** user runs `./local-setup.sh` and `settings.env` exists with valid values
- **THEN** script loads saved settings and asks nothing

#### Scenario: Complete a partial saved settings file
- **WHEN** saved settings answer some questions but not all, including the Google Drive questions when the import is enabled
- **THEN** script keeps every saved answer, asks only the unanswered questions with their defaults, saves the completed set, and treats an answer left empty for an optional value as answered so it is not asked again

#### Scenario: Reject an invalid saved setting before any host change
- **WHEN** a saved setting fails validation, such as a channel, port, boolean or yes/no value
- **THEN** script exits naming the setting and the rejected value, before generating secrets, installing or configuring OpenSearch, changing sysctl settings or starting any service

#### Scenario: Validate and apply edited heap value
- **WHEN** user edits OPENSEARCH_HEAP_GIB to valid positive integer and reruns setup
- **THEN** script validates edited value, applies it (even if OPENSEARCH_SETUP_COMPLETED exists), restarts daemon if already running

#### Scenario: Apply the local default when heap is unset
- **WHEN** settings are collected without a heap answer, or saved settings omit OPENSEARCH_HEAP_GIB
- **THEN** script uses 6 GiB as the effective heap

#### Scenario: Reject an invalid heap before any deployment change
- **WHEN** the effective OPENSEARCH_HEAP_GIB is empty, zero, negative, fractional or non-numeric, whether just entered or loaded from saved settings
- **THEN** script exits naming the setting, the rejected value and the expected form, before generating deployment secrets, installing or configuring OpenSearch, changing sysctl settings or starting any service

### Requirement: OpenSearch installation and management

The script SHALL install OpenSearch snap if missing, refuse to modify installations not managed by this setup, and track installation source for safe operations.

#### Scenario: Install OpenSearch when missing
- **WHEN** OpenSearch snap not installed and user runs setup
- **THEN** script installs OpenSearch from configured channel (default 2/stable), records OPENSEARCH_INSTALLED_BY_US=yes with ISO 8601 timestamp in state.env

#### Scenario: Refuse unmanaged existing installation
- **WHEN** OpenSearch snap already installed with state.env missing or OPENSEARCH_INSTALLED_BY_US absent/no
- **THEN** script exits with error explaining refusal, providing three remediation options: use different machine, remove with `sudo snap remove opensearch --purge`, or configure manually

#### Scenario: Resume managed installation
- **WHEN** OpenSearch snap present and state.env shows OPENSEARCH_INSTALLED_BY_US=yes
- **THEN** script continues without error

#### Scenario: Recover from interrupted installation
- **WHEN** setup interrupted after installing snap but before recording state, and user reruns
- **THEN** script exits with error instructing: verify installation is theirs, stop daemon with `sudo snap stop --disable opensearch.daemon`, remove with `sudo snap remove opensearch --purge`, rerun setup

### Requirement: OpenSearch bootstrap

The script SHALL use shared bootstrap implementation from AWS with explicit local parameters for heap configuration, network binding, and sysctl policy.

#### Scenario: Bootstrap fresh OpenSearch using shared implementation
- **WHEN** OpenSearch security not initialized (verified via authentication test)
- **THEN** script calls shared bootstrap: validates heap value, applies heap to jvm.options.d/heap.options, edits opensearch.yml for network.host=127.0.0.1, ensures vm.max_map_count minimum without lowering higher values, generates TLS passwords via shared functions, runs opensearch.setup, sets admin hash, starts daemon, runs security-init, waits for authenticated ready

#### Scenario: Skip bootstrap if security already initialized
- **WHEN** OpenSearch security initialized (verified via successful authenticated health check)
- **THEN** script reports "security already initialized" and skips TLS generation, password hash update, and security-init

#### Scenario: Apply edited heap value before daemon starts
- **WHEN** heap configuration phase runs
- **THEN** script validates OPENSEARCH_HEAP_GIB (positive integer), writes `-Xms<HEAP>g` and `-Xmx<HEAP>g` to heap.options, applies regardless of cluster reachability

#### Scenario: Recover from oversized heap that prevented startup
- **WHEN** user lowers OPENSEARCH_HEAP_GIB in settings after failed startup, reruns setup
- **THEN** script validates new value, applies to heap.options (without requiring OpenSearch reachable), then starts daemon successfully

#### Scenario: Validate the shared bootstrap's heap argument when parsed
- **WHEN** bootstrap is invoked with `--heap` carrying an empty, zero, negative, fractional or non-numeric value, with no value, or with the next option where a value was expected
- **THEN** bootstrap exits with status 2 and a diagnostic naming the rejected value or the missing value, before reading the secrets file, changing sysctl settings, installing or configuring the snap, writing heap.options or operating any service, and without consuming the following option as a heap value

#### Scenario: Omitted heap argument keeps RAM-based sizing
- **WHEN** bootstrap is invoked without `--heap` (the AWS case)
- **THEN** bootstrap calculates the heap from MemTotal using the 3/8 formula, rounded and bounded 1-31 GiB

#### Scenario: Bind to loopback before first startup
- **WHEN** bootstrap configures OpenSearch network
- **THEN** script sets `network.host`, `http.host` and `transport.host` to 127.0.0.1 in opensearch.yml before the daemon is started, after `opensearch.setup` has run, because that command rewrites `network.host` on every invocation and defaults it to `[_local_, _site_]`

#### Scenario: Correct an existing non-loopback binding
- **WHEN** opensearch.yml already carries a `network.host` key, or an `http.*`/`transport.*` host or bind_host override, whose value is not loopback
- **THEN** script replaces every one of those keys with 127.0.0.1 rather than treating the presence of a key as sufficient, preserves unrelated settings including `http.port`, and preserves the file's ownership and mode

#### Scenario: Re-apply binding on a rerun of a managed installation
- **WHEN** setup reruns against a managed installation whose binding is wrong, with the daemon running
- **THEN** script corrects the configuration before any readiness check and restarts the daemon so the change takes effect

#### Scenario: Apply heap and binding before a single restart
- **WHEN** a rerun changes both the heap and the binding while the daemon is running
- **THEN** script writes both before restarting, restarts once, and never brings the node back up on the superseded binding

#### Scenario: Leave an already correct binding alone
- **WHEN** the binding already matches the required configuration
- **THEN** script rewrites nothing and does not restart the daemon

#### Scenario: Verify effective binding before reporting success
- **WHEN** the node is ready and a binding policy was supplied
- **THEN** script reads the node's own `http.bound_address` and `transport.bound_address`, requires each to be a non-empty list whose entries are all strings, checks every entry, and fails naming the reason if either list is missing, empty, the wrong type or mixed, or if any entry is a wildcard or an address other than the requested one or the IPv6 loopback, because a successful request to localhost does not show whether other interfaces are listening

#### Scenario: Replace the block-list form opensearch.setup writes
- **WHEN** opensearch.yml carries `network.host` as a block list, which is how `opensearch.setup` writes it
- **THEN** the key and its list items are replaced by single-line `network.host`, `http.host` and `transport.host` values pointing at the requested address, leaving the rest of the file in place

#### Scenario: Refuse a nested network mapping
- **WHEN** opensearch.yml opens a bare `network`, `http` or `transport` mapping, which this release cannot edit without a YAML parser
- **THEN** script exits explaining which keys to set as single-line values, leaving the file unchanged and the daemon unrestarted

#### Scenario: Ensure minimum vm.max_map_count without lowering higher values
- **WHEN** bootstrap checks sysctl policy
- **THEN** script reads the effective vm.max_map_count, raises it to 262144 with `sudo sysctl -w` only when it is lower, never lowers an existing higher value, and leaves vm.swappiness and net.ipv4.tcp_retries2 unchanged

#### Scenario: Fail when the required sysctl value cannot be reached
- **WHEN** the write is rejected, or is accepted but the value still reads below 262144, as happens in a container where the setting belongs to the host
- **THEN** script exits naming the effective value and the host command that fixes it, rather than warning and continuing

#### Scenario: Disable and verify the snap's sysctl handler
- **WHEN** the shared bootstrap configures the snap, before the daemon is started
- **THEN** script sets `set-sysctl-props=no` and reads the saved value back, failing if either step fails, because when enabled the snap sets vm.swappiness, vm.max_map_count and net.ipv4.tcp_retries2 itself

#### Scenario: Persist only through a file this setup owns
- **WHEN** persisting the minimum
- **THEN** script creates `/etc/sysctl.d/60-rag-local-opensearch.conf` with mode 0644 only if it does not already exist, recording SYSCTL_FILE_CREATED=yes and the exact content written in state.env; an existing file is left byte-identical and is not claimed, and a rerun keeps the recorded ownership

#### Scenario: Wait for authenticated readiness
- **WHEN** OpenSearch daemon starts after security initialization
- **THEN** script uses shared await_ready with bounded wait, exits with error if timeout exceeded, authentication failure does not trigger security reset

#### Scenario: Preserve valid certificates when resuming
- **WHEN** setup interrupted after TLS generation but before security-init, reruns
- **THEN** script detects existing valid certificates and does not regenerate

### Requirement: rag-cli configuration

The script SHALL write credentials file with correct ownership, set config keys via shared rag_set with verification, and start Tika before checking readiness.

#### Scenario: Create credentials file with correct format
- **WHEN** configuration phase runs
- **THEN** both deployments call the shared write_credentials_json writer, which sets OPENSEARCH_USERNAME, OPENSEARCH_PASSWORD (from OPENSEARCH_ADMIN_PASSWORD) and, when a value is supplied, CHAT_API_KEY, atomically and mode 0600, printing no secret. Local layers it on the existing object so an existing API key and every unrelated key are kept, and refuses a file that is not a JSON object; AWS replaces the file after confirming over different existing credentials.

#### Scenario: Set and verify config keys using shared function
- **WHEN** configuration phase runs
- **THEN** both deployments call the shared configure_rag_cli with the OpenSearch endpoint host, which sets each key through rag_set and verifies the effective value

#### Scenario: Start Tika before readiness check
- **WHEN** Tika configuration applied
- **THEN** both deployments call the shared start, which runs `sudo snap start --enable ${RAG_SNAP_INSTANCE}.tika-server` before checking readiness

#### Scenario: Fail when Tika cannot start or does not become ready
- **WHEN** the Tika server cannot be started, or does not answer on http://127.0.0.1:9998/tika within the bounded wait
- **THEN** setup exits with an actionable message, records no successful configuration and prints no success summary

### Requirement: Knowledge base initialization

The script SHALL run `knowledge init` which uses CLI's existing model handling, extract model IDs, save to config and state, and skip if models already deployed.

#### Scenario: Initialize pipelines and models
- **WHEN** knowledge init phase runs and models not deployed (verified by checking deployment status)
- **THEN** script runs `${RAG}.rag knowledge init` which calls registerAndDeploy functions (reuse deployed, deploy registered, register+deploy if absent), extracts model IDs from output, saves via rag_set and to state.env

#### Scenario: Skip if models already deployed
- **WHEN** the two configured model IDs are verified deployed in the cluster
- **THEN** script reports that the models are already deployed and reuses them without running init or registering new models

#### Scenario: Do not treat a saved timestamp as readiness
- **WHEN** a saved KNOWLEDGE_INIT_COMPLETED timestamp exists but a configured model is missing or not deployed
- **THEN** script runs knowledge init again

#### Scenario: Do not treat a request failure as a missing model
- **WHEN** the deployment-state check fails with an authentication, network or other API error
- **THEN** script exits with an actionable message and does not run knowledge init as if the model were missing

#### Scenario: Redeploy if models exist but undeployed
- **WHEN** models exist in REGISTERED state
- **THEN** CLI's registerAndDeploy functions deploy existing models without creating replacements

#### Scenario: Report API failures without misinterpreting as missing
- **WHEN** knowledge init or model verification encounters authentication, network, timeout, or response-parsing failure
- **THEN** script propagates error with actionable message, does not treat request failure as "model missing"

#### Scenario: Check model deployment using bootstrap HTTP helper
- **WHEN** verifying model deployment status
- **THEN** script uses os_curl from bootstrap-opensearch.sh with established TLS, credential, and timeout handling, distinguishes HTTP 200 with DEPLOYED state (success), HTTP 404 (missing model), HTTP 200 with other state (existing undeployed), and request failures (auth/network/timeout)

### Requirement: Start command

The script SHALL start managed services with boot startup enabled, verify readiness, refuse unmanaged installations, and perform no setup work.

#### Scenario: Start services with boot startup
- **WHEN** user runs `./local-setup.sh start`
- **THEN** script verifies OPENSEARCH_INSTALLED_BY_US=yes, runs `sudo snap start --enable opensearch.daemon` and `sudo snap start --enable ${RAG_SNAP_INSTANCE}.tika-server`, waits for authenticated OpenSearch ready and Tika ready

#### Scenario: Refuse unmanaged installation
- **WHEN** user runs start and OPENSEARCH_INSTALLED_BY_US is no/absent
- **THEN** script exits with error

#### Scenario: No setup work during start
- **WHEN** start command runs
- **THEN** script does NOT run opensearch.setup, does NOT register models, does NOT import knowledge

#### Scenario: Services survive reboot after start
- **WHEN** services started via start command and system reboots
- **THEN** OpenSearch and Tika automatically start after boot due to --enable flag

### Requirement: Stop command

The script SHALL stop services, disable automatic startup, retain all data.

#### Scenario: Stop services and disable boot startup
- **WHEN** user runs `./local-setup.sh stop`
- **THEN** script verifies managed installation, runs `sudo snap stop --disable opensearch.daemon` and `sudo snap stop --disable ${RAG_SNAP_INSTANCE}.tika-server`, reports "All services stopped"

#### Scenario: Services remain stopped after reboot
- **WHEN** services stopped via stop command and system reboots
- **THEN** OpenSearch and Tika remain inactive due to --disable flag

#### Scenario: Retain data and configuration
- **WHEN** stop command completes
- **THEN** all indexed data, configuration files, credentials, state remain unchanged

### Requirement: Destroy command

The script SHALL confirm before changes, stop and disable services (OpenSearch + Tika), remove managed OpenSearch without snapshot, clear the shared bootstrap's record that it started this node (it lives outside the snap and would otherwise make a redeploy skip TLS setup), clean deployment-specific OpenSearch credentials from credentials.json, clear deployment secrets and obsolete state, preserve reusable credentials.

#### Scenario: Confirm before any destructive changes
- **WHEN** user runs `./local-setup.sh destroy`
- **THEN** script checks OPENSEARCH_INSTALLED_BY_US in state.env, if no/absent reports "OpenSearch not installed by this setup" and exits without modifying services or files, otherwise prompts "This will stop services and remove the managed OpenSearch installation. Continue? [y/N]:", proceeds only on y/yes, makes no changes if declined

#### Scenario: Stop and disable both services after ownership check
- **WHEN** destroy runs after ownership verification and confirmation
- **THEN** script runs `sudo snap stop --disable opensearch.daemon` and `sudo snap stop --disable ${RAG_SNAP_INSTANCE}.tika-server`

#### Scenario: Remove managed OpenSearch without snapshot
- **WHEN** destroy removes OpenSearch with OPENSEARCH_INSTALLED_BY_US=yes
- **THEN** script runs `sudo snap remove opensearch --purge` (no automatic snapshot), clears OPENSEARCH_INSTALLED_BY_US after successful removal

#### Scenario: Preserve unmanaged OpenSearch
- **WHEN** destroy runs with OPENSEARCH_INSTALLED_BY_US=no
- **THEN** script reports "not installed by us" and skips snap removal

#### Scenario: Clean deployment-specific OpenSearch credentials from credentials.json
- **WHEN** destroy cleans credentials
- **THEN** script removes OPENSEARCH_USERNAME and OPENSEARCH_PASSWORD from credentials.json only if OPENSEARCH_PASSWORD matches generated value, preserves CHAT_API_KEY and other credentials

#### Scenario: Clear deployment secrets and obsolete state
- **WHEN** destroy removes managed resources successfully
- **THEN** script removes TLS_*_PASS and OPENSEARCH_ADMIN_PASSWORD from secrets.env, removes OPENSEARCH_SETUP_COMPLETED, KNOWLEDGE_INIT_COMPLETED, EMBEDDING_MODEL_ID, RERANK_MODEL_ID, DRIVE_IMPORT_LAST_ATTEMPT, SYSCTL_FILE_CREATED, OPENSEARCH_INSTALLED_BY_US from state.env

#### Scenario: Preserve reusable credentials
- **WHEN** destroy completes
- **THEN** script preserves all settings.env, preserves CHAT_API_KEY and GOOGLE_DRIVE_CLIENT_* in secrets.env, preserves OAuth token cache, does not blank rag-cli config keys serving other endpoints

#### Scenario: Clean up sysctl configuration created by setup
- **WHEN** destroy cleans up system configuration
- **THEN** script removes `/etc/sysctl.d/60-rag-local-opensearch.conf` only if SYSCTL_FILE_CREATED=yes in state.env AND the file content still matches SYSCTL_FILE_CONTENT, clears both keys from state.env, does not restore the previous runtime sysctl value, and keeps a file that was edited after creation or that this setup did not create, saying which of the two applies

#### Scenario: Support redeploy after destroy
- **WHEN** user runs setup after destroy
- **THEN** script generates fresh deployment secrets, initializes new cluster, registers new models, writes new credentials

#### Scenario: Preserve state for retry if removal fails
- **WHEN** `snap remove opensearch --purge` fails
- **THEN** script reports error, retains OPENSEARCH_INSTALLED_BY_US=yes to allow retry

#### Scenario: Recovery if removal succeeds but cleanup incomplete
- **WHEN** snap removed successfully but script interrupted before clearing state
- **THEN** next destroy detects OpenSearch absent, clears stale state including OPENSEARCH_INSTALLED_BY_US, SYSCTL_FILE_CREATED, and completion markers without attempting re-removal

### Requirement: State tracking

The script SHALL track installation source and completion markers, verify actual state before skipping phases.

#### Scenario: Track installation source with timestamp
- **WHEN** script installs OpenSearch snap
- **THEN** state.env contains OPENSEARCH_INSTALLED_BY_US=yes with ISO 8601 timestamp

#### Scenario: Track sysctl file creation
- **WHEN** script creates /etc/sysctl.d/60-rag-local-opensearch.conf
- **THEN** state.env contains SYSCTL_FILE_CREATED=yes and SYSCTL_FILE_CONTENT holding exactly what was written, so cleanup can tell an untouched file from an edited one

#### Scenario: Verify actual state before skipping phases
- **WHEN** setup reads completion marker (e.g., OPENSEARCH_SETUP_COMPLETED)
- **THEN** script also verifies actual cluster state (security initialized via authentication test, certificates present, models deployed in OpenSearch) before skipping

#### Scenario: Reapply if actual state missing
- **WHEN** OPENSEARCH_SETUP_COMPLETED present but certificates missing
- **THEN** script reruns bootstrap steps to restore actual state

#### Scenario: Redeploy models if undeployed
- **WHEN** KNOWLEDGE_INIT_COMPLETED present but models not in DEPLOYED state
- **THEN** script reruns knowledge init, CLI's registerAndDeploy logic handles deploying existing models

### Requirement: Secret management

The script SHALL generate deployment secrets using shared implementation, preserve across reruns, preserve reusable credentials across destroy, mask in output.

#### Scenario: Generate deployment secrets
- **WHEN** setup runs without deployment secrets
- **THEN** script calls shared gen_secret to generate 32-character alphanumeric values for TLS_*_PASS and OPENSEARCH_ADMIN_PASSWORD, saves to secrets.env mode 0600

#### Scenario: Preserve deployment secrets across reruns
- **WHEN** secrets.env contains deployment secrets
- **THEN** script loads existing secrets without regenerating unless destroy cleared them

#### Scenario: Preserve reusable credentials across destroy
- **WHEN** destroy runs
- **THEN** script preserves CHAT_API_KEY, GOOGLE_DRIVE_CLIENT_ID, GOOGLE_DRIVE_CLIENT_SECRET in secrets.env

#### Scenario: Mask secrets in output
- **WHEN** script logs commands or output containing secrets
- **THEN** shared mask function replaces secret values with `***`

### Requirement: Google Drive import support

The script SHALL optionally import from Drive using shared implementation, reuse CLI's token cache.

#### Scenario: Skip if disabled
- **WHEN** DRIVE_IMPORT=no in settings.env
- **THEN** import phase skipped

#### Scenario: Run if enabled
- **WHEN** DRIVE_IMPORT=yes with DRIVE_FOLDER_URL and client credentials
- **THEN** script displays headless authentication guidance, runs the import through the shared helper, logs to its own file so this works whether or not knowledge initialization ran in the same run, masks secret values in the output while leaving the Google authorization URL usable, reports the import command's own exit status without claiming per-archive success, and updates DRIVE_IMPORT_LAST_ATTEMPT timestamp

#### Scenario: Reuse CLI's OAuth token cache
- **WHEN** Drive import runs with existing token at `$SNAP_USER_DATA/gdrive-token.json` or `~/.config/rag-cli/gdrive-token.json`
- **THEN** CLI reuses cached token without prompting

### Requirement: Shared implementation extraction

Local and AWS SHALL call the same implementation for common operations. Bootstrap assets SHALL be exported completely.

#### Scenario: Extract shared functions
- **WHEN** refactoring opensearch-on-aws.sh
- **THEN** common.sh holds the shared mask, say and confirm helpers, load_env/save_env, gen_secret, rag_set, configure_rag_cli, start_tika_and_wait, write_credentials_json, knowledge_init_and_extract, drive_import_guidance and drive_import_folder, and both callers use them

#### Scenario: Local and AWS use same bootstrap implementation
- **WHEN** local bootstrap runs
- **THEN** calls shared TLS setup, admin hash, security initialization, bounded waits from bootstrap-opensearch.sh, passing explicit parameters for local differences

#### Scenario: Local export includes all bootstrap dependencies
- **WHEN** prepare-script exports local assets
- **THEN** output directory contains local-setup.sh, common.sh, bootstrap-opensearch.sh, snap-context.env

#### Scenario: AWS remote transfer includes all dependencies
- **WHEN** AWS bootstrap copies files to remote
- **THEN** transferred files include bootstrap-opensearch.sh and common.sh

#### Scenario: AWS behavior preserved after extraction
- **WHEN** opensearch-on-aws.sh refactored to use common.sh
- **THEN** it keeps its credential-replacement confirmation and its masking of command output, and it fails the run when the import command exits non-zero, while local reports the same status without failing

### Requirement: Testing validation

The implementation SHALL include integration tests and single consolidated AWS regression.

#### Scenario: Core functionality tests
- **WHEN** implementation validated
- **THEN** tests verify: export from installed snap, fresh setup with default heap (6 GiB), fresh setup with custom heap, edited heap applied without requiring OpenSearch reachable, recovery from oversized heap, saved answers loaded, safe reruns skip per actual state verification, interruption recovery resumes correctly, unmanaged installation refused, loopback binding verified (127.0.0.1 only), sysctl policy applied (never lowers existing, respects existing file), start with --enable survives reboot, stop with --disable prevents reboot startup, destroy removes with --purge, destroy cancellation changes nothing, credentials.json cleanup preserves non-OpenSearch entries, redeploy succeeds with fresh secrets

#### Scenario: Consolidated AWS regression
- **WHEN** shared implementation extraction integrated
- **THEN** single AWS end-to-end test campaign verifies no regression
