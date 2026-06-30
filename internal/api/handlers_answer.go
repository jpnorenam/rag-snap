package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
)

// defaultBatchTemperature matches the CLI `answer batch` default: low sampling
// temperature for deterministic, grounded answers.
const defaultBatchTemperature = 0.1

// batchManifestRequest is the prepared batch manifest accepted by
// POST /1.0/answer/batch. It mirrors chat.BatchManifest but is JSON-tagged and
// decoupled from the YAML on-disk format. The interactive document-to-manifest
// "build" flow is intentionally CLI-only (see the rest-api-answer spec).
type batchManifestRequest struct {
	Version        string                 `json:"version,omitempty"`
	Model          string                 `json:"model,omitempty"`
	KnowledgeBases []string               `json:"knowledge_bases,omitempty"`
	Prompt         string                 `json:"prompt,omitempty"`
	Temperature    *float64               `json:"temperature,omitempty"`
	Questions      []batchQuestionRequest `json:"questions"`
}

// batchQuestionRequest is a single question in a posted manifest.
type batchQuestionRequest struct {
	ID       string   `json:"id,omitempty"`
	Question string   `json:"question"`
	Keywords []string `json:"keywords,omitempty"`
}

// toManifest converts the API request to the chat package's manifest type.
func (req batchManifestRequest) toManifest() *chat.BatchManifest {
	questions := make([]chat.BatchQuestion, len(req.Questions))
	for i, q := range req.Questions {
		questions[i] = chat.BatchQuestion{
			ID:       q.ID,
			Question: q.Question,
			Keywords: chat.KeywordList(q.Keywords),
		}
	}
	return &chat.BatchManifest{
		Version:        req.Version,
		Model:          req.Model,
		KnowledgeBases: req.KnowledgeBases,
		Prompt:         req.Prompt,
		Questions:      questions,
	}
}

// handleAnswerBatch implements POST /1.0/answer/batch: run a prepared batch
// manifest of questions through the RAG+LLM pipeline as an async operation.
// Progress is reported in the operation metadata across the questions, and the
// structured results are stored on the operation for retrieval on completion.
func (s *Server) handleAnswerBatch(w http.ResponseWriter, r *http.Request) {
	var req batchManifestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if len(req.Questions) == 0 {
		respondError(w, http.StatusBadRequest, "manifest contains no questions")
		return
	}
	for i, q := range req.Questions {
		if strings.TrimSpace(q.Question) == "" {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("question %d has an empty question field", i+1))
			return
		}
	}

	baseURL := s.clients.openAIURL()
	if baseURL == "" {
		respondError(w, http.StatusInternalServerError, "inference backend URL is not configured")
		return
	}

	// RAG retrieval is enabled only when the knowledge backend and an embedding
	// model are both available; otherwise questions answer without retrieval
	// (and therefore yield the fixed no-context response). This mirrors the
	// search/chat guards.
	knowledgeClient, _ := s.clients.openSearchClient()
	embeddingModelID := ""
	if knowledgeClient != nil {
		if id, err := s.clients.embeddingModelID(); err == nil {
			embeddingModelID = id
		} else {
			knowledgeClient = nil
		}
	}

	manifest := req.toManifest()
	if manifest.Model == "" {
		manifest.Model = s.clients.chatModelID()
	}
	temperature := defaultBatchTemperature
	if req.Temperature != nil {
		temperature = *req.Temperature
	}
	prompts := chat.LoadPrompts()

	resources := map[string][]string{}
	if len(manifest.KnowledgeBases) > 0 {
		bases := make([]string, len(manifest.KnowledgeBases))
		for i, b := range manifest.KnowledgeBases {
			bases[i] = "/1.0/knowledge/" + b
		}
		resources["knowledge"] = bases
	}

	total := len(manifest.Questions)
	op, err := s.ops.runTask(
		fmt.Sprintf("Answering a batch of %d question(s)", total),
		resources, true,
		func(ctx context.Context, op *Operation) error {
			op.UpdateMetadata(map[string]any{"questions_total": total, "questions_done": 0})
			hooks := chat.BatchHooks{
				OnResult: func(i, total int, _ chat.BatchResult) {
					op.UpdateMetadata(map[string]any{"questions_total": total, "questions_done": i + 1})
				},
				OnError: func(i, total int, _ chat.BatchQuestion, _ error) {
					op.UpdateMetadata(map[string]any{"questions_total": total, "questions_done": i + 1})
				},
			}
			out, err := chat.RunBatch(ctx, baseURL, knowledgeClient, embeddingModelID, manifest, prompts, temperature, hooks, s.ctx.Verbose)
			if err != nil {
				return err
			}
			// Publish the structured results on the operation so a client can
			// retrieve them once the operation completes.
			op.UpdateMetadata(map[string]any{
				"generated_at": out.GeneratedAt,
				"model":        out.Model,
				"results":      out.Results,
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
