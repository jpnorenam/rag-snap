package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
)

// gdriveFlowTimeout bounds how long the daemon waits for the user to complete
// Google's consent screen before the pending flow is abandoned.
const gdriveFlowTimeout = 5 * time.Minute

// gdriveFlowState holds at most one in-progress OAuth flow. A second connect
// supersedes any prior pending flow. It is guarded by the enclosing mutex.
type gdriveFlowState struct {
	flow      *knowledge.DriveOAuthFlow
	pending   bool
	lastError string
	cancel    context.CancelFunc
}

// gdriveStatusView is the body of GET /1.0/knowledge/gdrive/status.
type gdriveStatusView struct {
	Configured bool   `json:"configured"`
	Connected  bool   `json:"connected"`
	Pending    bool   `json:"pending"`
	Account    string `json:"account,omitempty"`
	Error      string `json:"error,omitempty"`
}

// swagger:route GET /1.0/knowledge/gdrive/status knowledge gdriveStatus
//
// Report Google Drive connection status.
//
// Reports whether Drive import is configured, whether a valid token is stored,
// whether an OAuth flow is pending, and the connected account when available.
// The token itself is never returned.
//
//	Responses:
//	  200: syncResponse
//	  403: errorResponse
func (s *Server) handleGdriveStatus(w http.ResponseWriter, r *http.Request) {
	view := gdriveStatusView{Configured: knowledge.DriveConfigured(s.ctx.Config)}

	s.gdriveMu.Lock()
	view.Pending = s.gdrive.pending
	view.Error = s.gdrive.lastError
	s.gdriveMu.Unlock()

	if view.Configured && !view.Pending {
		if token, err := knowledge.DriveAccessToken(r.Context(), s.ctx.Config); err == nil && token != "" {
			view.Connected = true
			// Account email is best-effort; omit it on failure.
			if email, err := knowledge.GetDriveAccountEmail(r.Context(), token); err == nil {
				view.Account = email
			}
		}
	}
	respondSync(w, view)
}

// gdriveConnectView is the body of POST /1.0/knowledge/gdrive/connect.
type gdriveConnectView struct {
	ConsentURL string `json:"consent_url"`
}

// swagger:route POST /1.0/knowledge/gdrive/connect knowledge gdriveConnect
//
// Start the Google Drive OAuth flow.
//
// Starts a loopback OAuth flow in the background and returns the Google consent
// URL for the UI to open in a new tab. Does not block for consent; the UI polls
// status for completion.
//
//	Responses:
//	  200: syncResponse
//	  403: errorResponse
//	  412: errorResponse
//	  500: errorResponse
func (s *Server) handleGdriveConnect(w http.ResponseWriter, _ *http.Request) {
	if !knowledge.DriveConfigured(s.ctx.Config) {
		respondError(w, http.StatusPreconditionFailed,
			"Google Drive is not configured: set gdrive.client.id and gdrive.client.secret (package config)")
		return
	}

	flow, err := knowledge.StartDriveOAuthFlow(s.ctx.Config)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// A background context (not the request's) so the flow outlives the HTTP
	// request; a timeout bounds an abandoned consent.
	ctx, cancel := context.WithTimeout(context.Background(), gdriveFlowTimeout)

	s.gdriveMu.Lock()
	// Supersede any prior pending flow.
	if s.gdrive.cancel != nil {
		s.gdrive.cancel()
	}
	s.gdrive = gdriveFlowState{flow: flow, pending: true, cancel: cancel}
	s.gdriveMu.Unlock()

	go s.awaitGdriveFlow(ctx, cancel, flow)

	respondSync(w, gdriveConnectView{ConsentURL: flow.ConsentURL()})
}

// awaitGdriveFlow blocks on the OAuth callback, persists the token on success,
// and records the outcome — but only while this flow is still the active one, so
// a superseded flow cannot clobber a newer flow's state.
func (s *Server) awaitGdriveFlow(ctx context.Context, cancel context.CancelFunc, flow *knowledge.DriveOAuthFlow) {
	defer cancel()
	tok, err := flow.Await(ctx, gdriveFlowTimeout)

	s.gdriveMu.Lock()
	defer s.gdriveMu.Unlock()
	if s.gdrive.flow != flow {
		return // superseded by a newer flow; leave its state untouched
	}
	s.gdrive.pending = false
	s.gdrive.cancel = nil
	if err != nil {
		s.gdrive.lastError = err.Error()
		return
	}
	if err := knowledge.SaveDriveToken(tok); err != nil {
		s.gdrive.lastError = err.Error()
		return
	}
	s.gdrive.lastError = ""
}

// swagger:route POST /1.0/knowledge/gdrive/disconnect knowledge gdriveDisconnect
//
// Disconnect Google Drive.
//
// Deletes the stored Drive token, disconnecting the account.
//
//	Responses:
//	  200: syncResponse
//	  403: errorResponse
//	  500: errorResponse
func (s *Server) handleGdriveDisconnect(w http.ResponseWriter, _ *http.Request) {
	if err := knowledge.DeleteDriveToken(); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.gdriveMu.Lock()
	s.gdrive.lastError = ""
	s.gdriveMu.Unlock()
	respondSync(w, map[string]string{"status": "disconnected"})
}

// gdriveResolveRequest is the body of POST /1.0/knowledge/gdrive/resolve.
type gdriveResolveRequest struct {
	URL string `json:"url"`
}

// gdriveArchiveView describes one discovered archive.
type gdriveArchiveView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified string `json:"modified,omitempty"`
}

// gdriveResolveView is the body returned by resolve.
type gdriveResolveView struct {
	Kind     string              `json:"kind"` // "file" or "folder"
	Archives []gdriveArchiveView `json:"archives"`
}

// swagger:route POST /1.0/knowledge/gdrive/resolve knowledge gdriveResolve
//
// Resolve a Google Drive URL into archives.
//
// Resolves a Drive folder or file URL into the list of discovered .tar.gz
// archives, distinguishing not found, no access, and unrecognised-URL errors.
//
//	Responses:
//	  200: syncResponse
//	  400: errorResponse
//	  403: errorResponse
//	  404: errorResponse
//	  412: errorResponse
//	  502: errorResponse
func (s *Server) handleGdriveResolve(w http.ResponseWriter, r *http.Request) {
	var req gdriveResolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if strings.TrimSpace(req.URL) == "" {
		respondError(w, http.StatusBadRequest, "url is required")
		return
	}

	kind, resourceID, err := knowledge.ParseDriveURL(req.URL)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	token, err := knowledge.DriveAccessToken(r.Context(), s.ctx.Config)
	if err != nil {
		respondError(w, http.StatusPreconditionFailed, err.Error())
		return
	}
	if token == "" {
		respondError(w, http.StatusPreconditionFailed,
			"not connected to Google Drive: connect a Google account first")
		return
	}

	switch kind {
	case knowledge.DriveKindFolder:
		archives, err := knowledge.ListDriveArchives(r.Context(), resourceID, token)
		if err != nil {
			respondError(w, driveErrorStatus(err), driveErrorMessage(err))
			return
		}
		respondSync(w, gdriveResolveView{Kind: "folder", Archives: toArchiveViews(archives)})
	case knowledge.DriveKindFile:
		archive, err := knowledge.GetDriveFile(r.Context(), resourceID, token)
		if err != nil {
			respondError(w, driveErrorStatus(err), driveErrorMessage(err))
			return
		}
		respondSync(w, gdriveResolveView{Kind: "file", Archives: toArchiveViews([]knowledge.DriveArchive{archive})})
	}
}

// toArchiveViews maps Drive archives to their JSON views.
func toArchiveViews(archives []knowledge.DriveArchive) []gdriveArchiveView {
	views := make([]gdriveArchiveView, 0, len(archives))
	for _, a := range archives {
		views = append(views, gdriveArchiveView{ID: a.ID, Name: a.Name, Size: a.Size, Modified: a.Modified})
	}
	return views
}

// driveErrorStatus classifies a Drive API error into an HTTP status so the UI
// can render a specific message (not found / no access / other).
func driveErrorStatus(err error) int {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "HTTP 404"):
		return http.StatusNotFound
	case strings.Contains(msg, "HTTP 401"), strings.Contains(msg, "HTTP 403"):
		return http.StatusForbidden
	default:
		return http.StatusBadGateway
	}
}

// driveErrorMessage turns a raw Drive error into an actionable sentence.
func driveErrorMessage(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "HTTP 404"):
		return "not found in Google Drive with this account — check the URL and that the account has access"
	case strings.Contains(msg, "HTTP 401"), strings.Contains(msg, "HTTP 403"):
		return "the connected Google account cannot access this URL — reconnect with an account that has access"
	default:
		return err.Error()
	}
}

// gdriveImportRequest is the body of POST /1.0/knowledge/gdrive/import. Each
// call imports one archive; the UI issues one call per selected archive so each
// becomes an independently tracked operation.
type gdriveImportRequest struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Target string `json:"target,omitempty"`
	Force  bool   `json:"force,omitempty"`
}

// swagger:route POST /1.0/knowledge/gdrive/import knowledge gdriveImport
//
// Import a Google Drive archive.
//
// Downloads a single Drive archive and imports it as a tracked async operation.
// The UI issues one call per selected archive so each is tracked independently.
//
//	Responses:
//	  202: asyncResponse
//	  400: errorResponse
//	  403: errorResponse
//	  412: errorResponse
//	  500: errorResponse
func (s *Server) handleGdriveImport(w http.ResponseWriter, r *http.Request) {
	var req gdriveImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if strings.TrimSpace(req.ID) == "" {
		respondError(w, http.StatusBadRequest, "id is required")
		return
	}

	token, err := knowledge.DriveAccessToken(r.Context(), s.ctx.Config)
	if err != nil {
		respondError(w, http.StatusPreconditionFailed, err.Error())
		return
	}
	if token == "" {
		respondError(w, http.StatusPreconditionFailed,
			"not connected to Google Drive: connect a Google account first")
		return
	}

	client, err := s.clients.openSearchClient()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Derive the target knowledge-base name from the archive filename when the
	// caller does not override it, mirroring the CLI's Drive import.
	target := strings.TrimSpace(req.Target)
	if target == "" {
		target = knowledge.ArchiveStem(req.Name)
	}
	archive := knowledge.DriveArchive{ID: req.ID, Name: req.Name}
	force := req.Force

	op, err := s.ops.runTask(
		fmt.Sprintf("Importing %q from Google Drive", req.Name),
		map[string][]string{"knowledge": {"/1.0/knowledge"}}, false,
		func(ctx context.Context, _ *Operation) error {
			tmpPath, cleanup, err := knowledge.DownloadDriveArchive(ctx, archive, token)
			if err != nil {
				return fmt.Errorf("downloading %q: %w", req.Name, err)
			}
			defer cleanup()
			return knowledge.ImportKnowledgeBase(ctx, client, target, knowledge.ImportOptions{
				InputDir: tmpPath,
				Force:    force,
			})
		},
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondAsync(w, op.url(), op.view())
}
