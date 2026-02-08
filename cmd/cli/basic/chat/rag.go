package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
)

const defaultRAGTopK = 5

// retrieveContext searches the active knowledge base indexes for content
// relevant to query. Returns an empty string when RAG is unavailable or
// the search yields no results, allowing the caller to fall back to a
// plain prompt.
func retrieveContext(session *Session, query string, verbose bool) string {
	if session.KnowledgeClient == nil || len(session.ActiveIndexes) == 0 || session.EmbeddingModelID == "" {
		return ""
	}

	hits, err := session.KnowledgeClient.Search(
		context.Background(),
		session.ActiveIndexes,
		query,
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

// buildRAGPrompt wraps the user's original prompt with the retrieved
// context so the LLM can ground its answer.
func buildRAGPrompt(ragContext, prompt string) string {
	return fmt.Sprintf(`Use the following context to answer the question. If the context is not relevant, answer based on your own knowledge.

Context:
%s

Question: %s`, ragContext, prompt)
}
