// Package api implements the ragd local REST API.
//
// The spec below is consumed by go-swagger to generate rest-api.yaml. Handlers
// carry swagger:route annotations; the shared envelope and parameter models in
// this file are referenced by those routes. Keep annotations in sync with the
// routes registered in server.go — `make spec-check` fails the build on drift.
//
// Package api ragd REST API.
//
// ragd exposes knowledge management, hybrid search, interactive chat, and batch
// answering over a local unix socket. Every JSON response uses a uniform
// sync/async/error envelope; long-running work is modelled as operations with a
// companion events websocket. Local access is authenticated by socket peer
// credentials (root or a member of the configured group); there is no
// per-route authorization.
//
//	Version: 1.0
//	BasePath: /
//	Consumes:
//	- application/json
//	Produces:
//	- application/json
//	Schemes: http
//
// swagger:meta
package api

// A successful, immediately-completed response.
//
// swagger:response syncResponse
type syncResponseWrapper struct {
	// in: body
	Body syncResponse
}

// A reference to a background operation accepted for asynchronous processing.
//
// swagger:response asyncResponse
type asyncResponseWrapper struct {
	// The canonical operation URL (also echoed in the body).
	Location string
	// in: body
	Body asyncResponse
}

// An error response. error_code mirrors the HTTP status.
//
// swagger:response errorResponse
type errorResponseWrapper struct {
	// in: body
	Body errorResponse
}

// swagger:parameters knowledgeGet knowledgeDelete knowledgeExport sourcesList sourcesIngest sourceGet sourceDelete
type knowledgeNameParam struct {
	// The knowledge base name.
	//
	// in: path
	// required: true
	Name string `json:"name"`
}

// swagger:parameters sourceGet sourceDelete
type sourceIDParam struct {
	// The source identifier.
	//
	// in: path
	// required: true
	ID string `json:"id"`
}

// swagger:parameters operationGet operationDelete operationWait chatConnect
type operationIDParam struct {
	// The operation UUID.
	//
	// in: path
	// required: true
	ID string `json:"id"`
}

// swagger:parameters operationWait
type operationWaitParams struct {
	// Maximum seconds to block waiting for the operation to reach a terminal
	// state. A timeout that elapses returns the current state, not an error.
	//
	// in: query
	Timeout int `json:"timeout"`
}

// swagger:parameters events
type eventsParams struct {
	// Comma-separated event types to subscribe to (e.g. "operation,logging").
	//
	// in: query
	Type string `json:"type"`
}

// swagger:parameters chatConnect
type chatConnectParams struct {
	// The one-time websocket secret from the chat operation metadata.
	//
	// in: query
	// required: true
	Secret string `json:"secret"`
}

// swagger:parameters knowledgeCreate
type knowledgeCreateBody struct {
	// in: body
	Body createKnowledgeRequest
}

// swagger:parameters knowledgeImport
type knowledgeImportBody struct {
	// in: body
	Body importRequest
}

// swagger:parameters knowledgeExport
type knowledgeExportBody struct {
	// in: body
	Body exportRequest
}

// swagger:parameters gdriveResolve
type gdriveResolveBody struct {
	// in: body
	Body gdriveResolveRequest
}

// swagger:parameters gdriveImport
type gdriveImportBody struct {
	// in: body
	Body gdriveImportRequest
}

// swagger:parameters sourcesIngest
type sourcesIngestBody struct {
	// in: body
	Body ingestRequest
}

// swagger:parameters search
type searchBody struct {
	// in: body
	Body searchRequest
}

// swagger:parameters chatStart
type chatStartBody struct {
	// in: body
	Body chatStartRequest
}

// swagger:parameters answerBatch
type answerBatchBody struct {
	// in: body
	Body batchManifestRequest
}
