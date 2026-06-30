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
	"time"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/processing"
)

// ingestItem describes a single source to ingest. Exactly one of File (a
// staged upload path) or URL is set per item.
type ingestItem struct {
	SourceID string `json:"source_id"`
	URL      string `json:"url,omitempty"`
	filePath string // server-side path to the staged upload; not from JSON
	cleanup  func() // optional cleanup for crawled/uploaded temp files
}

// ingestRequest is the JSON body for URL or batch ingestion of
// POST /1.0/knowledge/{name}/sources. File uploads use multipart instead.
type ingestRequest struct {
	SourceID string       `json:"source_id,omitempty"`
	URL      string       `json:"url,omitempty"`
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

	items, err := s.collectIngestItems(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(items) == 0 {
		respondError(w, http.StatusBadRequest, "no sources to ingest: provide a file upload, a url, or a batch")
		return
	}

	tikaURL := s.clients.tikaURL()
	resources := map[string][]string{"knowledge": {"/1.0/knowledge/" + name}}

	op, err := s.ops.runTask(
		fmt.Sprintf("Ingesting %d source(s) into %q", len(items), name),
		resources, true,
		func(ctx context.Context, op *Operation) error {
			defer func() {
				for _, it := range items {
					if it.cleanup != nil {
						it.cleanup()
					}
				}
			}()
			return runIngest(ctx, op, client, tikaURL, index, items)
		},
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondAsync(w, op.url(), op.view())
}

// collectIngestItems parses the request into ingest items, staging any uploaded
// file to a temp path. The caller arranges cleanup via item.cleanup.
func (s *Server) collectIngestItems(r *http.Request) ([]ingestItem, error) {
	contentType := r.Header.Get("Content-Type")

	if strings.HasPrefix(contentType, "multipart/form-data") {
		return s.collectUploadedItems(r)
	}

	var req ingestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, fmt.Errorf("invalid request body: %w", err)
	}
	if len(req.Batch) > 0 {
		return req.Batch, nil
	}
	if req.URL != "" {
		return []ingestItem{{SourceID: req.SourceID, URL: req.URL}}, nil
	}
	return nil, nil
}

// collectUploadedItems stages an uploaded file to a temp path.
func (s *Server) collectUploadedItems(r *http.Request) ([]ingestItem, error) {
	if err := r.ParseMultipartForm(processing.MaxIngestFileSize); err != nil {
		return nil, fmt.Errorf("parsing upload: %w", err)
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return nil, fmt.Errorf("missing file upload field %q: %w", "file", err)
	}
	defer file.Close()

	tmp, err := os.CreateTemp("", "ragd-upload-*"+filepath.Ext(header.Filename))
	if err != nil {
		return nil, fmt.Errorf("staging upload: %w", err)
	}
	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, fmt.Errorf("staging upload: %w", err)
	}
	tmp.Close()

	sourceID := r.FormValue("source_id")
	if sourceID == "" {
		sourceID = header.Filename
	}
	path := tmp.Name()
	return []ingestItem{{
		SourceID: sourceID,
		filePath: path,
		cleanup:  func() { _ = os.Remove(path) },
	}}, nil
}

// runIngest processes each item through the download → Tika → chunk → index
// pipeline, updating operation progress and honouring cancellation between
// items. It mirrors the CLI ingest wiring (metadata written before bulk index;
// status moved to completed/failed).
func runIngest(ctx context.Context, op *Operation, client *knowledge.OpenSearchClient, tikaURL, index string, items []ingestItem) error {
	total := len(items)
	op.UpdateMetadata(map[string]any{"sources_total": total, "sources_done": 0})

	for i := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := ingestOne(ctx, client, tikaURL, index, &items[i]); err != nil {
			return fmt.Errorf("ingesting %q: %w", items[i].SourceID, err)
		}
		op.UpdateMetadata(map[string]any{"sources_total": total, "sources_done": i + 1})
	}
	return nil
}

// ingestOne ingests a single source. For URL items it crawls first; for file
// items it uses the staged path.
func ingestOne(ctx context.Context, client *knowledge.OpenSearchClient, tikaURL, index string, item *ingestItem) error {
	filePath := item.filePath
	metadataPath := filePath
	var crawlCleanup func()
	if item.URL != "" {
		path, _, cleanup, err := processing.CrawlURL(item.URL)
		if err != nil {
			return fmt.Errorf("crawling URL: %w", err)
		}
		crawlCleanup = cleanup
		filePath = path
		metadataPath = item.URL
		if item.SourceID == "" {
			item.SourceID = item.URL
		}
	}
	if crawlCleanup != nil {
		defer crawlCleanup()
	}
	if filePath == "" {
		return fmt.Errorf("no file or URL provided")
	}
	if item.SourceID == "" {
		item.SourceID = filepath.Base(filePath)
	}

	result, err := processing.Ingest(tikaURL, filePath, item.SourceID)
	if err != nil {
		return fmt.Errorf("extracting content: %w", err)
	}

	now := time.Now().UTC().Format(knowledge.DateFormat)
	meta := knowledge.SourceMetadata{
		SourceID:      item.SourceID,
		FileName:      filepath.Base(filePath),
		FilePath:      metadataPath,
		Checksum:      result.Checksum,
		IndexName:     index,
		ChunkCount:    len(result.Chunks),
		ChunkSize:     processing.DefaultChunkSize,
		ChunkOverlap:  processing.DefaultChunkOverlap,
		ContentLength: result.ContentLength,
		Status:        knowledge.StatusProcessing,
		IngestedAt:    now,
		UpdatedAt:     now,
	}
	if result.TikaMetadata != nil {
		meta.ContentType = result.TikaMetadata.ContentType
		meta.Title = result.TikaMetadata.Title
		meta.Author = result.TikaMetadata.Author
		meta.Language = result.TikaMetadata.Language
	}
	if err := client.IndexSourceMetadata(ctx, meta); err != nil {
		return fmt.Errorf("writing source metadata: %w", err)
	}

	docs := make([]knowledge.Document, len(result.Chunks))
	for i, c := range result.Chunks {
		docs[i] = knowledge.Document{Content: c.Content, SourceID: c.SourceID, CreatedAt: c.CreatedAt}
	}
	if _, err := client.BulkIndex(ctx, index, docs); err != nil {
		_ = client.UpdateSourceStatus(ctx, item.SourceID, knowledge.StatusFailed)
		return fmt.Errorf("indexing chunks: %w", err)
	}
	if err := client.UpdateSourceStatus(ctx, item.SourceID, knowledge.StatusCompleted); err != nil {
		return fmt.Errorf("updating source status: %w", err)
	}
	return nil
}
