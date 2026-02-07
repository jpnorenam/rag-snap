package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"

	"github.com/jpnorenam/rag-snap/cmd/cli/common"
)

// SearchHit represents a single search result with its relevance score.
type SearchHit struct {
	Index     string  `json:"index"`
	Score     float64 `json:"score"`
	Content   string  `json:"content"`
	SourceID  string  `json:"source_id"`
	CreatedAt string  `json:"created_at"`
}

// Search performs a neural search with reranking across the given indexes,
// merges the results, and returns them sorted by score descending.
// Indexes should be full index names (e.g. "rag-snap-context-default").
// The embeddingModelID is the deployed sentence-transformer model ID
// previously stored by 'knowledge init'.
func (c *OpenSearchClient) Search(ctx context.Context, indexes []string, query, embeddingModelID string, k int) ([]SearchHit, error) {
	stopProgress := common.StartProgressSpinner("Searching knowledge base")
	defer stopProgress()

	return c.search(ctx, indexes, query, embeddingModelID, k)
}

func (c *OpenSearchClient) search(ctx context.Context, indexes []string, query, embeddingModelID string, k int) ([]SearchHit, error) {
	// Search each index individually and collect all hits.
	var allHits []SearchHit
	for _, index := range indexes {
		hits, err := c.neuralSearch(ctx, index, query, embeddingModelID, k)
		if err != nil {
			return nil, fmt.Errorf("searching index %q: %w", index, err)
		}
		allHits = append(allHits, hits...)
	}

	// Merge results from all indexes sorted by score descending.
	sort.Slice(allHits, func(i, j int) bool {
		return allHits[i].Score > allHits[j].Score
	})

	return allHits, nil
}

// neuralSearch executes a neural search with reranking on a single index.
func (c *OpenSearchClient) neuralSearch(
	ctx context.Context,
	indexName, query, embeddingModelID string,
	k int,
) ([]SearchHit, error) {
	body := buildSearchBody(query, embeddingModelID, k)

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling search body: %w", err)
	}

	path := fmt.Sprintf("/%s/_search?search_pipeline=%s", indexName, searchPipelineName)
	req, err := c.newAuthenticatedRequest(http.MethodGet, path, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.client.Client.Perform(req.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("executing search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("search request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var searchResp neuralSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("decoding search response: %w", err)
	}

	hits := make([]SearchHit, 0, len(searchResp.Hits.Hits))
	for _, hit := range searchResp.Hits.Hits {
		hits = append(hits, SearchHit{
			Index:     hit.Index,
			Score:     hit.Score,
			Content:   hit.Source.Content,
			SourceID:  hit.Source.SourceID,
			CreatedAt: hit.Source.CreatedAt,
		})
	}

	return hits, nil
}

// buildSearchBody constructs the neural search request body with reranking context.
func buildSearchBody(query, embeddingModelID string, k int) map[string]any {
	return map[string]any{
		"_source": map[string]any{
			"excludes": []string{"embedding"},
		},
		"query": map[string]any{
			"neural": map[string]any{
				"embedding": map[string]any{
					"query_text": query,
					"model_id":   embeddingModelID,
					"k":          k,
				},
			},
		},
		"ext": map[string]any{
			"rerank": map[string]any{
				"query_context": map[string]any{
					"query_text": query,
				},
			},
		},
	}
}

// neuralSearchResponse represents the OpenSearch response for a neural search query.
type neuralSearchResponse struct {
	Hits struct {
		Total struct {
			Value int `json:"value"`
		} `json:"total"`
		Hits []struct {
			Index  string  `json:"_index"`
			ID     string  `json:"_id"`
			Score  float64 `json:"_score"`
			Source struct {
				Content   string `json:"content"`
				SourceID  string `json:"source_id"`
				CreatedAt string `json:"created_at"`
			} `json:"_source"`
		} `json:"hits"`
	} `json:"hits"`
}
