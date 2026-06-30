package api

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// SocketConfig describes where to create the API unix socket and how to permission it.
type SocketConfig struct {
	// Path is the absolute socket path, e.g. $SNAP_COMMON/ragd/unix.socket.
	Path string
	// Group is the host group whose members are granted access (api.socket.group).
	// Membership is enforced by the SO_PEERCRED check in authenticate, NOT by the
	// socket's file ownership: under strict confinement the daemon cannot chown the
	// socket to an arbitrary group (snapd's seccomp profile denies it). The socket
	// is therefore left root-owned and world-connectable at the DAC layer, with the
	// peercred check as the sole access gate. See design Decision 1.
	Group string
	// Mode is the socket file permission bits (api.socket.mode). Defaults to 0666
	// so any local user can reach the socket; the peercred check then admits only
	// root and members of Group.
	Mode os.FileMode
}

// listenUnix creates the API unix socket. The daemon creates the socket itself
// (snapd's `sockets:` activation stanza cannot set the owning group) and chmods
// it to the configured mode. It does NOT chown the socket to api.socket.group:
// the strict-confinement seccomp profile denies chowning to an arbitrary group,
// so access is gated entirely by the SO_PEERCRED check in authenticate rather
// than by file ownership. See design Decision 1.
func listenUnix(cfg SocketConfig) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.Path), 0o755); err != nil {
		return nil, fmt.Errorf("creating socket directory: %w", err)
	}

	// Remove a stale socket left by an unclean shutdown so Listen does not fail
	// with "address already in use".
	if err := removeStaleSocket(cfg.Path); err != nil {
		return nil, err
	}

	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "unix", cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", cfg.Path, err)
	}

	if err := applySocketPermissions(cfg); err != nil {
		_ = ln.Close()
		return nil, err
	}

	// Wrap so each accepted connection carries its peer's SO_PEERCRED creds,
	// which the auth middleware reads to authorize the request.
	return peerCredListener{Listener: ln}, nil
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

// applySocketPermissions sets the mode on the socket file. It deliberately does
// NOT chown the socket to api.socket.group: under strict confinement snapd's
// seccomp profile denies chowning to an arbitrary group, which would crash the
// daemon. Access is instead gated by the SO_PEERCRED check in authenticate, so
// the socket is left root-owned and the mode (default 0666) only governs whether
// a local process can reach it at the DAC layer.
func applySocketPermissions(cfg SocketConfig) error {
	if cfg.Mode != 0 {
		if err := os.Chmod(cfg.Path, cfg.Mode); err != nil {
			return fmt.Errorf("setting socket mode to %o: %w", cfg.Mode, err)
		}
	}
	return nil
}
