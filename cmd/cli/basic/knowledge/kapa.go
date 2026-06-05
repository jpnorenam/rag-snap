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
	ConfKapaAPIKey    = "kapa.api.key"
	ConfKapaProjectID = "kapa.project.id"

	kapaBaseURL   = "https://api.kapa.ai"
	kapaIndexName = "kapa-canonical"
)

// KapaClient queries the kapa.ai retrieval API for semantic search over
// ingested Canonical documentation without LLM generation.
type KapaClient struct {
	projectID  string
	apiKey     string
	httpClient *http.Client
}

func NewKapaClient(projectID, apiKey string) *KapaClient {
	return &KapaClient{
		projectID:  projectID,
		apiKey:     apiKey,
		httpClient: &http.Client{},
	}
}

type kapaRetrievalRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type kapaChunk struct {
	SourceURL string `json:"source_url"`
	Content   string `json:"content"`
}

// Search performs semantic retrieval against the kapa.ai knowledge base and
// returns results as SearchHit so they integrate with the existing RAG pipeline.
// Results are returned in descending order of relevance; Score is rank-based.
func (c *KapaClient) Search(ctx context.Context, query string, limit int) ([]SearchHit, error) {
	reqBody, err := json.Marshal(kapaRetrievalRequest{Query: query, Limit: limit})
	if err != nil {
		return nil, fmt.Errorf("kapa: marshaling request: %w", err)
	}

	url := fmt.Sprintf("%s/query/v1/projects/%s/retrieval/", kapaBaseURL, c.projectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("kapa: creating request: %w", err)
	}
	req.Header.Set("X-API-KEY", c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kapa: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("kapa: unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var chunks []kapaChunk
	if err := json.NewDecoder(resp.Body).Decode(&chunks); err != nil {
		return nil, fmt.Errorf("kapa: decoding response: %w", err)
	}

	hits := make([]SearchHit, len(chunks))
	for i, chunk := range chunks {
		hits[i] = SearchHit{
			Index:    kapaIndexName,
			Score:    1.0 / float64(i+1),
			Content:  chunk.Content,
			SourceID: chunk.SourceURL,
		}
	}
	return hits, nil
}
