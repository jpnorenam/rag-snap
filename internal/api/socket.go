package api

import (
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
)

// SocketConfig describes where to create the API unix socket and how to permission it.
type SocketConfig struct {
	// Path is the absolute socket path, e.g. $SNAP_COMMON/ragd/unix.socket.
	Path string
	// Group is the host group granted access (api.socket.group). Members of this
	// group, plus root, may connect. Empty means leave the group owner as-is.
	Group string
	// Mode is the socket file permission bits (api.socket.mode), e.g. 0660.
	Mode os.FileMode
}

// listenUnix creates the API unix socket per the resolved spike-0.1 approach:
// the daemon creates the socket itself (snapd's `sockets:` stanza cannot set the
// owning group), then chowns it to root:<group> and chmods it to the configured
// mode. The group + file mode are the first access gate; the SO_PEERCRED check
// (phase 2) is the second.
func listenUnix(cfg SocketConfig) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.Path), 0o755); err != nil {
		return nil, fmt.Errorf("creating socket directory: %w", err)
	}

	// Remove a stale socket left by an unclean shutdown so Listen does not fail
	// with "address already in use".
	if err := removeStaleSocket(cfg.Path); err != nil {
		return nil, err
	}

	ln, err := net.Listen("unix", cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", cfg.Path, err)
	}

	if err := applySocketPermissions(cfg); err != nil {
		_ = ln.Close()
		return nil, err
	}

	return ln, nil
}

// removeStaleSocket unlinks an existing socket file at path, if present.
func removeStaleSocket(path string) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat socket path: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("path %s exists and is not a socket", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("removing stale socket: %w", err)
	}
	return nil
}

// applySocketPermissions sets the group owner and mode on the socket file.
func applySocketPermissions(cfg SocketConfig) error {
	if cfg.Group != "" {
		gid, err := lookupGID(cfg.Group)
		if err != nil {
			return err
		}
		// chown to root:<group> (uid -1 leaves the user owner unchanged).
		if err := os.Chown(cfg.Path, -1, gid); err != nil {
			return fmt.Errorf("setting socket group to %q: %w", cfg.Group, err)
		}
	}
	if cfg.Mode != 0 {
		if err := os.Chmod(cfg.Path, cfg.Mode); err != nil {
			return fmt.Errorf("setting socket mode to %o: %w", cfg.Mode, err)
		}
	}
	return nil
}

// lookupGID resolves a group name to its numeric GID.
func lookupGID(group string) (int, error) {
	g, err := user.LookupGroup(group)
	if err != nil {
		return 0, fmt.Errorf("looking up group %q: %w", group, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, fmt.Errorf("parsing gid %q for group %q: %w", g.Gid, group, err)
	}
	return gid, nil
}
