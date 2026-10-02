package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"

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
	// KapaGroups also searches kapa.ai, scoped to these source-group ids. Absent
	// or empty means kapa.ai is not queried.
	KapaGroups []string `json:"kapa_groups,omitempty"`
}

// kapaBaseLabel is the base reported for kapa.ai hits, which have no local
// knowledge base.
const kapaBaseLabel = "kapa.ai"

// searchResult is the API view of a single hit. Label is the hit's resolved
// knowledge label (stored chunk label, with index-name fallback for chunks
// ingested before labels existed), already resolved by the knowledge package.
type searchResult struct {
	Score     float64 `json:"score"`
	Base      string  `json:"base"`
	SourceID  string  `json:"source_id"`
	CreatedAt string  `json:"created_at"`
	Label     string  `json:"label"`
	Content   string  `json:"content"`
}

// swagger:route POST /1.0/search search search
//
// Hybrid search over knowledge bases.
//
// Runs hybrid (neural + lexical) retrieval over the named bases. Requires a
// configured embedding model when bases are given.
//
// "kapa_groups" (source-group ids) additionally searches kapa.ai, concurrently.
// Local hits are listed first, ordered by score, followed by kapa.ai hits in
// kapa.ai's order (their scores are rank-derived and not comparable); a kapa.ai
// hit has base "kapa.ai" and label "kapa-canonical". Absent or empty
// "kapa_groups" means kapa.ai is not queried. When kapa.ai is not configured or
// its request fails, the local hits are still returned and the response's
// top-level "warnings" says kapa.ai could not be applied.
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
	if len(req.Bases) == 0 && len(req.KapaGroups) == 0 {
		respondError(w, http.StatusBadRequest, "at least one knowledge base or kapa.ai source group is required")
		return
	}
	k := req.Count
	if k <= 0 {
		k = defaultSearchK
	}

	// Local search needs the embedding model and OpenSearch; resolve both before
	// starting so a misconfiguration fails the request as it always has.
	var (
		client           *knowledge.OpenSearchClient
		embeddingModelID string
	)
	if len(req.Bases) > 0 {
		var err error
		if embeddingModelID, err = s.clients.embeddingModelID(); err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if client, err = s.clients.openSearchClient(); err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	var (
		localHits, kapaHits []knowledge.SearchHit
		localErr, kapaErr   error
		warnings            []string
		wg                  sync.WaitGroup
	)
	if client != nil {
		indexes := make([]string, len(req.Bases))
		for i, b := range req.Bases {
			indexes[i] = knowledge.FullIndexName(b)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			// The CLI /search uses the verbatim query for both the neural and
			// lexical arms; do the same here (no LLM query rewrite for raw search).
			localHits, localErr = client.Search(r.Context(), indexes, req.Query, req.Query, embeddingModelID, k)
		}()
	}
	if len(req.KapaGroups) > 0 {
		if s.kapa == nil {
			warnings = append(warnings, "kapa.ai was requested but is not configured on the daemon "+
				"(set kapa.project.id and KAPA_API_KEY, then restart ragd); showing local results only")
		} else {
			wg.Add(1)
			go func() {
				defer wg.Done()
				kapaHits, kapaErr = s.kapa.Search(r.Context(), req.Query, k, req.KapaGroups)
			}()
		}
	}
	wg.Wait()

	if localErr != nil {
		respondError(w, http.StatusInternalServerError, localErr.Error())
		return
	}
	if kapaErr != nil {
		warnings = append(warnings, "kapa.ai search failed, showing local results only: "+kapaErr.Error())
	}

	results := make([]searchResult, 0, len(localHits)+len(kapaHits))
	for _, h := range append(localHits, kapaHits...) {
		base := kapaBaseLabel
		if h.Index != knowledge.KapaIndexName {
			base, _ = knowledge.KnowledgeBaseNameFromIndex(h.Index)
		}
		results = append(results, searchResult{
			Score:     h.Score,
			Base:      base,
			SourceID:  h.SourceID,
			CreatedAt: h.CreatedAt,
			Label:     h.Label,
			Content:   h.Content,
		})
	}
	respondSyncWarnings(w, results, warnings)
}
