package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/processing"
)

// ingestItem describes a single source to ingest. For URL items URL is set; for
// repo items Type is "github"/"gitea" and Source carries the repo reference; for
// uploaded files filePath is the staged path.
type ingestItem struct {
	SourceID   string   `json:"source_id"`
	URL        string   `json:"url,omitempty"`
	Type       string   `json:"type,omitempty"`       // "", "url", "github", "gitea", "file"
	Source     string   `json:"source,omitempty"`     // repo reference for github/gitea
	Branch     string   `json:"branch,omitempty"`     // repo branch (github/gitea)
	Path       string   `json:"path,omitempty"`       // repo subpath (github/gitea)
	Extensions []string `json:"extensions,omitempty"` // repo file extensions (github/gitea)
	filePath   string   // server-side path to the staged upload; not from JSON
	cleanup    func()   // optional cleanup for crawled/uploaded temp files
}

// ingestRequest is the JSON body for URL or batch ingestion of
// POST /1.0/knowledge/{name}/sources. File uploads use multipart instead.
type ingestRequest struct {
	SourceID string       `json:"source_id,omitempty"`
	URL      string       `json:"url,omitempty"`
	Force    bool         `json:"force,omitempty"`
	Batch    []ingestItem `json:"batch,omitempty"`
}

// swagger:route POST /1.0/knowledge/{name}/sources knowledge sourcesIngest
//
// Ingest sources into a knowledge base.
//
// Ingests one or more sources as an async operation. Accepts either a multipart
// file upload or a JSON body describing a URL or a batch of URLs.
//
//	Consumes:
//	- application/json
//	- multipart/form-data
//
//	Responses:
//	  202: asyncResponse
//	  400: errorResponse
//	  403: errorResponse
//	  404: errorResponse
//	  500: errorResponse
func (s *Server) handleSourcesIngest(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	client, err := s.clients.openSearchClient()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	index := knowledge.FullIndexName(name)
	exists, err := client.IndexExists(r.Context(), index)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !exists {
		respondError(w, http.StatusNotFound, "knowledge base not found: "+name)
		return
	}

	items, force, err := s.collectIngestItems(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(items) == 0 {
		respondError(w, http.StatusBadRequest, "no sources to ingest: provide a file upload, a url, or a batch")
		return
	}

	// Synchronous duplicate pre-check for a single non-repo ingest, so the client
	// gets a conflict it can surface inline instead of a failed async operation.
	if len(items) == 1 && !force && isSingleFileOrURL(items[0]) {
		if id := effectiveSourceID(items[0]); id != "" && client.SourceCompleted(r.Context(), id) {
			cleanupItems(items)
			respondError(w, http.StatusConflict, fmt.Sprintf("source %q already exists; re-ingest with force to replace it", id))
			return
		}
	}

	tikaURL := s.clients.tikaURL()
	resources := map[string][]string{"knowledge": {"/1.0/knowledge/" + name}}

	op, err := s.ops.runTask(
		fmt.Sprintf("Ingesting %d source(s) into %q", len(items), name),
		resources, true,
		func(ctx context.Context, op *Operation) error {
			defer cleanupItems(items)
			return runIngest(ctx, op, client, tikaURL, index, items, force)
		},
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondAsync(w, op.url(), op.view())
}

// cleanupItems runs any staged-file cleanups on the given items.
func cleanupItems(items []ingestItem) {
	for _, it := range items {
		if it.cleanup != nil {
			it.cleanup()
		}
	}
}

// isSingleFileOrURL reports whether the item is an individual file or URL source
// (as opposed to a repo that expands into many sources).
func isSingleFileOrURL(it ingestItem) bool {
	switch it.Type {
	case "github", "gitea":
		return false
	default:
		return true
	}
}

// effectiveSourceID returns the source id an item will ingest under, mirroring
// the defaults IngestSource applies.
func effectiveSourceID(it ingestItem) string {
	if it.SourceID != "" {
		return it.SourceID
	}
	if it.URL != "" {
		return it.URL
	}
	if it.filePath != "" {
		return filepath.Base(it.filePath)
	}
	return ""
}

// collectIngestItems parses the request into ingest items plus the force flag,
// staging any uploaded file to a temp path. The caller arranges cleanup via
// item.cleanup.
func (s *Server) collectIngestItems(r *http.Request) ([]ingestItem, bool, error) {
	contentType := r.Header.Get("Content-Type")

	if strings.HasPrefix(contentType, "multipart/form-data") {
		return s.collectUploadedItems(r)
	}

	var req ingestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, false, fmt.Errorf("invalid request body: %w", err)
	}
	if len(req.Batch) > 0 {
		return req.Batch, req.Force, nil
	}
	if req.URL != "" {
		return []ingestItem{{SourceID: req.SourceID, URL: req.URL, Type: "url"}}, req.Force, nil
	}
	return nil, req.Force, nil
}

// collectUploadedItems stages an uploaded file to a temp path.
func (s *Server) collectUploadedItems(r *http.Request) ([]ingestItem, bool, error) {
	if err := r.ParseMultipartForm(processing.MaxIngestFileSize); err != nil {
		return nil, false, fmt.Errorf("parsing upload: %w", err)
	}
	force := r.FormValue("force") == "true"
	file, header, err := r.FormFile("file")
	if err != nil {
		return nil, false, fmt.Errorf("missing file upload field %q: %w", "file", err)
	}
	defer file.Close()

	tmp, err := os.CreateTemp("", "ragd-upload-*"+filepath.Ext(header.Filename))
	if err != nil {
		return nil, false, fmt.Errorf("staging upload: %w", err)
	}
	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, false, fmt.Errorf("staging upload: %w", err)
	}
	tmp.Close()

	sourceID := r.FormValue("source_id")
	if sourceID == "" {
		sourceID = header.Filename
	}
	path := tmp.Name()
	return []ingestItem{{
		SourceID: sourceID,
		Type:     "file",
		filePath: path,
		cleanup:  func() { _ = os.Remove(path) },
	}}, force, nil
}

// runIngest processes each item, updating operation progress and honouring
// cancellation between items. Repo items expand into many files server-side.
func runIngest(ctx context.Context, op *Operation, client *knowledge.OpenSearchClient, tikaURL, index string, items []ingestItem, force bool) error {
	total := len(items)
	op.UpdateMetadata(map[string]any{"sources_total": total, "sources_done": 0})

	for i := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := ingestOneItem(ctx, client, tikaURL, index, items[i], force); err != nil {
			return fmt.Errorf("ingesting %q: %w", effectiveSourceID(items[i]), err)
		}
		op.UpdateMetadata(map[string]any{"sources_total": total, "sources_done": i + 1})
	}
	return nil
}

// ingestOneItem dispatches one item by type. github/gitea items expand into
// multiple files; url/file items ingest a single source. In batch context an
// already-completed source is skipped unless force is set.
func ingestOneItem(ctx context.Context, client *knowledge.OpenSearchClient, tikaURL, index string, item ingestItem, force bool) error {
	switch item.Type {
	case "github":
		return ingestGitHubRepo(ctx, client, tikaURL, index, item, force)
	case "gitea":
		return ingestGiteaRepo(ctx, client, tikaURL, index, item, force)
	case "url":
		path, _, cleanup, err := processing.CrawlURL(item.URL)
		if err != nil {
			return fmt.Errorf("crawling URL: %w", err)
		}
		defer cleanup()
		sourceID := item.SourceID
		if sourceID == "" {
			sourceID = item.URL
		}
		return ingestResolvedFile(ctx, client, tikaURL, index, path, sourceID, item.URL, force)
	default: // staged file upload
		if item.filePath == "" {
			if item.Type == "file" {
				return fmt.Errorf("batch %q entries referencing local paths are not supported over the API; upload the file directly instead", "file")
			}
			return fmt.Errorf("no file or URL provided")
		}
		sourceID := item.SourceID
		if sourceID == "" {
			sourceID = filepath.Base(item.filePath)
		}
		return ingestResolvedFile(ctx, client, tikaURL, index, item.filePath, sourceID, item.filePath, force)
	}
}

// ingestResolvedFile skips an already-completed source unless force is set, then
// runs the shared ingest core.
func ingestResolvedFile(ctx context.Context, client *knowledge.OpenSearchClient, tikaURL, index, filePath, sourceID, metadataPath string, force bool) error {
	if !force && client.SourceCompleted(ctx, sourceID) {
		return nil
	}
	return client.IngestSource(ctx, tikaURL, knowledge.IngestOptions{
		FilePath:     filePath,
		SourceID:     sourceID,
		MetadataPath: metadataPath,
		TargetIndex:  index,
		Force:        force,
	})
}

// ingestGitHubRepo lists a GitHub repo's matching files and ingests each. A
// missing token fails the whole repo entry with the exact env-var hint.
func ingestGitHubRepo(ctx context.Context, client *knowledge.OpenSearchClient, tikaURL, index string, item ingestItem, force bool) error {
	owner, repo, err := processing.ParseGitHubSource(item.Source)
	if err != nil {
		return fmt.Errorf("parsing GitHub source: %w", err)
	}
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return fmt.Errorf("GitHub ingestion requires the GITHUB_TOKEN environment variable")
	}
	entries, err := processing.ListGitHubRepoFiles(owner, repo, item.Branch, item.Path, item.Extensions, token)
	if err != nil {
		return fmt.Errorf("listing repository files: %w", err)
	}
	return ingestRepoEntries(ctx, client, tikaURL, index, entries, token, force)
}

// ingestGiteaRepo mirrors ingestGitHubRepo for Gitea.
func ingestGiteaRepo(ctx context.Context, client *knowledge.OpenSearchClient, tikaURL, index string, item ingestItem, force bool) error {
	baseURL, owner, repo, err := processing.ParseGiteaSource(item.Source)
	if err != nil {
		return fmt.Errorf("parsing Gitea source: %w", err)
	}
	token := os.Getenv("GITEA_TOKEN")
	if token == "" {
		return fmt.Errorf("Gitea ingestion requires the GITEA_TOKEN environment variable")
	}
	entries, err := processing.ListGiteaRepoFiles(baseURL, owner, repo, item.Branch, item.Path, item.Extensions, token)
	if err != nil {
		return fmt.Errorf("listing repository files: %w", err)
	}
	return ingestRepoEntries(ctx, client, tikaURL, index, entries, token, force)
}

// ingestRepoEntries fetches and ingests each repo file, honouring cancellation.
func ingestRepoEntries(ctx context.Context, client *knowledge.OpenSearchClient, tikaURL, index string, entries []processing.RepoEntry, token string, force bool) error {
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		tempPath, cleanup, err := processing.FetchRepoFile(entry.RawURL, entry.Path, token)
		if err != nil {
			return fmt.Errorf("fetching %q: %w", entry.Path, err)
		}
		err = ingestResolvedFile(ctx, client, tikaURL, index, tempPath, entry.Path, entry.Path, force)
		cleanup()
		if err != nil {
			return fmt.Errorf("ingesting %q: %w", entry.Path, err)
		}
	}
	return nil
}
