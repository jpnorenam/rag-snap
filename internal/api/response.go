package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
)

// Response type discriminators for the uniform API envelope.
const (
	responseTypeSync  = "sync"
	responseTypeAsync = "async"
	responseTypeError = "error"
)

// Operation/result status codes follow LXD's doubled numeric+text scheme:
// 100–199 are running/intermediate states, 200–399 success, 400–599 failure.
// Clients switch on the numeric code in preference to the text status.
const (
	statusCodeOperationCreated = 100
	statusCodeStarted          = 101
	statusCodeRunning          = 103
	statusCodeCancelling       = 104
	statusCodePending          = 105
	statusCodeSuccess          = 200
	statusCodeFailure          = 400
	statusCodeCancelled        = 401
)

// statusText maps a status code to its canonical text label.
func statusText(code int) string {
	switch code {
	case statusCodeOperationCreated:
		return "Operation created"
	case statusCodeStarted:
		return "Started"
	case statusCodeRunning:
		return "Running"
	case statusCodeCancelling:
		return "Cancelling"
	case statusCodePending:
		return "Pending"
	case statusCodeSuccess:
		return "Success"
	case statusCodeFailure:
		return "Failure"
	case statusCodeCancelled:
		return "Cancelled"
	default:
		return ""
	}
}

// syncResponse is the body of a successful, immediately-completed request.
type syncResponse struct {
	Type       string `json:"type"`
	Status     string `json:"status"`
	StatusCode int    `json:"status_code"`
	Metadata   any    `json:"metadata"`
}

// asyncResponse references a background operation. The operation object is
// carried in metadata; the operation URL is also echoed in the Location header.
type asyncResponse struct {
	Type       string `json:"type"`
	Status     string `json:"status"`
	StatusCode int    `json:"status_code"`
	Operation  string `json:"operation"`
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
		Status:     statusText(statusCodeSuccess),
		StatusCode: statusCodeSuccess,
		Metadata:   metadata,
	})
}

// respondSyncETag writes a 200 sync response and sets an ETag header derived
// from the metadata, so a later mutating PUT can be guarded with If-Match.
func respondSyncETag(w http.ResponseWriter, metadata any) {
	if tag, err := etagOf(metadata); err == nil {
		w.Header().Set("ETag", tag)
	}
	respondSync(w, metadata)
}

// respondAsync writes a 202 async response referencing a background operation.
// opURL is the operation's canonical URL (/1.0/operations/<uuid>); it is set in
// both the body and the Location header. metadata is the operation object.
func respondAsync(w http.ResponseWriter, opURL string, metadata any) {
	w.Header().Set("Location", opURL)
	writeJSON(w, http.StatusAccepted, asyncResponse{
		Type:       responseTypeAsync,
		Status:     statusText(statusCodeOperationCreated),
		StatusCode: statusCodeOperationCreated,
		Operation:  opURL,
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

// etagOf returns a stable ETag for a value, computed as the SHA-256 of its
// canonical JSON encoding and quoted per RFC 7232.
func etagOf(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return `"` + hex.EncodeToString(sum[:]) + `"`, nil
}

// ifMatchFails reports whether an If-Match precondition on the request is
// present and does not match the current ETag, in which case the caller must
// respond 412 Precondition Failed. A missing If-Match header never fails.
func ifMatchFails(r *http.Request, currentETag string) bool {
	want := r.Header.Get("If-Match")
	if want == "" {
		return false
	}
	if want == "*" {
		return false
	}
	return want != currentETag
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
