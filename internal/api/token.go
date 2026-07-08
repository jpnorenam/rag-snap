package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/canonical/go-snapctl/env"
)

// tokenRelPath is the token file location under $SNAP_COMMON. The name stays
// "ui.token" for continuity with the (deferred) UI change, whose browser client
// is the eventual consumer of this same loopback surface (design Decision 4).
const tokenRelPath = "ragd/ui.token"

// tokenBytes is the entropy of a freshly generated token (256 bits), hex-encoded
// to a 64-character string.
const tokenBytes = 32

// localhostToken ensures a localhost access token exists and returns its file
// path and value. A non-empty existing file is reused so the token survives
// daemon restarts (clients may hold it across a reload); otherwise a 256-bit
// token is generated with crypto/rand and written owner-only (0600).
//
// It deliberately performs NO chown / group-permission change: under strict
// confinement snapd's seccomp profile denies chowning to an arbitrary group, and
// a fatal chown would crash-loop the daemon (design Decision 4). Clients obtain
// the value over the peercred-gated GET /1.0, never by reading this file.
func localhostToken() (path, value string, err error) {
	path, err = tokenPath()
	if err != nil {
		return "", "", err
	}

	if data, readErr := os.ReadFile(path); readErr == nil {
		if existing := strings.TrimSpace(string(data)); existing != "" {
			return path, existing, nil
		}
	}

	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("generating localhost token: %w", err)
	}
	value = hex.EncodeToString(buf)

	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		return "", "", fmt.Errorf("writing localhost token to %s: %w", path, err)
	}
	return path, value, nil
}

// tokenPath resolves the token file path under $SNAP_COMMON (temp-dir fallback
// off-snap) and ensures its parent directory exists (0755).
func tokenPath() (string, error) {
	base := env.SnapCommon()
	if base == "" {
		// Outside a snap (e.g. local dev / tests), fall back to a temp dir.
		base = os.TempDir()
	}
	path := filepath.Join(base, tokenRelPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("creating token directory: %w", err)
	}
	return path, nil
}
