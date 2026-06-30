package api

import (
	"encoding/json"
	"net/http"
)

// Response type discriminators for the uniform API envelope.
//
// This is the minimal envelope needed by the daemon skeleton (sync + error).
// The full set of helpers — including the async/operation response and the
// doubled numeric+text status codes — is added in phase 2 (task 2.2).
const (
	responseTypeSync  = "sync"
	responseTypeAsync = "async"
	responseTypeError = "error"
)

// syncResponse is the body of a successful, immediately-completed request.
type syncResponse struct {
	Type       string `json:"type"`
	Status     string `json:"status"`
	StatusCode int    `json:"status_code"`
	Metadata   any    `json:"metadata"`
}

// errorResponse is the body of a failed request. error_code mirrors the HTTP status.
type errorResponse struct {
	Type      string `json:"type"`
	ErrorCode int    `json:"error_code"`
	Error     string `json:"error"`
}

// respondSync writes a 200 sync response wrapping metadata.
func respondSync(w http.ResponseWriter, metadata any) {
	writeJSON(w, http.StatusOK, syncResponse{
		Type:       responseTypeSync,
		Status:     http.StatusText(http.StatusOK),
		StatusCode: http.StatusOK,
		Metadata:   metadata,
	})
}

// respondError writes an error response whose error_code equals the HTTP status.
func respondError(w http.ResponseWriter, code int, message string) {
	writeJSON(w, code, errorResponse{
		Type:      responseTypeError,
		ErrorCode: code,
		Error:     message,
	})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
