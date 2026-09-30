# Design

## Context

See proposal.md for motivation. Constraints that shape the approach:

- rag-cli is strictly confined. The `rag` app has `network`, `network-bind`, `home`, `desktop`
  plugs (`snap/snapcraft.yaml`); it cannot run Terraform, the AWS CLI, `ssh` or `sudo snap ...` on
  the host. The top-level snap environment sets `HOME=${SNAP_COMMON}/home/snap_daemon` for every
  app, so inside the CLI `$HOME` is not the user's home.
- Config is snapctl-only. `rag-cli.rag set` requires root, takes exactly one `key=value`
  (`cmd/cli/config/set.go`), and user-scope writes reject keys with no package value
  (`pkg/storage/config.go`). The install hook seeds only `gdrive.*`, `kapa.*` and `api.*` package
  keys (`snap/hooks/install`), so backend keys must be written with the hidden `--package` flag, as
  INSTALL.md already documents.
- Secrets are environment-only today: `OPENSEARCH_USERNAME`/`OPENSEARCH_PASSWORD` via
  `os.LookupEnv` in `knowledge.newClient` (`cmd/cli/basic/knowledge/client.go`), `CHAT_API_KEY`
  via `os.Getenv` in `chat.clientOptions` (`cmd/cli/basic/chat/client.go`). Both functions are
  also reached by `ragd` (`internal/api/clients.go`; `chat.NewLiveSession`, `RunBatch`,
  `RefineQuestions`, `FindModelName`), whose secrets come from a systemd drop-in.
- `knowledge init` in direct mode prints `Embedding model ID: <id>` / `Rerank model ID: <id>` and
  a suggested `set --package` command but does not persist them (`printModelID`,
  `cmd/cli/basic/knowledge.go`). When a trusted `ragd` socket is detected the CLI delegates to the
  daemon instead (`daemonClient` → `apiclient.Detect`).
- The OpenSearch client uses `InsecureSkipVerify: true` (`newOpenSearchClient`): transport is
  encrypted but the server certificate is not verified. This change preserves that behavior and
  documents it; it does not claim server identity verification.
- OpenSearch snap facts are taken from canonical/opensearch-snap `2/edge` @ `6d4a652`, whose
  scripts are identical to `release-2.19.4` @ `f58163b`; the `2/stable` artifact this targets is
  2.19.4 (rev 98), whose wrapper scripts, helpers and hooks match `f58163b`. Upstream source does
  not prove installed behaviour, so the bootstrap checks every path and identity assumption at run
  time and stops naming the stage that failed.

## Goals / Non-Goals

**Goals:**
- One exported host script with `setup` and `destroy`, one guest bootstrap script, bundled
  Terraform templates, and one small CLI command to export them.
- Every step safe to rerun: reuse saved answers, state, keys, passwords and TLS material; skip
  verified-complete work; stop with a diagnostic instead of resetting security.
- Ordinary `rag-cli.rag` commands work from a fresh shell with no `.bashrc` edits.

**Non-Goals:**
- Running orchestration inside the snap, a general orchestration or state framework,
  fingerprints, Unix group management, daemon management beyond starting the bundled Tika
  service, new status commands, or unrelated fixes.
- Configuring `ragd` (its secrets remain the operator's systemd drop-in).
- Provisioning Bedrock access or creating a Google OAuth client.
- Server certificate verification for OpenSearch (existing client behavior is kept).
- Multi-node clusters, clouds other than AWS, arm64.

## Decisions

### D1. Confined export, unconfined orchestration

`rag-cli.rag prepare-script aws --output <dir>` (new `cmd/cli/config/prepare_script.go`, grouped
with the configuration commands after `set`) writes files embedded with `go:embed` from
`internal/awssetup/assets/`. It refuses to run as root, creates `<dir>` with mode 0700 when absent,
refuses an existing `<dir>` not owned by the caller or writable by group/other, and resolves
`<dir>` against the working directory. It rewrites the shipped assets on every run (so a newer
rag-cli refreshes them) and never touches saved answers, secrets, keys, or Terraform state. The
snap's private `/tmp` and hidden home directories are not reachable through the `home` plug, so
the command rejects `/tmp` and `/var/tmp` prefixes with guidance to use a non-hidden directory
under the home directory.

It also writes `snap-context.env` with `RAG_SNAP_INSTANCE=$SNAP_INSTANCE_NAME` and
`RAG_SNAP_USER_COMMON=$SNAP_USER_COMMON`. snapd sets `SNAP_USER_COMMON` from the real user's home
(`~/snap/<instance>/common`), independent of the snap's overridden `HOME`. The host script
cross-checks that value against `$(getent passwd "$(id -un)" | cut -d: -f6)/snap/<instance>/common`
and stops on mismatch (for example, `prepare-script` run under `sudo -E`).

Alternatives: shipping assets as files under `$SNAP` and copying them in the script (requires a
repository-independent bootstrap anyway, and still needs the CLI to know `SNAP_USER_COMMON`);
running Terraform in the snap (needs classic confinement or broad plugs, which the proposal rules
out).

Deployment directory layout (all files 0600 and directories 0700, except the script at 0700):

```
<dir>/opensearch-on-aws.sh              host script
<dir>/bootstrap-opensearch.sh guest script (copied to the instance)
<dir>/terraform/*.tf          templates; terraform.tfstate, tfplan, destroy.tfplan, .terraform/
<dir>/snap-context.env        written by prepare-script
<dir>/settings.env            editable saved answers (non-secret)
<dir>/secrets.env             recovery secrets
<dir>/ssh/id_ed25519{,.pub}   generated once; ssh/known_hosts
<dir>/logs/                   run logs without secret values
```

### D2. Saved answers are data

`settings.env` and `secrets.env` share one parser: blank lines and `#` comments are skipped; each
remaining line must match `^[A-Z][A-Z0-9_]*=`. The writer always emits `KEY="value"`, with `\"`
and `\\` as the only escapes, so every value, including quotes, backslashes, spaces, `=`, `$` and
an empty string, reads back unchanged. The reader accepts that form, `KEY='value'` (literal), and
unquoted `KEY=value` (literal, and it must not start with a quote). No variable, command, or other
escape processing is done. Unknown keys stop the run with the line number. A key present with an empty
value records an explicit "none" answer (optional API key, declined import); an absent key means
"not asked yet". Files are rewritten atomically (`mktemp` in the same directory, then `mv`) with
keys in a fixed order.

Prompts show the default in brackets and Enter accepts it. Defaults come from the saved value if
valid, otherwise the current `rag-cli.rag get <key>` value for rag-cli settings, otherwise the
built-in default. Invalid saved values are reported and re-prompted without offering them as
defaults. Secrets are read with `read -rs`; the log records only that a secret was provided.

| Key (settings.env) | Default / validation |
| --- | --- |
| `AWS_PROFILE` | `default`; must pass `sts get-caller-identity` |
| `AWS_REGION` | `us-east-1`; `^[a-z]{2}(-[a-z]+)+-[0-9]+$` |
| `INSTANCE_TYPE` | `t3.xlarge`; `^[a-z0-9]+\.[a-z0-9]+$` |
| `UBUNTU_RELEASE` | `24.04`; one of `24.04`, `26.04` |
| `ROOT_VOLUME_GIB` | `50`; integer 20–16384; passed to Terraform as the root `volume_size` |
| `AMI_ID` | not prompted; resolved and saved once (see D4); `^ami-[0-9a-f]{8,17}$` |
| `OPENSEARCH_CHANNEL` | `2/stable`; `^[0-9]+/(stable|candidate|beta|edge)$` |
| `ALLOWED_CIDR` | detected public IPv4 + `/32` (see D4) |
| `DEPLOYMENT_ID` | generated once, `rag-` + 8 hex chars; not prompted |
| `CHAT_HOST`, `CHAT_PORT`, `CHAT_PATH`, `CHAT_TLS`, `CHAT_MODEL` | from existing `chat.*` config, else `bedrock-runtime.<AWS_REGION>.amazonaws.com`, `443`, `openai/v1`, `true`; model has no built-in default |
| `DRIVE_IMPORT` | `yes`/`no`, no default |
| `DRIVE_FOLDER_URL` | required when `DRIVE_IMPORT=yes`; `https://drive.google.com/` prefix |
| `DRIVE_IMPORT_LAST_ATTEMPT` | timestamp written when an import command exits 0; records only that an attempt finished |

`secrets.env` keys: `OPENSEARCH_ADMIN_PASSWORD`, `TLS_ROOT_PASS`, `TLS_ADMIN_PASS`,
`TLS_NODE_PASS` (each 32 alphanumeric characters from `/dev/urandom`, generated when absent),
`CHAT_API_KEY` (optional, may be empty), `GOOGLE_DRIVE_CLIENT_ID`, `GOOGLE_DRIVE_CLIENT_SECRET`.
Alphanumeric generated secrets need no quoting in YAML, JSON, shell, or URLs. User-provided
secrets are rejected if they contain control characters or newlines.

### D3. Host script flow

`setup` runs these phases in order; each verifies before acting, so rerunning resumes:

1. **Preflight.** Refuse root (`sudo` is used per command). Require `bash`, `curl`, `ssh`,
   `ssh-keygen`, `scp`, `sudo`, `snap`, and the rag-cli snap. Validate `snap-context.env` (D1).
   If `snap services <instance>.ragd` reports it active, stop: the CLI would delegate
   `knowledge init` to `ragd`, whose secrets this script does not manage. The message points to
   INSTALL.md "Secrets" and suggests stopping `ragd` for the duration.
2. **Tools.** Reuse `aws` if on `PATH`; otherwise offer `sudo snap install aws-cli --classic`
   (the store lists `aws-cli`, publisher `aws`, as classic). Declining exits with instructions.
   Reuse Terraform if on `PATH`; otherwise offer `sudo snap install terraform --classic`.
   After an accepted install, `hash -r` and verify the command runs; Terraform's version must be
   in the supported range (1.6 or later, below 2.0). Declining, an install failure, or a bad
   version exits with instructions. Terraform and its providers stay external to the snap.
3. **AWS identity.** Resolve `AWS_PROFILE`, then immediately run
   `aws --profile "$AWS_PROFILE" sts get-caller-identity`, before any other question. If the
   profile is not in `aws configure list-profiles`, say that the next step stores access keys and
   offer to run `aws configure --profile <p>`; that command runs interactively on the terminal and
   is never captured, piped, logged or saved. Declining exits with instructions. After it, verify
   identity again in the same run. If the profile exists but authentication fails, exit with the
   AWS error and guidance (`aws sso login --profile <p>`, a new session token, or a re-run of
   `aws configure`) without touching the profile's configuration.
4. **Answers and secrets.** Collect missing/invalid answers (D2), including the Drive import
   decision only when none is saved, generate missing secrets, resolve and verify `AMI_ID` when it
   is absent (D4), and generate `ssh/id_ed25519` with `ssh-keygen -t ed25519 -N ''` if absent. All
   are saved before step 5.
5. **Terraform.** `terraform -chdir=terraform init -input=false`, then
   `plan -input=false -out=tfplan` with variables passed via `-var` (none secret), including
   `ami_id=$AMI_ID` and `root_volume_gib=$ROOT_VOLUME_GIB`. If the plan has
   changes, show it with `terraform show tfplan`, ask for `yes`, and run `apply tfplan`; the exact
   saved plan is applied. `umask 077` is set for the whole script and state files are `chmod 600`.
6. **Guest bootstrap.** Wait for SSH (bounded, 300 s) using
   `-i ssh/id_ed25519 -o UserKnownHostsFile=ssh/known_hosts -o StrictHostKeyChecking=accept-new`
   as user `ubuntu`, then `cloud-init status --wait --long` (bounded). Any non-zero exit, or a
   timeout, stops the run with the status in the phase log, before anything is copied. Copy
   `bootstrap-opensearch.sh` with `scp`; send the four guest secrets over SSH stdin into a 0600 file created under
   `umask 077`; run `sudo bash ~/rag-bootstrap/bootstrap-opensearch.sh --node-name rag0
   --channel <channel> --secrets-file ~/rag-bootstrap/secrets.env`. No secret appears in a local or
   remote command line built by the host script.
7. **Remote check.** From the host, `GET https://<public_ip>:9200/` with `admin` and the saved
   password must return 200 (proves the /32 rule and the credential). `curl` reads the credential
   from a config on stdin (`curl -K -`), never from argv.
8. **Local configuration.** Write the credentials file (D6). Set, one `sudo rag-cli.rag set
   --package` call per key: `knowledge.http.host=<public_ip>`, `knowledge.http.port=9200`,
   `knowledge.http.tls=true`, `tika.http.host=127.0.0.1`, `tika.http.port=9998`,
   `tika.http.path=tika`, `chat.http.host`, `chat.http.port`, `chat.http.path`, `chat.http.tls`,
   `chat.model`. After each, `rag-cli.rag get <key>` must print the value; a mismatch means a
   user-layer override shadows it and the script stops naming the key. Start Tika with
   `sudo snap start --enable <instance>.tika-server` (port 9998 is fixed in `apps/tika-start.sh`)
   and wait up to 120 s for `http://127.0.0.1:9998/tika`.
9. **Models and pipelines.** Run `rag-cli.rag knowledge init` as the invoking user. Strip
   carriage returns and ANSI sequences from its output and extract
   `^(Embedding|Rerank) model ID: ([A-Za-z0-9_-]+)$`; both must be present. Persist with
   `sudo rag-cli.rag set --package knowledge.model.embedding=<id>` and `...rerank=<id>` and verify
   with `get`. `init` is documented as safe to rerun (it reuses existing models).
10. **Optional Drive import** (D8).
11. **Summary.** Endpoint, deployment directory, where secrets are stored, the TLS limitation, and
    `rag-cli.rag status` output.

Every wait is bounded; on failure the script prints the phase name, the command that failed, and
the relevant log excerpt with every known secret value replaced by `***`.

### D4. Terraform resources

Templates (`versions.tf`, `variables.tf`, `main.tf`, `outputs.tf`) use the `hashicorp/aws`
provider with `profile` and `region` from variables and `default_tags` carrying
`rag-cli:deployment = <DEPLOYMENT_ID>`. Resources: `aws_vpc` (10.42.0.0/16), `aws_internet_gateway`,
`aws_subnet` (10.42.1.0/24, `map_public_ip_on_launch`), `aws_route_table` with a default route
and its association, `aws_security_group` (ingress TCP 22 and 9200 from `var.allowed_cidr`; egress
all, needed for snap and apt), `aws_key_pair` from `ssh/id_ed25519.pub`, and `aws_instance` with
`root_block_device { volume_size = var.root_volume_gib, volume_type = "gp3", encrypted = true,
delete_on_termination = true }` (`root_volume_gib` defaults to 50, the saved `ROOT_VOLUME_GIB`) and
IMDSv2 required. The subnet's availability zone is the first (sorted) zone returned by
`aws_ec2_instance_type_offerings` for the instance type.

AMI: the image is chosen once and then pinned. When `AMI_ID` is absent from `settings.env`, the
host script selects it with `aws ec2 describe-images --owners 099720109477` in the region,
filtering `Name=name` to Canonical's pattern for the release,
`ubuntu/images/hvm-ssd-gp3/ubuntu-<codename>-<release>-amd64-server-*` (`noble` for 24.04,
`resolute` for 26.04, verified against Canonical's "Find Ubuntu images on AWS" documentation; the
`hvm-ssd-gp3` component is the HVM/EBS-gp3 image), plus `architecture=x86_64`,
`virtualization-type=hvm`, `root-device-type=ebs` and `state=available`, and takes the newest
match with `sort_by(Images,&CreationDate)[-1].ImageId`. It then checks the chosen ID with
`aws ec2 describe-images --image-ids <id> --owners 099720109477` (it must return exactly that
image, architecture `x86_64`, with the release in its name) and saves it as `AMI_ID` before the
first plan. A failed lookup reports the AWS error. This needs only `ec2:DescribeImages`, which
the ownership check already used. Later runs
pass the saved `AMI_ID` as `var.ami_id` and do not consult `stable/current` again, so a newer
Canonical image never replaces the instance implicitly. Terraform re-checks ownership on every
plan with `data "aws_ami"` filtered by `image-id = var.ami_id` and `owners = ["099720109477"]` in
the provider's region, so a non-Canonical ID, or an ID from another region after `AWS_REGION` is
edited, fails the plan. If a hand-edited `UBUNTU_RELEASE` no longer matches the saved image's name,
the script stops and says to clear `AMI_ID` to select a new image. Changing the image is explicit:
the user removes or edits `AMI_ID`, and the resulting instance replacement appears in the
reviewed plan like any other change. No AMI ID is hardcoded. The private key is generated by
`ssh-keygen` on the host, never by Terraform, so it is not in state.

`ALLOWED_CIDR` defaults to `$(curl -4fsS https://checkip.amazonaws.com)/32`. On reruns the saved
value is kept; if the detected address differs, the script says so and asks whether to update it
(the change then appears in the plan).

The instance has a non-elastic public IP; stopping and starting it changes the address. The
documentation says so; rerunning `setup` updates `knowledge.http.host`.

### D5. Guest bootstrap

Runs as root on the instance. Paths are the snap's environment values
(`opensearch-snap/snap/snapcraft.yaml`, top-level `environment`):
`CONF=/var/snap/opensearch/current/etc/opensearch` (`OPENSEARCH_PATH_CONF`),
`CERTS=$CONF/certificates`, `PLUGINS=/var/snap/opensearch/current/usr/share/opensearch/plugins`
(`OPENSEARCH_HOME/plugins`, copied to `SNAP_DATA` by the install hook),
`JDK=/snap/opensearch/current/usr/lib/jvm/java-21-openjdk-amd64` (`JAVA_HOME`).
The daemon runs as `snap_daemon` via `setpriv` (`start.sh`). The install hook leaves config files
owned `snap_daemon:root`, mode 660 (`helpers/io.sh`, `hooks/install`). Every file the bootstrap
writes under `$CONF` is written to a temporary file in the same directory, given the target's
owner and mode (`chown --reference`, `chmod --reference`; new files `snap_daemon:root 0660`), and
renamed into place. The bootstrap reads the secrets file into variables and deletes it on exit
(`trap`); the host resends it on every run.

Stages, each check-then-act:

1. **Sysctl.** Write `/etc/sysctl.d/60-rag-opensearch.conf` with `vm.max_map_count=262144`,
   `vm.swappiness=0`, `net.ipv4.tcp_retries2=5`; `sysctl -p` that file; verify each with
   `sysctl -n`. (The snap's own `set-sys-config.sh` only applies these when the
   `set-sysctl-props` snap option is `yes`, which defaults to `no`, and does not persist them.)
2. **Snap and interfaces.** If `opensearch` is absent, `snap install opensearch --channel=<channel>`;
   if present on another channel, report it and continue with the installed snap. Run
   `snap set opensearch set-sysctl-props=no` and check it with `snap get` (both privileged), so the
   snap's own sysctl handling stays off and the stage 1 file is authoritative. Run
   `snap connect opensearch:process-control` (README). `start.sh` exits unless `log-observe`,
   `mount-observe`, `sys-fs-cgroup-service` and `system-observe` are connected, so the bootstrap
   checks `snap connections opensearch` for all five and connects any that are missing; any still
   disconnected stops the run with the plug name.
3. **Passphrases.** Taken from the secrets file; the bootstrap fails if any is empty. It never
   generates them, so every run uses the saved values.
4. **TLS setup.** Complete means `root-ca.pem`, `admin.pem`, `admin-key.pem`,
   `node-rag0.pem` and `node-rag0-key.pem` exist in `$CERTS` and `$CONF/opensearch.yml` has
   `node.name` `rag0`. If complete, skip. If incomplete and the marker
   `/var/lib/rag-bootstrap/daemon-started` exists, stop: setup would regenerate the CA under a
   started node. Otherwise run `snap run opensearch.setup --node-name rag0 --node-roles
   cluster_manager,data,ingest,ml --tls-priv-key-root-pass … --tls-priv-key-admin-pass …
   --tls-priv-key-node-pass … --tls-init-setup yes` (flags from `setup.sh` `parse_args`). A setup
   interrupted before the first start is safely rerun from scratch.
5. **Heap.** `heap_gib = round(MemTotal_KiB × 3 / 8 / 1,048,576)`, clamped to 1–31 (`MemTotal` in
   `/proc/meminfo` is KiB). A t3.xlarge reports roughly 15.5 GiB, giving 6 GiB and leaving about
   9.5 GiB for ML-model native memory, page cache and the OS. Write `-Xms<N>g` and `-Xmx<N>g` to
   `$CONF/jvm.options.d/heap.options` (the path INSTALL.md uses). If the content changes while the
   daemon is running, restart it.
6. **Admin password.** Only while security is uninitialized: when the daemon is already running
   (resumed run), step 8's probe runs first and this stage is skipped unless it reports
   "not initialized"; when the start marker is absent it always runs. The resulting
   `internal_users.yml` is in place before the first start. The rev 98 `opensearch.yml` contains
   only comments and the security plugin's `allow_default_init_securityindex` defaults to `false`,
   so the index is expected to wait for `security-init`; the order above gives the same result if a
   runtime check shows otherwise. Back up `internal_users.yml` once to
   `internal_users.yml.rag-orig` (root, 0600).
   Hash with the packaged tool through `bash`; in rev 98 `hash.sh` is mode 0644 (no executable
   bit):
   `OPENSEARCH_JAVA_HOME=$JDK OS_ADMIN_PW=<pw> bash $PLUGINS/opensearch-security/tools/hash.sh -env OS_ADMIN_PW`.
   `hash.sh` prefers `OPENSEARCH_JAVA_HOME`, and `Hasher` supports `-env <VAR>` (security plugin
   2.19.4.0 `tools/hash.sh`, `Hasher.java`); in rev 98 it prints one `$2y$12$` line, so the last
   output line must match `^\$2[aby]\$`. Replace only
   the `hash:` line inside the top-level `admin:` block with `awk`, leaving every other byte of the
   file unchanged, then check that exactly one line changed. Running the tool at the installed
   path on the guest is part of the group 1 runtime checks; if the snap's JDK cannot be used there,
   the fallback is Ubuntu's `openjdk-21-jre-headless` on the guest.
7. **Start.** Create the marker, then `snap start --enable opensearch.daemon`. Wait up to 180 s
   for any HTTP response on `https://localhost:9200/`. On timeout print
   `journalctl -u snap.opensearch.daemon -n 100` and the tail of
   `/var/snap/opensearch/common/var/log/opensearch/opensearch-cluster.log`.
8. **Security initialization.** Probe `GET /_plugins/_security/authinfo` as `admin` with the saved
   password (`curl -K -` from stdin), retrying for up to 180 s until the answer is definitive; no
   HTTP response or any other status is transient and never counts as "not initialized":
   - 200: initialized with the saved password, skip;
   - 503 whose body contains `Security not initialized` (`OpenSearch Security not initialized.`
     from `BackendRegistry` in security 2.19; the runtime response is checked in task 1.3). Stage 7
     therefore waits only for *any* HTTP response, never for an authenticated 200, which would
     deadlock before initialization. Then run
     `snap run opensearch.security-init --tls-priv-key-admin-pass=<pass>`. The wrapper sleeps
     10 s and then *sources* `securityadmin.sh -cd $CONF/opensearch-security/ -icl -nhnv …` under
     `set -eu` (`security-init.sh`), so it is synchronous and its exit status is the tool's.
     `securityadmin.sh` sends the tool's stderr to `/dev/null`, so the bootstrap keeps the
     command's stdout for diagnostics. Then re-probe until 200 (bounded, 60 s);
   - 401: probe `admin:admin`. If that succeeds, stop with "security was initialized from the
     unmodified internal_users.yml; the saved password was never applied". Otherwise stop with
     "saved OpenSearch credentials are rejected". Never re-run setup or security-init and never
     edit the security index in this case.
   After a successful `security-init`, wait (bounded, 36 × 5 s) for the saved password to
   authenticate: the security index lags the command that creates it (observed live: success
   reported, then "Not yet initialized" milliseconds later). During that wait a 503 — including
   the "not initialized" body — and a connection failure are pending, never a reason to re-run
   security-init. A 401 is reported as a credential rejection and running out of tries as a
   readiness timeout, with different messages.
9. **Readiness.** Authenticated `GET /_cluster/health?wait_for_status=yellow&timeout=60s` and
   `GET /_cat/nodes?h=node.roles,heap.max`, whose `node.roles` must contain `cluster_manager`,
   `data`, `ingest` and `ml` as complete comma-separated tokens (`node.role` is the abbreviated
   form, e.g. `dim`, and is not used). The health status and the heap value are reported.

The snap's `test-*` apps default to `admin:admin` and are not used after the password changes.
`setup.sh` and `security-init.sh` accept passphrases only as arguments, so they appear briefly in
the guest's process list; the guest is single-tenant and `setup.sh` also stores the node
passphrase in `opensearch.yml` (`pemkey_password`) by design.

### D6. CLI credentials file

New package `pkg/credentials`:

- `Enable(path string)` is called once from `cmd/cli/main.go` with
  `$SNAP_USER_COMMON/credentials.json` (skipped when `SNAP_USER_COMMON` is empty). `ragd` never
  calls it, so in `ragd`, `Lookup` is exactly an environment lookup and its contract is unchanged.
- `Lookup(name) (value string, found bool, err error)`: an environment value (as
  `os.LookupEnv`, so an explicitly empty value counts) wins and never touches the file. Only when
  the requested variable is unset, and the fallback is enabled, does `Lookup` read and validate the
  file; the parsed result or the validation error is cached for the rest of the process. A command
  whose required credentials all come from the environment therefore never reads the file and is
  unaffected by an invalid one.
- File rules: regular file (checked with `Lstat`, symlinks rejected), owned by the effective UID,
  no group/other permission bits, a single JSON object whose keys are a subset of
  `OPENSEARCH_USERNAME`, `OPENSEARCH_PASSWORD`, `CHAT_API_KEY` with string values. A missing file
  is not an error. When the file is needed and violates a rule, `Lookup` returns an error naming the
  path and the rule, for example
  `credentials file ~/snap/rag-cli/common/credentials.json must not be readable by group or
  others (run: chmod 600 …)`. Values never appear in errors.
- `knowledge.newClient` uses `Lookup` for both OpenSearch keys and returns its error; the
  existing "env var is not set" message gains "or add it to <path>". `chat.clientOptions` uses
  `Lookup("CHAT_API_KEY")`, keeps "empty means no key", and now returns an error, which its callers
  in the chat package (`batch.go`, `client.go`, `refine.go`, `turn.go`) propagate. Because
  `CHAT_API_KEY` is looked up whenever a chat client is built, a user with an invalid file who
  wants no key can set `CHAT_API_KEY=` explicitly. The exported signatures used by `ragd` stay the
  same.

Alternative considered: injecting file values into the process environment at CLI startup. It
touches no client code, but it would pass secrets to every child process (`xdg-open`,
`elasticdump`) and fail unrelated commands on a bad file.

The host script writes the file as the invoking user. It requires `$RAG_SNAP_USER_COMMON` to
exist, be owned by the user, and not be writable by group/other (it does not change snapd's
directory mode). It writes a `mktemp` file there (0600), fills it with JSON (values are validated
to contain no control characters; `\` and `"` are escaped), and renames it over
`credentials.json`. If an existing file holds a different `OPENSEARCH_PASSWORD`, the script asks
before replacing it, without printing values. No shell startup file is touched.

### D7. Local secrets and identities

`sudo` is used only for `rag-cli.rag set --package`, `snap start --enable <instance>.tika-server`
and the optional `snap install aws-cli`. `knowledge init`, `knowledge import`, the credentials
file, the Drive token cache (`$SNAP_USER_DATA/gdrive-token.json`, `driveTokenCachePath`) and all
deployment files belong to the invoking user.

### D8. Google Drive import

Reuses `rag-cli.rag knowledge import --url <folder-url> --all` (flags in `importCommand`). The
existing flow resolves client credentials from `GOOGLE_DRIVE_CLIENT_ID` /
`GOOGLE_DRIVE_CLIENT_SECRET`, then `gdrive.client.id` / `gdrive.client.secret` config
(`resolveClientCredentials`). The script keeps them in `secrets.env` (not in snapctl config, which
this project does not treat as a secret store) and exports them only into the environment of the
import process. If the user asked for an import and a credential is missing, the script prompts for it;
an empty answer re-prompts, and the only way to skip is to change the saved decision to `no`
explicitly. Masking covers every stored secret except `GOOGLE_DRIVE_CLIENT_ID`: the client ID is a
public OAuth identifier that the importer prints inside the authorization URL, so masking it would
replace `client_id` with `***` and make Google reject the consent request. The client secret, and
every other secret, stay masked in the terminal output and in the phase log.

The existing OAuth flow is loopback plus PKCE: it listens on `127.0.0.1:<random port>` on the
machine running the CLI, prints the consent URL (whose `redirect_uri` carries that port), tries
`xdg-open`, and waits up to 5 minutes. The callback handler accepts any path and checks only
`state` and `code`. On a machine without a usable browser the user can:
(a) open the printed URL in a browser on another machine, after
`ssh -L <port>:127.0.0.1:<port> <user>@<this-host>`, so the redirect reaches the listener; or
(b) finish consent in any browser, copy the address of the failed `http://127.0.0.1:<port>/?…`
redirect, and run `curl '<that URL>'` on this machine within the timeout.
The script prints both options before starting the import; it does not infer failure from a
missing `DISPLAY`.

The existing command can report individual archive failures (download errors, "already contains N
documents; use --force") and still exit 0. That is an existing bug outside this feature; the
import implementation is not changed and the script does not parse the output to infer
per-archive success. The script shows the command's output unchanged. On exit 0 it prints
"Import command finished; review its output for archive failures." and records
`DRIVE_IMPORT_LAST_ATTEMPT`, which means only that an attempt finished. A non-zero exit is a
failed setup phase (D3 failure reporting) and records nothing. On a later run with
`DRIVE_IMPORT=yes` and a recorded attempt, the script shows the attempt time and offers to retry
the import; a saved `DRIVE_IMPORT=no` is kept and not asked again. It never passes `--force`.

### D9. Destroy

`destroy` checks the saved profile with `sts get-caller-identity`, runs
`terraform plan -destroy -out=destroy.tfplan`, shows it, requires typing the deployment ID, and
applies the saved plan. This covers the instance and its root volume (`delete_on_termination`),
the key pair, security group, subnet, route table, internet gateway and VPC. Until the apply
succeeds nothing local is removed. After it succeeds:

- delete `credentials.json` only if its `OPENSEARCH_PASSWORD` equals the saved
  `OPENSEARCH_ADMIN_PASSWORD`; otherwise leave it and say so;
- drop the four generated secrets (`OPENSEARCH_ADMIN_PASSWORD`, `TLS_ROOT_PASS`, `TLS_ADMIN_PASS`,
  `TLS_NODE_PASS`) from `secrets.env` and clear `DRIVE_IMPORT_LAST_ATTEMPT` in `settings.env`, then
  rewrite both files with the existing atomic writer (`save_env`), so the redeploy mints fresh
  secrets and runs the import instead of seeing the destroyed cluster's attempt;
- keep `CHAT_API_KEY`, `GOOGLE_DRIVE_CLIENT_ID`, `GOOGLE_DRIVE_CLIENT_SECRET` and every saved answer,
  including an explicitly empty `CHAT_API_KEY`; the next setup's `write_credentials` writes the API
  key back into `credentials.json`, and `ensure_generated_secrets` / `ensure_ssh_key` supply the new
  deployment material;
- delete `ssh/` (private key, public key, known_hosts) and the plan files;
- keep `settings.env`, the (now empty) Terraform state and logs;
- leave rag-cli configuration, the Tika service, the cached Google token and installed tools
  unchanged, and print that `knowledge.http.host` still points at the destroyed address.
- A declined or failed destroy performs none of the cleanup.

## Risks / Trade-offs

- [Installed snap differs from source (paths, plug auto-connection, uninitialized-security
  response, snap JDK usable outside confinement)] → The bootstrap checks each assumption at run
  time and stops with the stage name; the group 1 runtime checks confirm them on the real
  `2/stable` snap.
- [The packaged `internal_users.yml` in rev 98 contains the demo users `anomalyadmin`,
  `kibanaserver`, `kibanaro`, `logstash`, `readall` and `snapshotrestore` besides `admin`, with the
  upstream published hashes, which the preserve-unrelated-configuration rule keeps] → Ingress is
  limited to one /32; the docs state it. Removing them is a separate decision.
- [TLS without server verification allows an on-path attacker between the host and the /32
  endpoint to impersonate the server] → Documented plainly; unchanged from today's client.
- [Guest process list briefly shows passphrases passed to the snap's scripts] → Dedicated
  single-tenant instance; the host never places them in command lines.
- [Non-elastic public IP changes on stop/start] → Documented; rerunning `setup` reconfigures.
- [`knowledge init` output format is parsed] → The regex matches `printModelID`'s format, and a
  test pins it to the Go source's format string.
- [Existing import bug: archive failures with exit 0] → Out of scope to fix; the output is shown
  with an explicit "review its output" message, only the attempt is recorded, and reruns offer a
  retry.
- [The `aws-cli` snap is AWS CLI v2; IAM Identity Center login differs from v1] → The script only
  needs `sts`, `ec2 describe-images`, and profiles; the authentication guidance names
  `aws configure` and `aws sso login`.
- [`aws configure list-profiles` is used to tell a missing profile from a rejected one; an AWS CLI
  without it falls back to treating the profile as existing] → That fallback never overwrites an
  existing profile; it only changes which guidance is printed.

## Migration Plan

Additive. Existing installs keep working: no config keys change, and without a credentials file
the CLI behaves exactly as before. Rollback is reverting the change; exported deployment
directories remain usable with their own script copy.
