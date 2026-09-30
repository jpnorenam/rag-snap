package awssetup

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteModesContextAndPreservedState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rag-aws")
	if err := Write(dir, "rag-cli", "/home/alice/snap/rag-cli/common"); err != nil {
		t.Fatal(err)
	}
	wantModes := map[string]os.FileMode{
		".":                       0o700,
		"opensearch-on-aws.sh":    0o700,
		"bootstrap-opensearch.sh": 0o600,
		"terraform":               0o700,
		"terraform/main.tf":       0o600,
		"terraform/variables.tf":  0o600,
		"terraform/versions.tf":   0o600,
		"terraform/outputs.tf":    0o600,
		ContextFile:               0o600,
	}
	for rel, want := range wantModes {
		fi, err := os.Stat(filepath.Join(dir, rel))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != want {
			t.Errorf("%s: mode %o, want %o", rel, fi.Mode().Perm(), want)
		}
	}
	ctx, _ := os.ReadFile(filepath.Join(dir, ContextFile))
	if string(ctx) != "RAG_SNAP_INSTANCE=rag-cli\nRAG_SNAP_USER_COMMON=/home/alice/snap/rag-cli/common\n" {
		t.Errorf("context file: %q", ctx)
	}

	state := map[string]string{
		"settings.env":                "AWS_PROFILE=dev\n",
		"secrets.env":                 "OPENSEARCH_ADMIN_PASSWORD=x\n",
		"ssh/id_ed25519":              "key",
		"terraform/terraform.tfstate": "{}",
	}
	for rel, content := range state {
		p := filepath.Join(dir, rel)
		mustMkdir(t, filepath.Dir(p), 0o700)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, filepath.Join(dir, "opensearch-on-aws.sh"), []byte("stale"), 0o700)
	if err := Write(dir, "rag-cli", "/home/alice/snap/rag-cli/common"); err != nil {
		t.Fatal(err)
	}
	for rel, content := range state {
		got, _ := os.ReadFile(filepath.Join(dir, rel))
		if string(got) != content {
			t.Errorf("%s changed on re-export", rel)
		}
	}
	shipped, _ := assets.ReadFile("assets/opensearch-on-aws.sh")
	got, _ := os.ReadFile(filepath.Join(dir, "opensearch-on-aws.sh"))
	if !bytes.Equal(got, shipped) {
		t.Error("opensearch-on-aws.sh was not refreshed")
	}
}

func TestWriteRejectsUnsafeLocations(t *testing.T) {
	open := filepath.Join(t.TempDir(), "open")
	mustMkdir(t, open, 0o700)
	mustChmod(t, open, 0o777)
	if err := Write(open, "rag-cli", "/c"); err == nil || !strings.Contains(err.Error(), "writable by group or others") {
		t.Errorf("0777 dir: %v", err)
	}
	if err := Write(filepath.Join(t.TempDir(), "x"), "", ""); err == nil {
		t.Error("missing snap context accepted")
	}
}

func TestScriptsParse(t *testing.T) {
	for _, s := range []string{"assets/opensearch-on-aws.sh", "assets/bootstrap-opensearch.sh"} {
		if out, err := exec.Command("bash", "-n", s).CombinedOutput(); err != nil {
			t.Errorf("bash -n %s: %v\n%s", s, err, out)
		}
		if _, err := exec.LookPath("shellcheck"); err == nil {
			if out, err := exec.Command("shellcheck", "-s", "bash", s).CombinedOutput(); err != nil {
				t.Errorf("shellcheck %s:\n%s", s, out)
			}
		}
	}
}

// bash runs a snippet with the given asset sourced (its main is not run).
func bash(t *testing.T, asset, snippet string, env ...string) (string, error) {
	return bashIn(t, asset, "", snippet, env...)
}

// bashIn is bash with the snippet's standard input, so prompts can be answered.
func bashIn(t *testing.T, asset, stdin, snippet string, env ...string) (string, error) {
	t.Helper()
	abs, _ := filepath.Abs(filepath.Join("assets", asset))
	cmd := exec.Command("bash", "-c", "source "+abs+"\n"+snippet)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// stubPath builds a PATH of only the given stubs plus the base system
// directories, so "command -v terraform" (or aws) is deterministic. sudo is
// stubbed too: the real one resolves commands through secure_path and would
// bypass the stubs, so a test could install a snap for real.
func stubPath(t *testing.T, scripts map[string]string) (dir, path string) {
	t.Helper()
	dir = t.TempDir()
	scripts["sudo"] = `exec "$@"`
	for name, body := range scripts {
		mustWrite(t, filepath.Join(dir, name), []byte("#!/usr/bin/env bash\n"+body+"\n"), 0o755)
	}
	return dir, "PATH=" + dir + ":/usr/bin:/bin"
}

func TestEnvFilesAreData(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "settings.env")
	mustWrite(t, f, []byte("# comment\n\nCHAT_MODEL=$(touch "+dir+"/pwned)\nAWS_REGION=\"eu-west-1\"\nCHAT_PATH='a b'\nDRIVE_IMPORT=\n"), 0o600)
	out, err := bash(t, "opensearch-on-aws.sh", `
		load_env "$F" "${SETTINGS_KEYS[@]}"
		printf '%s|%s|%s|%s|%s\n' "${CFG[CHAT_MODEL]}" "${CFG[AWS_REGION]}" "${CFG[CHAT_PATH]}" "$(has DRIVE_IMPORT && echo set)" "$(has AMI_ID || echo absent)"
		save_env "$F" "${SETTINGS_KEYS[@]}"; stat -c %a "$F"; cat "$F"`, "F="+f)
	if err != nil {
		t.Fatal(err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
		t.Fatal("value was executed")
	}
	want := "$(touch " + dir + "/pwned)|eu-west-1|a b|set|absent\n600\n"
	if !strings.HasPrefix(out, want) || !strings.Contains(out, "DRIVE_IMPORT=\"\"\n") {
		t.Errorf("got:\n%s", out)
	}

	mustWrite(t, f, []byte("AWS_REGION=us-east-1\nBOGUS=1\n"), 0o600)
	if out, err := bash(t, "opensearch-on-aws.sh", `load_env "$F" "${SETTINGS_KEYS[@]}"`, "F="+f); err == nil || !strings.Contains(out, "line 2: unknown key BOGUS") {
		t.Errorf("unknown key not rejected: %v %s", err, out)
	}
}

func TestEnvValuesRoundTrip(t *testing.T) {
	values := []string{`"quoted"`, `'single'`, `back\slash\\two`, "a b  c", "k=v=w", "$HOME $(id) `x`", "", `end\`, `\"mixed\"`, " lead"}
	f := filepath.Join(t.TempDir(), "secrets.env")
	for _, v := range values {
		out, err := bash(t, "opensearch-on-aws.sh", `CFG[CHAT_API_KEY]=$V; save_env "$F" CHAT_API_KEY; unset 'CFG[CHAT_API_KEY]'
			load_env "$F" CHAT_API_KEY; has CHAT_API_KEY && printf '%s' "${CFG[CHAT_API_KEY]}"`, "F="+f, "V="+v)
		if err != nil || out != v {
			t.Errorf("value %q came back as %q (%v)", v, out, err)
		}
	}
	mustWrite(t, f, []byte("CHAT_API_KEY=\"unbalanced\n"), 0o600)
	if out, err := bash(t, "opensearch-on-aws.sh", `load_env "$F" CHAT_API_KEY`, "F="+f); err == nil || !strings.Contains(out, "unbalanced quotes") {
		t.Errorf("unbalanced quotes accepted: %v %s", err, out)
	}
}

func TestCloudInitFailureStopsBootstrap(t *testing.T) {
	calls := filepath.Join(t.TempDir(), "calls")
	path := stubs(t, map[string]string{
		"ssh": `echo "ssh $*" >>` + calls + `
case "$*" in *cloud-init*) echo "status: error"; echo "errors: - module failed"; exit 1;; esac`,
		"scp": `echo "scp $*" >>` + calls,
	})
	dir := t.TempDir()
	out, err := bash(t, "opensearch-on-aws.sh", `LOG_DIR=$D/logs IP=192.0.2.10 SSH_KEY=$D/key KNOWN_HOSTS=$D/kh
		CFG[OPENSEARCH_ADMIN_PASSWORD]=pw1 CFG[TLS_ROOT_PASS]=pw2 CFG[TLS_ADMIN_PASS]=pw3 CFG[TLS_NODE_PASS]=pw4
		bootstrap; echo "NOT REACHED"`, path, "D="+dir)
	c, _ := os.ReadFile(calls)
	if err == nil || strings.Contains(out, "NOT REACHED") {
		t.Fatalf("cloud-init failure reported as success:\n%s", out)
	}
	if !strings.Contains(out, "did not complete successfully (exit 1)") || !strings.Contains(out, "module failed") {
		t.Errorf("missing diagnostic:\n%s", out)
	}
	if strings.Contains(string(c), "scp ") || strings.Contains(string(c), "secrets.env") || strings.Contains(string(c), "bootstrap-opensearch") {
		t.Errorf("continued after cloud-init failure:\n%s", c)
	}
}

// The script's own preflight needs a rag-cli command and a snap that reports no
// running ragd service.
func baseStubs(calls string) map[string]string {
	return map[string]string{
		"rag-cli.rag": "exit 1",
		"aws":         "exit 0",
		"snap": `echo "snap $*" >>` + calls + `
case "$*" in services*) echo "Service Startup Current Notes";; esac`,
	}
}

func TestTerraformInstallOffered(t *testing.T) {
	run := func(t *testing.T, answer, snapBody string) (string, string, error) {
		calls := filepath.Join(t.TempDir(), "calls")
		stubs := baseStubs(calls)
		if snapBody != "" {
			stubs["snap"] = snapBody
		}
		_, path := stubPath(t, stubs)
		out, err := bashIn(t, "opensearch-on-aws.sh", answer, `LOG_DIR=$D/logs CFG[RAG_SNAP_INSTANCE]=rag-cli RAG=rag-cli.rag ACTION=setup preflight`, path, "D="+t.TempDir())
		c, _ := os.ReadFile(calls)
		return out, string(c), err
	}

	t.Run("accept installs and verifies", func(t *testing.T) {
		calls := filepath.Join(t.TempDir(), "calls")
		bin, path := stubPath(t, baseStubs(calls))
		// The snap stub creates terraform, as a successful install would.
		mustWrite(t, filepath.Join(bin, "snap"), []byte("#!/usr/bin/env bash\necho \"snap $*\" >>"+calls+"\n"+
			"case \"$*\" in *'install terraform'*) printf '#!/usr/bin/env bash\\necho \"Terraform v1.16.4\"\\n' >"+bin+"/terraform; chmod +x "+bin+"/terraform;; esac\n"), 0o755)
		out, err := bashIn(t, "opensearch-on-aws.sh", "y\n", `LOG_DIR=$D/logs CFG[RAG_SNAP_INSTANCE]=rag-cli RAG=rag-cli.rag ACTION=setup preflight`, path, "D="+t.TempDir())
		c, _ := os.ReadFile(calls)
		if err != nil || !strings.Contains(string(c), "install terraform --classic") {
			t.Fatalf("install not offered/accepted: %v\n%s\n%s", err, out, c)
		}
		if !strings.Contains(out, "terraform: ") || !strings.Contains(out, "v1.16.4") {
			t.Errorf("installed Terraform not verified:\n%s", out)
		}
	})

	t.Run("decline stops", func(t *testing.T) {
		out, calls, err := run(t, "n\n", "")
		if err == nil || !strings.Contains(out, "install Terraform yourself") {
			t.Fatalf("%v\n%s", err, out)
		}
		if strings.Contains(calls, "install") {
			t.Errorf("installed after decline: %s", calls)
		}
	})

	t.Run("install failure stops", func(t *testing.T) {
		calls := filepath.Join(t.TempDir(), "calls")
		stubs := baseStubs(calls)
		stubs["snap"] = `echo "snap $*" >>` + calls + `
case "$*" in *'install terraform'*) echo "error: snap not found" >&2; exit 1;; services*) echo Notes;; esac`
		_, path := stubPath(t, stubs)
		out, err := bashIn(t, "opensearch-on-aws.sh", "y\n", `LOG_DIR=$D/logs CFG[RAG_SNAP_INSTANCE]=rag-cli RAG=rag-cli.rag ACTION=setup preflight`, path, "D="+t.TempDir())
		if err == nil || !strings.Contains(out, "could not install the Terraform snap") {
			t.Fatalf("%v\n%s", err, out)
		}
	})

	t.Run("installed Terraform is reused and version-checked", func(t *testing.T) {
		calls := filepath.Join(t.TempDir(), "calls")
		stubs := baseStubs(calls)
		stubs["terraform"] = `echo "Terraform v1.16.4"`
		_, path := stubPath(t, stubs)
		out, err := bashIn(t, "opensearch-on-aws.sh", "", `LOG_DIR=$D/logs CFG[RAG_SNAP_INSTANCE]=rag-cli RAG=rag-cli.rag ACTION=setup preflight`, path, "D="+t.TempDir())
		c, _ := os.ReadFile(calls)
		if err != nil || strings.Contains(string(c), "install terraform") {
			t.Fatalf("reuse: %v\n%s\n%s", err, out, c)
		}
	})

	t.Run("unsupported version stops", func(t *testing.T) {
		calls := filepath.Join(t.TempDir(), "calls")
		stubs := baseStubs(calls)
		stubs["terraform"] = `echo "Terraform v1.4.0"`
		_, path := stubPath(t, stubs)
		out, err := bashIn(t, "opensearch-on-aws.sh", "", `LOG_DIR=$D/logs CFG[RAG_SNAP_INSTANCE]=rag-cli RAG=rag-cli.rag ACTION=setup preflight`, path, "D="+t.TempDir())
		if err == nil || !strings.Contains(out, "Terraform 1.4.0 is not supported") {
			t.Fatalf("%v\n%s", err, out)
		}
	})
}

func TestAWSProfileResolution(t *testing.T) {
	// awsStub records its calls; the profile only becomes usable once
	// "configure" has run, and only when the test expects it to be missing.
	awsStub := func(calls, marker string, profileExists bool) string {
		list := "exit 0" // no profiles configured
		if profileExists {
			list = `echo "dev"`
		}
		return `echo "aws $*" >>` + calls + `
case "$*" in
  "configure list-profiles") ` + list + `;;
  "configure --profile dev") echo configure >>` + calls + `; : >` + marker + `; exit 0;;
  *sts*) [ -f ` + marker + ` ] && { echo arn:aws:iam::123456789012:user/tester; exit 0; } || { echo "An error occurred (InvalidClientTokenId): the security token is invalid" >&2; exit 255; };;
esac`
	}
	setup := func(t *testing.T, profileExists bool, answer string) (string, string, error) {
		dir := t.TempDir()
		calls := filepath.Join(dir, "calls")
		marker := filepath.Join(dir, "configured")
		_, path := stubPath(t, map[string]string{"aws": awsStub(calls, marker, profileExists)})
		out, err := bashIn(t, "opensearch-on-aws.sh", answer, `LOG_DIR=$D/logs SETTINGS=$D/settings.env SECRETS=$D/secrets.env
			CFG[AWS_REGION]=us-east-1 RAG=rag-cli.rag ACTION=setup
			resolve_identity`, path, "D="+dir)
		c, _ := os.ReadFile(calls)
		return out, string(c), err
	}

	t.Run("missing profile is offered configure and continues", func(t *testing.T) {
		out, calls, err := setup(t, false, "dev\ny\n")
		if err != nil || !strings.Contains(out, "is not configured") || !strings.Contains(out, "access key") {
			t.Fatalf("%v\n%s", err, out)
		}
		if !strings.Contains(calls, "configure --profile dev") || !strings.Contains(out, "AWS identity: arn:") {
			t.Fatalf("configure not run or identity not verified:\n%s\n%s", out, calls)
		}
	})

	t.Run("declining configure stops", func(t *testing.T) {
		out, calls, err := setup(t, false, "dev\nn\n")
		if err == nil || !strings.Contains(out, "configure profile 'dev' yourself") {
			t.Fatalf("%v\n%s", err, out)
		}
		if strings.Contains(calls, "configure --profile") {
			t.Errorf("configure ran after decline: %s", calls)
		}
	})

	t.Run("existing profile with rejected credentials keeps its configuration", func(t *testing.T) {
		out, calls, err := setup(t, true, "dev\n")
		if err == nil || !strings.Contains(out, "AWS rejected the credentials for profile 'dev'") {
			t.Fatalf("%v\n%s", err, out)
		}
		if !strings.Contains(out, "aws sso login --profile dev") {
			t.Errorf("no authentication guidance:\n%s", out)
		}
		if strings.Contains(calls, "configure --profile") {
			t.Errorf("an existing profile was reconfigured: %s", calls)
		}
		if strings.Contains(out, "InvalidClientTokenId") == false {
			t.Errorf("underlying AWS error not shown:\n%s", out)
		}
	})
}

func TestEC2ImageDiscoveryAndReuse(t *testing.T) {
	// The catalogue returns an image whose name matches the requested release, so a
	// selection for 26.04 is exercised as well as 24.04.
	discover := func(t *testing.T, release string) (string, string, error) {
		dir := t.TempDir()
		calls := filepath.Join(dir, "calls")
		aws := `echo "aws $*" >>` + calls + `
case "$*" in
  *describe-images*name,Values=*resolute-26.04*) echo ami-0bbbbbbbbbbbbbbb1;;
  *describe-images*name,Values=*noble-24.04*) echo ami-0aaaaaaaaaaaaaaa1;;
  *describe-images*image-ids*ami-0bbbbbbbbbbbbbbb1*)
    printf 'ami-0bbbbbbbbbbbbbbb1\tx86_64\tubuntu/images/hvm-ssd-gp3/ubuntu-resolute-26.04-amd64-server-20260401\n';;
  *describe-images*image-ids*ami-0aaaaaaaaaaaaaaa1*)
    printf 'ami-0aaaaaaaaaaaaaaa1\tx86_64\tubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-20260101\n';;
  *ssm*) echo "SSM CALLED" >>` + calls + `; echo ami-bad;;
esac`
		_, path := stubPath(t, map[string]string{"aws": aws})
		out, err := bashIn(t, "opensearch-on-aws.sh", "", `LOG_DIR=$D/logs SETTINGS=$D/settings.env SECRETS=$D/secrets.env
			CFG[AWS_PROFILE]=dev CFG[AWS_REGION]=us-east-1 CFG[UBUNTU_RELEASE]=`+release+`
			RAG=rag-cli.rag ACTION=setup
			select_ami; echo "AMI=${CFG[AMI_ID]}"`, path, "D="+dir)
		c, _ := os.ReadFile(calls)
		return out, string(c), err
	}

	t.Run("selects the newest Canonical image with EC2 DescribeImages", func(t *testing.T) {
		out, calls, err := discover(t, "24.04")
		if err != nil || !strings.Contains(out, "AMI=ami-0aaaaaaaaaaaaaaa1") {
			t.Fatalf("%v\n%s", err, out)
		}
		if strings.Contains(calls, "SSM CALLED") {
			t.Errorf("SSM was still used: %s", calls)
		}
		for _, want := range []string{"--owners 099720109477", "ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*",
			"Name=architecture,Values=x86_64", "Name=virtualization-type,Values=hvm", "Name=root-device-type,Values=ebs",
			"sort_by(@, &CreationDate)"} {
			if !strings.Contains(calls, want) {
				t.Errorf("discovery call lacks %q:\n%s", want, calls)
			}
		}
	})

	t.Run("uses the 26.04 filter for resolute and saves the matching image", func(t *testing.T) {
		out, calls, err := discover(t, "26.04")
		if err != nil || !strings.Contains(out, "AMI=ami-0bbbbbbbbbbbbbbb1") {
			t.Fatalf("26.04 selection failed: %v\n%s", err, out)
		}
		if !strings.Contains(calls, "ubuntu/images/hvm-ssd-gp3/ubuntu-resolute-26.04-amd64-server-*") {
			t.Errorf("26.04 filter wrong:\n%s", calls)
		}
	})

	t.Run("reuses a saved AMI_ID without rediscovering", func(t *testing.T) {
		dir := t.TempDir()
		calls := filepath.Join(dir, "calls")
		aws := `echo "aws $*" >>` + calls + `
case "$*" in
  *describe-images*name,Values=*) echo "REDISCOVERED" >>` + calls + `; echo ami-ffffffffffffffff0;;
  *describe-images*image-ids*) printf 'ami-0123456789abcdef0\tx86_64\tubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-20260101\n';;
esac`
		_, path := stubPath(t, map[string]string{"aws": aws})
		out, err := bashIn(t, "opensearch-on-aws.sh", "", `LOG_DIR=$D/logs SETTINGS=$D/settings.env SECRETS=$D/secrets.env
			CFG[AWS_PROFILE]=dev CFG[AWS_REGION]=us-east-1 CFG[UBUNTU_RELEASE]=24.04 CFG[AMI_ID]=ami-0123456789abcdef0
			select_ami; echo "AMI=${CFG[AMI_ID]}"`, path, "D="+dir)
		c, _ := os.ReadFile(calls)
		if err != nil || !strings.Contains(out, "AMI=ami-0123456789abcdef0") {
			t.Fatalf("%v\n%s", err, out)
		}
		if strings.Contains(string(c), "REDISCOVERED") {
			t.Errorf("saved AMI was rediscovered: %s", c)
		}
	})

	t.Run("shows the AWS error when the lookup fails", func(t *testing.T) {
		dir := t.TempDir()
		_, path := stubPath(t, map[string]string{"aws": `echo "An error occurred (UnauthorizedOperation) when calling the DescribeImages operation" >&2; exit 254`})
		out, err := bashIn(t, "opensearch-on-aws.sh", "", `LOG_DIR=$D/logs SETTINGS=$D/settings.env SECRETS=$D/secrets.env
			CFG[AWS_PROFILE]=dev CFG[AWS_REGION]=us-east-1 CFG[UBUNTU_RELEASE]=24.04
			select_ami`, path, "D="+dir)
		if err == nil || !strings.Contains(out, "UnauthorizedOperation") {
			t.Fatalf("underlying AWS error not shown: %v\n%s", err, out)
		}
	})
}

func TestDriveAuthorizationURLIsNotRedacted(t *testing.T) {
	const (
		clientID = "1234567890-abc123def.apps.googleusercontent.com"
		secret   = "GOCSPX-synthetic-client-secret"
		authURL  = "https://accounts.google.com/o/oauth2/v2/auth?client_id=" + clientID +
			"&redirect_uri=http%3A%2F%2F127.0.0.1%3A45321&response_type=code" +
			"&scope=https%3A%2F%2Fwww.googleapis.com%2Fauth%2Fdrive.readonly&access_type=offline" +
			"&prompt=consent&state=St4t3Nonce&code_challenge=PKCEcH4ll3ng3&code_challenge_method=S256"
	)
	// A stub importer that prints a realistic consent URL and, separately, the client
	// secret. Nothing here contacts Google.
	importer := `echo "To authenticate with Google Drive, open the following URL in your browser:"; ` +
		`echo "` + authURL + `"; echo "client secret in use: $GOOGLE_DRIVE_CLIENT_SECRET"`
	_, path := stubPath(t, map[string]string{"rag-cli.rag": importer})

	dir := t.TempDir()
	out, err := bash(t, "opensearch-on-aws.sh", `LOG_DIR=$D/logs SETTINGS=$D/settings.env SECRETS=$D/secrets.env RAG=rag-cli.rag
		CFG[DRIVE_IMPORT]=yes CFG[DRIVE_FOLDER_URL]=https://drive.google.com/drive/folders/x
		CFG[GOOGLE_DRIVE_CLIENT_ID]=`+clientID+`
		CFG[GOOGLE_DRIVE_CLIENT_SECRET]=`+secret+`
		drive_import`, path, "D="+dir)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	log, _ := os.ReadFile(filepath.Join(dir, "logs", "drive.log"))

	if !strings.Contains(out, authURL) {
		t.Errorf("authorization URL was altered on the terminal:\n%s", out)
	}
	for _, part := range []string{"client_id=" + clientID, "redirect_uri=", "state=St4t3Nonce", "code_challenge=PKCEcH4ll3ng3"} {
		if !strings.Contains(out, part) {
			t.Errorf("terminal output lost %q:\n%s", part, out)
		}
	}
	if strings.Contains(out, "client_id=***") || strings.Contains(string(log), "client_id=***") {
		t.Errorf("the client ID was redacted:\n%s", out)
	}
	for name, text := range map[string]string{"terminal": out, "phase log": string(log)} {
		if strings.Contains(text, secret) {
			t.Errorf("%s leaked the client secret:\n%s", name, text)
		}
		if !strings.Contains(text, "***") {
			t.Errorf("%s did not mask the client secret:\n%s", name, text)
		}
	}

	// Every stored secret except the public client ID must still be masked.
	invariant, err := bash(t, "opensearch-on-aws.sh", `
		missing=
		for k in "${SECRET_KEYS[@]}"; do
			[ "$k" = GOOGLE_DRIVE_CLIENT_ID ] && continue
			found=0
			for m in "${MASKED_KEYS[@]}"; do [ "$m" = "$k" ] && found=1; done
			[ "$found" = 1 ] || missing="$missing $k"
		done
		[ -z "$missing" ] && echo all-masked || echo "unmasked:$missing"`)
	if err != nil || !strings.Contains(invariant, "all-masked") {
		t.Errorf("a stored secret is no longer masked: %v %s", err, invariant)
	}
}

func TestSecurityInitWaitsForReadiness(t *testing.T) {
	// 503 then 200: the lag between security-init and a usable security index. The
	// counter is a file because os_curl runs inside a command substitution.
	out, err := bash(t, "bootstrap-opensearch.sh", `
		S[OPENSEARCH_ADMIN_PASSWORD]=pw WAIT_TRIES=5 WAIT_INTERVAL=0
		os_curl() { c=$(cat "$D/c" 2>/dev/null || echo 0); c=$((c+1)); echo "$c" >"$D/c"
			if [ "$c" -lt 3 ]; then echo 503; else echo 200; fi; }
		await_ready && echo READY`, "D="+t.TempDir())
	if err != nil || !strings.Contains(out, "READY") {
		t.Fatalf("a pending 503 was not waited out: %v\n%s", err, out)
	}

	// Never ready: a readiness timeout, distinct from a rejection.
	out, err = bash(t, "bootstrap-opensearch.sh", `
		S[OPENSEARCH_ADMIN_PASSWORD]=pw WAIT_TRIES=3 WAIT_INTERVAL=0
		os_curl() { echo 503; }
		await_ready`)
	if err == nil || !strings.Contains(out, "may still be initializing") {
		t.Fatalf("timeout not reported: %v\n%s", err, out)
	}
	if strings.Contains(out, "rejected") {
		t.Errorf("timeout was reported as a rejection:\n%s", out)
	}

	// A definite rejection stops immediately.
	out, err = bash(t, "bootstrap-opensearch.sh", `
		S[OPENSEARCH_ADMIN_PASSWORD]=pw WAIT_TRIES=5 WAIT_INTERVAL=0
		os_curl() { echo 401; }
		await_ready`)
	if err == nil || !strings.Contains(out, "rejected after security initialization") {
		t.Fatalf("rejection not reported: %v\n%s", err, out)
	}
}

func TestCheckRoles(t *testing.T) {
	for _, list := range []string{
		"cluster_manager,data,ingest,ml", // the full role names the bootstrap requires
		"cluster_manager,data,ingest,ml,remote_cluster_client",
	} {
		out, err := bash(t, "bootstrap-opensearch.sh", `check_roles "$L"; echo OK`, "L="+list)
		if err != nil || !strings.Contains(out, "OK") {
			t.Errorf("%q rejected: %v\n%s", list, err, out)
		}
	}
	for _, tc := range []struct{ list, want string }{
		{"cluster_manager,data,ingest", "do not include ml"},
		{"cluster_manager,data,ingest,ml_extra", "do not include ml"}, // complete tokens only
		{"dim", "do not include cluster_manager"},                     // the abbreviated form is not enough
	} {
		out, err := bash(t, "bootstrap-opensearch.sh", `check_roles "$L"`, "L="+tc.list)
		if err == nil || !strings.Contains(out, tc.want) {
			t.Errorf("%q: want %q, got %v\n%s", tc.list, tc.want, err, out)
		}
	}
}

// --- destroy / redeploy retention ------------------------------------------

func mustWrite(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(path, mode); err != nil {
		t.Fatal(err)
	}
}

func mustChmod(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// awsTerraformStubs returns a PATH with stubbed aws (identity) and terraform.
func awsTerraformStubs(t *testing.T, terraformBody string) string {
	t.Helper()
	_, path := stubPath(t, map[string]string{
		"aws": `case "$*" in
  "configure list-profiles") echo dev;;
  *sts*) echo arn:aws:iam::123456789012:user/tester;;
esac`,
		// state list reports a tracked resource unless STUB_EMPTY_STATE is set,
		// as it would after a completed destroy.
		"terraform": `case "$*" in *"state list"*) [ -n "${STUB_EMPTY_STATE:-}" ] || echo aws_instance.opensearch; exit 0;; esac
` + terraformBody,
	})
	return path
}

// destroyFixture writes the settings.env, secrets.env, credentials.json, SSH key,
// Terraform state and plans a real destroy sees, and returns the directory and the
// snippet that prepares the script's globals.
func destroyFixture(t *testing.T) (dir, prelude string) {
	t.Helper()
	dir = t.TempDir()
	for _, d := range []string{"ssh", "terraform", "logs", "common"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, filepath.Join(dir, "terraform", "terraform.tfstate"), []byte("{}"), 0o600)
	mustWrite(t, filepath.Join(dir, "terraform", "tfplan"), []byte("plan"), 0o600)
	mustWrite(t, filepath.Join(dir, "terraform", "destroy.tfplan"), []byte("plan"), 0o600)
	mustWrite(t, filepath.Join(dir, "ssh", "id_ed25519"), []byte("old private key"), 0o600)
	mustWrite(t, filepath.Join(dir, "ssh", "id_ed25519.pub"), []byte("ssh-ed25519 AAAA old"), 0o600)
	prelude = `LOG_DIR=$D/logs SETTINGS=$D/settings.env SECRETS=$D/secrets.env DIR=$D
		TF_DIR=$D/terraform SSH_KEY=$D/ssh/id_ed25519 KNOWN_HOSTS=$D/ssh/known_hosts
		CFG[RAG_SNAP_USER_COMMON]=$D/common CFG[AWS_PROFILE]=dev CFG[AWS_REGION]=us-east-1
		CFG[DEPLOYMENT_ID]=rag-deadbeef CFG[INSTANCE_TYPE]=t3.xlarge CFG[AMI_ID]=ami-0123456789abcdef0
		CFG[ROOT_VOLUME_GIB]=50 CFG[ALLOWED_CIDR]=203.0.113.7/32
		CFG[DRIVE_IMPORT]=yes CFG[DRIVE_FOLDER_URL]=https://drive.google.com/drive/folders/keepme
		CFG[DRIVE_IMPORT_LAST_ATTEMPT]=2026-09-28T19:00:00Z
		CFG[OPENSEARCH_ADMIN_PASSWORD]=old-admin-pw CFG[TLS_ROOT_PASS]=old-root-pw
		CFG[TLS_ADMIN_PASS]=old-admin-passphrase CFG[TLS_NODE_PASS]=old-node-pw
		CFG[CHAT_API_KEY]=bedrock-api-key-keepme
		CFG[GOOGLE_DRIVE_CLIENT_ID]=1234-abc.apps.googleusercontent.com
		CFG[GOOGLE_DRIVE_CLIENT_SECRET]=GOCSPX-keepme-secret
		save_env "$SETTINGS" "${SETTINGS_KEYS[@]}"; save_env "$SECRETS" "${SECRET_KEYS[@]}"
		write_credentials
		`
	// Materialise the files the way a real run would, so tests can read them before
	// running destroy.
	if out, err := bashIn(t, "opensearch-on-aws.sh", "", prelude, "D="+dir); err != nil {
		t.Fatalf("fixture setup failed: %v\n%s", err, out)
	}
	return dir, prelude
}

func TestDestroyRetainsReusableCredentials(t *testing.T) {
	dir, prelude := destroyFixture(t)
	out, err := bashIn(t, "opensearch-on-aws.sh", "rag-deadbeef\n", prelude+"destroy",
		awsTerraformStubs(t, "exit 0"), "D="+dir)
	if err != nil {
		t.Fatalf("destroy failed: %v\n%s", err, out)
	}

	secrets := readFile(t, filepath.Join(dir, "secrets.env"))
	for _, want := range []string{`CHAT_API_KEY="bedrock-api-key-keepme"`,
		`GOOGLE_DRIVE_CLIENT_ID="1234-abc.apps.googleusercontent.com"`,
		`GOOGLE_DRIVE_CLIENT_SECRET="GOCSPX-keepme-secret"`} {
		if !strings.Contains(secrets, want) {
			t.Errorf("secrets.env lost the reusable credential %q:\n%s", want, secrets)
		}
	}
	for _, gone := range []string{"OPENSEARCH_ADMIN_PASSWORD", "TLS_ROOT_PASS", "TLS_ADMIN_PASS", "TLS_NODE_PASS"} {
		if strings.Contains(secrets, gone) {
			t.Errorf("secrets.env still holds the deployment secret %s:\n%s", gone, secrets)
		}
	}

	settings := readFile(t, filepath.Join(dir, "settings.env"))
	for _, want := range []string{`DRIVE_IMPORT="yes"`,
		`DRIVE_FOLDER_URL="https://drive.google.com/drive/folders/keepme"`} {
		if !strings.Contains(settings, want) {
			t.Errorf("settings.env lost the saved answer %q:\n%s", want, settings)
		}
	}
	if strings.Contains(settings, "DRIVE_IMPORT_LAST_ATTEMPT") {
		t.Errorf("DRIVE_IMPORT_LAST_ATTEMPT was not cleared:\n%s", settings)
	}

	if exists(filepath.Join(dir, "ssh")) {
		t.Error("the SSH key was kept")
	}
	if exists(filepath.Join(dir, "terraform", "tfplan")) || exists(filepath.Join(dir, "terraform", "destroy.tfplan")) {
		t.Error("plan files were kept")
	}
	if !exists(filepath.Join(dir, "terraform", "terraform.tfstate")) {
		t.Error("Terraform state was removed")
	}
	if !exists(filepath.Join(dir, "logs")) {
		t.Error("logs were removed")
	}
	if exists(filepath.Join(dir, "common", "credentials.json")) {
		t.Error("this deployment's credentials.json was kept")
	}
}

func TestDestroyNotConfirmedKeepsEverything(t *testing.T) {
	for name, tc := range map[string]struct {
		stdin string
		tf    string
	}{
		"declined":  {"wrong-id\n", "exit 0"},
		"tf failed": {"rag-deadbeef\n", `case "$*" in *" apply "*) echo "destroy failed" >&2; exit 1;; *) exit 0;; esac`},
	} {
		t.Run(name, func(t *testing.T) {
			dir, prelude := destroyFixture(t)
			beforeSecrets := readFile(t, filepath.Join(dir, "secrets.env"))
			beforeSettings := readFile(t, filepath.Join(dir, "settings.env"))
			out, err := bashIn(t, "opensearch-on-aws.sh", tc.stdin, prelude+"destroy", awsTerraformStubs(t, tc.tf), "D="+dir)
			if err == nil {
				t.Fatalf("destroy should not have succeeded:\n%s", out)
			}
			if got := readFile(t, filepath.Join(dir, "secrets.env")); got != beforeSecrets {
				t.Errorf("secrets.env changed:\n%s", got)
			}
			if got := readFile(t, filepath.Join(dir, "settings.env")); got != beforeSettings {
				t.Errorf("settings.env changed:\n%s", got)
			}
			if !exists(filepath.Join(dir, "ssh", "id_ed25519")) {
				t.Error("the SSH key was removed")
			}
			if !exists(filepath.Join(dir, "common", "credentials.json")) {
				t.Error("credentials.json was removed")
			}
			if !strings.Contains(readFile(t, filepath.Join(dir, "settings.env")), "DRIVE_IMPORT_LAST_ATTEMPT") {
				t.Error("the import marker was cleared")
			}
		})
	}
}

func TestDestroyAfterDestroyStopsBeforePlan(t *testing.T) {
	dir, prelude := destroyFixture(t)
	// A completed destroy removed the SSH key and left an empty state behind.
	if err := os.RemoveAll(filepath.Join(dir, "ssh")); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(dir, "tf-calls")
	out, err := bashIn(t, "opensearch-on-aws.sh", "rag-deadbeef\n", prelude+"destroy",
		awsTerraformStubs(t, `echo "$*" >>"$D/tf-calls"`), "D="+dir, "STUB_EMPTY_STATE=1")
	if err == nil || !strings.Contains(out, "nothing to destroy") {
		t.Fatalf("destroy of an empty state did not stop cleanly: %v\n%s", err, out)
	}
	if strings.Contains(readFile(t, calls), "plan") {
		t.Errorf("terraform plan ran against an empty state:\n%s", readFile(t, calls))
	}
}

func TestGeneratedSecretsRejectJSONUnsafeValues(t *testing.T) {
	for _, v := range []string{`pa"ss`, `pa\ss`} {
		out, err := bash(t, "opensearch-on-aws.sh", `LOG_DIR=$(mktemp -d) SECRETS=secrets.env CFG[OPENSEARCH_ADMIN_PASSWORD]=$V
			ensure_generated_secrets`, "V="+v)
		if err == nil || !strings.Contains(out, "OPENSEARCH_ADMIN_PASSWORD in secrets.env must not contain") {
			t.Errorf("password %q was accepted: %v\n%s", v, err, out)
		}
	}
}

func TestSetupAfterDestroyReusesCredentialsAndRunsImport(t *testing.T) {
	dir, prelude := destroyFixture(t)
	if _, err := bashIn(t, "opensearch-on-aws.sh", "rag-deadbeef\n", prelude+"destroy",
		awsTerraformStubs(t, "exit 0"), "D="+dir); err != nil {
		t.Fatal(err)
	}

	// A new run: load the retained files, then the parts of setup that use them.
	importer := `echo "called" >>"$D/import-called"
		echo "import output"
		echo "importer client: $GOOGLE_DRIVE_CLIENT_ID"
		[ -n "$GOOGLE_DRIVE_CLIENT_SECRET" ] && echo "importer secret: present"`
	_, path := stubPath(t, map[string]string{"rag-cli.rag": importer})
	out, err := bashIn(t, "opensearch-on-aws.sh", "", `
		LOG_DIR=$D/logs SETTINGS=$D/settings.env SECRETS=$D/secrets.env DIR=$D
		SSH_KEY=$D/ssh/id_ed25519 RAG=rag-cli.rag CFG[RAG_SNAP_USER_COMMON]=$D/common
		load_env "$SETTINGS" "${SETTINGS_KEYS[@]}"; load_env "$SECRETS" "${SECRET_KEYS[@]}"
		printf 'RETAINED_API_KEY=%s\n' "${CFG[CHAT_API_KEY]}"
		ensure_generated_secrets
		ensure_ssh_key
		save_all
		write_credentials
		printf 'SECRETS_SET=%s\n' "$(grep -cE '^(OPENSEARCH_ADMIN_PASSWORD|TLS_ROOT_PASS|TLS_ADMIN_PASS|TLS_NODE_PASS)="[A-Za-z0-9]{32}"$' "$SECRETS")"
		printf 'OLD_SECRETS=%s\n' "$(grep -cE 'old-(admin|root|node)' "$SECRETS" || true)"
		drive_import
		printf 'PROMPTED=%s\n' "$(grep -c 'Run the import again' "$LOG_DIR/drive.log" || true)"
		`, path, "D="+dir)
	if err != nil {
		t.Fatalf("redeploy steps failed: %v\n%s", err, out)
	}

	if !strings.Contains(out, "RETAINED_API_KEY=bedrock-api-key-keepme") {
		t.Errorf("the API key was not reused:\n%s", out)
	}
	if !strings.Contains(out, "SECRETS_SET=4") || !strings.Contains(out, "OLD_SECRETS=0") {
		t.Errorf("deployment secrets were not regenerated:\n%s", out)
	}
	if !strings.Contains(out, "PROMPTED=0") {
		t.Errorf("the previous import attempt was still treated as done:\n%s", out)
	}
	if !exists(filepath.Join(dir, "import-called")) {
		t.Errorf("the import did not run:\n%s", out)
	}
	if !strings.Contains(out, "importer client: 1234-abc.apps.googleusercontent.com") ||
		!strings.Contains(out, "importer secret: present") {
		t.Errorf("the retained Google credentials did not reach the importer:\n%s", out)
	}

	key := readFile(t, filepath.Join(dir, "ssh", "id_ed25519"))
	if !strings.HasPrefix(key, "-----BEGIN OPENSSH PRIVATE KEY-----") {
		t.Errorf("a new SSH key was not generated:\n%s", key)
	}
	creds := readFile(t, filepath.Join(dir, "common", "credentials.json"))
	if !strings.Contains(creds, "bedrock-api-key-keepme") {
		t.Errorf("the API key was not written back to credentials.json:\n%s", creds)
	}
	if !strings.Contains(creds, `"OPENSEARCH_PASSWORD"`) {
		t.Errorf("credentials.json lacks the new password:\n%s", creds)
	}
}

func TestModelIDExtraction(t *testing.T) {
	log := filepath.Join(t.TempDir(), "init.log")
	mustWrite(t, log, []byte("\r\x1b[2K⠋ Registering models\rEmbedding model ID: aB3-x_9\n  Run \"sudo rag-cli.rag set ...\"\nRerank model ID: Zz9\r\n"), 0o600)
	out, err := bash(t, "opensearch-on-aws.sh", `model_id Embedding "$L"; model_id Rerank "$L"`, "L="+log)
	if err != nil || out != "aB3-x_9\nZz9\n" {
		t.Errorf("got %q %v", out, err)
	}
}

// stubs creates executables in a directory prepended to PATH.
func stubs(t *testing.T, scripts map[string]string) string {
	t.Helper()
	bin := t.TempDir()
	for name, body := range scripts {
		mustWrite(t, filepath.Join(bin, name), []byte("#!/usr/bin/env bash\n"+body+"\n"), 0o755)
	}
	return "PATH=" + bin + ":" + os.Getenv("PATH")
}

func TestSavedAMIIsReused(t *testing.T) {
	calls := filepath.Join(t.TempDir(), "calls")
	path := stubs(t, map[string]string{"aws": `echo "$*" >>` + calls + `
case "$*" in *describe-images*) printf 'ami-0123456789abcdef0\tx86_64\tubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-20260101\n' ;; *ssm*) echo ami-0fffffffffffffff0 ;; esac`})
	dir := t.TempDir()
	out, err := bash(t, "opensearch-on-aws.sh", `LOG_DIR=$D/logs SETTINGS=$D/s SECRETS=$D/x
		CFG[AWS_PROFILE]=p CFG[AWS_REGION]=us-east-1 CFG[UBUNTU_RELEASE]=24.04 CFG[AMI_ID]=ami-0123456789abcdef0
		select_ami; echo "AMI=${CFG[AMI_ID]}"`, path, "D="+dir)
	if err != nil || !strings.Contains(out, "AMI=ami-0123456789abcdef0") {
		t.Fatalf("%v\n%s", err, out)
	}
	c, _ := os.ReadFile(calls)
	if strings.Contains(string(c), "ssm") || !strings.Contains(string(c), "--owners 099720109477") {
		t.Errorf("aws calls: %s", c)
	}
}

func TestDriveImportRecordsOnlyAnAttempt(t *testing.T) {
	for _, tc := range []struct {
		exit    string
		wantErr bool
	}{{"0", false}, {"1", true}} {
		dir := t.TempDir()
		path := stubs(t, map[string]string{"rag-cli.rag": `echo "  skip: download failed"; [ -n "$GOOGLE_DRIVE_CLIENT_SECRET" ] || exit 9; exit ` + tc.exit})
		out, err := bash(t, "opensearch-on-aws.sh", `LOG_DIR=$D/logs SETTINGS=$D/settings.env SECRETS=$D/secrets.env RAG=rag-cli.rag
			CFG[DRIVE_IMPORT]=yes CFG[DRIVE_FOLDER_URL]=https://drive.google.com/x CFG[GOOGLE_DRIVE_CLIENT_ID]=id CFG[GOOGLE_DRIVE_CLIENT_SECRET]=topsecret
			drive_import`, path, "D="+dir)
		settings, _ := os.ReadFile(filepath.Join(dir, "settings.env"))
		if (err != nil) != tc.wantErr {
			t.Fatalf("exit %s: err=%v\n%s", tc.exit, err, out)
		}
		if strings.Contains(out, "topsecret") || !strings.Contains(out, "skip: download failed") {
			t.Errorf("exit %s: output not shown or secret leaked:\n%s", tc.exit, out)
		}
		recorded := strings.Contains(string(settings), "DRIVE_IMPORT_LAST_ATTEMPT=")
		finished := strings.Contains(out, "Import command finished; review its output for archive failures.")
		if recorded == tc.wantErr || finished == tc.wantErr {
			t.Errorf("exit %s: recorded=%v finished-message=%v\n%s", tc.exit, recorded, finished, out)
		}
	}
}

func TestCredentialsFile(t *testing.T) {
	common := t.TempDir()
	mustChmod(t, common, 0o700)
	out, err := bash(t, "opensearch-on-aws.sh", `CFG[RAG_SNAP_USER_COMMON]=$C CFG[OPENSEARCH_ADMIN_PASSWORD]=Abc123 CFG[CHAT_API_KEY]='k"\y'
		write_credentials; stat -c %a "$C/credentials.json"; cat "$C/credentials.json"`, "C="+common)
	want := "600\n{\n  \"OPENSEARCH_USERNAME\": \"admin\",\n  \"OPENSEARCH_PASSWORD\": \"Abc123\",\n  \"CHAT_API_KEY\": \"k\\\"\\\\y\"\n}\n"
	if err != nil || !strings.HasSuffix(out, want) {
		t.Errorf("%v\n%s", err, out)
	}
}

func TestBootstrapHeapAndHashEdit(t *testing.T) {
	out, err := bash(t, "bootstrap-opensearch.sh", `for kib in 4026532 8052064 16106128 16384000 65536000; do heap_gib $kib; done`)
	if err != nil || out != "1\n3\n6\n6\n23\n" {
		t.Errorf("heap: %q %v", out, err)
	}

	users := filepath.Join(t.TempDir(), "internal_users.yml")
	mustWrite(t, users, []byte("---\n_meta:\n  type: \"internalusers\"\n  config_version: 2\n\nadmin:\n  hash: \"$2a$12$old\"\n  reserved: true\n\nkibanaserver:\n  hash: \"$2a$12$other\"\n  reserved: true\n"), 0o600)
	out, err = bash(t, "bootstrap-opensearch.sh", `replace_admin_hash "$U" '$2y$12$new/hash.'`, "U="+users)
	want := "---\n_meta:\n  type: \"internalusers\"\n  config_version: 2\n\nadmin:\n  hash: \"$2y$12$new/hash.\"\n  reserved: true\n\nkibanaserver:\n  hash: \"$2a$12$other\"\n  reserved: true\n"
	if err != nil || out != want {
		t.Errorf("hash edit:\n%s", out)
	}

	out, err = bash(t, "bootstrap-opensearch.sh", `S[TLS_ADMIN_PASS]=pa55word; echo "setup --pass pa55word ok" | mask`)
	if err != nil || out != "setup --pass *** ok\n" {
		t.Errorf("mask: %q %v", out, err)
	}
}
