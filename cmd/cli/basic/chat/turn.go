package chat

import (
	"context"
	"fmt"
	"log"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/internal/chatstore"
)

// TokenKind distinguishes streamed final-answer content from reasoning/<think>
// content so a transport can render or label the two differently.
type TokenKind string

const (
	// TokenAnswer is final answer content.
	TokenAnswer TokenKind = "token"
	// TokenThink is reasoning/<think> content.
	TokenThink TokenKind = "think"
)

// StreamFunc receives streamed model output as it is produced. Returning a
// non-nil error aborts streaming (e.g. the client disconnected).
type StreamFunc func(kind TokenKind, content string) error

// conversation is the running state of one multi-turn chat: the resolved model,
// the system prompt (kept apart from the turns, since the two wire protocols
// place it differently), the user/assistant history, and the sampling
// temperature the OpenAI backend honours.
type conversation struct {
	model       string
	system      string
	history     []Message
	temperature *float64
}

// turnRequest builds the request for the next turn: the history plus prompt as
// the new user message. The history itself is not modified, so an augmented RAG
// prompt can be sent without being stored.
func (c *conversation) turnRequest(prompt string) Request {
	messages := make([]Message, len(c.history), len(c.history)+1)
	copy(messages, c.history)
	messages = append(messages, Message{Role: RoleUser, Content: prompt})
	return Request{
		Model:       c.model,
		System:      c.system,
		Messages:    messages,
		Temperature: c.temperature,
		Purpose:     PurposeChat,
	}
}

// record appends a finished turn: the original prompt (not the augmented one)
// and, when the model produced text, the reply.
func (c *conversation) record(prompt, reply string) {
	c.history = append(c.history, Message{Role: RoleUser, Content: prompt})
	if reply != "" {
		c.history = append(c.history, Message{Role: RoleAssistant, Content: reply})
	}
}

// temperatureNotice returns the verbose-mode notice for a temperature the
// selected backend will not send, or "" when it is honoured.
func temperatureNotice(api string, temperature float64) string {
	if resolved, _ := ResolveAPI(api); resolved != APIAnthropic {
		return ""
	}
	return fmt.Sprintf("Note: temperature %.2g is ignored with %s=%s (current Claude models reject sampling parameters)", temperature, ConfAPI, APIAnthropic)
}

// LiveSession is a server-owned, multi-turn chat session bundling the inference
// client, the running conversation history, and the RAG retrieval state. It is
// the presentation-free counterpart of the chat REPL: the daemon owns one per
// websocket connection and drives it with Prompt. A LiveSession is not safe for
// concurrent use; a single connection goroutine must own it.
type LiveSession struct {
	backend Backend
	// conv carries the model, the resolved system prompt the session started
	// with (a restored history runs under the same prompt), and the turns.
	conv    conversation
	session *Session
	verbose bool
	// promptRef is the provenance reference of the resolved system prompt
	// ("variant@version", or empty for the built-in default), stamped onto a
	// saved chat so a later reader knows which prompt this conversation ran on.
	promptRef string
	// chatID pins the saved-chat record this session persists to, so a second
	// save updates in place rather than creating a duplicate. Empty until the
	// session is resumed from a saved chat or saved for the first time.
	chatID string
}

// NewLiveSession creates a session against the inference server addressed by
// backend. model is the resolved chat model; when empty it is looked up from the
// server, which fails for a backend that cannot list models.
// knowledgeClient and embeddingModelID enable RAG retrieval; pass a nil client
// to disable it. activeBases are initial active knowledge-base names (resolved
// to indexes). systemPrompt and temperature seed the conversation.
func NewLiveSession(backend BackendConfig, model string, knowledgeClient *knowledge.OpenSearchClient, embeddingModelID string, activeBases []string, systemPrompt string, temperature float64, verbose bool) (*LiveSession, error) {
	b, err := NewBackend(backend)
	if err != nil {
		return nil, err
	}
	if model == "" {
		model, err = FindModelName(backend)
		if err != nil {
			return nil, err
		}
	}
	if notice := temperatureNotice(backend.API, temperature); notice != "" && verbose {
		log.Print(notice)
	}

	indexes := make([]string, 0, len(activeBases))
	for _, b := range activeBases {
		indexes = append(indexes, knowledge.FullIndexName(b))
	}

	ls := &LiveSession{
		backend: b,
		conv: conversation{
			model:       model,
			system:      systemPrompt,
			temperature: &temperature,
		},
		session: &Session{
			KnowledgeClient:  knowledgeClient,
			EmbeddingModelID: embeddingModelID,
			ActiveIndexes:    indexes,
		},
		verbose: verbose,
	}
	return ls, nil
}

// Model returns the resolved chat model name.
func (ls *LiveSession) Model() string { return ls.conv.model }

// Restore seeds the session with a saved conversation, keeping the system prompt
// resolved at construction, and pins the chat id so a later Save updates the same
// record. It is called before the session's websocket is driven.
func (ls *LiveSession) Restore(turns []chatstore.Turn, chatID string) {
	ls.conv.history = turnsToHistory(turns)
	ls.chatID = chatID
}

// Turns returns the conversation so far as store turns (system prompt excluded),
// ready to persist.
func (ls *LiveSession) Turns() []chatstore.Turn {
	return historyToTurns(ls.conv.history)
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

// Prompt runs one RAG turn for text, streaming output through emit, and appends
// the user prompt and assistant reply to the session history so the next turn
// continues the conversation. It is the presentation-free counterpart of the
// REPL's handlePrompt: the same rewrite → retrieve → augment → stream pipeline.
// Retrieval augmentation is applied only when a knowledge client and at least
// one active base are present; with no active bases the prompt is answered
// without retrieval.
func (ls *LiveSession) Prompt(ctx context.Context, text string, emit StreamFunc) error {
	hasRAG := ls.session.KnowledgeClient != nil && len(ls.session.ActiveIndexes) > 0

	lexicalQuery := text
	ragContext := ""
	if hasRAG {
		lexicalQuery = rewriteSearchQuery(ls.backend, ls.conv.model, ls.conv.history, text, ls.verbose)
		ragContext = retrieveContext(ls.session, text, lexicalQuery, ls.verbose)
	}

	llmPrompt := text
	if ragContext != "" {
		llmPrompt = buildRAGPrompt(ragContext, "", "", text)
	} else if hasRAG {
		// A base is active but retrieval returned nothing: inject an explicit
		// empty-context note so the grounding rules apply and the model does
		// not answer from parametric knowledge (matching the REPL).
		llmPrompt = buildRAGPrompt("No relevant context was retrieved for this query.", "", "", text)
	}

	// Send the augmented prompt to the API but keep only the original prompt in
	// history, mirroring the REPL's handlePrompt.
	reply, err := ls.backend.Stream(ctx, ls.conv.turnRequest(llmPrompt), emit)
	if err != nil {
		return err
	}
	ls.conv.record(text, reply)
	return nil
}
