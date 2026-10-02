package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"
)

const (
	ConfKapaProjectID = "kapa.project.id"
	ConfKapaEnabled   = "kapa.enabled"

	// EnvKapaAPIKey is the only source of the API key: like CHAT_API_KEY and the
	// OpenSearch credentials, it is a secret and never lives in config.
	EnvKapaAPIKey = "KAPA_API_KEY"
	// EnvKapaProjectID overrides the kapa.project.id config key.
	EnvKapaProjectID = "KAPA_PROJECT_ID"

	kapaBaseURL   = "https://api.kapa.ai"
	KapaIndexName = "kapa-canonical"
)

// KapaEnabled interprets the raw kapa.enabled config value. An unset value means
// enabled, so an installation that never set the key is not treated as having
// explicitly disabled the integration.
func KapaEnabled(raw string) bool {
	if raw == "" {
		return true
	}
	return raw == "true" || raw == "1"
}

// ResolveKapaClient is the single rule for when a kapa client exists: the
// integration is enabled and both a project ID and an API key are present. It
// takes plain values and touches neither config nor the environment, so the CLI
// and the daemon share it and it is testable without snapctl. Returns nil when
// no client can be built.
func ResolveKapaClient(enabled bool, projectID, apiKey string) *KapaClient {
	if !enabled || projectID == "" || apiKey == "" {
		return nil
	}
	return NewKapaClient(projectID, apiKey)
}

// KapaClient queries the kapa.ai retrieval API for semantic search over
// ingested Canonical documentation without LLM generation.
type KapaClient struct {
	baseURL    string
	projectID  string
	apiKey     string
	httpClient *http.Client
}

func NewKapaClient(projectID, apiKey string) *KapaClient {
	return NewKapaClientAt(kapaBaseURL, projectID, apiKey)
}

// NewKapaClientAt is NewKapaClient against another kapa.ai API root, such as a
// test server standing in for https://api.kapa.ai.
func NewKapaClientAt(baseURL, projectID, apiKey string) *KapaClient {
	return &KapaClient{
		baseURL:    baseURL,
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
	nextURL := fmt.Sprintf("%s/ingestion/v1/projects/%s/sources/", c.baseURL, c.projectID)

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

	url := fmt.Sprintf("%s/query/v1/projects/%s/retrieval/", c.baseURL, c.projectID)
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

	// kapa.ai treats the limit as a hint and can return more chunks; cap them so
	// a caller's result count holds for kapa.ai as it does per local base.
	if limit > 0 && len(chunks) > limit {
		chunks = chunks[:limit]
	}
	hits := make([]SearchHit, len(chunks))
	for i, chunk := range chunks {
		hits[i] = SearchHit{
			Index:    KapaIndexName,
			Score:    1.0 / float64(i+1),
			Content:  chunk.Content,
			SourceID: chunk.SourceURL,
			Label:    LabelKapa,
		}
	}
	return hits, nil
}

// SearchWithKapa runs local hybrid search over indexes and kapa.ai retrieval
// over groups concurrently, returning local hits (sorted by score) followed by
// kapa.ai hits in kapa.ai's order: kapa.ai scores are rank-derived and not
// comparable with local ones. Either side is skipped when it has no client or
// nothing selected. A kapa.ai failure does not fail the search: it is returned
// as kapaErr so the caller can warn and keep the local results.
func SearchWithKapa(ctx context.Context, local *OpenSearchClient, indexes []string, query, lexicalQuery, embeddingModelID string,
	k int, kapa *KapaClient, groups []string) (hits []SearchHit, kapaErr, err error) {
	var (
		localHits, kapaHits []SearchHit
		wg                  sync.WaitGroup
	)
	if local != nil && len(indexes) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			localHits, err = local.Search(ctx, indexes, query, lexicalQuery, embeddingModelID, k)
		}()
	}
	if kapa != nil && len(groups) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			kapaHits, kapaErr = kapa.Search(ctx, query, k, groups)
		}()
	}
	wg.Wait()
	if err != nil {
		return nil, kapaErr, err
	}
	hits = make([]SearchHit, 0, len(localHits)+len(kapaHits))
	hits = append(hits, localHits...)
	return append(hits, kapaHits...), kapaErr, nil
}
