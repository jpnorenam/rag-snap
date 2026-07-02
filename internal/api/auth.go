package api

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"os/user"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// transportKind identifies which listener accepted a connection, so the auth
// seam can pick the right check: peercred for the unix socket, the localhost
// token for the loopback listener.
type transportKind int

const (
	// transportUnix is the unix-socket transport (SO_PEERCRED). It is the zero
	// value so an unmarked connection (e.g. an older test dialling directly)
	// defaults to the historical peercred path.
	transportUnix transportKind = iota
	// transportLoopback is the loopback TCP transport (bearer-token auth).
	transportLoopback
)

// transportContextKey is the context key under which a connection's transport
// kind is stored by connContext and read by authenticate.
type transportContextKey struct{}

// uiTokenCookie is the cookie name carrying the localhost token for clients that
// cannot set an Authorization header (notably a browser websocket upgrade). The
// name is retained from the UI branch for continuity (design Decision 3).
const uiTokenCookie = "rag_ui_token"

// transportFromRequest returns the transport the request's connection arrived
// on. An unmarked connection defaults to transportUnix, preserving the original
// single-listener behaviour and keeping the existing peercred tests valid.
func transportFromRequest(r *http.Request) transportKind {
	if t, ok := r.Context().Value(transportContextKey{}).(transportKind); ok {
		return t
	}
	return transportUnix
}

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

// connContext stamps each connection's transport (and, for the unix socket, its
// captured peer credentials) onto the per-connection base context, so the auth
// seam can pick the right check. Wired into http.Server.ConnContext for both
// listeners.
func connContext(ctx context.Context, c net.Conn) context.Context {
	switch cc := c.(type) {
	case *credConn:
		ctx = context.WithValue(ctx, transportContextKey{}, transportUnix)
		return context.WithValue(ctx, credContextKey{}, cc.cred)
	case *loopbackConn:
		return context.WithValue(ctx, transportContextKey{}, transportLoopback)
	default:
		return ctx
	}
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

// authenticate decides whether a request's peer is trusted. It is the single
// auth seam, transport-aware: a loopback request is authenticated by the
// localhost bearer token; a unix-socket request is authenticated by SO_PEERCRED
// (root or a member of the configured access group).
func (s *Server) authenticate(r *http.Request) authResult {
	if transportFromRequest(r) == transportLoopback {
		return s.authenticateToken(r)
	}
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

// authenticateToken authenticates a loopback request by the localhost bearer
// token, presented as an "Authorization: Bearer <token>" header or the
// rag_ui_token cookie (the cookie path lets a browser websocket upgrade, which
// cannot set headers, carry the token). Comparison is constant-time.
func (s *Server) authenticateToken(r *http.Request) authResult {
	if s.token == "" {
		return authResult{reason: "localhost token is not configured"}
	}
	presented := bearerToken(r)
	if presented == "" {
		presented = uiTokenFromCookie(r)
	}
	if presented == "" {
		return authResult{reason: "missing localhost token"}
	}
	if subtle.ConstantTimeCompare([]byte(presented), []byte(s.token)) == 1 {
		return authResult{trusted: true}
	}
	return authResult{reason: "invalid localhost token"}
}

// bearerToken extracts the token from an "Authorization: Bearer <token>" header,
// or "" if the header is absent or not a bearer credential.
func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) < len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(h[len(prefix):])
}

// uiTokenFromCookie returns the localhost token carried in the rag_ui_token
// cookie, or "" when the cookie is absent.
func uiTokenFromCookie(r *http.Request) string {
	c, err := r.Cookie(uiTokenCookie)
	if err != nil {
		return ""
	}
	return c.Value
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
