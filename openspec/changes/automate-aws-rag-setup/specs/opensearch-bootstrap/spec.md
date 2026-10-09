# Spec Delta

## Purpose

Defines how the guest bootstrap script turns a fresh Ubuntu instance into a single-node,
TLS-enabled OpenSearch cluster from Canonical's OpenSearch snap, secured with a generated admin
password, and how it behaves when re-run or interrupted.

## ADDED Requirements

### Requirement: Host settings on the guest OS
Before OpenSearch starts, the bootstrap SHALL persist `vm.max_map_count=262144`,
`vm.swappiness=0` and `net.ipv4.tcp_retries2=5` in a file under `/etc/sysctl.d/`, apply them, and
verify the running values. It SHALL set the snap option `set-sysctl-props=no` with privilege and
verify it, so the snap's own sysctl handling does not run.

#### Scenario: Fresh instance
- **WHEN** the bootstrap runs on a new instance
- **THEN** `sysctl -n` reports the three values and they are still in effect after a reboot

### Requirement: Snap install and interfaces
The bootstrap SHALL install the `opensearch` snap from the configured channel (default `2/stable`)
if it is absent and reuse it if present, connect `opensearch:process-control`, and ensure the
plugs the daemon's start script requires (`log-observe`, `mount-observe`, `sys-fs-cgroup-service`,
`system-observe`) are connected. It SHALL stop naming any plug that remains disconnected.

#### Scenario: Required plug cannot be connected
- **WHEN** `sys-fs-cgroup-service` is still disconnected after the bootstrap tries to connect it
- **THEN** the bootstrap stops before starting the daemon and names the plug and the command to
  connect it

### Requirement: TLS setup with saved passphrases
The bootstrap SHALL run the snap's `opensearch.setup` once per node, with node roles
`cluster_manager,data,ingest,ml`, `--tls-init-setup yes`, and the root, admin and node key
passphrases supplied by the host from saved secrets. The bootstrap SHALL NOT generate passphrases.
It SHALL treat setup as complete when the root CA, admin and node certificate and key files exist
and the node name is configured, and SHALL NOT run setup again once the daemon has been started.

#### Scenario: Setup interrupted before first start
- **WHEN** a previous run stopped during `opensearch.setup` and the daemon was never started
- **THEN** a re-run runs `opensearch.setup` again with the same passphrases and continues

#### Scenario: Certificates missing after the daemon started
- **WHEN** certificate files are missing but the daemon has previously been started by the
  bootstrap
- **THEN** the bootstrap stops with a diagnostic and does not regenerate certificates

### Requirement: Heap sizing
The bootstrap SHALL set equal minimum and maximum JVM heap to 3/8 of `MemTotal`, rounded to the
nearest GiB and clamped to 1–31 GiB, in the snap's `jvm.options.d` directory, and SHALL restart a
running daemon when the value changes.

#### Scenario: Default instance size
- **WHEN** the instance is a t3.xlarge (16 GiB nominal)
- **THEN** the heap options file contains `-Xms6g` and `-Xmx6g` and `GET /_nodes/jvm` reports a
  maximum heap of about 6 GiB

### Requirement: Generated admin password
Before security is initialized, the bootstrap SHALL hash the saved admin password with the
security plugin's packaged `hash.sh` (invoked through `bash`), replace only the `admin` user's
hash in `internal_users.yml`, keep the file's ownership and mode, and leave all other content of
the file unchanged. The password SHALL NOT be passed on a command line built by the bootstrap and
SHALL NOT be printed. The final REST credential SHALL NOT be `admin:admin`.

#### Scenario: Password applied
- **WHEN** the bootstrap completes on a fresh instance
- **THEN** `admin` authenticates with the saved password, `admin:admin` is rejected with 401, and
  a diff of `internal_users.yml` against its backup shows only the `admin` hash line changed

### Requirement: Start, security initialization, and readiness
The bootstrap SHALL start and enable the daemon, wait (bounded) for the HTTPS endpoint, and run
`opensearch.security-init` with the saved admin key passphrase only when the pre-initialization
probe recognizes that security is not initialized. After a successful `security-init` it SHALL
wait (bounded) for the saved admin password to authenticate, because the security index can lag
the command that creates it. During that wait an HTTP 503, including a "not initialized" body,
and a connection failure SHALL be treated as pending; the bootstrap SHALL NOT re-run
security-init while waiting, SHALL report a 401 as a credential rejection, and SHALL report
running out of attempts as a readiness timeout, distinctly from a rejection. It SHALL then verify
readiness by authenticating as `admin` with the saved password, requiring cluster health of at
least yellow and a node whose `node.roles` contains `cluster_manager`, `data`, `ingest` and `ml`
as complete tokens; the health status and heap value are reported.

#### Scenario: Successful bootstrap
- **WHEN** the bootstrap finishes
- **THEN** it exits 0 after an authenticated health check returns yellow or green

#### Scenario: Security initialization needs a moment to take effect
- **WHEN** `security-init` succeeds but the node still answers 503, then starts accepting the saved
  password
- **THEN** the bootstrap waits, does not re-run security-init, and continues

#### Scenario: Readiness never arrives
- **WHEN** the node keeps answering 503 until the bounded wait is exhausted
- **THEN** the bootstrap exits non-zero reporting a readiness timeout, not a credential rejection

#### Scenario: Saved password rejected after initialization
- **WHEN** the node answers 401 after `security-init`
- **THEN** the bootstrap exits non-zero reporting a credential rejection and does not re-run
  security-init

#### Scenario: Endpoint never comes up
- **WHEN** the endpoint does not answer within the bounded wait
- **THEN** the bootstrap exits non-zero naming the start stage and prints the recent daemon journal
  and OpenSearch log lines with secret values masked

### Requirement: Idempotent re-runs without security resets
On re-run the bootstrap SHALL verify each completed stage from the node's actual state and skip
it. When security is already initialized and the saved password authenticates, it SHALL NOT run
setup or security initialization. When the saved password is rejected it SHALL stop with a
diagnostic that distinguishes "default admin:admin still active" from "saved credentials
rejected", and SHALL NOT re-run setup, security initialization, or change security
configuration.

#### Scenario: Re-run after a later local failure
- **WHEN** the host re-runs the bootstrap after the node was fully initialized
- **THEN** no setup or security-init command is executed and the bootstrap exits 0 after the
  readiness check

#### Scenario: Interrupted between start and security initialization
- **WHEN** the daemon is running and security is not initialized
- **THEN** a re-run applies the admin hash if needed, runs security initialization once, and
  completes

#### Scenario: Saved password does not match the cluster
- **WHEN** the saved password is rejected
- **THEN** the bootstrap exits non-zero with a diagnostic and the security index is unchanged

### Requirement: Secret handling on the guest
The bootstrap SHALL read secrets from a file the host created with mode 0600, delete that file
when it exits, and mask every secret value in anything it prints.

#### Scenario: Secrets file removed
- **WHEN** the bootstrap exits, successfully or not
- **THEN** the transferred secrets file no longer exists on the instance
