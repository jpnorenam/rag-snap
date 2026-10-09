# Spec Delta

## Purpose

Lets a user who installed the rag-cli snap export the bundled AWS setup assets into a deployment
directory they own, without a repository checkout and without the confined CLI provisioning
anything itself.

## ADDED Requirements

### Requirement: Export command writes the bundled setup assets
The CLI SHALL provide `rag-cli.rag prepare-script aws --output <directory>`, which writes into the
directory: an executable host script `opensearch-on-aws.sh`, a guest script `bootstrap-opensearch.sh`, the
Terraform templates under `terraform/`, and `snap-context.env`. The command SHALL NOT contact AWS,
run Terraform, SSH, or `sudo`, or change rag-cli configuration. `--output` SHALL be required.

#### Scenario: Export into a new directory
- **WHEN** a non-root user runs `rag-cli.rag prepare-script aws --output ~/rag-aws` and the
  directory does not exist
- **THEN** the directory is created with mode 0700, contains `opensearch-on-aws.sh` (mode 0700),
  `bootstrap-opensearch.sh` and every Terraform template (mode 0600), and `snap-context.env`
  (mode 0600), and the command prints the next step `cd ~/rag-aws && ./opensearch-on-aws.sh setup`

#### Scenario: Missing output flag
- **WHEN** the user runs `rag-cli.rag prepare-script aws` without `--output`
- **THEN** the command fails with a usage error and writes nothing

### Requirement: Snap context for the host script
`snap-context.env` SHALL record the snap instance name and the value of `SNAP_USER_COMMON` seen by
the CLI for the invoking user, as `KEY=value` lines, so the unconfined host script can locate the
snap's per-user common directory without relying on the snap's overridden `HOME`.

#### Scenario: Context file contents
- **WHEN** user `alice` with home `/home/alice` exports assets from the `rag-cli` snap
- **THEN** `snap-context.env` contains `RAG_SNAP_INSTANCE=rag-cli` and
  `RAG_SNAP_USER_COMMON=/home/alice/snap/rag-cli/common`

### Requirement: Safe output locations and ownership
The command SHALL refuse to run as root. It SHALL refuse an existing output directory that is not
owned by the invoking user or is writable by group or others. It SHALL reject output paths under
`/tmp` or `/var/tmp`, explaining that they are private to the snap and asking for a non-hidden
directory under the user's home.

#### Scenario: Run with sudo
- **WHEN** the command is run as root
- **THEN** it fails with a message to run it as the normal user and writes nothing

#### Scenario: Snap-private temporary directory
- **WHEN** `--output /tmp/rag-aws` is given
- **THEN** the command fails and explains that the snap's `/tmp` is not the host's `/tmp`

#### Scenario: Insecure existing directory
- **WHEN** the output directory exists with mode 0777
- **THEN** the command fails naming the directory and the required permissions

### Requirement: Re-export preserves deployment state
Re-running the command on an existing deployment directory SHALL replace only the shipped asset
files and `snap-context.env`, and SHALL NOT modify or delete saved answers, secrets, SSH keys,
Terraform state or plans, or logs.

#### Scenario: Refresh assets after a snap update
- **WHEN** the command is re-run on a directory containing `settings.env`, `secrets.env`,
  `ssh/id_ed25519`, and `terraform/terraform.tfstate`
- **THEN** those files are byte-for-byte unchanged and the asset files match the installed
  rag-cli version
