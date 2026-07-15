package api

import (
	"errors"
	"net/http"

	"github.com/jpnorenam/rag-snap/internal/chatstore"
)

// swagger:route GET /1.0/chats chats chatsList
//
// List saved chats.
//
// Returns saved chat summaries newest-first by last-updated time, without the
// full transcript. An optional `search` query parameter filters by
// case-insensitive substring match against title and transcript content.
//
//	Responses:
//	  200: syncResponse
//	  403: errorResponse
//	  500: errorResponse
func (s *Server) handleChatsList(w http.ResponseWriter, r *http.Request) {
	summaries, err := s.chats.List(r.URL.Query().Get("search"))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "listing saved chats: "+err.Error())
		return
	}
	respondSync(w, summaries)
}

// swagger:route GET /1.0/chats/{id} chats chatGet
//
// Return one saved chat.
//
// Returns the full saved chat, including its ordered transcript.
//
//	Responses:
//	  200: syncResponse
//	  403: errorResponse
//	  404: errorResponse
//	  500: errorResponse
func (s *Server) handleChatGet(w http.ResponseWriter, r *http.Request) {
	chat, err := s.chats.Get(r.PathValue("id"))
	if err != nil {
		respondChatStoreError(w, err)
		return
	}
	respondSync(w, chat)
}

// swagger:route DELETE /1.0/chats/{id} chats chatDelete
//
// Delete a saved chat.
//
// Removes the saved chat. A live session resumed from it is unaffected; a later
// save from such a session recreates the record under the same id.
//
//	Responses:
//	  200: syncResponse
//	  403: errorResponse
//	  404: errorResponse
//	  500: errorResponse
func (s *Server) handleChatDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.chats.Delete(r.PathValue("id")); err != nil {
		respondChatStoreError(w, err)
		return
	}
	respondSync(w, map[string]any{"deleted": true})
}

// respondChatStoreError maps a store error to its HTTP status: an unknown id is a
// 404, anything else a 500.
func respondChatStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, chatstore.ErrNotFound) {
		respondError(w, http.StatusNotFound, "saved chat not found")
		return
	}
	respondError(w, http.StatusInternalServerError, err.Error())
}

// chatSaveErrorMessage is the human-facing message for a failed save control
// message: a friendly note for an empty session, the store's own message
// otherwise.
func chatSaveErrorMessage(err error) string {
	if errors.Is(err, chatstore.ErrEmpty) {
		return "nothing to save yet — ask a question first"
	}
	return "saving chat: " + err.Error()
}
