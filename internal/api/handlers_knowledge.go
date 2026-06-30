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
	Name      string `json:"name"`
	Index     string `json:"index"`
	Health    string `json:"health"`
	Status    string `json:"status"`
	DocsCount string `json:"docs_count"`
	StoreSize string `json:"store_size"`
}

// createKnowledgeRequest is the body of POST /1.0/knowledge.
type createKnowledgeRequest struct {
	Name string `json:"name"`
}

// handleKnowledgeList implements GET /1.0/knowledge: list knowledge bases.
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
	bases := make([]knowledgeBaseSummary, 0, len(indexes))
	for _, idx := range indexes {
		name, _ := knowledge.KnowledgeBaseNameFromIndex(idx.Name)
		bases = append(bases, knowledgeBaseSummary{
			Name:      name,
			Index:     idx.Name,
			Health:    idx.Health,
			Status:    idx.Status,
			DocsCount: idx.DocsCount,
			StoreSize: idx.StoreSize,
		})
	}
	respondSync(w, bases)
}

// handleKnowledgeCreate implements POST /1.0/knowledge: create a knowledge base.
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

// handleKnowledgeGet implements GET /1.0/knowledge/{name}: return KB detail.
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
	sources, err := client.ListSourceMetadata(r.Context(), index)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondSync(w, map[string]any{
		"name":         name,
		"index":        index,
		"chunk_count":  count,
		"source_count": len(sources),
	})
}

// handleKnowledgeDelete implements DELETE /1.0/knowledge/{name}: delete a base
// and its source metadata. No interactive confirmation at the API layer.
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

// handleSourcesList implements GET /1.0/knowledge/{name}/sources.
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

// handleSourceGet implements GET /1.0/knowledge/{name}/sources/{id}.
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

// handleSourceDelete implements DELETE /1.0/knowledge/{name}/sources/{id}:
// forget a source by removing its chunks and metadata.
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
