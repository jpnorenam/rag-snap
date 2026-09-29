# Proposal

## Why

Standing up rag-cli against a real OpenSearch cluster today means following INSTALL.md by hand:
provisioning a machine, bootstrapping the OpenSearch snap (sysctl, certificates, heap, security
initialization), then setting a dozen config keys and exporting secrets in every new shell. The
steps are error-prone (the documented path leaves `admin:admin` as the REST credential) and
require knowledge of both snaps' internals. Users who have installed rag-cli should be able to get
a dedicated, secured OpenSearch backend on AWS and a working local configuration from one guided
script, and remove it again, without cloning this repository.

## What Changes

- New CLI command `rag-cli.rag prepare-script aws --output <directory>` that writes the bundled
  setup assets (one host script, one guest bootstrap script, Terraform templates) plus a small
  snap-context file into a user-owned deployment directory. The command does no provisioning.
- New exported host script (`opensearch-on-aws.sh setup|destroy`) that runs **outside** snap confinement and
  uses the user's own AWS CLI, Terraform, SSH and `sudo`:
  - verifies the AWS profile with `aws sts get-caller-identity` before asking anything else;
  - collects and saves answers in an editable env file that is parsed as data, never sourced;
  - generates and saves the OpenSearch admin password and TLS key passphrases before any remote
    change, and an SSH key pair once;
  - provisions a dedicated VPC, subnet, internet gateway, security group (SSH and 9200 limited to
    the user's public IPv4 /32), key pair and an encrypted-gp3 EC2 instance (root volume size
    from the saved answers, default 50 GiB) from the official Canonical Ubuntu 24.04 (default) or
    26.04 amd64 AMI, resolved once and saved so later runs keep the same image, via a saved,
    reviewed Terraform plan;
  - runs the guest bootstrap over SSH, then configures the local rag-cli (OpenSearch, bundled Tika,
    an existing inference endpoint), initializes models/pipelines, persists the model IDs, and
    optionally imports Google Drive knowledge-base archives with the existing import command;
  - `destroy` removes every AWS resource the deployment created and then the deployment's local
    secrets and key material.
- New guest bootstrap script that configures sysctl on the guest OS, installs OpenSearch from
  `2/stable`, runs `opensearch.setup` with roles `cluster_manager,data,ingest,ml`, sizes the heap,
  replaces the `admin` bcrypt hash in `internal_users.yml` using the packaged security-plugin
  `hash.sh`, starts the daemon, runs `opensearch.security-init`, and verifies authenticated
  readiness. It is resumable and never re-runs setup or security initialization on an
  initialized node.
- CLI credential fallback: when `OPENSEARCH_USERNAME`, `OPENSEARCH_PASSWORD` or `CHAT_API_KEY` is
  not set in the environment, the `rag-cli.rag` CLI reads it from a mode-0600 JSON file at
  `$SNAP_USER_COMMON/credentials.json`. Environment values keep precedence (including explicitly
  empty ones), the file is read only when a needed credential is not in the environment, a missing
  file keeps today's behavior, and the `ragd` daemon's environment-only secret contract is unchanged.
- Documentation: new `docs/opensearch-on-aws.md`, links from INSTALL.md and README.md, and a
  `docs/usage.md` section for `prepare-script aws` and the credentials file.

## Capabilities

### New Capabilities
- `aws-setup-assets`: the `rag-cli.rag prepare-script aws` command and the contents, permissions,
  and rerun behavior of the exported deployment directory.
- `aws-rag-deployment`: the host script's setup and destroy behavior — prerequisites, AWS
  authentication, saved answers and secrets, Terraform-managed resources, local rag-cli
  configuration, optional Google Drive import, and cleanup.
- `opensearch-bootstrap`: the guest bootstrap contract for the OpenSearch snap — host settings,
  install, TLS setup, roles, heap, admin password, security initialization, readiness, and rerun
  semantics.
- `cli-credentials-file`: the CLI's fallback from environment variables to
  `$SNAP_USER_COMMON/credentials.json` for OpenSearch and chat secrets.

### Modified Capabilities
None. `ragd`'s secret handling (`rest-api-config`, `rest-api-server`) is deliberately unchanged.

## Impact

- **External services touched:** OpenSearch (provisioned remotely, then configured and
  initialized through existing `knowledge init`), the inference server (existing endpoint is
  configured, never provisioned), and Tika (bundled service is configured and started). No change
  to how any client talks to these services beyond where the CLI finds its secrets.
- **Config keys:** no new keys. The script sets existing **package**-scoped keys with
  `sudo rag-cli.rag set --package <key>=<value>` (one per call): `knowledge.http.{host,port,tls}`,
  `tika.http.{host,port,path}`, `chat.http.{host,port,path,tls}`, `chat.model`,
  `knowledge.model.{embedding,rerank}`. Package scope is required because the install hook does
  not seed these keys, and user-scope `set` rejects unknown keys.
- **Secrets:** still never stored in snapctl config. New CLI-only file
  `$SNAP_USER_COMMON/credentials.json` (0600, invoking user). Deployment secrets live in the
  deployment directory (0600).
- **User-facing surfaces:** new `prepare-script` command with an `aws` subcommand, and exported `opensearch-on-aws.sh`
  (`setup`, `destroy`). Docs to change: `docs/opensearch-on-aws.md` (new), `INSTALL.md`, `README.md`,
  `docs/usage.md`; command `--help` text. `apps/completion.bash` delegates to Cobra and needs no
  edit.
- **Code:** `cmd/cli/main.go` (register command, enable credential fallback), new
  `cmd/cli/config/prepare_script.go`, new `internal/awssetup/` (embedded assets), new
  `pkg/credentials/`,
  `cmd/cli/basic/knowledge/client.go` and `cmd/cli/basic/chat/{client,turn,batch,refine}.go`
  (credential lookup).
- **snapcraft.yaml:** no new plugs, parts, bundled binaries or hooks. Terraform, its providers and
  the AWS CLI are not bundled.
- **User dependencies:** Terraform (required, user-installed), AWS CLI (reused, or the `aws-cli`
  snap installed with `--classic` on consent), OpenSSH client, `curl`, `sudo`.
