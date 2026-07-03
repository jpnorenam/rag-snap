package api

import (
	"context"
	"fmt"
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
	backends *backendState
	clients  *clientCache
	events   *eventsHub
	ops      *operations
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

// routes builds the HTTP router. The version root GET / is reachable by an
// untrusted caller (so a client can discover whether it has access); every
// other endpoint requires a trusted peer. Later phases register knowledge,
// chat, and answer handlers on this mux.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// Discovery: open to untrusted callers.
	mux.HandleFunc("GET /{$}", s.handleRoot)

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

	return mux
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
	return map[string]any{
		"backend_urls": s.backends.urls,
		"socket": map[string]any{
			"path":  s.socket.Path,
			"group": s.socket.Group,
			"mode":  fmt.Sprintf("%#o", s.socket.Mode),
		},
	}
}
