package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

const (
	ConfKapaAPIKey    = "kapa.api.key"
	ConfKapaProjectID = "kapa.project.id"
	ConfKapaEnabled   = "kapa.enabled"

	kapaBaseURL   = "https://api.kapa.ai"
	KapaIndexName = "kapa-canonical"
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
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// SourceGroup is a named grouping of sources within a Kapa project.
type SourceGroup struct {
	ID   string
	Name string
}

type kapaRetrievalRequest struct {
	Query                 string   `json:"query"`
	Limit                 int      `json:"limit"`
	SourceGroupIDsInclude []string `json:"source_group_ids_include,omitempty"`
}

type kapaChunk struct {
	SourceURL string `json:"source_url"`
	Content   string `json:"content"`
}

type kapaSourceGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type kapaSource struct {
	SourceGroups []kapaSourceGroup `json:"source_groups"`
}

type kapaSourcesPage struct {
	Next    *string      `json:"next"`
	Results []kapaSource `json:"results"`
}

// ListSourceGroups paginates through all sources and returns the unique, sorted
// set of source groups. The returned IDs should be passed to Search to filter
// retrieval; Names are for display only.
func (c *KapaClient) ListSourceGroups(ctx context.Context) ([]SourceGroup, error) {
	nextURL := fmt.Sprintf("%s/ingestion/v1/projects/%s/sources/", kapaBaseURL, c.projectID)

	seen := make(map[string]struct{})
	var groups []SourceGroup

	for nextURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, nextURL, nil)
		if err != nil {
			return nil, fmt.Errorf("kapa: creating sources request: %w", err)
		}
		req.Header.Set("X-API-KEY", c.apiKey)
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("kapa: sources request failed: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("kapa: reading sources response: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("kapa: unexpected status %d: %s", resp.StatusCode, body)
		}

		var page kapaSourcesPage
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("kapa: decoding sources: %w", err)
		}

		for _, s := range page.Results {
			for _, g := range s.SourceGroups {
				if g.ID == "" {
					continue
				}
				if _, ok := seen[g.ID]; !ok {
					seen[g.ID] = struct{}{}
					groups = append(groups, SourceGroup{ID: g.ID, Name: g.Name})
				}
			}
		}

		if page.Next != nil {
			nextURL = *page.Next
		} else {
			nextURL = ""
		}
	}

	sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
	return groups, nil
}

// Search performs semantic retrieval against the kapa.ai knowledge base and
// returns results as SearchHit so they integrate with the existing RAG pipeline.
// Results are returned in descending order of relevance; Score is rank-based.
// sourceGroupIDs filters retrieval to specific groups by ID; nil means all groups.
func (c *KapaClient) Search(ctx context.Context, query string, limit int, sourceGroupIDs []string) ([]SearchHit, error) {
	reqBody, err := json.Marshal(kapaRetrievalRequest{
		Query:                 query,
		Limit:                 limit,
		SourceGroupIDsInclude: sourceGroupIDs,
	})
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
			Index:    KapaIndexName,
			Score:    1.0 / float64(i+1),
			Content:  chunk.Content,
			SourceID: chunk.SourceURL,
		}
	}
	return hits, nil
}
