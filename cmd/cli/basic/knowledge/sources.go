package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	opensearchapi "github.com/opensearch-project/opensearch-go/v4/opensearchapi"
)

const (
	sourcesIndexName = "rag-snap-metadata"

	StatusProcessing = "processing"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"

	// DateFormat matches the OpenSearch date mapping format.
	DateFormat = "2006-01-02 15:04:05"
)

// SourceMetadata tracks a single ingested source document.
type SourceMetadata struct {
	SourceID      string `json:"source_id"`
	FileName      string `json:"file_name"`
	FilePath      string `json:"file_path"`
	ContentType   string `json:"content_type,omitempty"`
	Checksum      string `json:"checksum"`
	IndexName     string `json:"index_name"`
	ChunkCount    int    `json:"chunk_count"`
	ChunkSize     int    `json:"chunk_size"`
	ChunkOverlap  int    `json:"chunk_overlap"`
	ContentLength int64  `json:"content_length"`
	Status        string `json:"status"`
	IngestedAt    string `json:"ingested_at"`
	UpdatedAt     string `json:"updated_at"`
	Title         string `json:"title,omitempty"`
	Author        string `json:"author,omitempty"`
	Language      string `json:"language,omitempty"`
}

// CreateSourcesIndex creates the sources metadata index if it does not exist.
func (c *OpenSearchClient) CreateSourcesIndex(ctx context.Context) error {
	return c.getOrCreateSourcesIndex(ctx)
}

// getOrCreateSourcesIndex checks if the sources metadata index exists and creates it if not.
func (c *OpenSearchClient) getOrCreateSourcesIndex(ctx context.Context) error {
	resp, err := c.client.Client.Do(
		ctx,
		opensearchapi.IndicesExistsReq{
			Indices: []string{sourcesIndexName},
		},
		nil,
	)
	if err != nil {
		return fmt.Errorf("error checking if sources index exists: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	body := buildSourcesIndexBody()
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("error marshaling sources index body: %w", err)
	}

	createResp, err := c.client.Client.Do(
		ctx,
		opensearchapi.IndicesCreateReq{
			Index: sourcesIndexName,
			Body:  bytes.NewReader(bodyBytes),
		},
		nil,
	)
	if err != nil {
		return fmt.Errorf("error creating sources index: %w", err)
	}
	defer createResp.Body.Close()

	if createResp.StatusCode != http.StatusOK && createResp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(createResp.Body)
		return fmt.Errorf("create sources index failed with status %d: %s", createResp.StatusCode, string(bodyBytes))
	}

	return nil
}

func buildSourcesIndexBody() map[string]any {
	return map[string]any{
		"settings": map[string]any{
			"index": map[string]any{
				"number_of_shards":   "1",
				"number_of_replicas": "1",
			},
		},
		"mappings": map[string]any{
			"properties": map[string]any{
				"source_id":      map[string]any{"type": "keyword"},
				"file_name":      map[string]any{"type": "keyword"},
				"file_path":      map[string]any{"type": "keyword"},
				"content_type":   map[string]any{"type": "keyword"},
				"checksum":       map[string]any{"type": "keyword"},
				"index_name":     map[string]any{"type": "keyword"},
				"chunk_count":    map[string]any{"type": "integer"},
				"chunk_size":     map[string]any{"type": "integer"},
				"chunk_overlap":  map[string]any{"type": "integer"},
				"content_length": map[string]any{"type": "long"},
				"status":         map[string]any{"type": "keyword"},
				"ingested_at": map[string]any{
					"type":   "date",
					"format": "yyyy-MM-dd HH:mm:ss",
				},
				"updated_at": map[string]any{
					"type":   "date",
					"format": "yyyy-MM-dd HH:mm:ss",
				},
				"title":    map[string]any{"type": "text"},
				"author":   map[string]any{"type": "keyword"},
				"language": map[string]any{"type": "keyword"},
			},
		},
	}
}

// IndexSourceMetadata indexes (upserts) a source metadata document.
func (c *OpenSearchClient) IndexSourceMetadata(ctx context.Context, meta SourceMetadata) error {
	return c.indexSourceMetadata(ctx, meta)
}

func (c *OpenSearchClient) indexSourceMetadata(ctx context.Context, meta SourceMetadata) error {
	bodyBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("error marshaling source metadata: %w", err)
	}

	path := fmt.Sprintf("/%s/_doc/%s", sourcesIndexName, meta.SourceID)
	req, err := c.newAuthenticatedRequest(http.MethodPut, path, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}

	resp, err := c.client.Client.Perform(req.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("error indexing source metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("index source metadata failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// UpdateSourceStatus updates the status and updated_at fields of a source metadata document.
func (c *OpenSearchClient) UpdateSourceStatus(ctx context.Context, sourceID, status string) error {
	return c.updateSourceStatus(ctx, sourceID, status)
}

func (c *OpenSearchClient) updateSourceStatus(ctx context.Context, sourceID, status string) error {
	updateBody := map[string]any{
		"doc": map[string]any{
			"status":     status,
			"updated_at": now(),
		},
	}

	bodyBytes, err := json.Marshal(updateBody)
	if err != nil {
		return fmt.Errorf("error marshaling update body: %w", err)
	}

	path := fmt.Sprintf("/%s/_update/%s", sourcesIndexName, sourceID)
	req, err := c.newAuthenticatedRequest(http.MethodPost, path, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}

	resp, err := c.client.Client.Perform(req.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("error updating source status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update source status failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// GetSourceMetadata retrieves a single source metadata document by ID.
func (c *OpenSearchClient) GetSourceMetadata(ctx context.Context, sourceID string) (*SourceMetadata, error) {
	return c.getSourceMetadata(ctx, sourceID)
}

func (c *OpenSearchClient) getSourceMetadata(ctx context.Context, sourceID string) (*SourceMetadata, error) {
	path := fmt.Sprintf("/%s/_doc/%s", sourcesIndexName, sourceID)
	req, err := c.newAuthenticatedRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	resp, err := c.client.Client.Perform(req.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("error getting source metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("source '%s' not found", sourceID)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get source metadata failed with status %d: %s", resp.StatusCode, string(body))
	}

	var docResp struct {
		Source SourceMetadata `json:"_source"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&docResp); err != nil {
		return nil, fmt.Errorf("error decoding source metadata: %w", err)
	}

	return &docResp.Source, nil
}

// ListSourceMetadata lists all source metadata documents, optionally filtered by index name.
func (c *OpenSearchClient) ListSourceMetadata(ctx context.Context, indexName string) ([]SourceMetadata, error) {
	return c.listSourceMetadata(ctx, indexName)
}

func (c *OpenSearchClient) listSourceMetadata(ctx context.Context, indexName string) ([]SourceMetadata, error) {
	var query map[string]any
	if indexName != "" {
		query = map[string]any{
			"query": map[string]any{
				"term": map[string]any{
					"index_name": indexName,
				},
			},
			"size": 1000,
		}
	} else {
		query = map[string]any{
			"query": map[string]any{
				"match_all": map[string]any{},
			},
			"size": 1000,
		}
	}

	bodyBytes, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("error marshaling search query: %w", err)
	}

	path := fmt.Sprintf("/%s/_search", sourcesIndexName)
	req, err := c.newAuthenticatedRequest(http.MethodPost, path, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	resp, err := c.client.Client.Perform(req.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("error listing source metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list source metadata failed with status %d: %s", resp.StatusCode, string(body))
	}

	var searchResp struct {
		Hits struct {
			Hits []struct {
				Source SourceMetadata `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("error decoding search response: %w", err)
	}

	sources := make([]SourceMetadata, 0, len(searchResp.Hits.Hits))
	for _, hit := range searchResp.Hits.Hits {
		sources = append(sources, hit.Source)
	}

	return sources, nil
}

// DeleteSourceMetadata deletes a source metadata document by ID.
func (c *OpenSearchClient) DeleteSourceMetadata(ctx context.Context, sourceID string) error {
	return c.deleteSourceMetadata(ctx, sourceID)
}

func (c *OpenSearchClient) deleteSourceMetadata(ctx context.Context, sourceID string) error {
	path := fmt.Sprintf("/%s/_doc/%s", sourcesIndexName, sourceID)
	req, err := c.newAuthenticatedRequest(http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}

	resp, err := c.client.Client.Perform(req.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("error deleting source metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete source metadata failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// DeleteChunksBySourceID deletes all chunks with the given source_id from a KNN index.
// Returns the number of deleted documents.
func (c *OpenSearchClient) DeleteChunksBySourceID(ctx context.Context, indexName string, sourceID string) (int, error) {
	return c.deleteChunksBySourceID(ctx, indexName, sourceID)
}

func (c *OpenSearchClient) deleteChunksBySourceID(ctx context.Context, indexName string, sourceID string) (int, error) {
	query := map[string]any{
		"query": map[string]any{
			"term": map[string]any{
				"source_id": sourceID,
			},
		},
	}

	bodyBytes, err := json.Marshal(query)
	if err != nil {
		return 0, fmt.Errorf("error marshaling delete query: %w", err)
	}

	path := fmt.Sprintf("/%s/_delete_by_query", indexName)
	req, err := c.newAuthenticatedRequest(http.MethodPost, path, bytes.NewReader(bodyBytes))
	if err != nil {
		return 0, fmt.Errorf("error creating request: %w", err)
	}

	resp, err := c.client.Client.Perform(req.WithContext(ctx))
	if err != nil {
		return 0, fmt.Errorf("error deleting chunks: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("delete chunks failed with status %d: %s", resp.StatusCode, string(body))
	}

	var deleteResp struct {
		Deleted int `json:"deleted"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&deleteResp); err != nil {
		return 0, fmt.Errorf("error decoding delete response: %w", err)
	}

	return deleteResp.Deleted, nil
}

// DeleteSourceMetadataByIndex deletes all source metadata records whose index_name matches
// the given indexName. Returns the number of deleted documents.
func (c *OpenSearchClient) DeleteSourceMetadataByIndex(ctx context.Context, indexName string) (int, error) {
	query := map[string]any{
		"query": map[string]any{
			"term": map[string]any{
				"index_name": indexName,
			},
		},
	}

	bodyBytes, err := json.Marshal(query)
	if err != nil {
		return 0, fmt.Errorf("error marshaling delete query: %w", err)
	}

	path := fmt.Sprintf("/%s/_delete_by_query", sourcesIndexName)
	req, err := c.newAuthenticatedRequest(http.MethodPost, path, bytes.NewReader(bodyBytes))
	if err != nil {
		return 0, fmt.Errorf("error creating request: %w", err)
	}

	resp, err := c.client.Client.Perform(req.WithContext(ctx))
	if err != nil {
		return 0, fmt.Errorf("error deleting source metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("delete source metadata by index failed with status %d: %s", resp.StatusCode, string(body))
	}

	var deleteResp struct {
		Deleted int `json:"deleted"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&deleteResp); err != nil {
		return 0, fmt.Errorf("error decoding delete response: %w", err)
	}

	return deleteResp.Deleted, nil
}

// now returns the current UTC time formatted for OpenSearch date fields.
func now() string {
	return time.Now().UTC().Format(DateFormat)
}
