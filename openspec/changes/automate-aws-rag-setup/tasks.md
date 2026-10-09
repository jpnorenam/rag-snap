# Tasks

## 1. Real OpenSearch snap runtime checks (deployment validation)

These checks run against the OpenSearch guest of a real deployment (16 GiB, about 20 GiB of disk);
the current guest runs Ubuntu 26.04 and a 24.04 deployment was also exercised. The `rag-cli.rag`
CLI side runs on the local CLI host, a separate Ubuntu 24.04 machine, so "guest" below always means
the OpenSearch instance. If a result contradicts design.md D5, update design.md, the affected specs
and the bootstrap. Items marked complete below carry live evidence unless noted. Platform coverage:
the interruption-recovery case was exercised on both 24.04 and 26.04; the other runtime checks were
exercised on 26.04, while an earlier 24.04 deployment covered install, roles, heap and reboot.

- [x] 1.1 Install the snap and record `snap list opensearch` (both deployments reported 2.19.4
      rev 98 tracking `2/stable`)
- [x] 1.1a Record the observed `snap connections opensearch` state and the owner, mode and
      existence of the D5 paths (live, on the 26.04 guest: all five required plugs observed
      connected, `process-control` noted `manual`; `opensearch.yml` and `internal_users.yml` 660
      `snap_daemon:root`, `jvm.options.d` and `certificates` 770, `hash.sh` 644 `snap_daemon:root`)
- [x] 1.1b Establish which interfaces auto-connect and whether `snap connect` works for each (live, on
      a fresh 26.04 guest, inspecting `snap connections opensearch` immediately after
      `snap install opensearch --channel=2/stable`: `log-observe`, `mount-observe`,
      `sys-fs-cgroup-service`, `system-observe` and `network`/`network-bind` auto-connect, while
      `process-control` does not and is connected explicitly by the bootstrap, which is why the
      final listing shows it as `manual`)
- [x] 1.2 As guest root, run `OPENSEARCH_JAVA_HOME=/snap/opensearch/current/usr/lib/jvm/java-21-openjdk-amd64 OS_ADMIN_PW=<test> bash <plugins>/opensearch-security/tools/hash.sh -env OS_ADMIN_PW`
      (the credential the packaged tool produced authenticated on both deployments, so the
      fallback JRE was not needed)
- [x] 1.3 Apply sysctl via `/etc/sysctl.d/`, run `opensearch.setup` with roles
      `cluster_manager,data,ingest,ml` and the saved passphrases, size the heap, replace the admin
      hash, start and enable the daemon and initialize security (both deployments completed the
      sequence and reported the expected roles and heap)
- [x] 1.3a Record the HTTP status and body of `/_plugins/_security/authinfo` before
      `security-init`, and whether the packaged `opensearch.yml` auto-initializes the security
      index (live, on a fresh 26.04 guest with the daemon running and security uninitialized:
      three probes six seconds apart each returned `503 OpenSearch Security not initialized.`, so
      the index is not created automatically and `security-init` is required)
- [x] 1.4 Run `opensearch.security-init --tls-priv-key-admin-pass=<pass>` and verify the generated
      admin password authenticates (retrieval and knowledge operations against the deployed
      cluster used the saved credential)
- [x] 1.4a Confirm `admin:admin` is rejected with 401 and record the users in the packaged
      `internal_users.yml` (live, on the 26.04 guest: `admin:admin` returned 401; the packaged
      users are `admin`, `anomalyadmin`, `kibanaserver`, `kibanaro`, `logstash`, `readall` and
      `snapshotrestore`)
- [x] 1.4b Record the `security-init` exit code and duration (live, on a fresh 26.04 guest:
      `snap run opensearch.security-init --tls-priv-key-admin-pass=…` exited 0 in 14.0 s wall clock;
      the saved admin password then returned 200 and `admin:admin` returned 401)
- [x] 1.5 Grep `/var/snap/opensearch/common/ops/snap/logs/*.log` and
      `journalctl -u snap.opensearch.daemon` for the generated password and passphrases; record
      whether any appear and confirm D5's masking covers a hit (live, per label: no match in the
      four guest snap logs, the OpenSearch cluster logs scanned as bounded tails, the daemon
      journal, the current deployment phase logs and the preserved run-log snapshot, or the host
      and guest process tables; the masking that would cover a hit is covered by the automated
      masking tests. No value or matching line was printed.)
- [x] 1.6 Check heap and roles: verified when `GET /_nodes/jvm` shows ~6 GiB max heap on the
      16 GiB guest, `/_cat/nodes?h=node.roles` lists `cluster_manager,data,ingest,ml`, and a reboot
      keeps the sysctl values and restarts the enabled daemon (live on the 26.04 guest: those roles
      and a 6 GiB heap, with `vm.max_map_count`, `vm.swappiness` and `net.ipv4.tcp_retries2` at
      their expected values after a reboot; the 24.04 deployment was also observed reachable with
      unchanged document counts after a reboot, but its sysctl persistence was not recorded)
- [x] 1.7 Re-run `bootstrap-opensearch.sh` after a completed deployment: it reuses the existing
      security configuration, makes no infrastructure changes and reports no plan changes
- [x] 1.7a Interrupt the bootstrap after `opensearch.setup` (before the daemon starts) and after the
      daemon starts (before `security-init`); confirm each resume path matches D5 (live, on
      disposable 24.04 and 26.04 guests, using the shipped bootstrap with one conditional exit added
      at each boundary and a clearly recorded diff: after the first interruption the certificates
      existed, the daemon was `disabled inactive` and no marker existed, and rerunning the unmodified
      bootstrap skipped `opensearch.setup` ("certificates for rag0 already present; not
      regenerating"), reused the same certificate fingerprints, initialized security and reached
      readiness. For the second boundary the pause copy exited 99 with the daemon `enabled active`,
      the marker present and `authinfo` still answering `503 OpenSearch Security not initialized.`;
      rerunning the unmodified bootstrap exited 0, ran `security-init` itself, reported readiness,
      kept the certificate fingerprints identical, authenticated the saved password (200) and
      rejected `admin:admin` (401). No manual `security-init` was used.)
- [x] 1.7b Confirm a wrong saved password fails with the diagnostic without resetting security or
      changing data (live, on an initialized 26.04 guest, with a temporary test secrets file holding
      a synthetic wrong password: the unmodified bootstrap stopped with "the saved OpenSearch admin
      credentials are rejected (HTTP 401). Not re-running setup or security-init", exit non-zero, and
      ran neither `opensearch.setup` nor `security-init`; the real credential, the certificate
      fingerprints and a synthetic marker index were unchanged)
- [x] 1.8 Run the checks above on the 26.04 guest: install and tracking, host sysctl, roles, heap
      and reboot were confirmed there with no differences from 24.04 to record
- [x] 1.8a Complete the remaining open checks (1.1b, 1.3a, 1.4b, 1.7a, 1.7b) on 26.04 too (all five
      were verified on 26.04; only 1.7a was additionally repeated on 24.04, as the platform note
      records)

## 2. CLI credentials file

- [x] 2.1 Add `pkg/credentials` (`Enable`, `Lookup`, file validation per design D6) with table
      tests for: env set, env set empty (file not consulted), file fallback, missing file, missing
      `SNAP_USER_COMMON`, mode 0644, wrong owner (skipped unless root), symlink, unknown key,
      non-string value, malformed JSON, an invalid file left unread when every requested key is in
      the environment, and that no error string contains a value; verified by
      `go test ./pkg/credentials/`
- [x] 2.2 Use `credentials.Lookup` in `knowledge.newClient` (both keys, error hint names the file)
      and in `chat.clientOptions` (now returning an error, propagated by its callers in the chat
      package); verified by `go build ./...` and a test that `Lookup` without `Enable` reads only
      the environment (the ragd contract)
- [x] 2.3 Call `credentials.Enable($SNAP_USER_COMMON/credentials.json)` from `cmd/cli/main.go`
      only; verified by `grep -rn "credentials.Enable" cmd internal` returning only that call
- [x] 2.4 Document the credentials file (format, precedence, permissions, CLI-only scope) in the
      INSTALL.md "Secrets" section and `docs/usage.md`; verified by reading the rendered sections
      against spec `cli-credentials-file`

## 3. Export command and embedded assets

- [x] 3.1 Add `internal/awssetup` with `go:embed` of `assets/` (placeholder files first) and a
      `Write(dir, ctx)` that applies modes from design D1, writes `snap-context.env`, and never
      touches state files; verified by a Go test on a temp dir that checks modes, context contents,
      and that pre-existing `settings.env`, `secrets.env`, `ssh/id_ed25519` and
      `terraform/terraform.tfstate` are byte-identical after a second `Write`
- [x] 3.2 Add `rag-cli.rag prepare-script aws --output <dir>` in `cmd/cli/config/prepare_script.go`,
      registered after `config.SetCommand` in `cmd/cli/main.go`, refusing root, `/tmp`, `/var/tmp`,
      and insecure existing directories; verified by unit tests for the path/ownership checks and
      `rag-cli.rag prepare-script aws --help` output
- [x] 3.3 Document `prepare-script aws` in `docs/usage.md` and start `docs/opensearch-on-aws.md`
      with prerequisites and the export step; link it from INSTALL.md and README.md; verified by
      the documented command running as written
- [x] 3.4 Build and install the snap, run `rag-cli.rag prepare-script aws --output ~/rag-aws-test`
      as a normal user (live: the snap was built and installed; its installation host is the local
      CLI host, which runs Ubuntu 24.04, where the export was exercised, while the OpenSearch guest
      is Ubuntu 26.04. The script fails on a snap-context mismatch, so a successful run proves
      `RAG_SNAP_USER_COMMON` matched `~/snap/<instance>/common`, normally `~/snap/rag-cli/common`)
- [x] 3.4a On the installed snap, confirm the exported file modes match D1 and that
      `--output /tmp/x` is refused with the documented message (live: the 0700/0600 modes, the
      `~/snap/rag-cli/common` context and the refusal with its documented message, with no `/tmp`
      path created)

## 4. Guest bootstrap script

- [x] 4.1 Write `assets/bootstrap-opensearch.sh` implementing D5 stages 1–9; verified by `bash -n`
      and `shellcheck` in the Go test for embedded assets
- [x] 4.2 Add Go tests in `internal/awssetup` that source the script for the heap calculation
      (MemTotal values for 4, 8, 16, 64 GiB), the admin-hash line replacement on a sample with the
      packaged `internal_users.yml` layout (exactly one line changes), and secret masking; verified
      by `go test ./internal/awssetup/`
- [x] 4.3 Run the bootstrap on a fresh real guest (from 9.2, or the group 1 guest) with a test
      secrets file, then re-run it (both releases ran it on a fresh instance and re-ran it,
      reusing the existing security configuration)
- [x] 4.3a Re-run after each interruption point from 1.7a and with a wrong password, and observe
      every `opensearch-bootstrap` scenario (live: the unmodified bootstrap resumed safely after the
      after-setup interruption, including the start → uninitialized → `security-init` branch; the
      after-start case was rerun on both 24.04 and 26.04 and the shipped bootstrap completed it; the
      wrong-password run produced its diagnostic. See 1.7a and 1.7b.)
- [x] 4.3b Confirm the transferred guest secrets file is deleted on the success path and on an
      early failure (live: absent after the successful run; isolated: running the deployed
      bootstrap with an invalid secrets file fails inside `load_secrets` before any stage runs and
      the file is gone)
- [x] 4.3c Exercise the remaining failure paths for that file (live: exiting inside the run at
      either interruption boundary removes the file, so the exit trap is observed, not only
      source-reviewed. The file is deleted by `load_secrets` as soon as it is read — about a second
      after the run starts — so a signal sent later finds it already gone; a signal arriving inside
      that read window, and SIGKILL or power loss, cannot be covered by an EXIT trap and are not
      claimed)
- [x] 4.4 Document the guest steps, the TLS limitation (encrypted, server certificate not
      verified), demo users kept in `internal_users.yml`, and the passphrase handling in
      `docs/opensearch-on-aws.md`; verified against spec `opensearch-bootstrap`

## 5. Terraform templates

- [x] 5.1 Write `assets/terraform/{versions,variables,main,outputs}.tf` per design D4 (dedicated
      VPC/subnet/IGW/route table/security group, key pair from file, instance with an encrypted
      gp3, delete-on-termination root volume sized by `var.root_volume_gib` (default 50) and IMDSv2,
      image from `var.ami_id` re-checked by `data "aws_ami"` with owner `099720109477`, AZ from
      instance-type offerings, `default_tags`); verified by `terraform fmt -check` and
      `terraform init -backend=false && terraform validate` in a scratch copy, and by a review that
      the templates contain no SSM lookup and no literal volume size
- [x] 5.2 Run `terraform plan` against a real account as part of the live trial (setup applied a
      reviewed plan, a rerun reported no changes, and destroy planned the eight deployment
      resources), with the templates `validate`d and reviewed for the deployment tag and
      secret-free variables

## 6. Host script: prerequisites, identity, saved answers

- [x] 6.1 Implement the env-file parser/writer, validators, and prompts (D2) in `assets/opensearch-on-aws.sh`,
      guarded so tests can source the functions without running an action; verified by a bash test
      covering quoting, `$(...)` kept literal, unknown keys, empty-vs-absent values, atomic
      rewrite, and 0600 modes
- [x] 6.2 Implement preflight (root refusal, required commands, snap-context cross-check against
      `getent passwd`, active `ragd` stop), AWS CLI and Terraform reuse with the classic-snap
      offer, refresh and verification (Terraform version range), and the profile-first identity
      gate that offers `aws configure --profile <p>` for a missing profile and keeps an existing
      profile's configuration when its credentials are rejected; verified by tests with stubbed
      `snap`/`aws` for accept, decline, install failure, unsupported version, missing profile and
      rejected credentials
- [x] 6.3 Implement secret generation (32 alphanumeric chars) and one-time `ssh-keygen`, saved
      before any remote step; verified by the live reruns (no infrastructure changes and the same
      credentials), and by the exporter test that a re-export preserves existing state. No unit test
      exercises a second provisioning run
- [x] 6.4 Implement one-time AMI selection (D4): when `AMI_ID` is absent, select the newest
      Canonical Ubuntu image with `aws ec2 describe-images --owners 099720109477` filtered by the
      release's image name (`noble-24.04` or `resolute-26.04`, amd64, HVM, EBS gp3), then verify
      the chosen ID and save `AMI_ID` before the first plan; when present, reuse it and stop if
      its name no longer matches `UBUNTU_RELEASE`; on a lookup failure show the AWS error;
      verified by tests with `aws` stubbed for discovery, the 26.04 filter, reuse of a saved ID,
      and the error message
- [x] 6.5 Implement action dispatch (`setup`, `destroy`, usage on anything else), phase tracking,
      bounded-wait helper, and the failure reporter that names the phase and prints log excerpts
      with every loaded secret value replaced by `***`; verified by a bash test that a failing
      phase's output contains the phase name and no secret value
- [x] 6.6 Document saved files, editing rules, and the authentication guidance in
      `docs/opensearch-on-aws.md`; verified against spec `aws-rag-deployment` requirements on saved answers

## 7. Host script: provisioning and bootstrap orchestration

- [x] 7.1 Implement public IPv4 detection and change prompt, and Terraform
      init/plan/show/confirm/apply of the saved plan (D3 step 5, D4); verified by `shellcheck` and
      live, where the plan carried the saved `AMI_ID` and a rerun showed no plan changes
- [x] 7.1a Confirm the plan carries the saved `ROOT_VOLUME_GIB`, and that the Terraform state and
      plan files are mode 0600 after a real run (live: present state and plan files are 0600, and
      the instance's root volume is 50 GiB gp3, encrypted, matching the saved `ROOT_VOLUME_GIB`;
      isolated: the argument construction is asserted with stubbed Terraform. No repository test
      asserts the `-var` arguments, which is a coverage gap.)
- [x] 7.2 Implement bounded SSH wait, `cloud-init status --wait`, `scp` of the bootstrap, secrets
      via SSH stdin into a 0600 file, remote invocation, and the host-side authenticated check with
      `curl -K -`; verified by `shellcheck` and live, where setup completed against a real instance;
      that a failed `cloud-init` stops the run before anything is copied is covered by an automated
      test, not a live failure
- [x] 7.2a Confirm with `ps` sampling that no secret appears in any host command line (live:
      sampling both machines every 2 s across the bootstrap windows produced 3,499 host lines and
      10,497 guest lines of process output, which the corrected scanner found to contain no match for
      any generated secret in the host process table. The guest process table did match the TLS admin
      key passphrase while the real `security-init` ran, which is the passphrase-as-flag limitation
      already recorded in design D5 and the spec, not a new finding)
- [x] 7.3 Document the provisioning flow, costs note, public-IP caveat, and failure output in
      `docs/opensearch-on-aws.md`; verified by reading it against D3/D4

## 8. Host script: local configuration, models, Drive import

- [x] 8.1 Implement the credentials-file writer (ownership/permission checks on
      `$RAG_SNAP_USER_COMMON`, JSON escaping, `mktemp` + `mv`, replace confirmation) and the
      `sudo rag-cli.rag set --package` + `rag-cli.rag get` verification loop for every key in D3
      step 8; verified by a bash test of the credentials file content and mode, and live, where the
      CLI authenticated from `credentials.json` during setup with nothing exported
- [x] 8.1a Confirm a conflicting user-layer `knowledge.http.host` makes the script stop naming the
      key (live, on the CLI host, with a harmless override on an inert package key rather than the
      real endpoint: the validation routine stopped with the key-specific diagnostic naming the key
      and the override, and the exact prior state was restored — effective value unchanged and no
      user-layer key left behind. An automated isolated stub test of the same routine also passes)
- [x] 8.2 Implement Tika start/enable and bounded readiness wait; verified by
      `curl http://127.0.0.1:9998/tika` succeeding after the phase and `snap services` showing the
      service enabled (the generated snap reported Tika active after setup)
- [x] 8.3 Implement `knowledge init` invocation, model-ID extraction, and persistence; verified by
      a bash test on captured init output (including spinner carriage returns) plus a Go test that
      fails if `printModelID`'s format string changes, and on the installed snap by
      `rag-cli.rag get knowledge.model.embedding` / `.rerank` (the deployed configurations reported
      the model IDs configured)
- [x] 8.4 Implement the Drive decision, credential prompts saved to `secrets.env`, headless OAuth
      guidance (SSH `-L` port forward, or `curl` of the copied redirect URL), import with
      credentials only in the child environment, output shown unchanged (no parsing), the
      "Import command finished; review its output for archive failures." message and
      `DRIVE_IMPORT_LAST_ATTEMPT` on exit 0, a failed phase on non-zero exit, a retry offer when a
      previous attempt is recorded, and no question when `DRIVE_IMPORT=no`; verified by a bash test
      with `rag-cli.rag` stubbed to exit 0 (with an archive error in its output) and 1, and by the
      trial imports, where a rerun filled the remaining knowledge bases after a first attempt whose
      later downloads failed with HTTP 401
- [ ] 8.4a Exercise the headless consent path (`curl` of the copied redirect URL on a machine
      without a browser)
- [x] 8.5 Document local configuration, the credentials file written for the user, `ragd` being
      out of scope, and headless Drive consent in `docs/opensearch-on-aws.md`; verified against D7/D8

## 9. Destroy and end-to-end validation

- [x] 9.1 Implement `destroy` (D9): identity check, saved destroy plan, typed deployment-ID
      confirmation, local cleanup only after success, `credentials.json` removal only on password
      match; verified by a bash test of the cleanup decision and live, where destroy removed the
      eight deployment resources and retained the reusable credentials; documented in
      `docs/opensearch-on-aws.md`
- [x] 9.2 With separate authorization for a live AWS trial: run `setup` with defaults from a fresh
      export, rerun it (no plan changes, same `AMI_ID`, no security re-init), chat against a
      knowledge base, then `destroy`; verified when `aws ec2 describe-*` with the deployment tag
      returns nothing (the trial destroy left no tagged volume, VPC, subnet, security group,
      internet gateway, route table or key pair, and the old volume was gone)
- [x] 9.2a Interrupt a live run once and confirm it resumes safely, and observe the remaining
      `aws-rag-deployment` scenarios that need AWS (live, on a disposable deployment: provisioning
      applied the reviewed plan, the bootstrap was interrupted at the two boundaries and resumed
      safely with certificates, credentials and a synthetic marker index preserved; the interruption
      was introduced at the bootstrap, not inside the host script's own phases)
- [x] 9.3 Build and install the snap, and read INSTALL.md, README.md, `docs/usage.md` and
      `docs/opensearch-on-aws.md` against the implemented commands and `--help` text (live: the snap
      was built and installed; its installation host is the local Ubuntu 24.04 CLI host, and the
      OpenSearch guest runs Ubuntu 26.04. The docs and `--help` were reviewed against the
      implemented commands)
- [ ] 9.3a Run `make all` to completion, including `golangci-lint` (run in an isolated copy with
      `make` and `golangci-lint` 2.14.0 installed: `tidy`, `fmt`, `vet`, `test`, `build` and
      `spec-check` all pass and produced no file drift, but `lint` stops on findings that already
      exist on `main`, so `make all` does not complete. The feature itself contributes none after its
      `errcheck` findings were fixed; the remaining ones are pre-existing and out of scope here.)
