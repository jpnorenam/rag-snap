package chat

import (
	"context"
	"strings"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/internal/chatstore"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/ssestream"
)

// TokenKind distinguishes streamed final-answer content from reasoning/<think>
// content so a transport can render or label the two differently.
type TokenKind string

const (
	// TokenAnswer is final answer content.
	TokenAnswer TokenKind = "token"
	// TokenThink is reasoning/<think> content.
	TokenThink TokenKind = "think"
	// TokenWarning is a non-fatal notice about the turn, such as a failed kapa.ai
	// request; the turn still completes. Clients must not treat it as an error.
	TokenWarning TokenKind = "warning"
)

// StreamFunc receives streamed model output as it is produced. Returning a
// non-nil error aborts streaming (e.g. the client disconnected).
type StreamFunc func(kind TokenKind, content string) error

// NewInferenceClient builds an OpenAI-compatible client for baseURL, applying
// CHAT_API_KEY when set.
func NewInferenceClient(baseURL string) (openai.Client, error) {
	opts, err := clientOptions(baseURL)
	if err != nil {
		return openai.Client{}, err
	}
	return openai.NewClient(opts...), nil
}

// LiveSession is a server-owned, multi-turn chat session bundling the inference
// client, the running conversation history, and the RAG retrieval state. It is
// the presentation-free counterpart of the chat REPL: the daemon owns one per
// websocket connection and drives it with Prompt. A LiveSession is not safe for
// concurrent use; a single connection goroutine must own it.
type LiveSession struct {
	client  openai.Client
	params  openai.ChatCompletionNewParams
	session *Session
	verbose bool
	// systemPrompt is the resolved system prompt the session started with, kept
	// so Restore can rebuild history under the same prompt.
	systemPrompt string
	// promptRef is the provenance reference of the resolved system prompt
	// ("variant@version", or empty for the built-in default), stamped onto a
	// saved chat so a later reader knows which prompt this conversation ran on.
	promptRef string
	// chatID pins the saved-chat record this session persists to, so a second
	// save updates in place rather than creating a duplicate. Empty until the
	// session is resumed from a saved chat or saved for the first time.
	chatID string
}

// NewLiveSession creates a session against the inference server at baseURL.
// model is the resolved chat model; when empty it is looked up from the server.
// knowledgeClient and embeddingModelID enable RAG retrieval; pass a nil client
// to disable it. kapaClient enables kapa.ai retrieval once source groups are
// selected (SetActiveKapaGroups); nil means kapa.ai is unavailable. activeBases are initial active knowledge-base names (resolved
// to indexes). systemPrompt and temperature seed the conversation.
func NewLiveSession(baseURL, model string, knowledgeClient *knowledge.OpenSearchClient, kapaClient *knowledge.KapaClient, embeddingModelID string, activeBases []string, systemPrompt string, temperature float64, verbose bool) (*LiveSession, error) {
	if model == "" {
		var err error
		model, err = FindModelName(baseURL)
		if err != nil {
			return nil, err
		}
	}

	indexes := make([]string, 0, len(activeBases))
	for _, b := range activeBases {
		indexes = append(indexes, knowledge.FullIndexName(b))
	}

	opts, err := clientOptions(baseURL)
	if err != nil {
		return nil, err
	}

	ls := &LiveSession{
		client: openai.NewClient(opts...),
		params: openai.ChatCompletionNewParams{
			Messages: []openai.ChatCompletionMessageParamUnion{
				openai.SystemMessage(systemPrompt),
			},
			Model:       model,
			Temperature: openai.Float(temperature),
		},
		// A session starts with no kapa.ai source groups selected, so it never
		// opens by querying an entire kapa.ai project.
		session: &Session{
			KnowledgeClient:  knowledgeClient,
			KapaClient:       kapaClient,
			EmbeddingModelID: embeddingModelID,
			ActiveIndexes:    indexes,
		},
		verbose:      verbose,
		systemPrompt: systemPrompt,
	}
	return ls, nil
}

// Model returns the resolved chat model name.
func (ls *LiveSession) Model() string { return ls.params.Model }

// Restore seeds the session with a saved conversation, keeping the system prompt
// resolved at construction, and pins the chat id so a later Save updates the same
// record. It is called before the session's websocket is driven.
func (ls *LiveSession) Restore(turns []chatstore.Turn, chatID string) {
	ls.params.Messages = turnsToHistory(ls.systemPrompt, turns)
	ls.chatID = chatID
}

// Turns returns the conversation so far as store turns (system prompt excluded),
// ready to persist.
func (ls *LiveSession) Turns() []chatstore.Turn {
	return historyToTurns(ls.params.Messages)
}

// SetPromptRef records the provenance reference of the session's resolved system
// prompt ("variant@version", or empty for the built-in default), so a save can
// stamp it onto the stored chat.
func (ls *LiveSession) SetPromptRef(ref string) { ls.promptRef = ref }

// PromptRef returns the session's prompt provenance reference.
func (ls *LiveSession) PromptRef() string { return ls.promptRef }

// ChatID returns the pinned saved-chat id, or empty if this session has not been
// resumed from or saved to a record yet.
func (ls *LiveSession) ChatID() string { return ls.chatID }

// SetChatID pins the saved-chat id, so the next save updates that record.
func (ls *LiveSession) SetChatID(id string) { ls.chatID = id }

// SetActiveBases replaces the session's active knowledge bases (by name); they
// are resolved to index names. Retrieval for subsequent prompts uses this set.
func (ls *LiveSession) SetActiveBases(names []string) {
	indexes := make([]string, 0, len(names))
	for _, n := range names {
		indexes = append(indexes, knowledge.FullIndexName(n))
	}
	ls.session.ActiveIndexes = indexes
}

// ActiveBases returns the current active knowledge-base names.
func (ls *LiveSession) ActiveBases() []string {
	names := make([]string, 0, len(ls.session.ActiveIndexes))
	for _, idx := range ls.session.ActiveIndexes {
		if name, err := knowledge.KnowledgeBaseNameFromIndex(idx); err == nil {
			names = append(names, name)
		} else {
			names = append(names, idx)
		}
	}
	return names
}

// KapaAvailable reports whether the session has a kapa.ai client, that is
// whether a source-group selection can be applied at all.
func (ls *LiveSession) KapaAvailable() bool { return ls.session.KapaClient != nil }

// SetActiveKapaGroups replaces the session's selected kapa.ai source groups (by
// id), independently of the active knowledge bases. Retrieval for subsequent
// prompts queries exactly these groups; an empty selection means no kapa.ai
// retrieval.
func (ls *LiveSession) SetActiveKapaGroups(ids []string) {
	ls.session.ActiveKapaGroups = append([]string(nil), ids...)
}

// ActiveKapaGroups returns the selected kapa.ai source-group ids.
func (ls *LiveSession) ActiveKapaGroups() []string {
	return append([]string(nil), ls.session.ActiveKapaGroups...)
}

// Prompt runs one RAG turn for text, streaming output through emit, and appends
// the user prompt and assistant reply to the session history so the next turn
// continues the conversation. It is the presentation-free counterpart of the
// REPL's handlePrompt: the same rewrite → retrieve → augment → stream pipeline.
// Retrieval augmentation is applied only when a knowledge client and at least
// one active base are present; with no active bases the prompt is answered
// without retrieval.
func (ls *LiveSession) Prompt(ctx context.Context, text string, emit StreamFunc) error {
	// Retrieval runs when local bases or kapa.ai source groups are active,
	// matching the REPL's handlePrompt.
	hasRAG := ls.session.KnowledgeClient != nil && len(ls.session.ActiveIndexes) > 0
	hasKapa := ls.session.KapaClient != nil && len(ls.session.ActiveKapaGroups) > 0
	hasContext := hasRAG || hasKapa

	lexicalQuery := text
	ragContext := ""
	if hasContext {
		// Retrieval problems (a failed kapa.ai request) reach the client as a
		// warning frame on this turn's stream instead of staying in the logs.
		ls.session.Warn = func(msg string) { _ = emit(TokenWarning, msg) }
		defer func() { ls.session.Warn = nil }()
		lexicalQuery = rewriteSearchQuery(ls.client, ls.params.Model, ls.params.Messages, text, ls.verbose)
		ragContext, _ = retrieveContext(ls.session, text, lexicalQuery, ls.verbose)
	}

	llmPrompt := text
	if ragContext != "" {
		llmPrompt = buildRAGPrompt(ragContext, "", "", text)
	} else if hasContext {
		// A base is active but retrieval returned nothing: inject an explicit
		// empty-context note so the grounding rules apply and the model does
		// not answer from parametric knowledge (matching the REPL).
		llmPrompt = buildRAGPrompt("No relevant context was retrieved for this query.", "", "", text)
	}

	// Send the augmented prompt to the API but keep only the original prompt in
	// history, mirroring the REPL's handlePrompt.
	apiMessages := make([]openai.ChatCompletionMessageParamUnion, len(ls.params.Messages))
	copy(apiMessages, ls.params.Messages)
	apiMessages = append(apiMessages, openai.UserMessage(llmPrompt))

	apiParams := ls.params
	apiParams.Messages = apiMessages

	stream := ls.client.Chat.Completions.NewStreaming(ctx, apiParams)
	appendParam, err := streamTurn(stream, emit)
	if err != nil {
		return err
	}

	ls.params.Messages = append(ls.params.Messages, openai.UserMessage(text))
	if appendParam != nil {
		ls.params.Messages = append(ls.params.Messages, *appendParam)
	}
	return nil
}

// streamTurn consumes the streaming completion, forwarding each delta through
// emit (labelled think vs answer based on <think> blocks), and returns the
// assistant message to append to history. It mirrors the REPL's processStream
// but writes to a callback instead of the terminal.
func streamTurn(stream *ssestream.Stream[openai.ChatCompletionChunk], emit StreamFunc) (*openai.ChatCompletionMessageParamUnion, error) {
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
			return nil, err
		}
		if strings.Contains(delta, "</think>") {
			thinking = false
		}
	}

	if err := stream.Err(); err != nil {
		return nil, err
	}
	if len(acc.Choices) == 0 || acc.Choices[0].Message.Content == "" {
		return nil, nil
	}
	appendParam := acc.Choices[0].Message.ToParam()
	return &appendParam, nil
}
