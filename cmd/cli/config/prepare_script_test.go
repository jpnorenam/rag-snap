package config

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/jpnorenam/rag-snap/cmd/cli/common"
)

// TestPrepareScriptCommandShape pins the command path: prepare-script is the
// parent and aws is its only subcommand.
func TestPrepareScriptCommandShape(t *testing.T) {
	cmd := PrepareScriptCommand(&common.Context{})
	var names []string
	for _, c := range cmd.Commands() {
		if c.Name() != "help" && c.Name() != "completion" {
			names = append(names, c.Name())
		}
	}
	if len(names) != 1 || names[0] != "aws" {
		t.Fatalf("prepare-script subcommands = %v, want [aws]", names)
	}
	if cmd.Name() != "prepare-script" {
		t.Errorf("parent command name = %q", cmd.Name())
	}
}

// TestPrepareScriptAWSHelp documents --output on the aws subcommand.
func TestPrepareScriptAWSHelp(t *testing.T) {
	cmd := PrepareScriptCommand(&common.Context{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"aws", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("help failed: %v", err)
	}
	if !strings.Contains(out.String(), "--output") {
		t.Errorf("aws --help does not document --output:\n%s", out.String())
	}
}

// TestPrepareScriptAWSRequiresOutput rejects a missing --output before any export.
func TestPrepareScriptAWSRequiresOutput(t *testing.T) {
	cmd := PrepareScriptCommand(&common.Context{})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"aws"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "output") {
		t.Fatalf("missing --output was not rejected: %v", err)
	}
}

// TestPrepareScriptOldSyntaxIsGone rejects the previous command path.
func TestPrepareScriptOldSyntaxIsGone(t *testing.T) {
	cmd := PrepareScriptCommand(&common.Context{})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"aws", "prepare-script"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("the old 'aws prepare-script' path is still accepted")
	}
}

func TestCheckNotSnapTmp(t *testing.T) {
	for dir, bad := range map[string]bool{"/tmp": true, "/tmp/rag-aws": true, "/var/tmp/x": true, "/home/alice/rag-aws": false, "/tmpfoo": false} {
		if err := checkNotSnapTmp(dir); (err != nil) != bad {
			t.Errorf("%s: %v", dir, err)
		}
	}
}
