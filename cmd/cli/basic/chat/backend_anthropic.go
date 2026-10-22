package chat

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Output ceilings per purpose. On current Claude models reasoning tokens count
// against max_tokens, so a ceiling sized for the answer alone can leave the text
// empty: these leave room for thinking on top of the expected output.
const (
	anthropicChatMaxTokens    = 32000
	anthropicAnswerMaxTokens  = 32000
	anthropicRefineMaxTokens  = 64000
	anthropicRewriteMaxTokens = 2048
	anthropicPingMaxTokens    = 16
)

// anthropicBackend speaks the Anthropic Messages API, e.g. Claude on Bedrock at
// https://bedrock-runtime.{region}.amazonaws.com/anthropic, authenticated with the
// Bedrock API key (CHAT_API_KEY) as a bearer token in x-api-key.
type anthropicBackend struct {
	client anthropic.Client
}

// newAnthropicBackend builds the client from explicit options only.
// WithoutEnvironmentDefaults stops the SDK autoloading ANTHROPIC_API_KEY,
// ANTHROPIC_AUTH_TOKEN, ANTHROPIC_BASE_URL, ANTHROPIC_PROFILE, workload-identity
// federation or an on-disk `ant` profile: CHAT_API_KEY is the chat backend's
// only credential, and the configured chat.http.* URL its only destination.
func newAnthropicBackend(baseURL string) *anthropicBackend {
	return &anthropicBackend{client: anthropic.NewClient(
		option.WithoutEnvironmentDefaults(),
		option.WithBaseURL(baseURL),
		option.WithAPIKey(os.Getenv("CHAT_API_KEY")),
		option.WithMaxRetries(2),
	)}
}

// params maps a neutral request onto the Messages API. The system prompt is the
// top-level field, max_tokens is always explicit, and temperature/top_p/top_k are
// never sent — current Claude models reject non-default sampling parameters.
func (b *anthropicBackend) params(r Request) anthropic.MessageNewParams {
	messages := make([]anthropic.MessageParam, 0, len(r.Messages))
	for _, m := range r.Messages {
		switch m.Role {
		case RoleUser:
			messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		case RoleAssistant:
			messages = append(messages, anthropic.NewAssistantMessage(anthropic.NewTextBlock(m.Content)))
		}
	}

	p := anthropic.MessageNewParams{
		Model:     r.Model,
		MaxTokens: anthropicMaxTokens(r.Purpose),
		Messages:  messages,
	}
	if r.System != "" {
		system := anthropic.TextBlockParam{Text: r.System}
		if r.Purpose == PurposeAnswer {
			// The batch system prompt is identical for every question: cache it.
			// A prompt below the model's minimum cacheable size is simply not
			// cached, so the breakpoint can never fail a request.
			system.CacheControl = anthropic.NewCacheControlEphemeralParam()
		}
		p.System = []anthropic.TextBlockParam{system}
	}

	switch r.Purpose {
	case PurposeChat:
		// Stream a reasoning summary so the REPL shows progress (rendered like a
		// <think> block) instead of a silent pause before the answer.
		p.Thinking = anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{
			Display: anthropic.ThinkingConfigAdaptiveDisplaySummarized,
		}}
	case PurposeRewrite:
		// Keyword extraction needs little reasoning; keep it from eating the budget.
		p.OutputConfig = anthropic.OutputConfigParam{Effort: anthropic.OutputConfigEffortLow}
	}
	return p
}

func anthropicMaxTokens(p Purpose) int64 {
	switch p {
	case PurposeAnswer:
		return anthropicAnswerMaxTokens
	case PurposeRefine:
		return anthropicRefineMaxTokens
	case PurposeRewrite:
		return anthropicRewriteMaxTokens
	default:
		return anthropicChatMaxTokens
	}
}

// Complete streams and accumulates: large ceilings on a non-streaming request
// risk HTTP timeouts while the model reasons.
func (b *anthropicBackend) Complete(ctx context.Context, r Request) (string, error) {
	return b.Stream(ctx, r, func(TokenKind, string) error { return nil })
}

// Stream forwards text deltas as answer tokens and thinking deltas as think
// tokens, and returns only the text — reasoning is never kept in history.
func (b *anthropicBackend) Stream(ctx context.Context, r Request, emit StreamFunc) (string, error) {
	params := b.params(r)
	stream := b.client.Messages.NewStreaming(ctx, params)
	defer stream.Close()

	message := anthropic.Message{}
	for stream.Next() {
		event := stream.Current()
		if err := message.Accumulate(event); err != nil {
			return "", err
		}
		delta, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent)
		if !ok {
			continue
		}
		switch d := delta.Delta.AsAny().(type) {
		case anthropic.TextDelta:
			if d.Text != "" {
				if err := emit(TokenAnswer, d.Text); err != nil {
					return "", err
				}
			}
		case anthropic.ThinkingDelta:
			if d.Thinking != "" {
				if err := emit(TokenThink, d.Thinking); err != nil {
					return "", err
				}
			}
		}
	}
	if err := stream.Err(); err != nil {
		return "", wrapAnthropicError(err)
	}
	return finalText(message, params.MaxTokens)
}

// finalText extracts the answer text and turns the stop reasons that mean "no
// usable answer" into errors, so neither a refusal nor a reasoning-exhausted
// budget is ever recorded as an empty answer.
func finalText(m anthropic.Message, maxTokens int64) (string, error) {
	var b strings.Builder
	for _, block := range m.Content {
		if text, ok := block.AsAny().(anthropic.TextBlock); ok {
			b.WriteString(text.Text)
		}
	}
	text := b.String()

	switch m.StopReason {
	case anthropic.StopReasonRefusal:
		return "", &RefusalError{Category: string(m.StopDetails.Category)}
	case anthropic.StopReasonMaxTokens:
		if strings.TrimSpace(text) == "" {
			return "", &OutputCeilingError{MaxTokens: maxTokens}
		}
	}
	return text, nil
}

// Ping sends one tiny request at low effort. Any 2xx is healthy — including a
// stop on max_tokens, which is the expected outcome of a 16-token ceiling.
func (b *anthropicBackend) Ping(ctx context.Context, model string) error {
	_, err := b.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:        model,
		MaxTokens:    anthropicPingMaxTokens,
		Messages:     []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("Are you up?"))},
		OutputConfig: anthropic.OutputConfigParam{Effort: anthropic.OutputConfigEffortLow},
	})
	return wrapAnthropicError(err)
}

// ListModels never lists: Bedrock's Messages endpoint has no Models API, so the
// model must always be configured explicitly.
func (b *anthropicBackend) ListModels(context.Context) ([]string, error) {
	return nil, ErrModelListingUnsupported
}

// wrapAnthropicError normalises an HTTP error response into a StatusError; any
// other error (connection refused, EOF) is returned unchanged.
func wrapAnthropicError(err error) error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		return &StatusError{StatusCode: apiErr.StatusCode, Err: err}
	}
	return err
}
