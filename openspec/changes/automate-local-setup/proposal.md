# Proposal

## Why

Users who want to run rag-cli locally currently face a 6-step manual setup process (install snap → configure 9+ config keys → inject secrets → start services → knowledge init → use). The AWS automation provides a one-command experience (`./opensearch-on-aws.sh setup`) that provisions infrastructure, configures everything, and leaves users with a working system. Local deployments — where OpenSearch runs on the same machine as rag-cli — should provide the same automation.

## What Changes

Add `local` backend to `rag-cli.rag prepare-script`, export a complete `local-setup.sh` orchestration script supporting setup/start/stop/destroy operations, and extract shared implementation from the AWS script for use by both local and remote deployments.

**User flow:**
```bash
# Pre-requisite: rag-cli snap installed  
rag-cli.rag prepare-script local --output ~/rag-local
cd ~/rag-local
./local-setup.sh              # Interactive setup

# After reboot or manual stop
./local-setup.sh start        # Restore services (enables boot startup)
./local-setup.sh stop         # Temporary stop (disables boot startup)

# Clean up for fresh deployment
./local-setup.sh destroy      # Confirm, remove managed resources
./local-setup.sh              # Redeploy with fresh cluster
```

**End state after setup:**
- OpenSearch running on localhost:9200 (loopback-bound, TLS-enabled)
- Inference endpoint configured (user-provided OpenAI-compatible API with optional API key)
- Tika running on localhost:9998
- rag-cli configured and ready
- Knowledge base initialized with pipelines/models
- Ready for CLI use (`rag-cli.rag chat`)
- ragd NOT started (user enables manually when needed)

**Shared implementation:**
Extract reusable functions from `opensearch-on-aws.sh` into `common.sh` (settings/secrets/state I/O, secret generation/masking, rag-cli configuration with verification, knowledge init with model ID extraction). Both local and AWS scripts call the same bootstrap implementation in `bootstrap-opensearch.sh` for TLS setup, admin password hashing, security initialization, and bounded readiness waits. Local passes explicit parameters for heap configuration (6 GiB default from settings, not RAM-based), network binding (127.0.0.1), and sysctl policy (vm.max_map_count minimum 262144 only).

**State tracking:**
- `OPENSEARCH_INSTALLED_BY_US` tracks installation ownership; cleared after successful removal
- Completion markers with actual state verification (security initialized, certificates present, models deployed)
- Model IDs persisted for configuration
- Safe reruns skip completed phases after verification; recover from interruptions

**Operations:**
- **setup**: Install and configure OpenSearch, initialize knowledge base, enable services for boot startup
- **start**: Start and enable OpenSearch + Tika at boot (verify ready, no setup work)
- **stop**: Stop and disable OpenSearch + Tika at boot
- **destroy**: Confirm, stop services, remove managed OpenSearch with `--purge`, clear deployment secrets and obsolete state, preserve reusable credentials (inference API keys, saved answers)

## Capabilities

### New Capabilities
- `local-setup-automation`: Automated provisioning and configuration of local rag-cli environment with OpenSearch snap and generic inference endpoint. Covers setup/start/stop/destroy operations, shared implementation extraction, state tracking, and idempotent reruns.

### Modified Capabilities
None—this is net-new automation that does not modify existing rag-cli requirements.

## Impact

**Code affected:**
- `internal/awssetup/assets/opensearch-on-aws.sh` - Refactored to extract shared implementation
- `internal/awssetup/assets/bootstrap-opensearch.sh` - Shared by local and AWS (local passes different parameters)
- `internal/awssetup/awssetup.go` - Extended to export local assets including common.sh and bootstrap-opensearch.sh
- `cmd/cli/config/prepare_script.go` - Add "local" backend option

**New files:**
- `internal/awssetup/assets/local-setup.sh` (local orchestration script)
- `internal/awssetup/assets/common.sh` (shared settings/secrets/configuration functions)
- `docs/local-setup.md` (user documentation)

**External services:**
- OpenSearch snap: installation via `snap install`, bootstrap via `opensearch.setup`, heap/network/sysctl configuration, loopback binding
- Inference server: configuration only (no installation)
- Tika: service startup via `snap start --enable`

**Config keys set (package layer):**
- `knowledge.http.{host=127.0.0.1,port=9200,tls=true}`
- `chat.http.{host,port,path,tls}`, `chat.model` (from settings.env)
- `tika.http.{host=127.0.0.1,port=9998,path=tika}`
- `knowledge.model.{embedding,rerank}` (after init)

**Secrets managed:**
- `${RAG_SNAP_USER_COMMON}/credentials.json`: OPENSEARCH_USERNAME, OPENSEARCH_PASSWORD (from generated OPENSEARCH_ADMIN_PASSWORD), optional CHAT_API_KEY
- `secrets.env`: Generated TLS passphrases and OPENSEARCH_ADMIN_PASSWORD; user-provided CHAT_API_KEY, Google OAuth client credentials (preserved across destroy/redeploy)
- OAuth token cache: `$SNAP_USER_DATA/gdrive-token.json` (snap) or `~/.config/rag-cli/gdrive-token.json` (preserved)

**User-facing changes:**
- New command option: `rag-cli.rag prepare-script local --output <dir>`
- New exported script with setup/start/stop/destroy operations
- Documentation: `docs/local-setup.md` added, `INSTALL.md` cross-reference

**Testing:**
- Shared extraction with AWS regression validation
- Local integration: export, fresh setup (default/custom heap), saved answers, safe reruns, interruption recovery, unmanaged refusal, loopback binding, sysctl policy, start/stop across reboot, destroy/redeploy
