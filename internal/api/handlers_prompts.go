package api

import (
	"encoding/json"
	"errors"
	"net/http"
)

// promptUpdateRequest is the body of PUT /1.0/prompts/{name}.
type promptUpdateRequest struct {
	Value string `json:"value"`
}

// swagger:route GET /1.0/prompts prompts promptsList
//
// List the prompt templates.
//
// Returns the three RAG prompt templates in the order the CLI presents them,
// each with its effective value, its built-in default, and whether an override
// is stored. Chat sessions and batch runs started by the daemon are seeded from
// these values.
//
//	Responses:
//	  200: syncResponse
//	  403: errorResponse
func (s *Server) handlePromptsList(w http.ResponseWriter, _ *http.Request) {
	respondSync(w, s.prompts.views())
}

// swagger:route GET /1.0/prompts/{name} prompts promptGet
//
// Return one prompt template.
//
//	Responses:
//	  200: syncResponse
//	  403: errorResponse
//	  404: errorResponse
func (s *Server) handlePromptGet(w http.ResponseWriter, r *http.Request) {
	view, err := s.prompts.view(promptName(r.PathValue("name")))
	if err != nil {
		respondPromptError(w, err)
		return
	}
	respondSync(w, view)
}

// swagger:route PUT /1.0/prompts/{name} prompts promptUpdate
//
// Customize a prompt template.
//
// Stores an override for the named prompt. An empty value is rejected — reset a
// prompt to its default with DELETE, so clearing an editor can never silently
// discard a customization. A value equal to the built-in default clears the
// override rather than storing a copy of it.
//
//	Responses:
//	  200: syncResponse
//	  400: errorResponse
//	  403: errorResponse
//	  404: errorResponse
//	  500: errorResponse
func (s *Server) handlePromptUpdate(w http.ResponseWriter, r *http.Request) {
	var req promptUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	view, err := s.prompts.set(promptName(r.PathValue("name")), req.Value)
	if err != nil {
		respondPromptError(w, err)
		return
	}
	respondSync(w, view)
}

// swagger:route DELETE /1.0/prompts/{name} prompts promptReset
//
// Reset a prompt template to its default.
//
// Drops the stored override so the prompt resolves to the built-in default of
// the running release. Resetting an uncustomized prompt succeeds as a no-op.
//
//	Responses:
//	  200: syncResponse
//	  403: errorResponse
//	  404: errorResponse
//	  500: errorResponse
func (s *Server) handlePromptReset(w http.ResponseWriter, r *http.Request) {
	view, err := s.prompts.reset(promptName(r.PathValue("name")))
	if err != nil {
		respondPromptError(w, err)
		return
	}
	respondSync(w, view)
}

// respondPromptError maps a store error to its HTTP status: an unaddressable
// prompt is a 404 (naming the valid prompts), an empty value is a 400, and a
// persistence failure is a 500.
func respondPromptError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errUnknownPrompt):
		respondError(w, http.StatusNotFound, "unknown prompt; valid prompts are: "+promptNames())
	case errors.Is(err, errEmptyPrompt):
		respondError(w, http.StatusBadRequest,
			"prompt value cannot be empty; use DELETE to reset a prompt to its default")
	default:
		respondError(w, http.StatusInternalServerError, "saving prompt: "+err.Error())
	}
}
