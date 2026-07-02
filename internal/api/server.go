package api

import (
	"context"
	"fmt"
	"log"
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
var apiExtensions = []string{
	"etag",
	"operations",
	"events",
	"knowledge",
	"knowledge_sources",
	"knowledge_ingest",
	"knowledge_export",
	"knowledge_import",
	"knowledge_engine_init",
	"search",
	"chat_websocket",
	"batch_answer",
}

// Server is the ragd HTTP API server. It owns the configuration snapshot, the
// long-lived backend readiness tracker, the unix-socket listener, the async
// operations registry, and the events hub.
type Server struct {
	ctx      *common.Context
	socket   SocketConfig
	loopback LoopbackConfig
	backends *backendState
	clients  *clientCache
	events   *eventsHub
	ops      *operations
	httpSrv  *http.Server
	listener net.Listener
	// token is the localhost bearer token authenticating loopback requests. It
	// is populated by startLoopback when the loopback listener is enabled and
	// left empty otherwise (no token → no token-authenticated surface).
	token string
	// loopbackSrv and loopbackListenAddr are the loopback HTTP server and its
	// resolved listen address, set when the loopback listener is enabled.
	loopbackSrv        *http.Server
	loopbackListenAddr string
}

// Options configure a Server.
type Options struct {
	// Context carries the snapctl-backed config the daemon reads at startup.
	Context *common.Context
	// Socket describes the unix socket path/group/mode.
	Socket SocketConfig
	// Loopback describes the opt-in loopback TCP listener (disabled by default).
	Loopback LoopbackConfig
	// BackendURLs maps service name ("opensearch"/"openai"/"tika") to base URL.
	BackendURLs map[string]string
}

// New constructs a Server from already-resolved options. It does not bind the
// socket or start polling; call Serve for that.
func New(opts Options) *Server {
	s := &Server{
		ctx:      opts.Context,
		socket:   opts.Socket,
		loopback: opts.Loopback,
		backends: newBackendState(opts.BackendURLs),
		clients:  newClientCache(opts.Context, opts.BackendURLs),
		events:   newEventsHub(),
	}
	s.httpSrv = &http.Server{
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		// Thread each connection's peer credentials into request contexts so
		// the auth middleware can authorize by SO_PEERCRED.
		ConnContext: connContext,
	}
	return s
}

// Serve binds the unix socket, starts background backend readiness polling, and
// serves the API until ctx is cancelled. Backend reachability never blocks the
// listener: the socket is served as soon as it is bound.
func (s *Server) Serve(ctx context.Context) error {
	// The operations registry is bound to the serve context so in-flight work
	// is cancelled on shutdown/reload.
	s.ops = newOperations(ctx, s.events)

	ln, err := listenUnix(s.socket)
	if err != nil {
		return err
	}
	s.listener = ln

	// Open the opt-in loopback listener before serving the socket, so a
	// misconfigured (non-loopback) bind is a fatal startup error rather than a
	// silent downgrade. Close the unix listener if it fails.
	var loopbackLn net.Listener
	if s.loopback.Enabled {
		loopbackLn, err = s.startLoopback()
		if err != nil {
			_ = ln.Close()
			return err
		}
	}

	go s.backends.poll(ctx, 10*time.Second)

	// Shut both HTTP servers down when the context is cancelled.
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpSrv.Shutdown(shutCtx)
		if s.loopbackSrv != nil {
			_ = s.loopbackSrv.Shutdown(shutCtx)
		}
	}()

	// Serve the loopback listener in the background; the unix socket remains the
	// primary blocking serve loop.
	if loopbackLn != nil {
		go func() {
			if err := s.loopbackSrv.Serve(loopbackLn); err != nil && err != http.ErrServerClosed {
				log.Printf("loopback listener stopped: %v", err)
			}
		}()
	}

	if err := s.httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// startLoopback ensures the localhost token, builds the loopback router, binds
// the loopback listener, and prepares the loopback HTTP server (records the
// resolved address and logs it). It returns the listener for Serve to serve in a
// goroutine. Any error is fatal to startup.
func (s *Server) startLoopback() (net.Listener, error) {
	_, token, err := localhostToken()
	if err != nil {
		return nil, fmt.Errorf("preparing localhost token: %w", err)
	}
	s.token = token

	ln, err := listenLoopback(s.loopback)
	if err != nil {
		return nil, err
	}
	s.loopbackListenAddr = ln.Addr().String()

	s.loopbackSrv = &http.Server{
		Handler:           s.loopbackRoutes(),
		ReadHeaderTimeout: 10 * time.Second,
		// Tag each connection as loopback so the auth seam uses token auth.
		ConnContext: connContext,
	}
	log.Printf("serving loopback API on %s", s.loopbackListenAddr)
	return ln, nil
}

// routes builds the HTTP router. The version root GET / is reachable by an
// untrusted caller (so a client can discover whether it has access); every
// other endpoint requires a trusted peer. Later phases register knowledge,
// chat, and answer handlers on this mux.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// Discovery: open to untrusted callers.
	mux.HandleFunc("GET /{$}", s.handleRoot)

	s.registerAPI(mux)

	return mux
}

// loopbackRoutes builds the router served on the loopback listener. It registers
// exactly the same /1.0/... API as the unix socket (via registerAPI) plus the
// discovery root, and nothing else: no /ui/ assets, no /ui/login, no root
// redirect — those belong to the deferred UI change.
func (s *Server) loopbackRoutes() http.Handler {
	mux := http.NewServeMux()

	// Discovery: open to untrusted callers, same as the socket.
	mux.HandleFunc("GET /{$}", s.handleRoot)

	s.registerAPI(mux)

	return mux
}

// registerAPI registers the /1.0/... handlers on mux. It is called from both
// routes() (unix socket) and loopbackRoutes() (loopback listener) so the two
// transports always expose an identical API surface and can never drift.
func (s *Server) registerAPI(mux *http.ServeMux) {
	// Server info.
	mux.HandleFunc("GET /1.0", s.requireAuth(s.handleServerInfo))
	mux.HandleFunc("GET /1.0/{$}", s.requireAuth(s.handleServerInfo))

	// Operations & events.
	mux.HandleFunc("GET /1.0/operations", s.requireAuth(s.handleOperationsList))
	mux.HandleFunc("GET /1.0/operations/{id}", s.requireAuth(s.handleOperationGet))
	mux.HandleFunc("DELETE /1.0/operations/{id}", s.requireAuth(s.handleOperationDelete))
	mux.HandleFunc("GET /1.0/operations/{id}/wait", s.requireAuth(s.handleOperationWait))
	mux.HandleFunc("GET /1.0/operations/{id}/websocket", s.requireAuth(s.handleChatConnect))
	mux.HandleFunc("GET /1.0/events", s.requireAuth(s.handleEvents))

	// Knowledge bases.
	mux.HandleFunc("GET /1.0/knowledge", s.requireAuth(s.handleKnowledgeList))
	mux.HandleFunc("POST /1.0/knowledge", s.requireAuth(s.handleKnowledgeCreate))
	mux.HandleFunc("POST /1.0/knowledge/import", s.requireAuth(s.handleKnowledgeImport))
	mux.HandleFunc("GET /1.0/knowledge/{name}", s.requireAuth(s.handleKnowledgeGet))
	mux.HandleFunc("DELETE /1.0/knowledge/{name}", s.requireAuth(s.handleKnowledgeDelete))
	mux.HandleFunc("POST /1.0/knowledge/{name}/export", s.requireAuth(s.handleKnowledgeExport))

	// Sources.
	mux.HandleFunc("GET /1.0/knowledge/{name}/sources", s.requireAuth(s.handleSourcesList))
	mux.HandleFunc("POST /1.0/knowledge/{name}/sources", s.requireAuth(s.handleSourcesIngest))
	mux.HandleFunc("GET /1.0/knowledge/{name}/sources/{id}", s.requireAuth(s.handleSourceGet))
	mux.HandleFunc("DELETE /1.0/knowledge/{name}/sources/{id}", s.requireAuth(s.handleSourceDelete))

	// Search and engine init.
	mux.HandleFunc("POST /1.0/search", s.requireAuth(s.handleSearch))
	mux.HandleFunc("POST /1.0/knowledge-engine", s.requireAuth(s.handleEngineInit))

	// Chat (interactive websocket session).
	mux.HandleFunc("POST /1.0/chat", s.requireAuth(s.handleChatStart))

	// Batch answering (prepared manifest, async operation).
	mux.HandleFunc("POST /1.0/answer/batch", s.requireAuth(s.handleAnswerBatch))
}

// swagger:route GET / server apiRoot
//
// Discover the API.
//
// Advertises the supported version(s), the caller's auth state, and the
// api_extensions list. Reachable by an untrusted caller so a client can
// discover whether it has access.
//
//	Responses:
//	  200: syncResponse
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	respondSync(w, map[string]any{
		"api_status":     "stable",
		"api_version":    apiVersion,
		"auth":           s.authState(r),
		"api_extensions": apiExtensions,
	})
}

// swagger:route GET /1.0 server serverInfo
//
// Return server information.
//
// Server metadata plus a read-only, secret-free summary of effective config and
// backend readiness for diagnostics.
//
//	Responses:
//	  200: syncResponse
//	  403: errorResponse
func (s *Server) handleServerInfo(w http.ResponseWriter, r *http.Request) {
	respondSync(w, map[string]any{
		"api_version":    apiVersion,
		"api_extensions": apiExtensions,
		"auth":           s.authState(r),
		"backends":       s.backends.snapshot(),
		"config":         s.configSummary(),
	})
}

// authState reports whether the caller is trusted ("trusted") or not
// ("untrusted"), based on the SO_PEERCRED check for the request's connection.
func (s *Server) authState(r *http.Request) string {
	if s.authenticate(r).trusted {
		return "trusted"
	}
	return "untrusted"
}

// configSummary returns a read-only, secret-free view of the effective config
// for diagnostics. It exposes backend URLs and the socket group/mode only; no
// credentials are read or returned (secrets live in env vars, never config).
func (s *Server) configSummary() map[string]any {
	summary := map[string]any{
		"backend_urls": s.backends.urls,
		"socket": map[string]any{
			"path":  s.socket.Path,
			"group": s.socket.Group,
			"mode":  fmt.Sprintf("%#o", s.socket.Mode),
		},
	}

	// Report the loopback listener state. When enabled, expose the resolved
	// address/url and the localhost token so a trusted client (this endpoint is
	// peercred-gated to exactly the token's grantees) can reach the listener
	// without reading the owner-only token file. Kept out of the summary when
	// disabled so no token is ever returned for an inactive listener.
	loopback := map[string]any{"enabled": s.loopback.Enabled}
	if s.loopback.Enabled {
		loopback["address"] = s.loopbackListenAddr
		loopback["url"] = "http://" + s.loopbackListenAddr
		loopback["token"] = s.token
		if path, err := tokenPath(); err == nil {
			loopback["token_path"] = path
		}
	}
	summary["loopback"] = loopback

	return summary
}
