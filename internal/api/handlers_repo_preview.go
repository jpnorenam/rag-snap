package api

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/processing"
)

// maxPreviewFiles caps the sampled paths returned by a repo preview; Total
// always carries the full match count.
const maxPreviewFiles = 200

// repoPreviewRequest is the body of POST /1.0/knowledge/repo-preview,
// mirroring the repo fields of an ingest batch item.
type repoPreviewRequest struct {
	Type       string   `json:"type"` // "github" or "gitea"
	Source     string   `json:"source"`
	Branch     string   `json:"branch,omitempty"`
	Path       string   `json:"path,omitempty"`
	Extensions []string `json:"extensions,omitempty"`
}

// repoPreviewResponse lists the files a repo ingest would match.
type repoPreviewResponse struct {
	Files     []string `json:"files"`
	Total     int      `json:"total"`
	Truncated bool     `json:"truncated"`
}

// swagger:route POST /1.0/knowledge/repo-preview knowledge repoPreview
//
// Preview the files a repository ingest would match.
//
// Lists the files of a GitHub or Gitea repository that match the given branch,
// path prefix, and extensions, without ingesting anything. Uses the same source
// parsing, filtering, and daemon environment tokens (GITHUB_TOKEN/GITEA_TOKEN)
// as ingestion. Returns a bounded sample of paths plus the full match count.
//
//	Responses:
//	  200: syncResponse
//	  400: errorResponse
//	  403: errorResponse
//	  500: errorResponse
func (s *Server) handleRepoPreview(w http.ResponseWriter, r *http.Request) {
	var req repoPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	req.Source = strings.TrimSpace(req.Source)
	if req.Source == "" {
		respondError(w, http.StatusBadRequest, "source is required")
		return
	}

	var entries []processing.RepoEntry
	var truncated bool
	switch req.Type {
	case "github":
		owner, repo, err := processing.ParseGitHubSource(req.Source)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		token := os.Getenv("GITHUB_TOKEN")
		if token == "" {
			respondError(w, http.StatusBadRequest, "GitHub ingestion requires the GITHUB_TOKEN environment variable")
			return
		}
		entries, truncated, err = processing.ListGitHubRepoFiles(owner, repo, req.Branch, req.Path, req.Extensions, token)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "listing repository files: "+err.Error())
			return
		}
	case "gitea":
		baseURL, owner, repo, err := processing.ParseGiteaSource(req.Source)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		token := os.Getenv("GITEA_TOKEN")
		if token == "" {
			respondError(w, http.StatusBadRequest, "Gitea ingestion requires the GITEA_TOKEN environment variable")
			return
		}
		entries, truncated, err = processing.ListGiteaRepoFiles(baseURL, owner, repo, req.Branch, req.Path, req.Extensions, token)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "listing repository files: "+err.Error())
			return
		}
	default:
		respondError(w, http.StatusBadRequest, `type must be "github" or "gitea"`)
		return
	}

	files := make([]string, 0, min(len(entries), maxPreviewFiles))
	for _, entry := range entries {
		if len(files) == maxPreviewFiles {
			break
		}
		files = append(files, entry.Path)
	}
	respondSync(w, repoPreviewResponse{Files: files, Total: len(entries), Truncated: truncated})
}
