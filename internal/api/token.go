package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/canonical/go-snapctl/env"
)

// tokenRelPath is the localhost token file location under $SNAP_COMMON,
// alongside the unix socket.
const tokenRelPath = "ragd/ui.token"

// tokenBytes is the entropy of the generated localhost token (256 bits).
const tokenBytes = 32

// localhostToken loads or generates the localhost bearer token used to
// authenticate the loopback UI listener. The token is persisted under
// $SNAP_COMMON readable by the configured access group (the same trust boundary
// as the unix socket), so group members can read it and other users cannot. On
// restart an existing token is reused rather than regenerated.
//
// Returns the token path and value.
func localhostToken(group string) (string, string, error) {
	path := tokenPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", "", fmt.Errorf("creating token directory: %w", err)
	}

	// Reuse an existing token if present and non-empty.
	if data, err := os.ReadFile(path); err == nil {
		if tok := strings.TrimSpace(string(data)); tok != "" {
			// Re-apply group ownership/permissions in case the group changed.
			_ = applyTokenPermissions(path, group)
			return path, tok, nil
		}
	} else if !os.IsNotExist(err) {
		return "", "", fmt.Errorf("reading token file: %w", err)
	}

	tok, err := generateToken()
	if err != nil {
		return "", "", err
	}
	// Write 0640 so the owner can read/write and the group can read; other
	// users have no access. Group readability is finalised by chown below.
	if err := os.WriteFile(path, []byte(tok+"\n"), 0o640); err != nil {
		return "", "", fmt.Errorf("writing token file: %w", err)
	}
	if err := applyTokenPermissions(path, group); err != nil {
		return "", "", err
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

// applyTokenPermissions chowns the token file's group to the named access group
// and re-applies 0640 so the group can read it. A failure to resolve the group
// (e.g. it does not exist on the host) is non-fatal for the file mode but is
// reported so the operator can fix group membership; the file remains
// owner-only readable in that case.
func applyTokenPermissions(path, group string) error {
	if err := os.Chmod(path, 0o640); err != nil {
		return fmt.Errorf("setting token file mode: %w", err)
	}
	if group == "" {
		return nil
	}
	g, err := user.LookupGroup(group)
	if err != nil {
		// Group not found on host: leave the file owner-readable. Surface a
		// soft error so the daemon can log it without failing startup.
		return fmt.Errorf("looking up access group %q for token file: %w", group, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return fmt.Errorf("parsing gid for group %q: %w", group, err)
	}
	if err := os.Chown(path, -1, gid); err != nil {
		return fmt.Errorf("chowning token file to group %q: %w", group, err)
	}
	return nil
}
