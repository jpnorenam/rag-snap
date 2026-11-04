package chat

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
)

// searchUsage is printed when /search is invoked with missing or invalid args.
var searchUsage = fmt.Sprintf("Usage: /search [-k N] [%[2]s] <query>\n"+
	"  Retrieve matching chunks from the active knowledge bases (no answer is generated).\n"+
	"  -k N            maximum number of results, in total across bases (default: %[1]d)\n"+
	"  %-14[2]s  also show similar results that don't match the query", defaultRAGTopK, knowledge.ShowSimilarFlag)

// searchArgs is a parsed /search argument string.
type searchArgs struct {
	k           int
	showSimilar bool
	terms       string
}

// handleSearch implements the /search slash command: a retrieval-only query
// against the active knowledge bases. It runs the same hybrid pipeline as the
// RAG loop but performs no query rewriting, no augmentation, and no LLM
// generation — it simply prints the matching chunks with their metadata.
func handleSearch(args string, session *Session) {
	parsed, ok := parseSearchArgs(args)
	if !ok {
		fmt.Println(searchUsage)
		return
	}

	// Preconditions mirror retrieveContext: without a client, active indexes,
	// and an embedding model, the hybrid pipeline cannot run.
	if session.KnowledgeClient == nil || session.EmbeddingModelID == "" {
		fmt.Println("Knowledge retrieval is unavailable for this session.")
		return
	}
	if len(session.ActiveIndexes) == 0 {
		fmt.Printf("No active knowledge bases. Select one with %s first.\n", cmdUseKnowledge)
		return
	}

	// Verbatim terms for both the lexical (BM25) and neural/rerank query —
	// no rewriteSearchQuery, so no inference-server round-trip.
	hits, err := session.KnowledgeClient.SearchTop(
		context.Background(),
		session.ActiveIndexes,
		parsed.terms,
		parsed.terms,
		session.EmbeddingModelID,
		parsed.k,
	)
	if err != nil {
		fmt.Printf("Search failed: %v\n", err)
		return
	}

	fmt.Print(formatSearchOutput(parsed.terms, directSearchHits(hits), parsed.showSimilar))
}

// searchHit is one /search result as both modes render it. Direct-mode and
// daemon hits are converted to it, so the two modes share one renderer and
// cannot drift apart.
type searchHit struct {
	Score     float64
	Base      string
	Label     string
	SourceID  string
	CreatedAt string
	Content   string
	Matched   bool
}

// directSearchHits converts direct-mode hits, resolving each base name from its index.
func directSearchHits(hits []knowledge.SearchHit) []searchHit {
	out := make([]searchHit, len(hits))
	for i, h := range hits {
		name, err := knowledge.KnowledgeBaseNameFromIndex(h.Index)
		if err != nil {
			name = h.Index
		}
		out[i] = searchHit{Score: h.Score, Base: name, Label: h.Label, SourceID: h.SourceID,
			CreatedAt: h.CreatedAt, Content: h.Content, Matched: h.Matched}
	}
	return out
}

// formatSearchOutput renders /search output: matched hits, then similar
// results hidden behind a notice or, with showSimilar, listed under a separator.
func formatSearchOutput(terms string, hits []searchHit, showSimilar bool) string {
	matched, similar := knowledge.SplitByMatch(hits, func(h searchHit) bool { return h.Matched })
	return knowledge.FormatSplit(terms,
		formatSearchResults(matched, 0), formatSearchResults(similar, len(matched)),
		len(matched), len(similar), showSimilar)
}

// parseSearchArgs extracts an optional "-k N" flag, an optional
// --show-similar flag, and the remaining query terms from a /search argument
// string. Flags may appear anywhere among the terms. Returns ok=false when the
// query is empty or when -k is missing/non-positive/non-integer.
func parseSearchArgs(args string) (parsed searchArgs, ok bool) {
	k := defaultRAGTopK
	showSimilar := false

	fields := strings.Fields(args)
	var queryTokens []string
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		switch {
		case f == "-k":
			// Value is the next token.
			if i+1 >= len(fields) {
				return searchArgs{}, false
			}
			n, err := strconv.Atoi(fields[i+1])
			if err != nil || n <= 0 {
				return searchArgs{}, false
			}
			k = n
			i++ // consume the value
		case strings.HasPrefix(f, "-k="):
			n, err := strconv.Atoi(strings.TrimPrefix(f, "-k="))
			if err != nil || n <= 0 {
				return searchArgs{}, false
			}
			k = n
		case f == knowledge.ShowSimilarFlag:
			showSimilar = true
		case strings.HasPrefix(f, knowledge.ShowSimilarFlag+"="):
			// The flag takes no value; a form like --show-similar=true is a
			// usage error rather than query text that skews the search.
			return searchArgs{}, false
		default:
			queryTokens = append(queryTokens, f)
		}
	}

	terms := strings.Join(queryTokens, " ")
	if terms == "" {
		return searchArgs{}, false
	}
	return searchArgs{k: k, showSimilar: showSimilar, terms: terms}, true
}

// formatSearchResults renders search hits for human reading. Unlike
// formatContext (which is tuned for LLM injection), this leads with provenance
// metadata and prints the full, untruncated chunk content. Hits are already
// sorted by score descending by Search. Numbering starts at start+1, so the
// similar results continue from the matched ones.
func formatSearchResults(hits []searchHit, start int) string {
	var b strings.Builder
	for i, hit := range hits {
		if i > 0 {
			b.WriteString("\n")
		}

		header := fmt.Sprintf("[%d] score %.4f  ·  %s  %s", start+i+1, hit.Score, hit.Base, knowledge.LabelTag(hit.Label))
		fmt.Fprintln(&b, color.New(color.Bold).Sprint(header))
		fmt.Fprintf(&b, "    source: %s   created: %s\n", hit.SourceID, hit.CreatedAt)
		fmt.Fprintln(&b, color.HiBlackString("    "+strings.Repeat("─", 56)))
		b.WriteString(hit.Content)
		b.WriteString("\n")
	}
	return b.String()
}
