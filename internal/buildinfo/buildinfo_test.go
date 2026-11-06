package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestVersion(t *testing.T) {
	t.Setenv("SNAP_VERSION", "0.0.9")
	if got := Version(); got != "0.0.9" {
		t.Errorf("Version() = %q, want 0.0.9", got)
	}
	t.Setenv("SNAP_VERSION", "")
	if got := Version(); got != devVersion {
		t.Errorf("Version() outside a snap = %q, want %q", got, devVersion)
	}
}

func TestCommitInjectedWins(t *testing.T) {
	old := commit
	t.Cleanup(func() { commit = old })
	commit = "abc123def456"
	if got := Commit(); got != "abc123def456" {
		t.Errorf("Commit() = %q, want the injected value", got)
	}
}

func TestCommitFromSettings(t *testing.T) {
	full := "cd3ac89255560e85f0f77ae4101b187b65d3ca35"
	cases := []struct {
		name     string
		settings []debug.BuildSetting
		want     string
	}{
		{"clean", []debug.BuildSetting{{Key: "vcs.revision", Value: full}, {Key: "vcs.modified", Value: "false"}}, "cd3ac8925556"},
		{"dirty", []debug.BuildSetting{{Key: "vcs.revision", Value: full}, {Key: "vcs.modified", Value: "true"}}, "cd3ac8925556-dirty"},
		{"no vcs info", []debug.BuildSetting{{Key: "GOOS", Value: "linux"}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := commitFromSettings(tc.settings); got != tc.want {
				t.Errorf("commitFromSettings = %q, want %q", got, tc.want)
			}
		})
	}
}
