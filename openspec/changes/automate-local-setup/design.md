# Design

## Context

The AWS automation (`opensearch-on-aws.sh`, ~664 lines with `bootstrap-opensearch.sh`, ~341 lines) provisions remote OpenSearch via Terraform and SSH bootstrap. Local automation targets the same outcome—a working rag-cli with configured OpenSearch, inference endpoint, and knowledge base—but with OpenSearch running on localhost.

Current AWS implementation provides patterns for data-only `.env` format, secret generation and masking, bounded waits, rag-cli configuration with verification, knowledge init with model ID extraction, and Google Drive import. The bootstrap script (`bootstrap-opensearch.sh`) provides TLS setup, admin password hashing, security initialization, and awaiting cluster readiness. Local reuses these patterns and shared bootstrap implementation.

**Constraints:**
- Script runs outside snap confinement (needs `sudo snap install`)
- Must run as regular user with `sudo` for system operations
- Must integrate with snapctl-based config layer
- credentials.json must be owned by current user for CLI access
- All configuration is read/written via snapctl (not files)

## Goals / Non-Goals

**Goals:**
- Local setup automation matching AWS UX
- Code reuse via shared implementation
- Idempotent operations with actual state verification
- Safe destroy (remove only managed resources, clear ownership after removal)
- Heap configuration: 6 GiB default, validate and apply edited values
- Loopback binding for OpenSearch  
- Local sysctl: vm.max_map_count minimum 262144, never lower existing higher values
- Refuse to modify unmanaged OpenSearch
- Start/stop with boot behavior control

**Non-Goals:**
- Docker or container deployments
- Inference server installation
- Ubuntu release restrictions
- Auto-updates
- Integration with unmanaged clusters

## Decisions

### Decision 1: Extract shared implementation

**Chosen:** Create `common.sh` with functions shared between local and AWS. Both scripts source this file. Local and AWS call the same bootstrap implementation in `bootstrap-opensearch.sh`, with local passing explicit parameters for differences.

**Functions extracted to common.sh:**
- `mask()` - secret masking in output
- `load_env()`, `save_env()` - settings/secrets/state I/O with state.env support
- `gen_secret()` - 32-char alphanumeric generation
- `rag_set()` - config with verification
- `knowledge_init_and_extract()` - init + model ID extraction

**Shared bootstrap (bootstrap-opensearch.sh):**
Bootstrap handles TLS setup, admin hash, heap configuration, security-init, and bounded waits. Local calls it with explicit parameters:
- `--heap` : Local passes OPENSEARCH_HEAP_GIB (default 6); AWS omits this, triggering auto-calculation
- `--skip-sysctl` : Local manages sysctl independently
- `--bind` : Local passes 127.0.0.1 to limit REST and transport to loopback; AWS omits this, leaving remote access as it is
- `--node-name` : Node identifier
- `--channel` : Snap channel
- `--secrets-file` : Admin password and TLS passphrases

**Asset distribution:**
- Local export: `local-setup.sh`, `common.sh`, `bootstrap-opensearch.sh`, `snap-context.env`
- AWS remote: `bootstrap-opensearch.sh`, `common.sh` copied to remote host

**Rationale:** Eliminates duplication and keeps the local and AWS paths consistent.

### Decision 2: State tracking

**Chosen:** Track installation ownership (clear after successful removal), completion markers (verify actual state before skipping), and model IDs.

**Format:** Data-only like settings/secrets.env, same load_env/save_env parser.

**State tracked:**
```bash
OPENSEARCH_INSTALLED_BY_US="yes"  # Cleared after successful snap removal
OPENSEARCH_CHANNEL_INSTALLED="2/stable"
OPENSEARCH_SETUP_COMPLETED="2026-10-09T12:34:56Z"  # Plus actual state checks
RAG_CLI_CONFIGURED="2026-10-09T12:35:10Z"
KNOWLEDGE_INIT_COMPLETED="2026-10-09T12:35:30Z"  # Plus model deployment checks
EMBEDDING_MODEL_ID="..."
RERANK_MODEL_ID="..."
SYSCTL_FILE_CREATED="yes"  # Records /etc/sysctl.d/60-rag-local-opensearch.conf ownership
```

**Actual state verification (not just timestamps):**
- Bootstrap: Check security initialized via authentication test, certificates present
- Knowledge: Check models deployed in OpenSearch (query /_plugins/_ml/models/{id})
- If timestamp present but actual state missing: reapply config, redeploy models as needed

**Rationale:** Enables safe destroy (conditional removal), supports robust interruption recovery, prevents lowering requirements by clearing ownership after removal.

### Decision 3: OpenSearch management

**Chosen:** Install if missing and record ownership. Refuse if present without ownership marker. Clear ownership after successful removal.

**Unmanaged installation refusal:**
If snap present with state.env missing or OPENSEARCH_INSTALLED_BY_US=no/absent, exit with:
```
ERROR: OpenSearch snap is installed but not managed by this setup.
This script will not modify an unmanaged OpenSearch installation.

Options:
1. Use a different machine for local rag-cli deployment
2. Remove OpenSearch: sudo snap remove opensearch --purge
3. Configure rag-cli manually (see INSTALL.md)
```

**Interrupted installation recovery:**
If interrupted after snap install but before state recorded:
```
ERROR: OpenSearch snap is installed but ownership unknown.

To recover, verify the installation is yours and stop any running daemon:
  sudo snap stop --disable opensearch.daemon

Then remove it and rerun setup:
  sudo snap remove opensearch --purge
  ./local-setup.sh
```

No manual state editing. User removes the orphaned installation and reruns setup cleanly.

**After successful destroy:**
Clear OPENSEARCH_INSTALLED_BY_US so a subsequent manual installation is not treated as owned.

**Rationale:** opensearch.setup resets security on unmanaged clusters. User may have existing OpenSearch for other purposes. No silent adoption of uncertain installations.

### Decision 4: OpenSearch bootstrap using shared implementation

**Chosen:** Call shared bootstrap from `bootstrap-opensearch.sh`, passing explicit local parameters.

**Shared operations:**
- TLS certificate generation
- Admin password hashing
- Security initialization
- Bounded waits (tested values from bootstrap)
- Certificate presence check

**Local-specific parameters:**

**Heap:** Local passes `--heap ${OPENSEARCH_HEAP_GIB}` (default 6) to bootstrap. Bootstrap applies the specified value, writing both Xms and Xmx to match. When no `--heap` parameter is provided (AWS case), bootstrap auto-calculates from RAM using the existing 3/8 formula (rounded, bounded 1-31 GiB). Validation happens at two boundaries, each before anything is mutated: the local script gates the effective setting, entered or saved, before it generates secrets, installs or configures OpenSearch, changes sysctl settings or starts services; bootstrap rejects an invalid, empty or missing `--heap` value with status 2 as it parses arguments, before it reads the secrets file. The writer itself stays single, shared and applied before the daemon is started, so heap changes on rerun restart the daemon only if it is already active, and recovery from an oversized heap that prevented startup needs no OpenSearch HTTP response.

**Network binding:** The shared bootstrap writes `network.host`, `http.host` and `transport.host` as 127.0.0.1 into `/var/snap/opensearch/current/etc/opensearch/opensearch.yml`, removing any `*.bind_host` override that would win over them, and preserving the rest of the file with its ownership and mode. This runs after `opensearch.setup`, which rewrites `network.host` itself and defaults it to `[_local_, _site_]`, and before the daemon is started, on fresh installs and on reruns alike. An unchanged configuration is left alone. The heap and the binding are both written before any restart, so a running daemon is restarted once and never comes back up on the superseded binding. The block list `opensearch.setup` writes for `network.host` is replaced together with its items; a bare `network`/`http`/`transport` mapping, which would need a YAML parser, is refused instead. Readiness then checks the node's own `http.bound_address` and `transport.bound_address`, requiring a non-empty list for each and checking every entry, since a working localhost request does not prove other interfaces are excluded and one list alone proves nothing. Parsing uses jq, which the local caller's preflight already requires.

**Sysctl policy:** Set `vm.max_map_count` to at least 262144 without ever lowering existing higher values, and fail with the host remedy when the effective value cannot be reached. The snap's own handler is disabled and verified by the shared bootstrap before the daemon starts, which is what keeps vm.swappiness and net.ipv4.tcp_retries2 untouched locally. The created file is recorded in `state.env` (`SYSCTL_FILE_CREATED`,
`SYSCTL_FILE_CONTENT`) so cleanup can tell it from a file the administrator owns.
An existing `/etc/sysctl.d/60-rag-local-opensearch.conf` is never overwritten.
Leave vm.swappiness and net.ipv4.tcp_retries2 unchanged.

**Sysctl cleanup on destroy:** Remove `/etc/sysctl.d/60-rag-local-opensearch.conf`
only if setup recorded creating it and the content is still what it wrote; an edited
or unowned file is kept, with the reason printed. The runtime value is never restored.

**Passphrase interface:** CLI arguments `--tls-priv-key-*-pass` per bootstrap-opensearch.sh do_tls function. Arguments visible in process list (unavoidable).

**Rationale:** Bootstrap-opensearch.sh is reused unchanged where possible; local adaptations are explicit and minimal. Heap validation independent of OpenSearch state enables recovery. Sysctl operations never lower existing values, respect administrator config.

### Decision 5: Service commands with boot behavior

**Chosen:**
- **start**: `sudo snap start --enable opensearch.daemon` and `sudo snap start --enable ${RAG_SNAP_INSTANCE}.tika-server`
- **stop**: `sudo snap stop --disable opensearch.daemon` and `sudo snap stop --disable ${RAG_SNAP_INSTANCE}.tika-server`

Use exported RAG_SNAP_INSTANCE variable, not hardcoded "rag-cli".

**Start behavior:**
- Start and enable services for boot startup
- Verify ready (authenticated OpenSearch, Tika response)
- NO setup work: does not run opensearch.setup, register models, or import knowledge
- Refuses unmanaged installations

**Stop behavior:**
- Stop and disable services from boot startup
- Data/config retained

**Rationale:** Single --enable/--disable operation. Boot behavior explicit. Using snap instance variable supports non-default snap names. Start only manages services—clear separation from setup.

### Decision 6: Destroy with credential preservation

**Chosen:** Check ownership first, confirm, stop services, remove managed OpenSearch with `--purge`, clear ownership after removal, preserve reusable credentials.

**Confirmation:**
```
This will stop services and remove the managed OpenSearch installation. Continue? [y/N]:
```
On no/decline: make no changes. On yes: proceed.

**Operations:**
1. **Check ownership before any service changes:** Verify OPENSEARCH_INSTALLED_BY_US=yes in state.env. If no/absent, report "OpenSearch not installed by this setup" and exit without modifying services, configuration, credentials, or files.
2. After confirmation: Stop and disable services: `sudo snap stop --disable opensearch.daemon` and `sudo snap stop --disable ${RAG_SNAP_INSTANCE}.tika-server`
3. Remove managed OpenSearch: `sudo snap remove opensearch --purge` (no automatic snapshot)
4. Clean up deployment-specific OpenSearch credentials from credentials.json: remove
   `OPENSEARCH_USERNAME` and `OPENSEARCH_PASSWORD` only when the stored password matches
   the generated one; keep `CHAT_API_KEY` and every other key.
5. Clean up the sysctl file: remove it only if setup created it and the content is unchanged
6. Remove deployment secrets from secrets.env: TLS_ROOT_PASS, TLS_ADMIN_PASS, TLS_NODE_PASS, OPENSEARCH_ADMIN_PASSWORD
7. Remove obsolete state from state.env: OPENSEARCH_SETUP_COMPLETED, KNOWLEDGE_INIT_COMPLETED, EMBEDDING_MODEL_ID, RERANK_MODEL_ID, DRIVE_IMPORT_LAST_ATTEMPT, OPENSEARCH_INSTALLED_BY_US (after successful removal)

**Preserved:**
- All of settings.env
- In secrets.env: CHAT_API_KEY, GOOGLE_DRIVE_CLIENT_ID, GOOGLE_DRIVE_CLIENT_SECRET
- OAuth token at `$SNAP_USER_DATA/gdrive-token.json` or `~/.config/rag-cli/gdrive-token.json`
- credentials.json non-OpenSearch entries
- rag-cli config keys

**Partial failure:**
If `snap remove` fails: report error, retain all state including OPENSEARCH_INSTALLED_BY_US=yes for retry.
If removal succeeds but cleanup interrupted: next destroy detects OpenSearch absent, clears stale state (including OPENSEARCH_INSTALLED_BY_US, SYSCTL_FILE_CREATED, completion markers) gracefully without attempting re-removal.

**Rationale:** Checking ownership before service changes prevents modifying unmanaged installations. Confirmation prevents accidental data loss. --purge avoids snapshot clutter. Clearing ownership after removal prevents treating later manual install as owned. Deployment secrets cleared for fresh cluster. Reusable credentials preserved. Credentials.json cleanup removes only this deployment's OpenSearch password. Sysctl cleanup only when we created the file and it's unchanged.

### Decision 7: Inference endpoint flexibility

**Chosen:** Accept any OpenAI-compatible endpoint (host/port/path/TLS/model) with optional API key. Do not install inference server.

**Rationale:** Users may have existing endpoints. Installing inference server is a separate concern.

### Decision 8: Google Drive import reuse

**Chosen:** Reuse AWS headless guidance, client ID not masked in URL, retry decision, CLI's token cache.

**Token cache paths:**
- Snap context: `$SNAP_USER_DATA/gdrive-token.json`
- Outside snap: `~/.config/rag-cli/gdrive-token.json`

CLI manages OAuth lifecycle.

**Rationale:** AWS behavior tested. CLI token handling proven.

### Decision 9: credentials.json format

**Chosen:** Key is OPENSEARCH_PASSWORD (not OPENSEARCH_ADMIN_PASSWORD), populated from OPENSEARCH_ADMIN_PASSWORD setup value.

**Creation:** Both deployments call the shared `write_credentials_json` helper. It
writes `OPENSEARCH_USERNAME`, `OPENSEARCH_PASSWORD` and, when a value is supplied,
`CHAT_API_KEY`, atomically and mode 0600, layered on a base object: local passes the
current file contents so unrelated keys and an existing API key are kept, AWS passes
`{}` so the file is replaced after its confirmation.

**Rationale:** One writer, with the per-deployment policy explicit at the caller.

### Decision 10: Model recovery

**Chosen:** Distinguish existing deployed, existing undeployed, genuinely absent, and API failures.

The CLI's `registerAndDeploySentenceTransformer` and `registerAndDeployCrossEncoder` functions (models.go) already implement:
- Model exists and DEPLOYED → reuse existing model ID
- Model exists and REGISTERED → deploy the existing model, wait for DEPLOYED
- Model genuinely absent → register new model, wait REGISTERED, deploy, wait DEPLOYED

Setup calls `rag-cli.rag knowledge init`, which invokes these functions. No additional recovery framework needed. If auth/network/API failure occurs, the CLI reports the failure; setup propagates the error without interpreting it as "model missing."

State verification: the local caller checks the two configured model IDs before
deciding whether to run `knowledge init`. A model whose `model_state` is `DEPLOYED` is
reused; a missing or undeployed model (HTTP 404 or another state) triggers
`knowledge init`, which redeploys registered models; an authentication, network or
other API failure stops setup with an actionable message instead of being read as a
missing model. A saved `KNOWLEDGE_INIT_COMPLETED` timestamp alone does not establish
readiness, and re-initialising with healthy models would register duplicates.

**Rationale:** Reuses existing CLI model handling and bootstrap HTTP helper. Distinguishes successful DEPLOYED response, missing model, and request failures. Narrow recovery without general reconciliation framework.

## Risks / Trade-offs

**Risk:** User modifies unmanaged OpenSearch  
**Mitigation:** Clear refusal with remediation options

**Risk:** Interruption between snap install and state recording  
**Mitigation:** Recovery procedure removes orphaned installation, reruns setup cleanly

**Risk:** Sysctl cleanup overwrites admin changes  
**Mitigation:** Remove file only if content matches setup-created value

**Risk:** Shared extraction breaks AWS  
**Mitigation:** Extract incrementally, test AWS end-to-end after integration

**Risk:** Heap recovery blocked by unreachable OpenSearch  
**Mitigation:** Heap validation and application independent of cluster state

## Migration Plan

**Phase 1: Shared extraction**
1. Create common.sh with extracted functions
2. Refactor opensearch-on-aws.sh to source common.sh
3. Test AWS end-to-end
4. Verify identical behavior

**Phase 2: Local structure**
1. Create local-setup.sh with mode routing
2. Sudo rejection, source common.sh and bootstrap-opensearch.sh
3. Usage function

**Phase 3: Setup**
1. Preflight, settings collection
2. OpenSearch installation with ownership tracking
3. Bootstrap using shared implementation with local parameters
4. rag-cli configuration using shared rag_set
5. Knowledge init using CLI's existing model handling
6. Drive import using shared implementation

**Phase 4: Operations**
1. Start with --enable, verify ready
2. Stop with --disable
3. Destroy with --purge, clear ownership, preserve reusable

**Phase 5: Integration**
1. prepare-script local backend
2. Asset export: local-setup.sh, common.sh, bootstrap-opensearch.sh, snap-context.env
3. Ensure common.sh reaches AWS remote

**Phase 6: Documentation and testing**
1. docs/local-setup.md with technical details
2. INSTALL.md cross-reference
3. Integration tests
4. AWS regression

**Validation:** AWS unchanged, local completes, start/stop survive reboot, destroy/redeploy succeeds, loopback verified, sysctl never lowers, heap recovery from oversized works, all spec scenarios pass.
