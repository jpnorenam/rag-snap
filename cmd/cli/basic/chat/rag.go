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
	maxRewriteTokens   = 256
	maxAssistantLength = 400
)

// extractedKeywords holds the two-stage extraction result.
type extractedKeywords struct {
	Anchors   []string `json:"anchors"`
	Expansion []string `json:"expansion"`
}

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

// sourceLabel returns a provenance tag for a search hit based on its index name.
// KB names containing "upstream" are tagged [UPSTREAM]; all others are [CANONICAL].
// Convention: when ingesting open-source / third-party documentation, the KB name
// must include "upstream" (e.g. "openstack-upstream", "kubernetes-upstream").
// The LLM uses these tags to enforce source priority rules in its answer.
func sourceLabel(indexName string) string {
	if strings.Contains(strings.ToLower(indexName), "upstream") {
		return "[UPSTREAM]"
	}
	return "[CANONICAL]"
}

// formatContext renders a slice of search hits into a single text block
// suitable for injection into a RAG prompt. Each chunk is prefixed with a
// provenance label ([CANONICAL] or [UPSTREAM]) so the LLM can apply source
// priority rules when formulating its answer.
func formatContext(hits []knowledge.SearchHit) string {
	var b strings.Builder
	for i, hit := range hits {
		if i > 0 {
			b.WriteString("\n---\n")
		}
		fmt.Fprintf(&b, "%s\n", sourceLabel(hit.Index))
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

	resp, err := client.Chat.Completions.New(context.Background(), openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(
				"You are a RAG query optimizer. Given a conversation and a follow-up question, output a JSON object with two fields:\n" +
					"- \"anchors\": verbatim technical terms, product names, and proper nouns from the text\n" +
					"- \"expansion\": closely related terms implied by context (abbreviations, synonyms, parent concepts)\n" +
					"Rules: expansion must be inferable from the conversation domain, not generic.\n" +
					"Output only valid JSON, no explanation.",
			),
			openai.UserMessage(conversationCtx + "Question: " + query),
		},
		Model:               model,
		MaxCompletionTokens: openai.Int(int64(maxRewriteTokens)),
		MaxTokens:           openai.Int(int64(maxRewriteTokens)),
	})
	// Stop spinner before any further output to avoid interleaving with verbose prints.
	stopProgress()
	if err != nil {
		if verbose {
			fmt.Printf("Keyword extraction failed: %v\n", err)
		}
		return query
	}

	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
		return query
	}

	raw := strings.TrimSpace(stripThinkTags(resp.Choices[0].Message.Content))
	if raw == "" {
		return query
	}

	// TrimSpace first so a trailing newline before ``` does not prevent fence removal.
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var kw extractedKeywords
	if err := json.Unmarshal([]byte(raw), &kw); err != nil {
		// Always fall back to the original query — never pass raw LLM text
		// (which may be an error message or truncated JSON) as a BM25 query.
		if verbose {
			fmt.Printf("Keyword JSON parse failed (%v), falling back to original query\n", err)
		}
		return query
	}

	// Build combined slice explicitly to avoid mutating kw.Anchors' backing array.
	all := make([]string, 0, len(kw.Anchors)+len(kw.Expansion))
	all = append(all, kw.Anchors...)
	all = append(all, kw.Expansion...)
	result := strings.Join(all, " ")
	if result == "" {
		return query
	}

	if verbose {
		fmt.Printf("Search keywords — anchors: %v | expansion: %v\n", kw.Anchors, kw.Expansion)
	}
	return result
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
	totalTurns := len(turns)
	for i, t := range turns {
		// Give the LLM explicit recency signal
		age := totalTurns - i // 1 = most recent
		label := fmt.Sprintf("[turn-%d, age=%d]", i+1, age)
		content := t.Content
		if t.Role == "assistant" && len(content) > maxAssistantLength {
			content = content[:maxAssistantLength] + "..."
		}
		fmt.Fprintf(&b, "%s %s: %s\n", label, t.Role, content)
	}
	return b.String()
}

// ragSourceRules is the non-negotiable source-grounding block appended to any
// custom manifest prompt to ensure [CANONICAL]/[UPSTREAM] rules are always active.
const ragSourceRules = "Source rules (mandatory, override any prior instruction):\n" +
	"- Context chunks are tagged [CANONICAL] or [UPSTREAM]. [CANONICAL] is the sole authoritative source.\n" +
	"- Only name a product or component if a [CANONICAL] chunk explicitly documents it. Do NOT name anything found only in [UPSTREAM] chunks.\n" +
	"- If the question names a product as an example, do not repeat or endorse it unless a [CANONICAL] chunk confirms it.\n" +
	"- Never speculate or use knowledge outside the provided context."

// ragAnswerSystemPrompt is the system-level instruction for batch answer (rag answer batch).
// Optimized for terse, structured Q&A — every word must earn its place.
const ragAnswerSystemPrompt = "You are a technical answer engine. Apply these rules strictly:\n" +
	"1. GROUNDING: Use ONLY information explicitly stated in the provided context. Never infer, extrapolate, or use outside knowledge.\n" +
	"2. SOURCE PRIORITY: Each context chunk is tagged [CANONICAL] or [UPSTREAM]. [CANONICAL] is the authoritative source. [UPSTREAM] chunks provide supplemental detail only — when [CANONICAL] and [UPSTREAM] address the same point, follow [CANONICAL] exclusively.\n" +
	"3. PRODUCTS: Only name a product or component if a [CANONICAL] chunk explicitly documents or endorses it. " +
	"Do NOT name any product found only in [UPSTREAM] chunks — not even as background context or an example. " +
	"If the question itself names a product as an example, do NOT repeat or endorse it unless a [CANONICAL] chunk explicitly confirms it. " +
	"Never mention proprietary third-party products.\n" +
	"4. FORMAT: Answer in 1–3 sentences. Use bullet points only when listing multiple distinct items. No preamble, no filler, no 'Based on the context…'.\n" +
	"5. NO ANSWER: If the context does not contain enough information, reply exactly: " +
	"\"The provided context does not contain enough information to answer this question.\""

// ragChatSystemPrompt is the system-level instruction for the interactive chat REPL (rag chat).
// Grounded and conversational — follows the same strict accuracy rules with natural phrasing.
const ragChatSystemPrompt = "You are a Canonical technical assistant. Apply these rules strictly:\n" +
	"1. GROUNDING: Use ONLY information explicitly stated in the provided context. Never infer, extrapolate, or use outside knowledge.\n" +
	"2. SOURCE PRIORITY: Each context chunk is tagged [CANONICAL] or [UPSTREAM]. When they conflict or overlap, [CANONICAL] always takes precedence.\n" +
	"3. PRODUCTS: Only name a product or component if a [CANONICAL] chunk explicitly documents or endorses it. " +
	"Do NOT name any product found only in [UPSTREAM] chunks — not even as background context or an example. " +
	"Never mention proprietary third-party products.\n" +
	"4. FORMAT: Be concise and direct. Use bullet points when listing multiple items. You may ask a clarifying question if the query is ambiguous.\n" +
	"5. NO ANSWER: If the context does not contain enough information, say so plainly and do not speculate."

// buildRAGPrompt wraps the user's original prompt with the retrieved
// context so the LLM can ground its answer.
func buildRAGPrompt(ragContext, prompt string) string {
	return fmt.Sprintf("Context:\n%s\n\nQuestion: %s", ragContext, prompt)
}
