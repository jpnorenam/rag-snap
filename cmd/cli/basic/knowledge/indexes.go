package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	opensearchapi "github.com/opensearch-project/opensearch-go/v4/opensearchapi"
)

const (
	indexTemplateName  = "rag-snap-index-template"
	indexPatterns      = "rag-snap-context-*"
	indexAlias         = "rag-snap-context"
	indexDefaultSubfix = "default"
	embeddingDimension = 768
	efConstruction     = 256
	bidirectionalLinks = 16
)

// getOrCreateIndexTemplate checks if the index template exists and creates or updates it.
func (c *OpenSearchClient) getOrCreateIndexTemplate(ctx context.Context) error {
	template, err := c.getIndexTemplate(ctx)
	if err != nil {
		return fmt.Errorf("error getting index template: %w", err)
	}

	if template != nil {
		// Template exists, update it to ensure it matches the expected structure
		if err := c.updateIndexTemplate(ctx); err != nil {
			return fmt.Errorf("error updating index template: %w", err)
		}
		return nil
	}

	// Template doesn't exist, create it
	if err := c.createIndexTemplate(ctx); err != nil {
		return fmt.Errorf("error creating index template: %w", err)
	}

	return nil
}

// getIndexTemplate retrieves the index template if it exists.
// Returns nil if the template is not found (404).
func (c *OpenSearchClient) getIndexTemplate(ctx context.Context) (*indexTemplateResponse, error) {
	resp, err := c.client.Client.Do(
		ctx,
		opensearchapi.IndexTemplateGetReq{
			IndexTemplates: []string{indexTemplateName},
		},
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("error executing get index template request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get index template request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var templateResp indexTemplateResponse
	if err := json.NewDecoder(resp.Body).Decode(&templateResp); err != nil {
		return nil, fmt.Errorf("error decoding index template response: %w", err)
	}

	return &templateResp, nil
}

// createIndexTemplate creates a new index template.
func (c *OpenSearchClient) createIndexTemplate(ctx context.Context) error {
	body := buildIndexTemplateBody()

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("error marshaling index template body: %w", err)
	}

	resp, err := c.client.Client.Do(
		ctx,
		opensearchapi.IndexTemplateCreateReq{
			IndexTemplate: indexTemplateName,
			Body:          bytes.NewReader(bodyBytes),
		},
		nil,
	)
	if err != nil {
		return fmt.Errorf("error executing create index template request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create index template request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// updateIndexTemplate updates an existing index template.
// PUT is idempotent, so this uses the same logic as create.
func (c *OpenSearchClient) updateIndexTemplate(ctx context.Context) error {
	body := buildIndexTemplateBody()

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("error marshaling index template body: %w", err)
	}

	resp, err := c.client.Client.Do(
		ctx,
		opensearchapi.IndexTemplateDeleteReq{
			IndexTemplate: indexTemplateName,
		},
		nil,
	)
	if err != nil {
		return fmt.Errorf("error executing update index template request: %w", err)
	}
	defer resp.Body.Close()

	resp, err = c.client.Client.Do(
		ctx,
		opensearchapi.IndexTemplateCreateReq{
			IndexTemplate: indexTemplateName,
			Body:          bytes.NewReader(bodyBytes),
		},
		nil,
	)
	if err != nil {
		return fmt.Errorf("error executing update index template request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update index template request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// buildIndexTemplateBody constructs the index template JSON body.
func buildIndexTemplateBody() map[string]any {
	return map[string]any{
		"index_patterns": []string{indexPatterns},
		"template": map[string]any{
			"aliases": map[string]any{
				indexAlias: map[string]any{},
			},
			"settings": map[string]any{
				"index": map[string]any{
					"knn":                      true,
					"knn.algo_param.ef_search": 100,
					"number_of_shards":         "2",
					"number_of_replicas":       "1",
				},
			},
			"mappings": map[string]any{
				"properties": map[string]any{
					"source_id": map[string]any{
						"type": "keyword",
					},
					"content": map[string]any{
						"type": "text",
					},
					"embedding": map[string]any{
						"type":       "knn_vector",
						"dimension":  embeddingDimension,
						"space_type": "l2",
						"method": map[string]any{
							"name":   "hnsw",
							"engine": "faiss",
							"parameters": map[string]any{
								"encoder": map[string]any{
									"name": "sq",
									"parameters": map[string]any{
										"type": "fp16",
									},
								},
								"ef_construction": efConstruction,
								"m":               bidirectionalLinks,
							},
						},
					},
					"created_at": map[string]any{
						"type":   "date",
						"format": "yyyy-MM-dd HH:mm:ss",
					},
				},
			},
		},
	}
}

// indexTemplateResponse represents the response from GET /_index_template/{name}
type indexTemplateResponse struct {
	IndexTemplates []struct {
		Name          string `json:"name"`
		IndexTemplate struct {
			IndexPatterns []string `json:"index_patterns"`
			Template      struct {
				Settings map[string]any `json:"settings"`
				Mappings map[string]any `json:"mappings"`
				Aliases  map[string]any `json:"aliases"`
			} `json:"template"`
		} `json:"index_template"`
	} `json:"index_templates"`
}

// IndexInfo contains information about an index.
type IndexInfo struct {
	Name      string `json:"index"`
	Health    string `json:"health"`
	Status    string `json:"status"`
	DocsCount string `json:"docs.count"`
	StoreSize string `json:"store.size"`
}

// listIndexes retrieves all indexes matching the indexPatterns pattern.
func (c *OpenSearchClient) catIndexes(ctx context.Context) ([]IndexInfo, error) {
	resp, err := c.client.Client.Do(
		ctx,
		opensearchapi.CatIndicesReq{
			Indices: []string{indexPatterns},
			Params: opensearchapi.CatIndicesParams{
				Pretty: true,
			},
		},
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("error listing indexes: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// No indexes match the pattern
		return []IndexInfo{}, nil
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list indexes request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var indexes []IndexInfo
	if err := json.NewDecoder(resp.Body).Decode(&indexes); err != nil {
		return nil, fmt.Errorf("error decoding indexes response: %w", err)
	}

	return indexes, nil
}

// getOrCreateIndex ensures the index exists.
// If the index already exists, this is a no-op.
func (c *OpenSearchClient) getOrCreateIndex(ctx context.Context, indexNameSubfix string) error {
	// Check if the index exists
	resp, err := c.client.Client.Do(
		ctx,
		opensearchapi.IndicesExistsReq{
			Indices: []string{fmt.Sprintf("%s-%s", indexAlias, indexNameSubfix)},
		},
		nil,
	)
	if err != nil {
		return fmt.Errorf("error checking if index exists: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		// Index already exists
		return nil
	}

	// Create the index (it will inherit settings from the template)
	createResp, err := c.client.Client.Do(
		ctx,
		opensearchapi.IndicesCreateReq{
			Index: fmt.Sprintf("%s-%s", indexAlias, indexNameSubfix),
		},
		nil,
	)
	if err != nil {
		return fmt.Errorf("error creating index: %w", err)
	}
	defer createResp.Body.Close()

	if createResp.StatusCode != http.StatusOK && createResp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(createResp.Body)
		return fmt.Errorf("create index request failed with status %d: %s", createResp.StatusCode, string(bodyBytes))
	}

	return nil
}
