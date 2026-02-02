package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const (
	ingestPipelineName = "rag-snap-ingest-pipeline"
	searchPipelineName = "rag-snap-search-pipeline"
)

// getOrCreateIngestPipeline checks if the ingest pipeline exists and creates or updates it.
// The embeddingModelID parameter specifies the model to use for text embedding.
func (c *OpenSearchClient) getOrCreateIngestPipeline(ctx context.Context, embeddingModelID string) error {
	pipeline, err := c.getIngestPipeline(ctx)
	if err != nil {
		return fmt.Errorf("error getting ingest pipeline: %w", err)
	}

	if pipeline != nil {
		// Pipeline exists, update it to ensure it uses the correct model
		if err := c.updateIngestPipeline(ctx, embeddingModelID); err != nil {
			return fmt.Errorf("error updating ingest pipeline: %w", err)
		}
		return nil
	}

	// Pipeline doesn't exist, create it
	if err := c.createIngestPipeline(ctx, embeddingModelID); err != nil {
		return fmt.Errorf("error creating ingest pipeline: %w", err)
	}

	return nil
}

// getIngestPipeline retrieves the ingest pipeline if it exists.
// Returns nil if the pipeline is not found (404).
func (c *OpenSearchClient) getIngestPipeline(ctx context.Context) (*ingestPipelineResponse, error) {
	req, err := c.newAuthenticatedRequest(http.MethodGet, fmt.Sprintf("/_ingest/pipeline/%s", ingestPipelineName), nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	resp, err := c.client.Client.Perform(req.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("error executing get ingest pipeline request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get ingest pipeline request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var pipelineResp ingestPipelineResponse
	if err := json.NewDecoder(resp.Body).Decode(&pipelineResp); err != nil {
		return nil, fmt.Errorf("error decoding ingest pipeline response: %w", err)
	}

	return &pipelineResp, nil
}

// createIngestPipeline creates a new ingest pipeline with the text embedding processor.
func (c *OpenSearchClient) createIngestPipeline(ctx context.Context, embeddingModelID string) error {
	body := buildIngestPipelineBody(embeddingModelID)

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("error marshaling ingest pipeline body: %w", err)
	}

	req, err := c.newAuthenticatedRequest(http.MethodPut, fmt.Sprintf("/_ingest/pipeline/%s", ingestPipelineName), bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}

	resp, err := c.client.Client.Perform(req.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("error executing create ingest pipeline request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create ingest pipeline request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// updateIngestPipeline updates an existing ingest pipeline.
// PUT is idempotent, so this uses the same logic as create.
func (c *OpenSearchClient) updateIngestPipeline(ctx context.Context, embeddingModelID string) error {
	body := buildIngestPipelineBody(embeddingModelID)

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("error marshaling ingest pipeline body: %w", err)
	}

	req, err := c.newAuthenticatedRequest(http.MethodPut, fmt.Sprintf("/_ingest/pipeline/%s", ingestPipelineName), bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}

	resp, err := c.client.Client.Perform(req.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("error executing update ingest pipeline request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update ingest pipeline request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// buildIngestPipelineBody constructs the ingest pipeline JSON body.
func buildIngestPipelineBody(embeddingModelID string) map[string]any {
	return map[string]any{
		"description": "rag-snap ingest pipeline",
		"processors": []map[string]any{
			{
				"text_embedding": map[string]any{
					"model_id": embeddingModelID,
					"field_map": map[string]any{
						"content": "embedding",
					},
				},
			},
		},
	}
}

// getOrCreateSearchPipeline checks if the search pipeline exists and creates or updates it.
// The rerankerModelID parameter specifies the cross-encoder model to use for reranking.
func (c *OpenSearchClient) getOrCreateSearchPipeline(ctx context.Context, rerankerModelID string) error {
	pipeline, err := c.getSearchPipeline(ctx)
	if err != nil {
		return fmt.Errorf("error getting search pipeline: %w", err)
	}

	if pipeline != nil {
		// Pipeline exists, update it to ensure it uses the correct model
		if err := c.updateSearchPipeline(ctx, rerankerModelID); err != nil {
			return fmt.Errorf("error updating search pipeline: %w", err)
		}
		return nil
	}

	// Pipeline doesn't exist, create it
	if err := c.createSearchPipeline(ctx, rerankerModelID); err != nil {
		return fmt.Errorf("error creating search pipeline: %w", err)
	}

	return nil
}

// getSearchPipeline retrieves the search pipeline if it exists.
// Returns nil if the pipeline is not found (404).
func (c *OpenSearchClient) getSearchPipeline(ctx context.Context) (*searchPipelineResponse, error) {
	req, err := c.newAuthenticatedRequest(http.MethodGet, fmt.Sprintf("/_search/pipeline/%s", searchPipelineName), nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	resp, err := c.client.Client.Perform(req.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("error executing get search pipeline request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get search pipeline request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var pipelineResp searchPipelineResponse
	if err := json.NewDecoder(resp.Body).Decode(&pipelineResp); err != nil {
		return nil, fmt.Errorf("error decoding search pipeline response: %w", err)
	}

	return &pipelineResp, nil
}

// createSearchPipeline creates a new search pipeline with the rerank processor.
func (c *OpenSearchClient) createSearchPipeline(ctx context.Context, rerankerModelID string) error {
	body := buildSearchPipelineBody(rerankerModelID)

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("error marshaling search pipeline body: %w", err)
	}

	req, err := c.newAuthenticatedRequest(http.MethodPut, fmt.Sprintf("/_search/pipeline/%s", searchPipelineName), bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}

	resp, err := c.client.Client.Perform(req.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("error executing create search pipeline request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create search pipeline request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// updateSearchPipeline updates an existing search pipeline.
// PUT is idempotent, so this uses the same logic as create.
func (c *OpenSearchClient) updateSearchPipeline(ctx context.Context, rerankerModelID string) error {
	body := buildSearchPipelineBody(rerankerModelID)

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("error marshaling search pipeline body: %w", err)
	}

	req, err := c.newAuthenticatedRequest(http.MethodPut, fmt.Sprintf("/_search/pipeline/%s", searchPipelineName), bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}

	resp, err := c.client.Client.Perform(req.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("error executing update search pipeline request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update search pipeline request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// buildSearchPipelineBody constructs the search pipeline JSON body with rerank processor.
func buildSearchPipelineBody(rerankerModelID string) map[string]any {
	return map[string]any{
		"response_processors": []map[string]any{
			{
				"rerank": map[string]any{
					"ml_opensearch": map[string]any{
						"model_id": rerankerModelID,
					},
					"context": map[string]any{
						"document_fields": []string{"content"},
					},
				},
			},
		},
	}
}

// ingestPipelineResponse represents the response from GET /_ingest/pipeline/{name}
type ingestPipelineResponse map[string]struct {
	Description string           `json:"description"`
	Processors  []map[string]any `json:"processors"`
}

// searchPipelineResponse represents the response from GET /_search/pipeline/{name}
type searchPipelineResponse map[string]struct {
	ResponseProcessors []map[string]any `json:"response_processors"`
}
