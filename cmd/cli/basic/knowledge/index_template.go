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
	indexPatterns      = "rag-snap-*"
	indexAlias         = "rag-snap-knowledge"
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
func buildIndexTemplateBody() map[string]interface{} {
	return map[string]interface{}{
		"index_patterns": []string{indexPatterns},
		"template": map[string]interface{}{
			"aliases": map[string]interface{}{
				indexAlias: map[string]interface{}{},
			},
			"settings": map[string]interface{}{
				"index": map[string]interface{}{
					"knn":                      true,
					"knn.algo_param.ef_search": 100,
					"number_of_shards":         "2",
					"number_of_replicas":       "1",
				},
			},
			"mappings": map[string]interface{}{
				"properties": map[string]interface{}{
					"source_id": map[string]interface{}{
						"type": "keyword",
					},
					"content": map[string]interface{}{
						"type": "text",
					},
					"embedding": map[string]interface{}{
						"type":       "knn_vector",
						"dimension":  embeddingDimension,
						"space_type": "l2",
						"method": map[string]interface{}{
							"name":   "hnsw",
							"engine": "faiss",
							"parameters": map[string]interface{}{
								"encoder": map[string]interface{}{
									"name": "sq",
									"parameters": map[string]interface{}{
										"type": "fp16",
									},
								},
								"ef_construction": efConstruction,
								"m":               bidirectionalLinks,
							},
						},
					},
					"created_at": map[string]interface{}{
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
				Settings map[string]interface{} `json:"settings"`
				Mappings map[string]interface{} `json:"mappings"`
				Aliases  map[string]interface{} `json:"aliases"`
			} `json:"template"`
		} `json:"index_template"`
	} `json:"index_templates"`
}
