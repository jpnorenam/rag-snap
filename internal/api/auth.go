package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/user"
	"strconv"

	"golang.org/x/sys/unix"
)

// peerCred holds the operating-system credentials of a unix-socket peer, read
// via SO_PEERCRED at accept time. err records any failure to read the creds so
// the auth middleware can deny cleanly rather than panic.
type peerCred struct {
	uid uint32
	gid uint32
	pid int32
	err error
}

// credContextKey is the context key under which the accepted connection's
// peerCred is stored by the listener and read by the auth middleware.
type credContextKey struct{}

// peerCredListener wraps a unix net.Listener so each accepted connection is a
// credConn carrying its peer's SO_PEERCRED credentials. The HTTP server then
// threads those credentials into each request's context via ConnContext, which
// is the seam the auth middleware reads. This mirrors how LXD propagates peer
// identity from the listener into handlers.
type peerCredListener struct {
	net.Listener
}

// credConn pairs an accepted connection with the peer credentials captured when
// it was accepted.
type credConn struct {
	net.Conn
	cred peerCred
}

func (l peerCredListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &credConn{Conn: conn, cred: readPeerCred(conn)}, nil
}

// readPeerCred extracts SO_PEERCRED from a unix connection.
func readPeerCred(conn net.Conn) peerCred {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return peerCred{err: fmt.Errorf("connection is not a unix socket")}
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return peerCred{err: fmt.Errorf("accessing socket fd: %w", err)}
	}
	var cred peerCred
	ctrlErr := raw.Control(func(fd uintptr) {
		ucred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if err != nil {
			cred.err = fmt.Errorf("reading SO_PEERCRED: %w", err)
			return
		}
		cred.uid, cred.gid, cred.pid = ucred.Uid, ucred.Gid, ucred.Pid
	})
	if ctrlErr != nil && cred.err == nil {
		cred.err = fmt.Errorf("controlling socket fd: %w", ctrlErr)
	}
	return cred
}

// connContext stamps each connection's captured peer credentials onto the
// per-connection base context, so handlers can read them via the request
// context. Wired into http.Server.ConnContext.
func connContext(ctx context.Context, c net.Conn) context.Context {
	if cc, ok := c.(*credConn); ok {
		return context.WithValue(ctx, credContextKey{}, cc.cred)
	}
	return ctx
}

// credFromRequest returns the peer credentials captured for the request's
// connection, and whether they were present (a non-unix transport, e.g. a
// test using a TCP loopback, yields ok=false).
func credFromRequest(r *http.Request) (peerCred, bool) {
	cred, ok := r.Context().Value(credContextKey{}).(peerCred)
	return cred, ok
}

// authResult is the outcome of authenticating a connection.
type authResult struct {
	trusted bool
	// reason is a human-readable denial message when trusted is false.
	reason string
}

// authenticate decides whether a request's peer is trusted. Access is granted
// iff the peer's effective user is root (uid 0) or a member of the configured
// access group. This is the single auth seam; the remote-auth change will add a
// non-peercred branch here for cert/token clients.
func (s *Server) authenticate(r *http.Request) authResult {
	cred, ok := credFromRequest(r)
	if !ok {
		// No peer credentials available. This only happens off the unix
		// socket (e.g. tests dialling a loopback). Treat as untrusted.
		return authResult{reason: "peer credentials unavailable"}
	}
	if cred.err != nil {
		return authResult{reason: fmt.Sprintf("could not read peer credentials: %v", cred.err)}
	}
	if cred.uid == 0 {
		return authResult{trusted: true}
	}
	member, err := userInGroup(cred.uid, s.socket.Group)
	if err != nil {
		return authResult{reason: fmt.Sprintf("could not resolve group membership: %v", err)}
	}
	if member {
		return authResult{trusted: true}
	}
	return authResult{reason: fmt.Sprintf("permission denied: user must be a member of the %q group", s.socket.Group)}
}

// userInGroup reports whether the user with the given uid is a member of the
// named group, resolved from the host passwd/group databases. The user's
// primary group counts as membership.
func userInGroup(uid uint32, group string) (bool, error) {
	if group == "" {
		return false, fmt.Errorf("no access group configured")
	}
	g, err := user.LookupGroup(group)
	if err != nil {
		return false, fmt.Errorf("looking up group %q: %w", group, err)
	}
	u, err := user.LookupId(strconv.FormatUint(uint64(uid), 10))
	if err != nil {
		return false, fmt.Errorf("looking up uid %d: %w", uid, err)
	}
	if u.Gid == g.Gid {
		return true, nil
	}
	gids, err := u.GroupIds()
	if err != nil {
		return false, fmt.Errorf("listing groups for uid %d: %w", uid, err)
	}
	for _, gid := range gids {
		if gid == g.Gid {
			return true, nil
		}
	}
	return false, nil
}

// requireAuth wraps a handler so a request is served only if its peer is
// trusted; otherwise it responds 403 with an actionable message.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if res := s.authenticate(r); !res.trusted {
			respondError(w, http.StatusForbidden, res.reason)
			return
		}
		next(w, r)
	}
}
