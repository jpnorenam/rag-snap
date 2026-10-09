# Set up an OpenSearch backend on AWS

`rag-cli` can provision a dedicated, single-node OpenSearch cluster on AWS for you and configure
the local CLI, bundled Tika and an existing inference endpoint against it. You do not need a
checkout of this repository.

The confined snap only **exports** the setup files. You then run the exported host script outside
the snap, with your own AWS CLI, Terraform, `ssh` and `sudo`.

- [What it creates](#what-it-creates)
- [Prerequisites](#prerequisites)
- [Export the setup files](#export-the-setup-files)
- [Run the setup](#run-the-setup)
- [Saved answers and secrets](#saved-answers-and-secrets)
- [Reruns and failures](#reruns-and-failures)
- [Google Drive import](#google-drive-import)
- [Destroy](#destroy)
- [Security notes](#security-notes)

## What it creates

The script creates the following resources, all tagged `rag-cli:deployment=<DEPLOYMENT_ID>`:

- A dedicated VPC, subnet, internet gateway and route table.
- A security group. It allows SSH (22) and OpenSearch HTTPS (9200) only from your current public
  IPv4 address as a `/32`.
- An EC2 key pair made from an SSH key generated once in the deployment directory.
- One EC2 instance. The defaults are `t3.xlarge` in `us-east-1`, running Canonical's official
  Ubuntu 24.04 amd64 image (26.04 is also supported), with an encrypted gp3 root volume of 50 GiB.
  You can change the volume size with `ROOT_VOLUME_GIB`.

On the instance, the script installs Canonical's `opensearch` snap from `2/stable` and sets it up:

- node roles `cluster_manager,data,ingest,ml`;
- a JVM heap of 3/8 of RAM, which is 6 GiB on a t3.xlarge;
- a generated `admin` password.

The EC2 instance and its volume cost money while they exist. Remove them with
[`destroy`](#destroy).

## Prerequisites

Before you start, make sure you have:

- The `rag-cli` snap installed. Run the script as your normal user; it calls `sudo` itself for
  `rag-cli.rag set --package`, for starting `rag-cli.tika-server`, and for the optional AWS CLI
  install.
- An AWS account and a configured AWS CLI profile. If `aws` is missing, the script offers to run
  `sudo snap install aws-cli --classic`. The profile needs these permissions:
  - `ec2:DescribeImages`, to find Canonical's Ubuntu image and to check that the saved one is
    Canonical's;
  - permission to create and delete the EC2 and VPC resources listed above.
  If the chosen profile is not configured yet, the script offers to run
  `aws configure --profile <profile>` for you (see [Run the setup](#run-the-setup)).
- [Terraform](https://developer.hashicorp.com/terraform/install), version 1.6 or later. If it is
  missing, the script offers to run `sudo snap install terraform --classic`. Terraform and its
  providers stay external to the snap; Terraform downloads the `hashicorp/aws` provider on first
  use.
- `curl`, `ssh`, `ssh-keygen` and `scp`.
- An existing OpenAI-compatible inference endpoint and its API key, if it needs one. For
  Bedrock, see [bedrock_guide.md](bedrock_guide.md). Provisioning Bedrock access is not part of
  this setup.
- `ragd` stopped (`sudo snap stop rag-cli.ragd`) while the script runs. While `ragd` is running,
  `rag-cli.rag` commands are served by the daemon, whose secrets this script does not manage (see
  [INSTALL.md](../INSTALL.md#secrets)).

## Export the setup files

```bash
rag-cli.rag prepare-script aws --output ~/rag-aws
```

This writes `opensearch-on-aws.sh`, `bootstrap-opensearch.sh`, `terraform/*.tf` and `snap-context.env`.
Keep these rules in mind:

- The directory is created with mode 0700.
- Use a non-hidden directory under your home directory. Inside the snap, `/tmp` and `/var/tmp` are
  private and not the host's, so the command refuses them.
- Do not run the command with `sudo`.
- Re-running it refreshes the scripts and templates and leaves your answers, secrets, keys and
  Terraform state alone.

## Run the setup

```bash
cd ~/rag-aws
./opensearch-on-aws.sh setup
```

The script runs these phases in order:

1. **AWS identity.** It asks for the AWS profile and immediately runs
   `aws --profile <profile> sts get-caller-identity`.
   - If the profile does not exist yet, the script offers to run
     `aws configure --profile <profile>` for you. That command stores an access key ID and secret
     access key in your AWS configuration and runs interactively; the script does not capture,
     log or save it. If you use SSO, an instance role, or another method, answer no, configure it
     yourself, and rerun. After configuring, the identity check runs again in the same run.
   - If the profile exists but its credentials are rejected or expired, the script keeps your
     configuration and stops with guidance (for example `aws sso login --profile <profile>`).
   Either way it stops before asking anything else.
2. **Answers.** It asks for region, instance type, Ubuntu release, root volume size, OpenSearch
   channel, allowed address, inference endpoint (host, port, base path, TLS, model, optional API
   key), and whether to import from Google Drive. Enter accepts the default shown in brackets.
3. **Secrets and SSH key.** It generates the OpenSearch admin password, the three TLS key
   passphrases and the SSH key, and saves them before any remote change.
4. **Image.** It resolves the current Canonical image for the release in the region, checks that
   Canonical owns it, and saves it as `AMI_ID`. Later runs keep using the same image.
5. **Terraform.** It saves a plan, shows it and applies exactly that plan after you type `yes`.
6. **Guest bootstrap.** It copies `bootstrap-opensearch.sh` to the instance, sends the secrets over
   SSH standard input, and runs the bootstrap. The bootstrap initializes security and then waits
   (up to three minutes) for the node to accept the admin password, because the security index can
   lag the command that creates it; it never re-runs security initialization during that wait. It
   then checks from your machine that `https://<ip>:9200/` accepts the admin password.
7. **Local configuration.** It writes `~/snap/rag-cli/common/credentials.json` (see
   [Credentials file](../INSTALL.md#credentials-file)), then sets
   `knowledge.http.{host,port,tls}`, `tika.http.{host,port,path}`,
   `chat.http.{host,port,path,tls}` and `chat.model`. Each key is set with
   `sudo rag-cli.rag set --package` and checked with `rag-cli.rag get`. Finally it starts and
   enables `rag-cli.tika-server`.
8. **Models.** It runs `rag-cli.rag knowledge init` and saves the reported embedding and rerank
   model IDs as `knowledge.model.embedding` and `knowledge.model.rerank`.
9. **Drive import**, if you chose it (see [Google Drive import](#google-drive-import)).

When setup finishes, ordinary `rag-cli.rag` commands work from any new shell, with no `export`
and no shell-profile edits:

```bash
rag-cli.rag k create default
rag-cli.rag chat
```

The instance's public IP is not an Elastic IP. If you stop and start the instance, rerun
`./opensearch-on-aws.sh setup` so that `knowledge.http.host` is updated.

## Saved answers and secrets

The script keeps everything for this deployment in the deployment directory. Files have mode 0600
and directories have mode 0700.

| File | Contents |
| --- | --- |
| `settings.env` | Answers such as `AWS_PROFILE`, `AWS_REGION`, `INSTANCE_TYPE`, `UBUNTU_RELEASE`, `ROOT_VOLUME_GIB`, `AMI_ID`, `OPENSEARCH_CHANNEL`, `ALLOWED_CIDR`, `DEPLOYMENT_ID`, `CHAT_*`, `DRIVE_IMPORT`, `DRIVE_FOLDER_URL`, `DRIVE_IMPORT_LAST_ATTEMPT` |
| `secrets.env` | `OPENSEARCH_ADMIN_PASSWORD`, `TLS_ROOT_PASS`, `TLS_ADMIN_PASS`, `TLS_NODE_PASS`, `CHAT_API_KEY`, `GOOGLE_DRIVE_CLIENT_ID`, `GOOGLE_DRIVE_CLIENT_SECRET` |
| `ssh/` | The generated SSH key and `known_hosts` |
| `terraform/` | The templates, local state and saved plans |
| `logs/` | The output of the last run of each phase, with secret values masked |

Both `.env` files are plain `KEY=value` lines that you may edit. The script reads them as data
and never sources them, so `$`, backticks and `$(...)` are never expanded. A value may be written
in three ways:

| Form | Meaning |
| --- | --- |
| `KEY="value"` | The format the script writes. Inside the quotes, `\"` is a literal `"` and `\\` is a literal `\`; everything else is literal. |
| `KEY='value'` | Taken literally; the value cannot contain `'`. |
| `KEY=value` | Everything after the first `=`, taken literally. It must not start with a quote. |

Other rules:

- An unknown key stops the script and names the line.
- The script asks only for missing or invalid values.
- An empty value (`KEY=` or `KEY=""`) is a saved "none" answer. For example, `CHAT_API_KEY=""`
  means no API key.
- To pick a newer Ubuntu image, delete the `AMI_ID` line. The next plan then shows the instance
  replacement, and you review it before anything is applied.

## Reruns and failures

Rerunning `./opensearch-on-aws.sh setup` is safe:

- It reuses the saved answers, secrets, SSH key, AMI and Terraform state.
- It asks for confirmation only when the plan has changes.
- On the instance, the bootstrap checks each stage against the node's real state:
  - it never regenerates certificates once the daemon has started;
  - it never re-runs security initialization on an initialized cluster.
- If the saved password is rejected, the bootstrap stops and says whether `admin:admin` is still
  active. It does not reset security.

Every wait is bounded. When something fails, the script names the phase and shows the end of that
phase's log, with secrets masked. On the instance, the bootstrap names the stage and shows the
daemon journal and the OpenSearch log.

## Google Drive import

If you answer `yes`, the script asks for the Drive folder URL and your Google OAuth client ID and
secret, and saves them in `secrets.env`. The client must be a "Desktop app" client; creating it is
up to you. The script then runs the existing importer:

```bash
rag-cli.rag knowledge import --url <folder-url> --all
```

The client ID and secret are passed only in that command's environment. The client ID is a public
OAuth identifier: the printed authorization URL shows it unchanged, so you can open that URL as
printed. The client secret is masked (`***`) wherever the script shows or logs output.

Google sign-in uses a local callback on `127.0.0.1:<port>` on the machine running the script. The
`<port>` appears in the `redirect_uri` of the printed URL. If no browser can open on that machine,
use one of these:

- On the machine with the browser, run `ssh -L <port>:127.0.0.1:<port> <you>@<this-host>`, then open
  the printed URL there.
- Open the URL anywhere and finish consent. Copy the address of the `http://127.0.0.1:<port>/?...`
  page that fails to load, and run `curl '<that address>'` on the machine running the script within
  5 minutes.

The importer can report a failed archive and still exit successfully; this is a known issue in
the importer. After a successful exit the script prints "Import command finished; review its
output for archive failures." and records `DRIVE_IMPORT_LAST_ATTEMPT`. That only means an attempt
finished. On later runs it offers to run the import again. A non-zero exit fails the setup phase.
A saved `DRIVE_IMPORT=no` is not asked again.

## Destroy

```bash
./opensearch-on-aws.sh destroy
```

Destroy works in two steps.

1. **AWS resources.** The script checks the AWS profile, shows a saved destroy plan, and asks you
   to type the deployment ID. It then removes every AWS resource the deployment created,
   including the instance's volume and the key pair. Nothing local is deleted until this step
   succeeds.
2. **Local cleanup.** After a successful destroy the deployment-specific material goes, and what
   you can reuse stays, so redeploying needs no re-entry:
   - deleted: the generated `OPENSEARCH_ADMIN_PASSWORD`, `TLS_ROOT_PASS`, `TLS_ADMIN_PASS` and
     `TLS_NODE_PASS` in `secrets.env` (the next setup makes new ones), `ssh/` and the plan files;
   - `DRIVE_IMPORT_LAST_ATTEMPT` is cleared in `settings.env`, so a new deployment with
     `DRIVE_IMPORT=yes` runs the import instead of treating the destroyed cluster's attempt as
     done;
   - kept: `CHAT_API_KEY`, `GOOGLE_DRIVE_CLIENT_ID` and `GOOGLE_DRIVE_CLIENT_SECRET` in
     `secrets.env` (including an explicitly empty API key), and every saved answer in
     `settings.env` (`DRIVE_IMPORT`, `DRIVE_FOLDER_URL`, region, instance type, `AMI_ID` and so on);
   - `credentials.json` is deleted only if it still holds this deployment's password; the next
     setup writes the preserved API key back into it;
   - the cached Google OAuth token (`$SNAP_USER_DATA/gdrive-token.json`) is not touched;
   - `settings.env`, the Terraform state and the logs are kept;
   - rag-cli configuration, the Tika service and your tools are not changed, so
     `knowledge.http.host` still points at the destroyed address.

   A declined or failed destroy changes none of this: the secrets, the SSH key and the import
   marker all stay as they were.

## Security notes

- **TLS:** connections between rag-cli and OpenSearch are encrypted, but rag-cli does not verify
  the server certificate. This is existing client behaviour. Your protection is the `/32` ingress
  rule.
- **Password:** the `admin` password is set by replacing only the admin hash in the snap's
  `internal_users.yml` before security initialization. It is never passed to Terraform, stored in
  Terraform state, or put in user-data. `admin:admin` does not work afterwards.
- **Demo users:** the OpenSearch package also ships demo users (`anomalyadmin`, `kibanaserver`,
  `kibanaro`, `logstash`, `readall`, `snapshotrestore`) with published default passwords. They are
  kept unchanged and are reachable only from the allowed `/32`.
- **Passphrases:** the snap's `opensearch.setup` and `opensearch.security-init` accept TLS key
  passphrases only as command-line arguments. They therefore appear briefly in the instance's
  process list. They never appear in a command line on your machine or in the logs.
