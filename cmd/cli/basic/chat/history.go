package chat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/fatih/color"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/internal/chatstore"
	"github.com/openai/openai-go/v3"
)

// localChatStore returns the daemonless CLI's client-local saved-chat store under
// <UserConfigDir>/rag-cli/chats, the sibling of the local prompts.json fallback.
func localChatStore() (*chatstore.Store, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return chatstore.New(filepath.Join(dir, "rag-cli", "chats")), nil
}

// renderTranscript prints a saved conversation with dim role labels, so a
// resumed session shows where it left off before the next prompt.
func renderTranscript(turns []chatstore.Turn) {
	for _, t := range turns {
		label := "You"
		if t.Role == "assistant" {
			label = "Assistant"
		}
		fmt.Printf("%s\n%s\n\n", color.HiBlackString("— %s —", label), t.Content)
	}
}

// pickSavedChat presents summaries (assumed newest-first) in a filterable select
// and returns the chosen one. ok is false when the user cancelled or there are no
// saved chats (in which case a friendly hint is printed).
func pickSavedChat(summaries []chatstore.Summary) (chatstore.Summary, bool) {
	if len(summaries) == 0 {
		fmt.Printf("No saved chats yet. Use %s to store the current conversation.\n", cmdSave)
		return chatstore.Summary{}, false
	}

	options := make([]huh.Option[string], len(summaries))
	index := make(map[string]chatstore.Summary, len(summaries))
	for i, s := range summaries {
		label := fmt.Sprintf("%s  ·  %s  ·  %d turns", s.Title, relativeTime(s.UpdatedAt), s.TurnCount)
		if s.Model != "" {
			label += "  ·  " + s.Model
		}
		options[i] = huh.NewOption(label, s.ID)
		index[s.ID] = s
	}

	var chosen string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Resume a saved chat").
				Options(options...).
				Filtering(true).
				Value(&chosen),
		),
	)
	if err := form.Run(); err != nil || chosen == "" {
		// Cancelled (Ctrl+C / Esc) — leave the current session untouched.
		return chatstore.Summary{}, false
	}
	return index[chosen], true
}

// saveDirectChat persists the direct-REPL conversation to the client-local
// store, creating or (when chatID is set) updating the record. It returns the
// stored id to pin so a later save updates the same record; ok is false when
// nothing was saved.
func saveDirectChat(store *chatstore.Store, chatID, title, model string, session *Session, messages []openai.ChatCompletionMessageParamUnion) (string, bool) {
	if store == nil {
		fmt.Println("Saved chats are unavailable: could not resolve the config directory.")
		return "", false
	}
	saved, err := store.Save(chatstore.Chat{
		ID:    chatID,
		Title: strings.TrimSpace(title),
		Model: model,
		Bases: activeBaseNames(session),
		Turns: historyToTurns(messages),
	})
	if err != nil {
		if errors.Is(err, chatstore.ErrEmpty) {
			fmt.Println("Nothing to save yet — ask a question first.")
		} else {
			fmt.Printf("Could not save chat: %v\n", err)
		}
		return "", false
	}
	fmt.Printf("Saved chat as %q.\n", saved.Title)
	return saved.ID, true
}

// resumeDirectChat lists the client-local store, lets the user pick a chat, and
// returns the rebuilt message history and the resumed id to pin. It restores the
// saved active bases (dropping any that no longer exist) into session and prints
// the transcript. ok is false when the user cancelled or nothing could be opened.
func resumeDirectChat(store *chatstore.Store, systemPrompt string, session *Session) ([]openai.ChatCompletionMessageParamUnion, string, bool) {
	if store == nil {
		fmt.Println("Saved chats are unavailable: could not resolve the config directory.")
		return nil, "", false
	}
	summaries, err := store.List("")
	if err != nil {
		fmt.Printf("Could not list saved chats: %v\n", err)
		return nil, "", false
	}
	picked, ok := pickSavedChat(summaries)
	if !ok {
		return nil, "", false
	}
	saved, err := store.Get(picked.ID)
	if err != nil {
		fmt.Printf("Could not open saved chat: %v\n", err)
		return nil, "", false
	}

	kept, dropped := splitExistingBases(session.KnowledgeClient, saved.Bases)
	setActiveBaseNames(session, kept)
	if len(dropped) > 0 {
		fmt.Printf("Note: skipping knowledge base(s) that no longer exist: %s\n", strings.Join(dropped, ", "))
	}

	renderTranscript(saved.Turns)
	fmt.Printf("Resumed %q. Continue the conversation below.\n", saved.Title)
	return turnsToHistory(systemPrompt, saved.Turns), saved.ID, true
}

// activeBaseNames returns the session's active knowledge bases as base names.
func activeBaseNames(s *Session) []string {
	names := make([]string, 0, len(s.ActiveIndexes))
	for _, idx := range s.ActiveIndexes {
		if n, err := knowledge.KnowledgeBaseNameFromIndex(idx); err == nil {
			names = append(names, n)
		}
	}
	return names
}

// setActiveBaseNames replaces the session's active indexes from base names.
func setActiveBaseNames(s *Session, names []string) {
	indexes := make([]string, 0, len(names))
	for _, n := range names {
		indexes = append(indexes, knowledge.FullIndexName(n))
	}
	s.ActiveIndexes = indexes
}

// splitExistingBases splits want into base names that still exist as knowledge
// indexes and those that are gone. A nil client (retrieval unavailable) drops
// every base.
func splitExistingBases(kc *knowledge.OpenSearchClient, want []string) (kept, dropped []string) {
	if len(want) == 0 {
		return nil, nil
	}
	existing := map[string]bool{}
	if kc != nil {
		if idxs, err := kc.ListIndexes(context.Background()); err == nil {
			for _, idx := range idxs {
				if n, err := knowledge.KnowledgeBaseNameFromIndex(idx.Name); err == nil {
					existing[n] = true
				}
			}
		}
	}
	for _, b := range want {
		if existing[b] {
			kept = append(kept, b)
		} else {
			dropped = append(dropped, b)
		}
	}
	return kept, dropped
}

// relativeTime renders t as a short human "… ago" string.
func relativeTime(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// historyToTurns projects an in-memory conversation to neutral store turns,
// dropping the leading system prompt (and any other non user/assistant role):
// the system prompt is freshly resolved on resume, so a customization applies to
// resumed sessions the same way it applies to new ones. Assistant <think> spans
// are stripped by the store on save, so callers need not pre-clean here.
func historyToTurns(messages []openai.ChatCompletionMessageParamUnion) []chatstore.Turn {
	turns := make([]chatstore.Turn, 0, len(messages))
	for _, m := range messages {
		switch {
		case m.OfUser != nil:
			turns = append(turns, chatstore.Turn{Role: "user", Content: m.OfUser.Content.OfString.Or("")})
		case m.OfAssistant != nil:
			turns = append(turns, chatstore.Turn{Role: "assistant", Content: m.OfAssistant.Content.OfString.Or("")})
		}
	}
	return turns
}

// turnsToHistory rebuilds a message list from a system prompt and saved turns,
// so a resumed session continues from where it left off. The system prompt is
// always the freshly resolved one, never a stored copy.
func turnsToHistory(systemPrompt string, turns []chatstore.Turn) []openai.ChatCompletionMessageParamUnion {
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(turns)+1)
	messages = append(messages, openai.SystemMessage(systemPrompt))
	for _, t := range turns {
		switch t.Role {
		case "user":
			messages = append(messages, openai.UserMessage(t.Content))
		case "assistant":
			messages = append(messages, openai.AssistantMessage(t.Content))
		}
	}
	return messages
}
