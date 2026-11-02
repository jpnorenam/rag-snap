package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/jpnorenam/rag-snap/cmd/cli/common"
)

// SearchHit represents a single search result with its relevance score.
// Label is always resolved (stored chunk label, or the legacy index-name
// inference for unlabeled chunks) — consumers use it directly and never
// re-derive provenance.
type SearchHit struct {
	Index     string  `json:"index"`
	Score     float64 `json:"score"`
	Content   string  `json:"content"`
	SourceID  string  `json:"source_id"`
	Label     string  `json:"label"`
	CreatedAt string  `json:"created_at"`
	// Matched reports whether the hit clears MatchScoreThreshold. Search
	// surfaces show matched hits and hide the rest as similar results; the chat
	// RAG loop ignores it.
	Matched bool `json:"matched"`
}

// MatchScoreThreshold is the relevance score a hit must exceed to count as a
// match. The search pipeline's rerank processor replaces each hit's score with
// the cross-encoder's output, and the default cross-encoder
// (ms-marco-MiniLM-L-12-v2) emits a relevance logit: sigmoid(score) is its
// estimate of P(relevant), so 0 is the model's own decision boundary. A
// reranker that emits probabilities (0..1) instead would need 0.5 here.
//
// This is the only place the rule lives: the REST API reports it per hit as
// "matched", and every client reads that flag rather than comparing scores.
const MatchScoreThreshold = 0.0

// IsMatch reports whether a relevance score counts as a match.
func IsMatch(score float64) bool {
	return score > MatchScoreThreshold
}

// ShowSimilarFlag is the CLI flag (and /search option) that reveals similar
// results, named in the hint printed when they are hidden.
const ShowSimilarFlag = "--show-similar"

// SplitByMatch partitions hits into matched and similar results, preserving
// their order. isMatch reports a hit's match flag; callers holding API hits
// treat a missing flag as matching.
func SplitByMatch[T any](hits []T, isMatch func(T) bool) (matched, similar []T) {
	for _, h := range hits {
		if isMatch(h) {
			matched = append(matched, h)
		} else {
			similar = append(similar, h)
		}
	}
	return matched, similar
}

// NoMatchesMessage is shown when a search returns no matching hit. The UI uses
// the same wording (ui/lib/searchSplit.ts); the shared fixture pins both.
func NoMatchesMessage(query string) string {
	return fmt.Sprintf("No results match %q.", query)
}

// SimilarNotice states how many similar results are hidden. The UI uses the
// same wording (ui/lib/searchSplit.ts); the shared fixture pins both.
func SimilarNotice(n int) string {
	if n == 1 {
		return "1 similar result doesn't match the query."
	}
	return fmt.Sprintf("%d similar results don't match the query.", n)
}

// SimilarSeparator heads the similar results when they are shown.
func SimilarSeparator() string {
	return "── Similar results (do not match the query) ──"
}

// FormatSplit assembles a search's terminal output from its rendered groups:
// the matched hits (or the no-matches message), then either a notice that the
// similar results are hidden or, with showSimilar, the similar results under a
// separator. Rendering of each hit stays with the command, whose formats differ.
func FormatSplit(query, matchedOut, similarOut string, nMatched, nSimilar int, showSimilar bool) string {
	var b strings.Builder
	if nMatched == 0 {
		b.WriteString(NoMatchesMessage(query) + "\n")
	} else {
		b.WriteString(matchedOut)
	}
	if nSimilar == 0 {
		return b.String()
	}
	b.WriteString("\n")
	if showSimilar {
		b.WriteString(SimilarSeparator() + "\n")
		b.WriteString(similarOut)
	} else {
		fmt.Fprintf(&b, "%s Use %s to show them.\n", SimilarNotice(nSimilar), ShowSimilarFlag)
	}
	return b.String()
}

// Search performs a hybrid search (BM25 + neural) with reranking across the
// given indexes, merges the results, and returns them sorted by score descending.
// Indexes should be full index names (e.g. "rag-snap-context-default").
// The query parameter is used for neural embedding and reranking.
// The lexicalQuery parameter is used for BM25 matching and may include
// additional context (e.g. recent conversation queries) for richer lexical recall.
func (c *OpenSearchClient) Search(ctx context.Context, indexes []string, query, lexicalQuery, embeddingModelID string, k int) ([]SearchHit, error) {
	stopProgress := common.StartProgressSpinner("Searching knowledge base")
	defer stopProgress()

	return c.search(ctx, indexes, query, lexicalQuery, embeddingModelID, k)
}

// SearchTop is Search for the search surfaces (k search, /search, the REST
// API): k caps the total number of hits across all indexes rather than each
// index's share. Each index returns its own top k, so the merged list always
// contains the global top k. The chat RAG loop keeps calling Search.
func (c *OpenSearchClient) SearchTop(ctx context.Context, indexes []string, query, lexicalQuery, embeddingModelID string, k int) ([]SearchHit, error) {
	stopProgress := common.StartProgressSpinner("Searching knowledge base")
	defer stopProgress()

	hits, err := c.search(ctx, indexes, query, lexicalQuery, embeddingModelID, k)
	if err != nil {
		return nil, err
	}
	return capHits(hits, k), nil
}

// capHits truncates score-sorted hits to at most k.
func capHits(hits []SearchHit, k int) []SearchHit {
	if k >= 0 && len(hits) > k {
		return hits[:k]
	}
	return hits
}

func (c *OpenSearchClient) search(ctx context.Context, indexes []string, query, lexicalQuery, embeddingModelID string, k int) ([]SearchHit, error) {
	// Search each index individually and collect all hits.
	var allHits []SearchHit
	for _, index := range indexes {
		hits, err := c.hybridSearch(ctx, index, query, lexicalQuery, embeddingModelID, k)
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

// hybridSearch executes a hybrid (BM25 + neural) search with reranking on a single index.
func (c *OpenSearchClient) hybridSearch(
	ctx context.Context,
	indexName, query, lexicalQuery, embeddingModelID string,
	k int,
) ([]SearchHit, error) {
	body := buildSearchBody(query, lexicalQuery, embeddingModelID, k)

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
			Label:     ResolveLabel(hit.Index, hit.Source.Label),
			CreatedAt: hit.Source.CreatedAt,
			Matched:   IsMatch(hit.Score),
		})
	}

	return hits, nil
}

// buildSearchBody constructs a hybrid search request body combining BM25
// lexical matching with neural KNN, plus reranking context.
// The lexicalQuery is used for BM25 matching and may be enriched with
// conversation history. The query is used for neural embedding and reranking.
func buildSearchBody(query, lexicalQuery, embeddingModelID string, k int) map[string]any {
	// Over-fetch candidates so the reranker has a larger pool to work with.
	// The final result count is capped back to k via "size".
	neuralK := k * 3
	return map[string]any{
		"size": k,
		"_source": map[string]any{
			"excludes": []string{"embedding"},
		},
		"query": map[string]any{
			"hybrid": map[string]any{
				"queries": []map[string]any{
					{
						"match": map[string]any{
							"content": map[string]any{
								"query": lexicalQuery,
							},
						},
					},
					{
						"neural": map[string]any{
							"embedding": map[string]any{
								"query_text": query,
								"model_id":   embeddingModelID,
								"k":          neuralK,
							},
						},
					},
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
				Label     string `json:"label"`
				CreatedAt string `json:"created_at"`
			} `json:"_source"`
		} `json:"hits"`
	} `json:"hits"`
}
