package api

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/jpnorenam/rag-snap/cmd/cli/config"
	"github.com/jpnorenam/rag-snap/internal/chatstore"
	"github.com/jpnorenam/rag-snap/internal/webui"
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
	"chat_history",
	"batch_answer",
	"answer_build",
	"answer_build_columns",
	"prompts",
	"prompt_variants",
	"status",
	"config",
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
	// prompts is the daemon-owned store of prompt-template overrides. Chat
	// sessions and batch operations are seeded from it at start, so a
	// customization applies to work started after it was saved.
	prompts *promptStore
	// chats is the daemon-owned store of saved chat conversations, shared by the
	// UI and the CLI's remote mode. Sessions save into it and resume from it.
	chats *chatstore.Store
	// builds stages parsed spreadsheet/CSV tables between the two answer-build
	// passes (inspect → extract) so the document is parsed once, not re-uploaded.
	builds   *buildStore
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
	// gdrive holds at most one in-progress Google Drive OAuth flow, guarded by
	// gdriveMu. See handlers_gdrive.go.
	gdriveMu sync.Mutex
	gdrive   gdriveFlowState
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
		prompts:  newPromptStore(),
		chats:    newChatStore(),
		builds:   newBuildStore(),
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

	handler, err := s.loopbackRoutes()
	if err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("building loopback routes: %w", err)
	}
	s.loopbackSrv = &http.Server{
		Handler:           handler,
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
// the same /1.0/... API as the unix socket (via registerAPI) plus, unlike the
// socket, the embedded browser UI: static assets under /ui/ (unauthenticated so
// the shell can load), a GET / redirect into /ui/, and the /ui/login token
// handoff. The loopback listener carries no peer credentials, so requireAuth
// resolves to the token check on this transport while /ui/ assets stay open.
func (s *Server) loopbackRoutes() (http.Handler, error) {
	uiHandler, err := webui.Handler()
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()

	// Discovery root: redirect a browser hitting / into the UI.
	mux.HandleFunc("GET /{$}", s.handleRootRedirect)

	// Token handoff: `rag ui` opens /ui/login?token=... which sets a loopback
	// cookie and redirects into the SPA, so same-origin API/websocket calls
	// authenticate automatically without the token touching the SPA's JS.
	mux.HandleFunc("GET /ui/login", s.handleUILogin)

	// Static UI assets (unauthenticated). StripPrefix so the embedded FS sees
	// paths rooted at the SPA root.
	mux.Handle("/ui/", http.StripPrefix("/ui", uiHandler))

	s.registerAPI(mux)

	return mux, nil
}

// handleRootRedirect sends a browser hitting / on the loopback listener to the
// UI under /ui/.
func (s *Server) handleRootRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/ui/", http.StatusFound)
}

// handleUILogin is the token handoff endpoint `rag ui` opens. It validates the
// token in the query string against the daemon token, sets it as an HttpOnly
// cookie scoped to the loopback origin, and redirects into the SPA. This keeps
// the token out of the SPA's JavaScript and out of the address bar (the cookie
// then travels with every same-origin API call and the chat websocket upgrade).
// It is reachable without prior authentication so a freshly launched browser can
// present the token it was given.
func (s *Server) handleUILogin(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if s.token == "" || token == "" ||
		subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) != 1 {
		http.Error(w, "invalid token", http.StatusForbidden)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     uiTokenCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, "/ui/", http.StatusFound)
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
	mux.HandleFunc("PATCH /1.0/knowledge/{name}", s.requireAuth(s.handleKnowledgePatch))
	mux.HandleFunc("DELETE /1.0/knowledge/{name}", s.requireAuth(s.handleKnowledgeDelete))
	mux.HandleFunc("POST /1.0/knowledge/{name}/export", s.requireAuth(s.handleKnowledgeExport))
	mux.HandleFunc("GET /1.0/knowledge/{name}/export/{opId}/archive", s.requireAuth(s.handleKnowledgeExportDownload))

	// Google Drive import (OAuth lifecycle, resolution, and archive import).
	mux.HandleFunc("GET /1.0/knowledge/gdrive/status", s.requireAuth(s.handleGdriveStatus))
	mux.HandleFunc("POST /1.0/knowledge/gdrive/connect", s.requireAuth(s.handleGdriveConnect))
	mux.HandleFunc("POST /1.0/knowledge/gdrive/disconnect", s.requireAuth(s.handleGdriveDisconnect))
	mux.HandleFunc("POST /1.0/knowledge/gdrive/resolve", s.requireAuth(s.handleGdriveResolve))
	mux.HandleFunc("POST /1.0/knowledge/gdrive/import", s.requireAuth(s.handleGdriveImport))

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

	// Saved chats (local history: list/search, get, delete).
	mux.HandleFunc("GET /1.0/chats", s.requireAuth(s.handleChatsList))
	mux.HandleFunc("GET /1.0/chats/{id}", s.requireAuth(s.handleChatGet))
	mux.HandleFunc("DELETE /1.0/chats/{id}", s.requireAuth(s.handleChatDelete))

	// Batch answering (prepared manifest, async operation).
	mux.HandleFunc("POST /1.0/answer/batch", s.requireAuth(s.handleAnswerBatch))
	// Building a manifest from a document (Tika extraction + optional LLM
	// refinement, async operation). Does not persist or run the manifest.
	mux.HandleFunc("POST /1.0/answer/build", s.requireAuth(s.handleAnswerBuild))
	// Column-scoped extraction for spreadsheet/CSV builds: extract questions
	// from a chosen column of tables staged by POST /1.0/answer/build.
	mux.HandleFunc("POST /1.0/answer/build/extract", s.requireAuth(s.handleAnswerBuildExtract))

	// Prompt slots (daemon-owned; seed chat sessions and batch runs). The
	// slot-level endpoints keep their pre-variants semantics; the nested variant
	// endpoints add named variants and version history on the generation slots.
	mux.HandleFunc("GET /1.0/prompts", s.requireAuth(s.handlePromptsList))
	mux.HandleFunc("GET /1.0/prompts/{name}", s.requireAuth(s.handlePromptGet))
	mux.HandleFunc("PUT /1.0/prompts/{name}", s.requireAuth(s.handlePromptUpdate))
	mux.HandleFunc("DELETE /1.0/prompts/{name}", s.requireAuth(s.handlePromptReset))
	mux.HandleFunc("PATCH /1.0/prompts/{slot}", s.requireAuth(s.handleSlotActivate))
	mux.HandleFunc("GET /1.0/prompts/{slot}/variants", s.requireAuth(s.handleVariantsList))
	mux.HandleFunc("POST /1.0/prompts/{slot}/variants", s.requireAuth(s.handleVariantCreate))
	mux.HandleFunc("GET /1.0/prompts/{slot}/variants/{name}", s.requireAuth(s.handleVariantGet))
	mux.HandleFunc("PUT /1.0/prompts/{slot}/variants/{name}", s.requireAuth(s.handleVariantUpdate))
	mux.HandleFunc("DELETE /1.0/prompts/{slot}/variants/{name}", s.requireAuth(s.handleVariantDelete))
	mux.HandleFunc("GET /1.0/prompts/{slot}/variants/{name}/versions", s.requireAuth(s.handleVariantVersions))
	mux.HandleFunc("POST /1.0/prompts/{slot}/variants/{name}/restore", s.requireAuth(s.handleVariantRestore))

	// Service status (live probes of the three backends plus the daemon itself).
	mux.HandleFunc("GET /1.0/status", s.requireAuth(s.handleStatus))

	// Configuration (snapctl-backed; writes land in the user layer).
	mux.HandleFunc("GET /1.0/config", s.requireAuth(s.handleConfigList))
	mux.HandleFunc("PUT /1.0/config/{key}", s.requireAuth(s.handleConfigSet))
	mux.HandleFunc("DELETE /1.0/config/{key}", s.requireAuth(s.handleConfigUnset))
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

	// Report whether the knowledge engine has been initialized, so the UI can
	// show its init gate. Both model IDs being set is the signal that
	// `knowledge init` (or POST /1.0/knowledge-engine) has run. Model IDs are not
	// secrets, but only their presence is reported here.
	embedding, _ := config.GetString(s.ctx.Config, knowledge.ConfEmbeddingModelID)
	rerank, _ := config.GetString(s.ctx.Config, knowledge.ConfRerankModelID)
	summary["knowledge"] = map[string]any{
		"initialized": embedding != "" && rerank != "",
	}

	return summary
}
