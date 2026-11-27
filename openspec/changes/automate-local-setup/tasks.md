# Tasks

## 1. Shared Implementation Extraction

- [x] 1.1 Create `internal/awssetup/assets/common.sh` with mask() (from opensearch-on-aws.sh mask function)
- [x] 1.2 Extract load_env/save_env to common.sh, add state.env support
- [x] 1.3 Extract gen_secret() to common.sh
- [x] 1.4 Extract rag_set() with verification to common.sh
- [x] 1.5 Extract the shared common.sh functions both deployments call: knowledge_init_and_extract, drive_import_guidance/drive_import_folder, configure_rag_cli, start_tika_and_wait, write_credentials_json
- [x] 1.6 Refactor opensearch-on-aws.sh to source common.sh, preserve all AWS code
- [x] 1.7 Update AWS remote transfer to include common.sh alongside bootstrap-opensearch.sh

## 2. Local Script Structure

- [x] 2.1 Create `internal/awssetup/assets/local-setup.sh` with shebang, set -euo pipefail, umask 077
- [x] 2.2 Implement sudo rejection: check EUID/SUDO_USER, exit with error
- [x] 2.3 Source common.sh and bootstrap-opensearch.sh
- [x] 2.4 Load snap-context.env for RAG_SNAP_INSTANCE
- [x] 2.5 Implement mode routing: setup/start/stop/destroy

## 3. Setup: Preflight and Settings

- [x] 3.1 Implement preflight: check bash, curl, sudo, snap, ${RAG_SNAP_INSTANCE} installed, ragd inactive
- [x] 3.2 Implement settings collection: load settings.env if exists, else prompt (channel, heap, inference, optional API key, Drive), save via save_env
- [x] 3.3 Add validation: heap positive integer (6 GiB default when unset, gated before any provisioning step), channel pattern

## 4. Setup: OpenSearch Installation

- [x] 4.1 Implement installation: if snap absent, install, record OPENSEARCH_INSTALLED_BY_US=yes via save_env
- [x] 4.2 Implement unmanaged refusal: if snap present without ownership marker, exit with error listing three remediation options
- [x] 4.3 Implement resume for managed: if OPENSEARCH_INSTALLED_BY_US=yes, continue
- [x] 4.4 Handle interrupted install: if snap present but state missing, exit with recovery procedure (stop, remove, rerun)

## 5. Setup: OpenSearch Bootstrap

- [x] 5.1 Implement security state check using authenticated health check
- [x] 5.2 Implement network binding in the shared bootstrap behind a caller-supplied `--bind`: set network.host, http.host and transport.host, drop conflicting `*.bind_host` overrides, run after opensearch.setup and before the daemon starts on fresh installs and reruns, restart only on change and only after both the heap and the binding are written, replace the block list opensearch.setup writes for network.host (refusing only a bare nested mapping), and verify the node's bound addresses for both http and transport before reporting success
- [x] 5.3 Implement TLS setup calling shared bootstrap functions with generated passwords
- [x] 5.4 Implement admin hash calling shared bootstrap functions
- [x] 5.5 Implement heap configuration: validate the `--heap` argument when parsing (status 2 for invalid, empty or missing values, without consuming a following option), write to heap.options before starting the daemon (works even if OpenSearch unreachable), restart daemon if already running and heap changed
- [x] 5.6 Implement sysctl: read the effective vm.max_map_count, raise to 262144 only if lower (never reduce), fail with the host remedy if the write is rejected or reads back too low, persist to /etc/sysctl.d/60-rag-local-opensearch.conf only if the file does not exist, record SYSCTL_FILE_CREATED=yes and SYSCTL_FILE_CONTENT via save_env when creating it, rely on the shared bootstrap to disable and verify the snap's set-sysctl-props handler, leave swappiness/tcp_retries2 unchanged
- [x] 5.7 Implement daemon start and security-init calling shared functions, await authenticated ready with bounded wait
- [x] 5.8 Save OPENSEARCH_SETUP_COMPLETED via save_env after bootstrap
- [x] 5.9 Preserve valid certificates when resuming: check before regenerating

## 6. Setup: rag-cli Configuration

- [x] 6.1 Write credentials.json through the shared write_credentials_json: OPENSEARCH_USERNAME, OPENSEARCH_PASSWORD and optional CHAT_API_KEY, mode 0600. Local merges into the existing object (keeping unrelated keys and an existing key); AWS replaces it after confirming over different credentials
- [x] 6.2 Set the endpoint and service keys through the shared configure_rag_cli(host): knowledge.http.*, tika.http.*, chat.http.*, verified via rag_set
- [x] 6.3 Start Tika through the shared start_tika_and_wait and fail the run when it cannot be started or does not become ready

## 7. Setup: Knowledge Init

- [x] 7.1 Knowledge init: verify the two configured models' deployment state before running init (DEPLOYED reused, 404 or another state triggers init, an authentication/network/API failure stops setup with an actionable error). A saved timestamp alone is not readiness; save IDs via rag_set and save_env
- [x] 7.2 If models exist REGISTERED but not DEPLOYED, CLI's registerAndDeploy deploys them
- [x] 7.3 Drive import through the shared drive_import_folder: masked output, the command's own exit status reported without claiming per-archive success, attempt recorded; AWS keeps its fatal-on-failure policy, local reports and continues

## 8. Start Command

- [x] 8.1 Implement mode_start(): verify OPENSEARCH_INSTALLED_BY_US=yes, run `sudo snap start --enable opensearch.daemon` and `sudo snap start --enable ${RAG_SNAP_INSTANCE}.tika-server`, wait for ready, report status
- [x] 8.2 Verify start performs NO setup/init/import, only service management
- [x] 8.3 Test boot survival: start, reboot, verify auto-start

## 9. Stop Command

- [x] 9.1 Implement mode_stop(): verify managed, run `sudo snap stop --disable opensearch.daemon` and `sudo snap stop --disable ${RAG_SNAP_INSTANCE}.tika-server`
- [x] 9.2 Test reboot after stop: stop, reboot, verify remain inactive

## 10. Destroy Command

- [x] 10.1 Implement mode_destroy(): check OPENSEARCH_INSTALLED_BY_US before any service changes, if no/absent exit with error without modifying services or files, otherwise prompt confirmation, on yes proceed else exit unchanged
- [x] 10.2 Stop and disable both services after ownership check and confirmation: opensearch.daemon and ${RAG_SNAP_INSTANCE}.tika-server
- [x] 10.3 Implement conditional removal: if OPENSEARCH_INSTALLED_BY_US=yes run `sudo snap remove opensearch --purge`, clear OPENSEARCH_INSTALLED_BY_US via save_env after successful removal
- [x] 10.4 Implement credentials.json cleanup: remove OPENSEARCH_USERNAME/PASSWORD only if PASSWORD matches generated value, preserve CHAT_API_KEY and others
- [x] 10.5 Implement state clearing via save_env: remove deployment secrets (TLS_*_PASS, OPENSEARCH_ADMIN_PASSWORD) from secrets.env, remove obsolete markers (OPENSEARCH_SETUP_COMPLETED, KNOWLEDGE_INIT_COMPLETED, model IDs, DRIVE_IMPORT_LAST_ATTEMPT, SYSCTL_FILE_CREATED, OPENSEARCH_INSTALLED_BY_US) from state.env
- [x] 10.6 Implement credential preservation: preserve settings.env, CHAT_API_KEY, GOOGLE_DRIVE_CLIENT_* in secrets.env, OAuth token cache
- [x] 10.7 Implement sysctl cleanup: remove /etc/sysctl.d/60-rag-local-opensearch.conf only if SYSCTL_FILE_CREATED=yes AND content matches setup-created value, clear SYSCTL_FILE_CREATED, preserve pre-existing file even if content matches
- [x] 10.8 Handle partial failures: if remove fails retain state for retry, if succeeds but cleanup interrupted next destroy clears stale state (OPENSEARCH_INSTALLED_BY_US, SYSCTL_FILE_CREATED, completion markers) without re-removal
- [x] 10.9 Test destroy/redeploy: destroy with confirmation, setup, verify fresh cluster

## 11. Integration with prepare-script

- [x] 11.1 Update `cmd/cli/config/prepare_script.go` to accept `local` backend
- [x] 11.2 Update `internal/awssetup/awssetup.go` to embed local-setup.sh, common.sh, bootstrap-opensearch.sh via `//go:embed`
- [x] 11.3 Implement WriteLocal() to export all assets with snap-context.env
- [x] 11.4 Wire prepare_script.go to call WriteLocal() when backend is local
- [x] 11.5 Test from installed snap: export, verify all files present

## 12. Documentation

- [x] 12.1 Create `docs/local-setup.md` documenting prerequisites, commands, configuration, troubleshooting
- [x] 12.2 Update `INSTALL.md` to cross-reference local-setup.md
- [x] 12.3 Update prepare-script help to show local backend

## 13. Testing

- [x] 13.1 Unit tests for common.sh functions
- [x] 13.2 Test export from installed snap
- [x] 13.3 Test fresh setup default heap (6 GiB)
- [x] 13.4 Test fresh setup custom heap
- [x] 13.5 Test heap edit recovery: oversized heap prevents startup, lower value in settings, rerun setup, verify recovery without requiring OpenSearch reachable
- [x] 13.6 Test safe reruns with actual state verification
- [ ] 13.7 Test interruption recovery: kill after TLS, rerun, verify preserves certificates
- [x] 13.8 Test unmanaged refusal
- [x] 13.9 Test loopback binding: missing configuration, an existing non-loopback value with conflicting http/transport overrides, opensearch.setup rewriting network.host, a managed rerun correcting and restarting, an unchanged rerun not restarting, verification rejecting wildcard and non-loopback listeners, and AWS binding unchanged
- [x] 13.10 Test sysctl: values below, equal to and above the minimum, a rejected write and a write that reads back too low, the snap handler failing to set or verify, an existing config file preserved and not claimed, recorded ownership surviving a rerun, cleanup removing only an owned unchanged file, and swappiness/tcp_retries2 untouched locally
- [x] 13.11 Test stop survives reboot: stop, reboot, verify inactive, start, verify active
- [x] 13.12 Test destroy: verify ownership checked before service changes, unmanaged refused without changes, services stopped/disabled after check, OpenSearch removed with --purge, credentials.json preserves non-OpenSearch, secrets cleared, ownership cleared, sysctl file removed only if we created AND matches
- [x] 13.13 Test destroy cancellation: verify no changes
- [x] 13.14 Test redeploy: destroy, setup, verify fresh cluster with reusable credentials
- [x] 13.15 Consolidated AWS regression: single end-to-end campaign after extraction integrated

## 14. Validation

- [x] 14.1 Run `openspec validate automate-local-setup`
- [x] 14.2 Review all documents for internal consistency
- [ ] 14.3 Run `make all` and verify build/lint/test pass
