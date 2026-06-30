package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/cmd/cli/config"
)

// handleEngineInit implements POST /1.0/knowledge-engine:init style endpoint:
// initialize the knowledge engine (models, pipelines, indexes) as an async
// operation. On success the operation metadata reports the resolved model IDs.
func (s *Server) handleEngineInit(w http.ResponseWriter, r *http.Request) {
	client, err := s.clients.openSearchClient()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	op, err := s.ops.runTask(
		"Initializing knowledge engine",
		map[string][]string{"knowledge": {"/1.0/knowledge"}}, false,
		func(ctx context.Context, op *Operation) error {
			if err := client.InitPipelines(ctx); err != nil {
				return err
			}
			// Surface the resolved model IDs from config (Init prints the
			// `rag set` instructions; the operator persists them, after which
			// they are readable here).
			embedding, _ := config.GetString(s.ctx.Config, knowledge.ConfEmbeddingModelID)
			rerank, _ := config.GetString(s.ctx.Config, knowledge.ConfRerankModelID)
			op.UpdateMetadata(map[string]any{
				"embedding_model_id": embedding,
				"rerank_model_id":    rerank,
			})
			return nil
		},
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondAsync(w, op.url(), op.view())
}

// exportRequest is the body of POST /1.0/knowledge/{name}/export.
type exportRequest struct {
	OutputDir string `json:"output_dir"`
	Compress  bool   `json:"compress"`
}

// handleKnowledgeExport implements POST /1.0/knowledge/{name}/export: export a
// base as an async operation (reuses the elasticdump-based exporter).
func (s *Server) handleKnowledgeExport(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var req exportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	client, err := s.clients.openSearchClient()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	opts := knowledge.ExportOptions{OutputDir: req.OutputDir, Compress: req.Compress}
	op, err := s.ops.runTask(
		"Exporting knowledge base "+name,
		map[string][]string{"knowledge": {"/1.0/knowledge/" + name}}, false,
		func(ctx context.Context, op *Operation) error {
			return knowledge.ExportKnowledgeBase(ctx, client, name, opts)
		},
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondAsync(w, op.url(), op.view())
}

// importRequest is the body of POST /1.0/knowledge:import.
type importRequest struct {
	Name     string `json:"name"`
	InputDir string `json:"input_dir"`
	Force    bool   `json:"force"`
}

// handleKnowledgeImport implements POST /1.0/knowledge/import: import a base
// from a previously exported artifact as an async operation. The interactive
// Google Drive auth flow is intentionally not exposed.
func (s *Server) handleKnowledgeImport(w http.ResponseWriter, r *http.Request) {
	var req importRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if strings.TrimSpace(req.InputDir) == "" {
		respondError(w, http.StatusBadRequest, "input_dir is required")
		return
	}

	client, err := s.clients.openSearchClient()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	opts := knowledge.ImportOptions{InputDir: req.InputDir, Force: req.Force}
	op, err := s.ops.runTask(
		"Importing knowledge base",
		map[string][]string{"knowledge": {"/1.0/knowledge"}}, false,
		func(ctx context.Context, op *Operation) error {
			return knowledge.ImportKnowledgeBase(ctx, client, req.Name, opts)
		},
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondAsync(w, op.url(), op.view())
}
