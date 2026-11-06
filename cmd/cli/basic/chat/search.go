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
	"  Retrieve matching chunks from the active knowledge bases and kapa.ai source groups\n"+
	"  (no answer is generated). Select sources with /use-knowledge and /use-kapa.\n"+
	"  -k N            maximum number of results, in total across bases (default: %[1]d);\n"+
	"                  kapa.ai adds up to as many again\n"+
	"  %-14[2]s  also show similar results that don't match the query", defaultRAGTopK, knowledge.ShowSimilarFlag)

// searchArgs is a parsed /search argument string.
type searchArgs struct {
	k           int
	showSimilar bool
	terms       string
}

// handleSearch implements the /search slash command: a retrieval-only query
// against the active knowledge bases and kapa.ai source groups. It runs the same hybrid pipeline as the
// RAG loop but performs no query rewriting, no augmentation, and no LLM
// generation — it simply prints the matching chunks with their metadata.
func handleSearch(args string, session *Session) {
	parsed, ok := parseSearchArgs(args)
	if !ok {
		fmt.Println(searchUsage)
		return
	}

	// Preconditions mirror retrieveContext: a source of either kind must be
	// active, and local bases additionally need a client and embedding model
	// for the hybrid pipeline. kapa.ai alone needs neither.
	hasLocal := len(session.ActiveIndexes) > 0
	hasKapa := session.KapaClient != nil && len(session.ActiveKapaGroups) > 0
	if !hasLocal && !hasKapa {
		fmt.Printf("No active knowledge sources. Select some with %s or %s first.\n", cmdUseKnowledge, cmdUseKapa)
		return
	}
	if hasLocal && (session.KnowledgeClient == nil || session.EmbeddingModelID == "") {
		if !hasKapa {
			fmt.Println("Knowledge retrieval is unavailable for this session.")
			return
		}
		fmt.Println("Warning: knowledge retrieval is unavailable for this session; searching kapa.ai only.")
		hasLocal = false
	}

	// Verbatim terms for both the lexical (BM25) and neural/rerank query —
	// no rewriteSearchQuery, so no inference-server round-trip. Local and
	// kapa.ai run concurrently; local hits come first, as in the RAG loop.
	var indexes []string
	if hasLocal {
		indexes = session.ActiveIndexes
	}
	res := knowledge.SearchWithKapa(context.Background(), session.KnowledgeClient, indexes,
		parsed.terms, parsed.terms, session.EmbeddingModelID, parsed.k, session.KapaClient, session.ActiveKapaGroups)
	if err := res.Err(); err != nil {
		fmt.Printf("Search failed: %v\n", err)
		return
	}
	if res.LocalErr != nil {
		fmt.Printf("Warning: local knowledge search failed, showing kapa.ai results only: %v\n", res.LocalErr)
	}
	if res.KapaErr != nil {
		fmt.Printf("Warning: kapa.ai search failed, showing local results only: %v\n", res.KapaErr)
	}
	hits := res.Hits

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
		switch {
		case h.Index == knowledge.KapaIndexName:
			// Same base name the daemon reports for kapa.ai hits.
			name = "kapa.ai"
		case err != nil:
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
