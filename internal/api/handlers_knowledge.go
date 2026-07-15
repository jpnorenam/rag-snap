package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
)

// knowledgeBaseSummary is the API view of a knowledge base, derived from its
// backing index.
type knowledgeBaseSummary struct {
	Name        string `json:"name"`
	Index       string `json:"index"`
	Health      string `json:"health"`
	Status      string `json:"status"`
	DocsCount   string `json:"docs_count"`
	StoreSize   string `json:"store_size"`
	SourceCount int    `json:"source_count"`
}

// createKnowledgeRequest is the body of POST /1.0/knowledge.
type createKnowledgeRequest struct {
	Name string `json:"name"`
}

// swagger:route GET /1.0/knowledge knowledge knowledgeList
//
// List knowledge bases.
//
//	Responses:
//	  200: syncResponse
//	  403: errorResponse
//	  500: errorResponse
func (s *Server) handleKnowledgeList(w http.ResponseWriter, r *http.Request) {
	client, err := s.clients.openSearchClient()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	indexes, err := client.ListIndexes(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Exact source counts per index via a single terms aggregation (listing is
	// page-capped, so it would under-count large bases and starve others).
	sourceCounts, err := client.SourceCountsByIndex(r.Context())
	if err != nil {
		sourceCounts = map[string]int{}
	}

	bases := make([]knowledgeBaseSummary, 0, len(indexes))
	for _, idx := range indexes {
		name, _ := knowledge.KnowledgeBaseNameFromIndex(idx.Name)
		bases = append(bases, knowledgeBaseSummary{
			Name:        name,
			Index:       idx.Name,
			Health:      idx.Health,
			Status:      idx.Status,
			DocsCount:   idx.DocsCount,
			StoreSize:   idx.StoreSize,
			SourceCount: sourceCounts[idx.Name],
		})
	}
	respondSync(w, bases)
}

// swagger:route POST /1.0/knowledge knowledge knowledgeCreate
//
// Create a knowledge base.
//
//	Responses:
//	  200: syncResponse
//	  400: errorResponse
//	  403: errorResponse
//	  409: errorResponse
//	  500: errorResponse
func (s *Server) handleKnowledgeCreate(w http.ResponseWriter, r *http.Request) {
	var req createKnowledgeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "knowledge base name is required")
		return
	}

	client, err := s.clients.openSearchClient()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	index := knowledge.FullIndexName(req.Name)
	exists, err := client.IndexExists(r.Context(), index)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if exists {
		respondError(w, http.StatusConflict, "knowledge base already exists: "+req.Name)
		return
	}
	if err := client.CreateIndex(r.Context(), index); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondSync(w, knowledgeBaseSummary{Name: req.Name, Index: index})
}

// swagger:route GET /1.0/knowledge/{name} knowledge knowledgeGet
//
// Return knowledge base detail.
//
//	Responses:
//	  200: syncResponse
//	  403: errorResponse
//	  404: errorResponse
//	  500: errorResponse
func (s *Server) handleKnowledgeGet(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	client, err := s.clients.openSearchClient()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	index := knowledge.FullIndexName(name)
	exists, err := client.IndexExists(r.Context(), index)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !exists {
		respondError(w, http.StatusNotFound, "knowledge base not found: "+name)
		return
	}

	count, err := client.CountDocuments(r.Context(), index)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sourceCounts, err := client.SourceCountsByIndex(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondSync(w, map[string]any{
		"name":         name,
		"index":        index,
		"chunk_count":  count,
		"source_count": sourceCounts[index],
	})
}

// swagger:route DELETE /1.0/knowledge/{name} knowledge knowledgeDelete
//
// Delete a knowledge base.
//
// Deletes the base and its source metadata. No interactive confirmation at the
// API layer.
//
//	Responses:
//	  200: syncResponse
//	  403: errorResponse
//	  404: errorResponse
//	  500: errorResponse
func (s *Server) handleKnowledgeDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	client, err := s.clients.openSearchClient()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	index := knowledge.FullIndexName(name)
	exists, err := client.IndexExists(r.Context(), index)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !exists {
		respondError(w, http.StatusNotFound, "knowledge base not found: "+name)
		return
	}
	if err := client.DeleteIndex(r.Context(), index); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	deleted, err := client.DeleteSourceMetadataByIndex(r.Context(), index)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondSync(w, map[string]any{
		"name":            name,
		"sources_removed": deleted,
	})
}

// swagger:route GET /1.0/knowledge/{name}/sources knowledge sourcesList
//
// List the sources in a knowledge base.
//
//	Responses:
//	  200: syncResponse
//	  403: errorResponse
//	  404: errorResponse
//	  500: errorResponse
func (s *Server) handleSourcesList(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	client, err := s.clients.openSearchClient()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	index := knowledge.FullIndexName(name)
	exists, err := client.IndexExists(r.Context(), index)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !exists {
		respondError(w, http.StatusNotFound, "knowledge base not found: "+name)
		return
	}
	sources, err := client.ListSourceMetadata(r.Context(), index)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondSync(w, sources)
}

// swagger:route GET /1.0/knowledge/{name}/sources/{id} knowledge sourceGet
//
// Return source metadata.
//
//	Responses:
//	  200: syncResponse
//	  403: errorResponse
//	  404: errorResponse
//	  500: errorResponse
func (s *Server) handleSourceGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	client, err := s.clients.openSearchClient()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	meta, err := client.GetSourceMetadata(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}
	respondSync(w, meta)
}

// swagger:route DELETE /1.0/knowledge/{name}/sources/{id} knowledge sourceDelete
//
// Forget a source.
//
// Removes the source's chunks and metadata from the knowledge base.
//
//	Responses:
//	  200: syncResponse
//	  403: errorResponse
//	  404: errorResponse
//	  500: errorResponse
func (s *Server) handleSourceDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	id := r.PathValue("id")
	client, err := s.clients.openSearchClient()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := client.GetSourceMetadata(r.Context(), id); err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}
	index := knowledge.FullIndexName(name)
	deleted, err := client.DeleteChunksBySourceID(r.Context(), index, id)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := client.DeleteSourceMetadata(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondSync(w, map[string]any{
		"source_id":      id,
		"chunks_removed": deleted,
	})
}
