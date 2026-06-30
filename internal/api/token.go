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

// tokenRelPath is the localhost token file location under $SNAP_COMMON,
// alongside the unix socket.
const tokenRelPath = "ragd/ui.token"

// tokenBytes is the entropy of the generated localhost token (256 bits).
const tokenBytes = 32

// localhostToken loads or generates the localhost bearer token used to
// authenticate the loopback UI listener. The token is persisted owner-only
// (0600) under $SNAP_COMMON, alongside the unix socket, so it survives daemon
// restarts. On restart an existing token is reused rather than regenerated.
//
// The token VALUE is never read off disk by clients: the daemon hands it to
// `rag ui` over the peercred-authenticated /1.0 endpoint, which already admits
// exactly the principals (root + access-group members) the token is meant to
// grant. This avoids relying on group-readability of the file, which strict
// confinement forbids the daemon from establishing (it cannot chown the file to
// an arbitrary group; snapd's seccomp profile denies it — the same restriction
// the unix socket sidesteps via peercred).
//
// Returns the token path and value.
func localhostToken() (string, string, error) {
	path := tokenPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", "", fmt.Errorf("creating token directory: %w", err)
	}

	// Reuse an existing token if present and non-empty.
	if data, err := os.ReadFile(path); err == nil {
		if tok := strings.TrimSpace(string(data)); tok != "" {
			return path, tok, nil
		}
	} else if !os.IsNotExist(err) {
		return "", "", fmt.Errorf("reading token file: %w", err)
	}

	tok, err := generateToken()
	if err != nil {
		return "", "", err
	}
	// Write 0600: only the daemon's user (root) can read the file. Clients
	// obtain the token over the trusted socket, not from this file.
	if err := os.WriteFile(path, []byte(tok+"\n"), 0o600); err != nil {
		return "", "", fmt.Errorf("writing token file: %w", err)
	}
	return path, tok, nil
}

// tokenPath resolves the token file path under $SNAP_COMMON, with a temp-dir
// fallback outside a snap (mirroring the socket path resolution).
func tokenPath() string {
	base := env.SnapCommon()
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, tokenRelPath)
}

// generateToken returns a high-entropy hex token.
func generateToken() (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
