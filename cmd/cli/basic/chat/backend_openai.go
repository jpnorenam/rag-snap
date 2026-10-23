package chat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// openAIBackend speaks OpenAI Chat Completions: a local Inference snap or a
// third party's OpenAI-compatible route.
type openAIBackend struct {
	client  openai.Client
	baseURL string
}

func clientOptions(baseURL string) []option.RequestOption {
	opts := []option.RequestOption{option.WithBaseURL(baseURL)}
	if key := os.Getenv("CHAT_API_KEY"); key != "" {
		opts = append(opts, option.WithAPIKey(key))
	}
	return opts
}

func newOpenAIBackend(baseURL string) *openAIBackend {
	return &openAIBackend{client: openai.NewClient(clientOptions(baseURL)...), baseURL: baseURL}
}

// params maps a neutral request onto Chat Completions: the system prompt leads
// the message list, and only the rewrite carries an explicit output ceiling.
func (b *openAIBackend) params(r Request) openai.ChatCompletionNewParams {
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(r.Messages)+1)
	if r.System != "" {
		messages = append(messages, openai.SystemMessage(r.System))
	}
	for _, m := range r.Messages {
		switch m.Role {
		case RoleUser:
			messages = append(messages, openai.UserMessage(m.Content))
		case RoleAssistant:
			messages = append(messages, openai.AssistantMessage(m.Content))
		}
	}
	p := openai.ChatCompletionNewParams{Messages: messages, Model: r.Model}
	if r.Temperature != nil {
		p.Temperature = openai.Float(*r.Temperature)
	}
	if r.Purpose == PurposeRewrite {
		p.MaxCompletionTokens = openai.Int(int64(maxRewriteTokens))
		p.MaxTokens = openai.Int(int64(maxRewriteTokens)) // for runtimes that don't yet support MaxCompletionTokens
	}
	return p
}

func (b *openAIBackend) Complete(ctx context.Context, r Request) (string, error) {
	resp, err := b.client.Chat.Completions.New(ctx, b.params(r))
	if err != nil {
		return "", wrapOpenAIError(err)
	}
	if len(resp.Choices) == 0 {
		return "", nil
	}
	return resp.Choices[0].Message.Content, nil
}

// Stream forwards each delta through emit, labelled think vs answer based on
// <think> blocks, and returns the accumulated content (think spans included, as
// before: the store and the rewrite formatter strip them).
func (b *openAIBackend) Stream(ctx context.Context, r Request, emit StreamFunc) (string, error) {
	stream := b.client.Chat.Completions.NewStreaming(ctx, b.params(r))
	acc := openai.ChatCompletionAccumulator{}
	thinking := false

	for stream.Next() {
		chunk := stream.Current()
		acc.AddChunk(chunk)

		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta.Content
		if delta == "" {
			continue
		}

		kind := TokenAnswer
		switch {
		case strings.Contains(delta, "<think>"):
			thinking = true
			kind = TokenThink
		case strings.Contains(delta, "</think>"):
			kind = TokenThink
		case thinking:
			kind = TokenThink
		}
		if err := emit(kind, delta); err != nil {
			return "", err
		}
		if strings.Contains(delta, "</think>") {
			thinking = false
		}
	}

	if err := stream.Err(); err != nil {
		return "", wrapOpenAIError(err)
	}
	if len(acc.Choices) == 0 {
		return "", nil
	}
	return acc.Choices[0].Message.Content, nil
}

func (b *openAIBackend) Ping(ctx context.Context, model string) error {
	_, err := b.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("Are you up?"),
		},
		Model:               model,
		MaxCompletionTokens: openai.Int(1),
		MaxTokens:           openai.Int(1), // for runtimes that don't yet support MaxCompletionTokens
	})
	var apiError *openai.Error
	if errors.As(err, &apiError) {
		// llama-server starting up
		// Error: POST "http://localhost:8328/v1/chat/completions": 503 Service Unavailable {"message":"Loading model","type":"unavailable_error","code":503}
		if apiError.StatusCode == http.StatusServiceUnavailable && apiError.Type == "unavailable_error" {
			return fmt.Errorf("%w: %v", ErrServerLoading, err)
		}
	}
	return wrapOpenAIError(err)
}

// ListModels maps a 404/405 on GET /models to ErrModelListingUnsupported. AWS
// Bedrock's OpenAI-compatible endpoint is the motivating case: it serves
// /openai/v1/chat/completions but has no /openai/v1/models route. A
// 401/403/5xx is a real fault and is returned as is.
func (b *openAIBackend) ListModels(ctx context.Context) ([]string, error) {
	modelService := openai.NewModelService(clientOptions(b.baseURL)...)
	page, err := modelService.List(ctx)
	if err != nil {
		var apiErr *openai.Error
		if errors.As(err, &apiErr) &&
			(apiErr.StatusCode == http.StatusNotFound || apiErr.StatusCode == http.StatusMethodNotAllowed) {
			return nil, fmt.Errorf("%w: %v", ErrModelListingUnsupported, err)
		}
		return nil, wrapOpenAIError(err)
	}
	ids := make([]string, 0, len(page.Data))
	for _, m := range page.Data {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// wrapOpenAIError normalises an HTTP error response into a StatusError; any
// other error (connection refused, EOF) is returned unchanged.
func wrapOpenAIError(err error) error {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		return &StatusError{StatusCode: apiErr.StatusCode, Err: err}
	}
	return err
}
