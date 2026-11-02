// Package buildinfo reports which build of rag-cli is running: the snap
// version and the source commit it was built from.
package buildinfo

import (
	"os"
	"runtime/debug"
)

// commit is the source commit, injected at build time by the snap build:
//
//	-ldflags "-X github.com/jpnorenam/rag-snap/internal/buildinfo.commit=<hash>"
//
// A "-dirty" suffix means the tree had uncommitted changes to tracked files.
var commit string

// devVersion is reported when not running inside a snap.
const devVersion = "dev"

// Version returns the installed snap's version (snapd sets SNAP_VERSION for
// every app and daemon), or "dev" outside a snap.
func Version() string {
	if v := os.Getenv("SNAP_VERSION"); v != "" {
		return v
	}
	return devVersion
}

// Commit returns the commit the binary was built from: the value injected by
// the snap build, else the VCS stamp Go records when building in a git
// checkout (e.g. `make build`), else "" when neither is available.
func Commit() string {
	if commit != "" {
		return commit
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return commitFromSettings(info.Settings)
}

// commitFromSettings extracts a short commit from Go's VCS build settings.
func commitFromSettings(settings []debug.BuildSetting) string {
	var rev string
	dirty := false
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return ""
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if dirty {
		rev += "-dirty"
	}
	return rev
}
