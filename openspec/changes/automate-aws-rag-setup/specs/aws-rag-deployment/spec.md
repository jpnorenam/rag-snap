# Spec Delta

## Purpose

Defines the exported host script `opensearch-on-aws.sh` that, running outside snap confinement with the
user's own tools, provisions a dedicated OpenSearch instance on AWS, configures the local rag-cli
against it, optionally imports Google Drive knowledge bases, and later destroys what it created.

## ADDED Requirements

### Requirement: Setup and destroy entry points
The host script SHALL accept exactly one action, `setup` or `destroy`, SHALL refuse to run as root,
and SHALL use `sudo` only for rag-cli `set --package` calls, starting the bundled Tika service, and
an optional AWS CLI snap install.

#### Scenario: Run with sudo
- **WHEN** the user runs `sudo ./opensearch-on-aws.sh setup`
- **THEN** the script exits non-zero asking to run it as the normal user

#### Scenario: Unknown action
- **WHEN** the user runs `./opensearch-on-aws.sh deploy`
- **THEN** the script prints usage listing `setup` and `destroy` and exits non-zero

### Requirement: Prerequisites
The script SHALL reuse an AWS CLI and Terraform found on `PATH`; otherwise it SHALL offer to
install each with its classic snap (`sudo snap install aws-cli --classic`,
`sudo snap install terraform --classic`). After an accepted install it SHALL refresh command
lookup and verify the tool runs; for Terraform it SHALL also verify the version is supported
(1.6 or later, below 2.0). If an offer is declined, or the install or the verification fails, the
script SHALL stop with instructions before any AWS call. Terraform and its providers SHALL remain
external to the snap.

#### Scenario: Terraform missing and install accepted
- **WHEN** `terraform` is not on `PATH` and the user accepts the offer
- **THEN** the script runs `sudo snap install terraform --classic`, refreshes command lookup,
  verifies `terraform version` meets the supported range, and continues

#### Scenario: Terraform missing and install declined
- **WHEN** `terraform` is not on `PATH` and the user declines the offer
- **THEN** the script exits before any AWS call, printing where to get Terraform

#### Scenario: Unsupported Terraform version
- **WHEN** the installed Terraform reports a version below 1.6
- **THEN** the script stops naming the version and the supported range

#### Scenario: AWS CLI missing and install accepted
- **WHEN** `aws` is not on `PATH` and the user accepts the offer
- **THEN** the script runs `sudo snap install aws-cli --classic` and continues

### Requirement: AWS authentication before other questions
The script SHALL determine the AWS profile first and immediately run
`aws --profile <profile> sts get-caller-identity`, before any other setup question and before
provisioning. When the profile is not configured, the script SHALL explain that the next step
stores access keys, SHALL offer to run `aws configure --profile <profile>` interactively on the
user's terminal, and SHALL NOT capture, pipe, log or save that interaction. Declining SHALL stop
the run with instructions. When the profile exists but authentication fails, the script SHALL
preserve the profile's configuration, SHALL NOT reconfigure it, and SHALL exit with
authentication guidance. After a successful `aws configure` the script SHALL verify identity
again in the same run. It SHALL never print AWS secret values.

#### Scenario: Missing profile is configured and verified in the same run
- **WHEN** the chosen profile is not in the AWS configuration and the user accepts the offer
- **THEN** the script runs `aws configure --profile <profile>` interactively, then
  `sts get-caller-identity` succeeds, and setup continues

#### Scenario: Missing profile offer declined
- **WHEN** the chosen profile is not in the AWS configuration and the user declines
- **THEN** the script exits non-zero with instructions, and `aws configure` was not run

#### Scenario: Existing profile with rejected credentials
- **WHEN** `sts get-caller-identity` fails for a profile that exists in the AWS configuration
- **THEN** the script exits non-zero showing the AWS error and authentication guidance, and the
  profile's configuration is left unchanged

### Requirement: Saved answers are editable data
Answers SHALL be stored in `settings.env` and secrets in `secrets.env` in the deployment directory,
both mode 0600, one `KEY=value` per line, and SHALL be parsed as data without sourcing, evaluating
or expanding values. The script SHALL prompt only for missing or invalid values, show the default,
accept the default on Enter, keep valid saved answers on reruns, record explicit "no" or "none"
answers for optional items, read secret input without echo, and never write secret values to the
terminal or logs. Unknown keys SHALL stop the run naming the line.

#### Scenario: Rerun with complete answers
- **WHEN** every saved answer is valid and the detected public IPv4 matches `ALLOWED_CIDR`
- **THEN** the script asks no configuration questions

#### Scenario: Hand-edited invalid value
- **WHEN** the user edits `settings.env` to `UBUNTU_RELEASE=22.04`
- **THEN** the script reports the invalid value and prompts for it, offering the default `24.04`

#### Scenario: Value is not executed
- **WHEN** `settings.env` contains `CHAT_MODEL=$(touch /tmp/x)`
- **THEN** the literal string is used as the model name and no command runs

### Requirement: Defaults
Unless the user chooses otherwise the deployment SHALL use region `us-east-1`, instance type
`t3.xlarge`, Ubuntu 24.04 (26.04 also accepted), a root volume of `ROOT_VOLUME_GIB` GiB (default
50), and OpenSearch channel `2/stable`, and SHALL limit SSH (22) and OpenSearch (9200) ingress to the
user's detected public IPv4 address as a /32. The root volume SHALL always be encrypted gp3 and
deleted when the instance terminates, and its size SHALL be the saved `ROOT_VOLUME_GIB` value.

#### Scenario: Accept all defaults
- **WHEN** the user presses Enter at every prompt that has a default
- **THEN** the saved plan creates a t3.xlarge in us-east-1 from the Canonical Ubuntu 24.04 amd64
  AMI with a 50 GiB encrypted gp3 root volume and ingress only from `<detected-ip>/32`

#### Scenario: Custom root volume size
- **WHEN** `ROOT_VOLUME_GIB=100` is saved
- **THEN** the plan shows an encrypted gp3 root volume of 100 GiB with delete-on-termination

### Requirement: Secrets and keys created before remote changes
Before any Terraform apply or SSH command, the script SHALL generate and save (if absent) the
OpenSearch admin password and the three TLS key passphrases, and generate the SSH key pair once.
Later runs SHALL reuse them.

#### Scenario: Lost connection during bootstrap
- **WHEN** the SSH session drops during the guest bootstrap
- **THEN** the admin password and passphrases are already in `secrets.env` and a rerun uses them

### Requirement: Dedicated AWS resources from reviewed plans
The script SHALL manage, with bundled Terraform templates and state stored in the deployment
directory, a dedicated VPC, subnet, internet gateway, route table, security group, EC2 key pair
from the generated public key, and one EC2 instance. When `AMI_ID` is absent, the script SHALL
select the newest available Canonical Ubuntu Server image for the chosen release in the chosen
region with EC2 `DescribeImages`, restricted to Canonical's owner account, matching the release's
Canonical image name for amd64, HVM and EBS gp3, and SHALL save its ID as `AMI_ID` in
`settings.env` before any apply. On a lookup failure it SHALL show the AWS error. Later runs SHALL
use the saved `AMI_ID` and SHALL NOT replace it with a newer image; every plan SHALL fail if the
saved image is not a Canonical image in the selected region. A different image SHALL be used only after
the user explicitly clears or edits `AMI_ID`, and any resulting instance replacement SHALL appear
in the reviewed plan. It SHALL save the plan to a file, show it, ask for confirmation, and apply
exactly that saved plan. Terraform state and plan files SHALL be mode 0600.

#### Scenario: Plan declined
- **WHEN** the user answers anything other than `yes` to the plan
- **THEN** nothing is applied and the script exits leaving saved answers and secrets in place

#### Scenario: No infrastructure changes on rerun
- **WHEN** the plan has no changes
- **THEN** the script proceeds without asking for confirmation

#### Scenario: Newer Canonical image published
- **WHEN** Canonical publishes a newer image for the release after the deployment was created
- **THEN** a rerun plans with the saved `AMI_ID` and shows no instance change

#### Scenario: Image changed explicitly
- **WHEN** the user removes `AMI_ID` from `settings.env` and reruns setup
- **THEN** the script resolves and verifies the current Canonical image, saves the new ID, and the
  plan shown for confirmation includes the instance replacement

#### Scenario: Saved image not valid in the region
- **WHEN** `AWS_REGION` is edited so the saved `AMI_ID` is not a Canonical image in that region
- **THEN** the plan fails naming the image and nothing is applied

#### Scenario: Public IP changed
- **WHEN** the detected public IPv4 differs from the saved `ALLOWED_CIDR`
- **THEN** the script asks whether to update it, and if so the security-group change appears in
  the plan

### Requirement: Guest bootstrap over SSH
The script SHALL wait (bounded) for SSH on the new instance, copy the guest bootstrap, transfer the
guest's secrets through SSH standard input into a mode-0600 file, and run the bootstrap. Secrets
SHALL NOT appear in any command line the script builds, locally or remotely. After the bootstrap,
the script SHALL verify from the host that the saved admin credential authenticates against
`https://<public-ip>:9200/`.

#### Scenario: Remote verification
- **WHEN** the bootstrap succeeds
- **THEN** an authenticated request from the host to the public endpoint returns 200 before any
  local rag-cli configuration changes

### Requirement: Local rag-cli configuration
After the remote check the script SHALL write the invoking user's
`$SNAP_USER_COMMON/credentials.json` (mode 0600, atomic replace, parent directory owned by the user
and not writable by others) with the OpenSearch username and password and the chat API key when
one is set; set with `sudo rag-cli.rag set --package`, one key per call,
`knowledge.http.host`, `knowledge.http.port`, `knowledge.http.tls=true`, `tika.http.host`,
`tika.http.port`, `tika.http.path`, `chat.http.host`, `chat.http.port`, `chat.http.path`,
`chat.http.tls` and `chat.model`; verify each effective value with `rag-cli.rag get`; start and
enable the bundled Tika service and wait (bounded) for it to answer. It SHALL NOT modify `.bashrc`
or any shell startup file. The script SHALL locate `SNAP_USER_COMMON` from the exported snap
context and SHALL stop if it is not the invoking user's `~/snap/<instance>/common` from the
password database.

#### Scenario: Fresh shell afterwards
- **WHEN** setup has completed and the user opens a new shell with no exports
- **THEN** `rag-cli.rag knowledge list` and `rag-cli.rag chat` authenticate without further steps

#### Scenario: User-layer override shadows a value
- **WHEN** a user-layer value for `knowledge.http.host` exists and differs from the value just set
- **THEN** the script stops naming the key and the conflicting effective value

#### Scenario: Unrelated credentials file present
- **WHEN** `credentials.json` exists with a different OpenSearch password
- **THEN** the script asks before replacing it and does not print either value

#### Scenario: Daemon would take over
- **WHEN** the `ragd` service of the rag-cli snap is active
- **THEN** the script stops before local configuration explaining that `ragd` secrets are
  configured separately and how to proceed

### Requirement: Models and pipelines
The script SHALL run `rag-cli.rag knowledge init` as the invoking user, obtain the embedding and
rerank model IDs from its output, persist them with `sudo rag-cli.rag set --package
knowledge.model.embedding=<id>` and `knowledge.model.rerank=<id>`, and verify both with
`rag-cli.rag get`.

#### Scenario: Model IDs persisted
- **WHEN** `knowledge init` succeeds
- **THEN** `rag-cli.rag get knowledge.model.embedding` and `... knowledge.model.rerank` print the
  IDs reported by init

#### Scenario: Model ID not reported
- **WHEN** init output lacks either model ID line
- **THEN** the script stops, shows the init output, and does not set either key

### Requirement: Optional Google Drive import
The script SHALL ask whether to import knowledge bases from Google Drive and save the decision. If
yes, it SHALL collect the folder URL and any missing `GOOGLE_DRIVE_CLIENT_ID` /
`GOOGLE_DRIVE_CLIENT_SECRET`, save them in `secrets.env`, print how to complete the existing
loopback OAuth consent on a machine without a local browser, and run
`rag-cli.rag knowledge import --url <folder-url> --all` as the invoking user with the client
credentials only in that process's environment. A requested import SHALL NOT be turned into a skip
because credentials are missing; skipping requires the user to change the saved decision to `no`.
The script SHALL display the import command's output unchanged and SHALL NOT parse it to infer
per-archive success. Output masking SHALL cover every stored secret except
`GOOGLE_DRIVE_CLIENT_ID`, which is a public OAuth identifier the importer prints inside the
authorization URL; masking it would break that URL. The client secret and every other secret SHALL
remain masked in the terminal output and in the phase log. On exit 0 it SHALL print "Import command finished; review its output for
archive failures." and record `DRIVE_IMPORT_LAST_ATTEMPT`, meaning only that an attempt finished.
A non-zero exit SHALL fail the setup phase. On later runs with `DRIVE_IMPORT=yes` and a recorded
attempt the script SHALL offer to retry the import; a saved `DRIVE_IMPORT=no` SHALL be kept
without asking again.

#### Scenario: Authorization URL is shown in full
- **WHEN** the importer prints the Google authorization URL
- **THEN** the URL reaches the terminal unchanged, including `client_id`, `redirect_uri`, `state`
  and the PKCE challenge, while a client secret in the same output is masked in both the terminal
  and the phase log

#### Scenario: Import requested without client secret
- **WHEN** `DRIVE_IMPORT=yes` and no client secret is saved
- **THEN** the script prompts for it and re-prompts on an empty answer

#### Scenario: Import command exits 0
- **WHEN** the import command exits 0, even if its output reports an archive failure
- **THEN** the script shows the output, prints "Import command finished; review its output for
  archive failures.", and records `DRIVE_IMPORT_LAST_ATTEMPT`

#### Scenario: Import command fails
- **WHEN** the import command exits non-zero
- **THEN** the setup phase fails with the import output shown and no attempt is recorded

#### Scenario: Retry offered after a previous attempt
- **WHEN** `DRIVE_IMPORT=yes`, `DRIVE_IMPORT_LAST_ATTEMPT` is set, and setup is run again
- **THEN** the script shows the previous attempt time and asks whether to run the import again

#### Scenario: Import declined
- **WHEN** the user answers `no`
- **THEN** `DRIVE_IMPORT=no` is saved and later runs do not ask about the import again

### Requirement: Rerun and failure reporting
Every setup phase SHALL be safe to rerun: it SHALL verify completed work and skip it, reuse saved
state, keys, passwords and TLS material, and resume after an interruption. Every wait SHALL be
bounded. On failure the script SHALL name the failing phase and show the relevant log excerpt with
secret values masked.

#### Scenario: Rerun after local failure
- **WHEN** a previous run failed during `knowledge init`
- **THEN** a rerun makes no infrastructure changes, does not reinitialize OpenSearch security, and
  resumes at local configuration

### Requirement: Destroy
`destroy` SHALL verify the saved AWS profile, save and show a destroy plan, require the user to
type the deployment ID, and apply exactly that plan, removing every AWS resource the deployment
manages including the instance's root volume and the generated key pair. Local state, the private
key and recovery secrets SHALL be kept until the destroy succeeds. After success it SHALL remove
the four generated secrets (`OPENSEARCH_ADMIN_PASSWORD`, `TLS_ROOT_PASS`, `TLS_ADMIN_PASS`,
`TLS_NODE_PASS`) from `secrets.env` and clear `DRIVE_IMPORT_LAST_ATTEMPT` in `settings.env`, using
the existing parser and atomic writer so permissions and exact values are preserved. It SHALL keep
`CHAT_API_KEY`, `GOOGLE_DRIVE_CLIENT_ID` and `GOOGLE_DRIVE_CLIENT_SECRET` (including an explicitly
empty API key) and every saved answer; the next setup SHALL generate fresh deployment secrets and a
new SSH key, write the preserved API key back into `credentials.json`, and, with `DRIVE_IMPORT=yes`,
run the import without an already-attempted prompt. It SHALL delete the SSH key material and plan
files; SHALL delete `credentials.json` only when its `OPENSEARCH_PASSWORD` equals the deployment's
saved admin password; and SHALL NOT delete other rag-cli configuration, stop services, remove the
cached Google token, or uninstall tools. A declined or failed destroy SHALL leave the secrets, the
SSH key and the import marker untouched.

#### Scenario: Destroy succeeds
- **WHEN** the user confirms with the deployment ID and Terraform destroy succeeds
- **THEN** no resource tagged with the deployment ID remains, the four generated secrets are gone
  from `secrets.env`, the reusable credentials and saved answers remain, the import marker is
  cleared, `ssh/` and the plan files are gone, and a `credentials.json` written by this deployment
  is removed

#### Scenario: Redeploy after a successful destroy
- **WHEN** setup runs again on the same deployment directory with `DRIVE_IMPORT=yes`
- **THEN** it reuses `CHAT_API_KEY` and the Google OAuth client, generates fresh deployment secrets
  and a new SSH key, writes the API key back into `credentials.json`, and runs the import without
  asking about the previous attempt

#### Scenario: Destroy fails
- **WHEN** Terraform destroy fails
- **THEN** state, private key, `secrets.env` (including the generated secrets), `settings.env`
  (including `DRIVE_IMPORT_LAST_ATTEMPT`) and `credentials.json` are unchanged and the script exits
  non-zero with the Terraform error

#### Scenario: Destroy not confirmed
- **WHEN** the user does not type the deployment ID
- **THEN** nothing is destroyed and the secrets, SSH key and import marker are unchanged

#### Scenario: Unrelated credentials file
- **WHEN** `credentials.json` holds a different OpenSearch password
- **THEN** destroy leaves it in place and says why
