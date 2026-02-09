package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/openai/openai-go/v3"
)

const (
	defaultRAGTopK     = 10
	maxRewriteTurns    = 3
	maxRewriteTokens   = 1024
	maxAssistantLength = 400
)

// retrieveContext searches the active knowledge base indexes for content
// relevant to query. Returns an empty string when RAG is unavailable or
// the search yields no results, allowing the caller to fall back to a
// plain prompt.
func retrieveContext(session *Session, query, lexicalQuery string, verbose bool) string {
	if session.KnowledgeClient == nil || len(session.ActiveIndexes) == 0 || session.EmbeddingModelID == "" {
		return ""
	}

	hits, err := session.KnowledgeClient.Search(
		context.Background(),
		session.ActiveIndexes,
		query,
		lexicalQuery,
		session.EmbeddingModelID,
		defaultRAGTopK,
	)
	if err != nil {
		if verbose {
			fmt.Printf("Knowledge search failed: %v\n", err)
		}
		return ""
	}

	if len(hits) == 0 {
		return ""
	}

	if verbose {
		fmt.Printf("Retrieved %d results from knowledge base\n", len(hits))
	}

	return formatContext(hits)
}

// formatContext renders a slice of search hits into a single text block
// suitable for injection into a RAG prompt.
func formatContext(hits []knowledge.SearchHit) string {
	var b strings.Builder
	for i, hit := range hits {
		if i > 0 {
			b.WriteString("\n---\n")
		}
		b.WriteString(hit.Content)
		fmt.Fprintf(&b, "\n(source: %s, score: %.4f)", hit.SourceID, hit.Score)
	}
	return b.String()
}

// rewriteSearchQuery uses the inference server to extract search keywords
// from a conversational follow-up. For example, after discussing VMware
// features, the follow-up "what about storage?" yields keywords like
// "VMware vSphere storage vSAN". Falls back to the original query on
// first turn or on error.
func rewriteSearchQuery(
	client openai.Client,
	model string,
	messages []openai.ChatCompletionMessageParamUnion,
	query string,
	verbose bool,
) string {
	conversationCtx := formatConversationForRewrite(messages, maxRewriteTurns)

	if verbose {
		fmt.Printf("Extracting search keywords from conversation context\n")
	}

	stopProgress := common.StartProgressSpinner("Extracting lexical keywords")
	defer stopProgress()

	resp, err := client.Chat.Completions.New(context.Background(), openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(
				"Extract search keywords from the conversation context and the latest question. " +
					"Include relevant product names, technical terms, and topics from the conversation. " +
					"Output only space-separated keywords. No explanation, no punctuation.",
			),
			openai.UserMessage(conversationCtx + "Question: " + query),
		},
		Model:               model,
		MaxCompletionTokens: openai.Int(int64(maxRewriteTokens)),
		MaxTokens:           openai.Int(int64(maxRewriteTokens)),
	})
	if err != nil {
		if verbose {
			fmt.Printf("Keyword extraction failed: %v\n", err)
		}
		return query
	}

	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
		return query
	}

	keywords := stripThinkTags(resp.Choices[0].Message.Content)
	keywords = strings.TrimSpace(keywords)
	if keywords == "" {
		return query
	}
	if verbose {
		fmt.Printf("Search keywords: %s\n", keywords)
	}
	return keywords
}

// stripThinkTags removes <think>...</think> reasoning blocks that
// reasoning models (e.g. DeepSeek R1) emit before their actual response.
func stripThinkTags(s string) string {
	for {
		start := strings.Index(s, "<think>")
		if start == -1 {
			return s
		}
		end := strings.Index(s, "</think>")
		if end == -1 {
			// Unclosed <think> — drop everything from the tag onward.
			return s[:start]
		}
		s = s[:start] + s[end+len("</think>"):]
	}
}

// conversationMessage is used to extract role and content from the
// ChatCompletionMessageParamUnion discriminated union via JSON round-tripping.
type conversationMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// formatConversationForRewrite extracts the last maxTurns user-assistant
// pairs from the message history and formats a compact context string.
// Think-tag reasoning is stripped from assistant responses and long
// responses are truncated to keep the prompt small.
// Returns an empty string when there are no prior user messages.
func formatConversationForRewrite(messages []openai.ChatCompletionMessageParamUnion, maxTurns int) string {
	var turns []conversationMessage
	for _, msg := range messages {
		data, err := json.Marshal(msg)
		if err != nil {
			continue
		}
		var cm conversationMessage
		if err := json.Unmarshal(data, &cm); err != nil {
			continue
		}
		if cm.Role != "user" && cm.Role != "assistant" {
			continue
		}
		// Strip reasoning blocks from assistant responses.
		if cm.Role == "assistant" {
			cm.Content = stripThinkTags(cm.Content)
			cm.Content = strings.TrimSpace(cm.Content)
		}
		if cm.Content == "" {
			continue
		}
		turns = append(turns, cm)
	}

	if len(turns) == 0 {
		return ""
	}

	// Keep last maxTurns user-assistant pairs.
	if len(turns) > maxTurns*2 {
		turns = turns[len(turns)-maxTurns*2:]
	}

	var b strings.Builder
	for _, t := range turns {
		content := t.Content
		if t.Role == "assistant" && len(content) > maxAssistantLength {
			content = content[:maxAssistantLength] + "..."
		}
		fmt.Fprintf(&b, "%s: %s\n", t.Role, content)
	}
	return b.String()
}

// buildRAGPrompt wraps the user's original prompt with the retrieved
// context so the LLM can ground its answer.
func buildRAGPrompt(ragContext, prompt string) string {
	return fmt.Sprintf(`Use the following context to answer the question. If the context is not relevant, answer based on your own knowledge.

Context:
%s

Question: %s`, ragContext, prompt)
}
