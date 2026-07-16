package api

import (
	"encoding/json"
	"errors"
	"net/http"
)

// promptUpdateRequest is the body of PUT /1.0/prompts/{name} and
// PUT /1.0/prompts/{slot}/variants/{name}.
type promptUpdateRequest struct {
	Value string `json:"value"`
}

// variantCreateRequest is the body of POST /1.0/prompts/{slot}/variants.
type variantCreateRequest struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// variantRestoreRequest is the body of POST
// /1.0/prompts/{slot}/variants/{name}/restore.
type variantRestoreRequest struct {
	Version int `json:"version"`
}

// slotActivateRequest is the body of PATCH /1.0/prompts/{slot}. An empty
// active selects the built-in default.
type slotActivateRequest struct {
	Active string `json:"active"`
}

// swagger:route GET /1.0/prompts prompts promptsList
//
// List the prompt slots.
//
// Returns the three RAG prompt slots in the order the CLI presents them, each
// with its effective value, its built-in default, and whether an override is
// active. Generation slots additionally carry the active variant name and the
// list of stored variant names. Chat sessions and batch runs started by the
// daemon are seeded from these values.
//
//	Responses:
//	  200: syncResponse
//	  403: errorResponse
func (s *Server) handlePromptsList(w http.ResponseWriter, _ *http.Request) {
	respondSync(w, s.prompts.views())
}

// swagger:route GET /1.0/prompts/{name} prompts promptGet
//
// Return one prompt slot.
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
// Customize a prompt slot.
//
// Writes the value through the slot's current selection: a new version on the
// active variant, or — when the built-in default is active on a generation slot
// — a `custom` variant that is created and activated. An empty value is rejected
// — reset a slot to its default with DELETE. A value equal to the built-in
// default clears the legacy `custom` variant (or the source_rules override)
// rather than storing a copy.
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
// Reset a prompt slot to its default.
//
// Returns the slot to the built-in default of the running release: a generation
// slot's active pointer is cleared (stored variants preserved); the source_rules
// override is cleared. Resetting an uncustomized slot succeeds as a no-op.
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

// swagger:route GET /1.0/prompts/{slot}/variants prompts variantsList
//
// List a generation slot's variants.
//
//	Responses:
//	  200: syncResponse
//	  403: errorResponse
//	  404: errorResponse
func (s *Server) handleVariantsList(w http.ResponseWriter, r *http.Request) {
	summaries, err := s.prompts.listVariants(promptName(r.PathValue("slot")))
	if err != nil {
		respondPromptError(w, err)
		return
	}
	respondSync(w, summaries)
}

// swagger:route POST /1.0/prompts/{slot}/variants prompts variantCreate
//
// Create a variant.
//
// Stores a new variant from an initial value. The name must match
// `^[a-z0-9][a-z0-9-]{0,63}$` and must not be the reserved name `default`. A
// name already in use is a conflict. The variant is not activated.
//
//	Responses:
//	  200: syncResponse
//	  400: errorResponse
//	  403: errorResponse
//	  404: errorResponse
//	  409: errorResponse
//	  500: errorResponse
func (s *Server) handleVariantCreate(w http.ResponseWriter, r *http.Request) {
	var req variantCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	view, err := s.prompts.createVariant(promptName(r.PathValue("slot")), req.Name, req.Value)
	if err != nil {
		respondPromptError(w, err)
		return
	}
	respondSync(w, view)
}

// swagger:route GET /1.0/prompts/{slot}/variants/{name} prompts variantGet
//
// Return one variant's head value.
//
//	Responses:
//	  200: syncResponse
//	  403: errorResponse
//	  404: errorResponse
func (s *Server) handleVariantGet(w http.ResponseWriter, r *http.Request) {
	view, err := s.prompts.getVariant(promptName(r.PathValue("slot")), r.PathValue("name"))
	if err != nil {
		respondPromptError(w, err)
		return
	}
	respondSync(w, view)
}

// swagger:route PUT /1.0/prompts/{slot}/variants/{name} prompts variantUpdate
//
// Save a new version of a variant.
//
// Appends a new version, creating the variant if absent. A value byte-identical
// to the head is a no-op. An empty value is rejected.
//
//	Responses:
//	  200: syncResponse
//	  400: errorResponse
//	  403: errorResponse
//	  404: errorResponse
//	  500: errorResponse
func (s *Server) handleVariantUpdate(w http.ResponseWriter, r *http.Request) {
	var req promptUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	view, err := s.prompts.saveVariant(promptName(r.PathValue("slot")), r.PathValue("name"), req.Value)
	if err != nil {
		respondPromptError(w, err)
		return
	}
	respondSync(w, view)
}

// swagger:route DELETE /1.0/prompts/{slot}/variants/{name} prompts variantDelete
//
// Delete a variant.
//
// Removes the variant and its history. The active variant cannot be deleted —
// activate another selection first.
//
//	Responses:
//	  200: syncResponse
//	  403: errorResponse
//	  404: errorResponse
//	  409: errorResponse
//	  500: errorResponse
func (s *Server) handleVariantDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.prompts.deleteVariant(promptName(r.PathValue("slot")), r.PathValue("name")); err != nil {
		respondPromptError(w, err)
		return
	}
	respondSync(w, map[string]string{"status": "deleted"})
}

// swagger:route GET /1.0/prompts/{slot}/variants/{name}/versions prompts variantVersions
//
// List a variant's version history.
//
//	Responses:
//	  200: syncResponse
//	  403: errorResponse
//	  404: errorResponse
func (s *Server) handleVariantVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := s.prompts.versions(promptName(r.PathValue("slot")), r.PathValue("name"))
	if err != nil {
		respondPromptError(w, err)
		return
	}
	respondSync(w, versions)
}

// swagger:route POST /1.0/prompts/{slot}/variants/{name}/restore prompts variantRestore
//
// Restore an earlier version.
//
// Appends a new head version carrying the named version's content. Restoring the
// current head is a no-op; an unknown version is a not-found.
//
//	Responses:
//	  200: syncResponse
//	  400: errorResponse
//	  403: errorResponse
//	  404: errorResponse
//	  500: errorResponse
func (s *Server) handleVariantRestore(w http.ResponseWriter, r *http.Request) {
	var req variantRestoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	view, err := s.prompts.restoreVersion(promptName(r.PathValue("slot")), r.PathValue("name"), req.Version)
	if err != nil {
		respondPromptError(w, err)
		return
	}
	respondSync(w, view)
}

// swagger:route PATCH /1.0/prompts/{slot} prompts slotActivate
//
// Activate a variant on a slot.
//
// Points the slot's active pointer at a stored variant, or at the built-in
// default when the value is empty. Activating an unknown variant is a not-found.
//
//	Responses:
//	  200: syncResponse
//	  400: errorResponse
//	  403: errorResponse
//	  404: errorResponse
//	  500: errorResponse
func (s *Server) handleSlotActivate(w http.ResponseWriter, r *http.Request) {
	var req slotActivateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	view, err := s.prompts.activate(promptName(r.PathValue("slot")), req.Active)
	if err != nil {
		respondPromptError(w, err)
		return
	}
	respondSync(w, view)
}

// respondPromptError maps a store error to its HTTP status: an unaddressable
// prompt or variant is a 404, a bad value or name is a 400, a delete of the
// active variant or a duplicate create is a 409, and a persistence failure is a
// 500.
func respondPromptError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errUnknownPrompt):
		respondError(w, http.StatusNotFound, "unknown prompt; valid prompts are: "+promptNames())
	case errors.Is(err, errNoVariants):
		respondError(w, http.StatusNotFound, "this prompt does not support variants")
	case errors.Is(err, errUnknownVariant):
		respondError(w, http.StatusNotFound, "unknown variant")
	case errors.Is(err, errUnknownVersion):
		respondError(w, http.StatusNotFound, "unknown version")
	case errors.Is(err, errEmptyPrompt):
		respondError(w, http.StatusBadRequest,
			"prompt value cannot be empty; use DELETE to reset a prompt to its default")
	case errors.Is(err, errInvalidName):
		respondError(w, http.StatusBadRequest,
			"invalid variant name; use lowercase letters, digits and hyphens (max 64 chars)")
	case errors.Is(err, errReservedName):
		respondError(w, http.StatusBadRequest, "\"default\" is reserved and cannot be used as a variant name")
	case errors.Is(err, errVariantExists):
		respondError(w, http.StatusConflict, "a variant with that name already exists")
	case errors.Is(err, errVariantActive):
		respondError(w, http.StatusConflict,
			"cannot delete the active variant; activate another selection or the default first")
	default:
		respondError(w, http.StatusInternalServerError, "saving prompt: "+err.Error())
	}
}
