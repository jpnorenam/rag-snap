package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
)

// defaultSearchK is the default result count when the request omits one,
// matching the chat REPL's retrieval top-K.
const defaultSearchK = 15

// searchRequest is the body of POST /1.0/search.
type searchRequest struct {
	Query string   `json:"query"`
	Bases []string `json:"bases"`
	Count int      `json:"count"`
}

// searchResult is the API view of a single hit, including provenance derived
// from the originating index.
type searchResult struct {
	Score      float64 `json:"score"`
	Base       string  `json:"base"`
	SourceID   string  `json:"source_id"`
	CreatedAt  string  `json:"created_at"`
	Provenance string  `json:"provenance"`
	Content    string  `json:"content"`
}

// provenanceLabel returns the provenance tag for an index, mirroring the chat
// REPL's source labelling ([UPSTREAM] vs [CANONICAL]).
func provenanceLabel(indexName string) string {
	if strings.Contains(strings.ToLower(indexName), "upstream") {
		return "upstream"
	}
	return "canonical"
}

// swagger:route POST /1.0/search search search
//
// Hybrid search over knowledge bases.
//
// Runs hybrid (neural + lexical) retrieval over the named bases. Requires a
// configured embedding model.
//
//	Responses:
//	  200: syncResponse
//	  400: errorResponse
//	  403: errorResponse
//	  500: errorResponse
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	var req searchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" {
		respondError(w, http.StatusBadRequest, "query is required")
		return
	}
	if len(req.Bases) == 0 {
		respondError(w, http.StatusBadRequest, "at least one knowledge base is required")
		return
	}
	k := req.Count
	if k <= 0 {
		k = defaultSearchK
	}

	embeddingModelID, err := s.clients.embeddingModelID()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	client, err := s.clients.openSearchClient()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	indexes := make([]string, len(req.Bases))
	for i, b := range req.Bases {
		indexes[i] = knowledge.FullIndexName(b)
	}

	// The CLI /search uses the verbatim query for both the neural and lexical
	// arms; do the same here (no LLM query rewrite for raw search).
	hits, err := client.Search(r.Context(), indexes, req.Query, req.Query, embeddingModelID, k)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	results := make([]searchResult, 0, len(hits))
	for _, h := range hits {
		base, _ := knowledge.KnowledgeBaseNameFromIndex(h.Index)
		results = append(results, searchResult{
			Score:      h.Score,
			Base:       base,
			SourceID:   h.SourceID,
			CreatedAt:  h.CreatedAt,
			Provenance: provenanceLabel(h.Index),
			Content:    h.Content,
		})
	}
	respondSync(w, results)
}
