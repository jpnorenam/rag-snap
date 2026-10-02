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
var searchUsage = fmt.Sprintf("Usage: /search [-k N] <query>\n"+
	"  Retrieve matching chunks from the active knowledge bases and kapa.ai source groups\n"+
	"  (no answer is generated). Select sources with /use-knowledge and /use-kapa.\n"+
	"  -k N   maximum number of results (default: %d)", defaultRAGTopK)

// handleSearch implements the /search slash command: a retrieval-only query
// against the active knowledge bases and kapa.ai source groups. It runs the same hybrid pipeline as the
// RAG loop but performs no query rewriting, no augmentation, and no LLM
// generation — it simply prints the matching chunks with their metadata.
func handleSearch(args string, session *Session) {
	k, terms, ok := parseSearchArgs(args)
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
	hits, kapaErr, err := knowledge.SearchWithKapa(context.Background(), session.KnowledgeClient, indexes,
		terms, terms, session.EmbeddingModelID, k, session.KapaClient, session.ActiveKapaGroups)
	if err != nil {
		fmt.Printf("Search failed: %v\n", err)
		return
	}
	if kapaErr != nil {
		fmt.Printf("Warning: kapa.ai search failed, showing local results only: %v\n", kapaErr)
	}

	if len(hits) == 0 {
		fmt.Println("No results found.")
		return
	}

	fmt.Print(formatSearchResults(hits))
}

// parseSearchArgs extracts an optional "-k N" flag and the remaining query
// terms from a /search argument string. Returns ok=false when the query is
// empty or when -k is missing/non-positive/non-integer.
func parseSearchArgs(args string) (k int, terms string, ok bool) {
	k = defaultRAGTopK

	fields := strings.Fields(args)
	var queryTokens []string
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		switch {
		case f == "-k":
			// Value is the next token.
			if i+1 >= len(fields) {
				return 0, "", false
			}
			n, err := strconv.Atoi(fields[i+1])
			if err != nil || n <= 0 {
				return 0, "", false
			}
			k = n
			i++ // consume the value
		case strings.HasPrefix(f, "-k="):
			n, err := strconv.Atoi(strings.TrimPrefix(f, "-k="))
			if err != nil || n <= 0 {
				return 0, "", false
			}
			k = n
		default:
			queryTokens = append(queryTokens, f)
		}
	}

	terms = strings.Join(queryTokens, " ")
	if terms == "" {
		return 0, "", false
	}
	return k, terms, true
}

// formatSearchResults renders search hits for human reading. Unlike
// formatContext (which is tuned for LLM injection), this leads with provenance
// metadata and prints the full, untruncated chunk content. Hits are already
// sorted by score descending by Search.
func formatSearchResults(hits []knowledge.SearchHit) string {
	var b strings.Builder
	for i, hit := range hits {
		if i > 0 {
			b.WriteString("\n")
		}

		name, err := knowledge.KnowledgeBaseNameFromIndex(hit.Index)
		if err != nil {
			name = hit.Index
		}

		header := fmt.Sprintf("[%d] score %.4f  ·  %s  %s", i+1, hit.Score, name, knowledge.LabelTag(hit.Label))
		fmt.Fprintln(&b, color.New(color.Bold).Sprint(header))
		fmt.Fprintf(&b, "    source: %s   created: %s\n", hit.SourceID, hit.CreatedAt)
		fmt.Fprintln(&b, color.HiBlackString("    "+strings.Repeat("─", 56)))
		b.WriteString(hit.Content)
		b.WriteString("\n")
	}
	return b.String()
}
