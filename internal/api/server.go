package api

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/jpnorenam/rag-snap/cmd/cli/common"
)

// apiVersion is the single supported major API version. New backward-compatible
// features are advertised through apiExtensions rather than a version bump.
const apiVersion = "1.0"

// apiExtensions names backward-compatible features clients can detect. It grows
// as later phases land (e.g. "operations", "chat_websocket", "batch_answer").
var apiExtensions = []string{}

// Server is the ragd HTTP API server. It owns the configuration snapshot, the
// long-lived backend readiness tracker, and the unix-socket listener.
type Server struct {
	ctx      *common.Context
	socket   SocketConfig
	backends *backendState
	httpSrv  *http.Server
	listener net.Listener
}

// Options configure a Server.
type Options struct {
	// Context carries the snapctl-backed config the daemon reads at startup.
	Context *common.Context
	// Socket describes the unix socket path/group/mode.
	Socket SocketConfig
	// BackendURLs maps service name ("opensearch"/"openai"/"tika") to base URL.
	BackendURLs map[string]string
}

// New constructs a Server from already-resolved options. It does not bind the
// socket or start polling; call Serve for that.
func New(opts Options) *Server {
	s := &Server{
		ctx:      opts.Context,
		socket:   opts.Socket,
		backends: newBackendState(opts.BackendURLs),
	}
	s.httpSrv = &http.Server{
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

// Serve binds the unix socket, starts background backend readiness polling, and
// serves the API until ctx is cancelled. Backend reachability never blocks the
// listener: the socket is served as soon as it is bound.
func (s *Server) Serve(ctx context.Context) error {
	ln, err := listenUnix(s.socket)
	if err != nil {
		return err
	}
	s.listener = ln

	go s.backends.poll(ctx, 10*time.Second)

	// Shut the HTTP server down when the context is cancelled.
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpSrv.Shutdown(shutCtx)
	}()

	if err := s.httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// routes builds the HTTP router. Phase 1 exposes only the version root and the
// /1.0 server-info endpoint; later phases register operations, knowledge, chat,
// and answer handlers on this mux.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleRoot)
	mux.HandleFunc("GET /1.0", s.handleServerInfo)
	mux.HandleFunc("GET /1.0/{$}", s.handleServerInfo)
	return mux
}

// handleRoot implements GET /: advertise the supported version(s), the caller's
// auth state, and the api_extensions list.
func (s *Server) handleRoot(w http.ResponseWriter, _ *http.Request) {
	respondSync(w, map[string]any{
		"api_status":     "stable",
		"api_version":    apiVersion,
		"auth":           s.authState(),
		"api_extensions": apiExtensions,
	})
}

// handleServerInfo implements GET /1.0: server metadata plus a read-only,
// secret-free summary of effective config and backend readiness for diagnostics.
func (s *Server) handleServerInfo(w http.ResponseWriter, _ *http.Request) {
	respondSync(w, map[string]any{
		"api_version":    apiVersion,
		"api_extensions": apiExtensions,
		"auth":           s.authState(),
		"backends":       s.backends.snapshot(),
	})
}

// authState reports whether the caller is trusted. Until the SO_PEERCRED check
// lands in phase 2, every connection arrives over the local unix socket and is
// treated as trusted; phase 2 replaces this with the real per-connection result.
func (s *Server) authState() string {
	return "trusted"
}
