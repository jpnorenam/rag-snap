package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// swagger:route GET /1.0/operations operations operationsList
//
// List operations.
//
// Returns a snapshot of all current operations.
//
//	Responses:
//	  200: syncResponse
//	  403: errorResponse
func (s *Server) handleOperationsList(w http.ResponseWriter, _ *http.Request) {
	respondSync(w, s.ops.list())
}

// swagger:route GET /1.0/operations/{id} operations operationGet
//
// Return an operation.
//
//	Responses:
//	  200: syncResponse
//	  404: errorResponse
func (s *Server) handleOperationGet(w http.ResponseWriter, r *http.Request) {
	op := s.ops.get(r.PathValue("id"))
	if op == nil {
		respondError(w, http.StatusNotFound, "operation not found")
		return
	}
	respondSync(w, op.view())
}

// swagger:route DELETE /1.0/operations/{id} operations operationDelete
//
// Cancel an operation.
//
// Requests cooperative cancellation. Fails if the operation cannot be cancelled
// or is already complete.
//
//	Responses:
//	  200: syncResponse
//	  400: errorResponse
//	  404: errorResponse
func (s *Server) handleOperationDelete(w http.ResponseWriter, r *http.Request) {
	op := s.ops.get(r.PathValue("id"))
	if op == nil {
		respondError(w, http.StatusNotFound, "operation not found")
		return
	}
	if err := op.requestCancel(); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondSync(w, op.view())
}

// swagger:route GET /1.0/operations/{id}/wait operations operationWait
//
// Wait for an operation.
//
// Blocks until the operation is terminal or the timeout (in seconds) elapses,
// then returns the operation. A timeout that elapses returns the current state,
// not an error.
//
//	Responses:
//	  200: syncResponse
//	  400: errorResponse
//	  404: errorResponse
func (s *Server) handleOperationWait(w http.ResponseWriter, r *http.Request) {
	op := s.ops.get(r.PathValue("id"))
	if op == nil {
		respondError(w, http.StatusNotFound, "operation not found")
		return
	}
	var timeout time.Duration
	if raw := r.URL.Query().Get("timeout"); raw != "" {
		secs, err := strconv.Atoi(raw)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid timeout: must be an integer number of seconds")
			return
		}
		if secs > 0 {
			timeout = time.Duration(secs) * time.Second
		}
	}
	respondSync(w, op.wait(r.Context(), timeout))
}

// swagger:route GET /1.0/events operations events
//
// Stream events over a websocket.
//
// Upgrades to a websocket and streams typed events. The type query filters the
// event types (e.g. "operation,logging"). Clients should subscribe before
// launching an operation to avoid a poll race.
//
//	Responses:
//	  101: syncResponse
//	  403: errorResponse
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept already wrote the error to the client.
	}
	defer func() { _ = conn.CloseNow() }()

	var types []string
	if raw := r.URL.Query().Get("type"); raw != "" {
		for _, t := range strings.Split(raw, ",") {
			if t = strings.TrimSpace(t); t != "" {
				types = append(types, t)
			}
		}
	}

	sub, unsubscribe := s.events.subscribe(types)
	defer unsubscribe()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-sub.ch:
			writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := wsjson.Write(writeCtx, conn, e)
			cancel()
			if err != nil {
				return
			}
		}
	}
}
