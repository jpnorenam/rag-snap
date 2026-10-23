// Package chat implements the chat REPL, the daemon's live chat sessions, batch
// answering and the RAG pipeline, over an inference Backend that speaks either
// OpenAI Chat Completions or the Anthropic Messages API.
package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ConfAPI is the config key selecting the inference wire protocol. Both the CLI
// and the daemon resolve it through ResolveAPI, so a single value seeds both.
const ConfAPI = "chat.api"

// Accepted chat.api values.
const (
	// APIOpenAI is OpenAI Chat Completions — a local Inference snap, or a third
	// party's OpenAI-compatible route (e.g. Bedrock's /openai/v1). The default.
	APIOpenAI = "openai"
	// APIAnthropic is the Anthropic Messages API — e.g. Claude on Bedrock at
	// bedrock-runtime.{region}.amazonaws.com/anthropic.
	APIAnthropic = "anthropic"
)

// ResolveAPI validates a chat.api value, mapping unset/empty to APIOpenAI. An
// unrecognised value is an error rather than a silent fallback: a wrong protocol
// would only surface later as an opaque 404 from the inference server.
func ResolveAPI(value string) (string, error) {
	switch v := strings.TrimSpace(value); v {
	case "":
		return APIOpenAI, nil
	case APIOpenAI, APIAnthropic:
		return v, nil
	default:
		return "", fmt.Errorf("invalid %s value %q: must be %q or %q", ConfAPI, value, APIOpenAI, APIAnthropic)
	}
}

// BackendConfig addresses the inference server: which wire protocol to speak
// (a chat.api value) and the base URL built from chat.http.*.
type BackendConfig struct {
	API     string
	BaseURL string
}

// Role is a conversation turn's author. The system prompt is not a turn: it is
// carried separately in Request.System, because the two protocols place it
// differently (a leading message vs a top-level field).
type Role string

// The two conversation roles.
const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one provider-neutral conversation turn.
type Message struct {
	Role    Role
	Content string
}

// Purpose names what a request is for, so each backend can tune its output
// ceiling, reasoning effort and thinking display without call sites branching
// on the provider.
type Purpose int

const (
	// PurposeChat is an interactive chat turn (REPL or daemon websocket).
	PurposeChat Purpose = iota
	// PurposeAnswer is a batch answer to one manifest question.
	PurposeAnswer
	// PurposeRewrite is the lexical keyword extraction for retrieval.
	PurposeRewrite
	// PurposeRefine is the RFP question refinement pass.
	PurposeRefine
)

// Request is one provider-neutral generation request.
type Request struct {
	Model    string
	System   string
	Messages []Message
	// Temperature is honoured by the OpenAI backend. The Anthropic backend never
	// sends sampling parameters (current Claude models reject them) and drops it.
	Temperature *float64
	Purpose     Purpose
}

// Backend is an inference server speaking one wire protocol.
type Backend interface {
	// Complete runs a request and returns the final text.
	Complete(ctx context.Context, r Request) (string, error)
	// Stream runs a request, forwarding answer and reasoning deltas through emit
	// as they arrive, and returns the final answer text (reasoning excluded) to
	// keep in history.
	Stream(ctx context.Context, r Request, emit StreamFunc) (string, error)
	// Ping sends the smallest possible request to confirm the server is up and
	// the credentials and model are accepted.
	Ping(ctx context.Context, model string) error
	// ListModels returns the server's model ids, or an error wrapping
	// ErrModelListingUnsupported when it has no listing endpoint.
	ListModels(ctx context.Context) ([]string, error)
}

// NewBackend builds the backend selected by cfg.API.
func NewBackend(cfg BackendConfig) (Backend, error) {
	api, err := ResolveAPI(cfg.API)
	if err != nil {
		return nil, err
	}
	if api == APIAnthropic {
		return newAnthropicBackend(cfg.BaseURL), nil
	}
	return newOpenAIBackend(cfg.BaseURL), nil
}

// ErrModelListingUnsupported reports that the inference server cannot name its
// models: AWS Bedrock's OpenAI-compatible route 404s on GET /models, and the
// Anthropic backend never lists. The model must then be configured explicitly
// (chat.model).
var ErrModelListingUnsupported = errors.New("inference server has no model-listing endpoint")

// ErrServerLoading reports that the inference server is up but still loading
// its model (llama-server answers 503 unavailable_error meanwhile); callers that
// wait for readiness retry on it.
var ErrServerLoading = errors.New("inference server is loading a model")

// StatusError is an HTTP error response from the inference server, normalised
// across SDKs so callers can branch on the status code.
type StatusError struct {
	StatusCode int
	Err        error
}

func (e *StatusError) Error() string { return e.Err.Error() }
func (e *StatusError) Unwrap() error { return e.Err }

// Unauthorized reports whether the server rejected the credentials.
func (e *StatusError) Unauthorized() bool { return e.StatusCode == 401 || e.StatusCode == 403 }

// RefusalError reports that the model declined to answer. It is an error, never
// an empty answer, so a batch records the question as failed instead of
// writing a blank.
type RefusalError struct {
	Category string
}

func (e *RefusalError) Error() string {
	if e.Category == "" {
		return "the model declined to answer"
	}
	return fmt.Sprintf("the model declined to answer (category: %s)", e.Category)
}

// OutputCeilingError reports that generation hit max_tokens before producing
// any answer text — reasoning consumed the whole output budget. It is a budget
// failure, never "the model found nothing".
type OutputCeilingError struct {
	MaxTokens int64
}

func (e *OutputCeilingError) Error() string {
	return fmt.Sprintf("the model reached its output ceiling (max_tokens=%d) before producing an answer", e.MaxTokens)
}

// ModelRequiredError is returned when the backend cannot list models and no
// model was configured.
func ModelRequiredError() error {
	return fmt.Errorf("%w; set the model name with `sudo rag-cli.rag set --package chat.model=<model>` (e.g. global.anthropic.claude-sonnet-5-5 or a Bedrock model id)",
		ErrModelListingUnsupported)
}
