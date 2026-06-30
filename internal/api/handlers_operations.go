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

// handleOperationsList implements GET /1.0/operations: list current operations.
func (s *Server) handleOperationsList(w http.ResponseWriter, _ *http.Request) {
	respondSync(w, s.ops.list())
}

// handleOperationGet implements GET /1.0/operations/{id}: return one operation.
func (s *Server) handleOperationGet(w http.ResponseWriter, r *http.Request) {
	op := s.ops.get(r.PathValue("id"))
	if op == nil {
		respondError(w, http.StatusNotFound, "operation not found")
		return
	}
	respondSync(w, op.view())
}

// handleOperationDelete implements DELETE /1.0/operations/{id}: request
// cooperative cancellation. Fails if the operation cannot be cancelled.
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

// handleOperationWait implements GET /1.0/operations/{id}/wait?timeout=N: block
// until the operation is terminal or the timeout (in seconds) elapses, then
// return the operation. A timeout that elapses returns the current state, not
// an error.
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

// handleEvents implements GET /1.0/events: upgrade to a websocket and stream
// typed events. The ?type=operation,logging query filters the event types.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept already wrote the error to the client.
	}
	defer conn.CloseNow()

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
