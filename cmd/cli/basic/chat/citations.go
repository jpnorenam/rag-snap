package chat

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
)

// CitedSource is a cited document as presented to a user: the marker number
// it carries in the answer (the n of "[n]") plus its resolved citation. It is
// the wire shape of a source in websocket frames, batch results, and (mirrored
// by chatstore.Source) saved chats.
type CitedSource struct {
	Marker int `json:"marker"`
	knowledge.Citation
}

// CitationRegistry assigns marker numbers to cited documents. A document keeps
// its number for the registry's lifetime: a chat session holds one registry so
// "[1]" means the same document in every turn and in saved history; a batch run
// uses a fresh registry per question.
type CitationRegistry struct {
	byKey map[string]int
	next  int
}

// NewCitationRegistry returns an empty registry numbering from 1.
func NewCitationRegistry() *CitationRegistry {
	return &CitationRegistry{byKey: make(map[string]int), next: 1}
}

// Number returns key's marker number, assigning the next unused one on first
// sight.
func (r *CitationRegistry) Number(key string) int {
	if n, ok := r.byKey[key]; ok {
		return n
	}
	n := r.next
	r.byKey[key] = n
	r.next++
	return n
}

// Seed restores numbering from previously cited sources (a resumed chat), so a
// re-retrieved document keeps its number and a new one numbers after the
// highest number seen.
func (r *CitationRegistry) Seed(sources []CitedSource) {
	for _, s := range sources {
		if s.Key == "" || s.Marker <= 0 {
			continue
		}
		r.byKey[s.Key] = s.Marker
		if s.Marker >= r.next {
			r.next = s.Marker + 1
		}
	}
}

// citationKey is the identity a hit is numbered under; the source id is the
// fallback for a hit whose citation was never resolved.
func citationKey(hit knowledge.SearchHit) string {
	if hit.Citation.Key != "" {
		return hit.Citation.Key
	}
	return hit.SourceID
}

// markerGroup matches one citation group the model may emit: "[1]", and the
// list forms "[1, 2]", "[1,2]", "[1; 2]". An "S" before a number ("[S1]") is
// tolerated, since models (and chats saved before markers were renumbered)
// sometimes write it. Leading horizontal whitespace is captured so a group
// removed entirely does not leave a dangling space.
var markerGroup = regexp.MustCompile(`([ \t]*)\[\s*[Ss]?(\d{1,3})((?:\s*[,;]\s*[Ss]?\d{1,3})*)\s*\]`)

var markerNumber = regexp.MustCompile(`\d+`)

// codeSpan matches fenced code blocks and inline code, whose brackets are code
// ("arr[1]", "[1, 2]"), never citations.
var codeSpan = regexp.MustCompile("(?s)```.*?(?:```|$)|`[^`\n]*`")

// rewriteMarkers calls fn for every citation group in text's prose and splices
// in what it returns. A bracketed number is a citation group only outside code
// and not glued to a preceding word ("arr[1]"). A marker followed by "(…)" is
// still a marker, never link text, so a citation cannot be dressed up as a link
// to an arbitrary URL (the UI renders it the same way).
func rewriteMarkers(text string, fn func(lead string, nums []int) string) string {
	var out strings.Builder
	prose := func(seg string) {
		last := 0
		for _, m := range markerGroup.FindAllStringSubmatchIndex(seg, -1) {
			start, end := m[0], m[1]
			lead := seg[m[2]:m[3]]
			if lead == "" && start > 0 && isWordByte(seg[start-1]) {
				continue
			}
			found := markerNumber.FindAllString(seg[m[4]:m[5]]+" "+seg[m[6]:m[7]], -1)
			nums := make([]int, 0, len(found))
			for _, num := range found {
				n, _ := strconv.Atoi(num)
				nums = append(nums, n)
			}
			out.WriteString(seg[last:start])
			out.WriteString(fn(lead, nums))
			last = end
		}
		out.WriteString(seg[last:])
	}
	last := 0
	for _, c := range codeSpan.FindAllStringIndex(text, -1) {
		prose(text[last:c[0]])
		out.WriteString(text[c[0]:c[1]])
		last = c[1]
	}
	prose(text[last:])
	return out.String()
}

func isWordByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// removeMarkers drops every citation group from text.
func removeMarkers(text string) string {
	return rewriteMarkers(text, func(string, []int) string { return "" })
}

// citeAnswer validates an answer's citation markers against the documents
// retrieved for the turn. Markers for documents not in retrieved are removed,
// list forms are normalized to "[1][2]", and the cited sources are returned in
// first-cited order. uncited reports that context was retrieved but the answer
// cites none of it: the answer is still returned, and the caller tells the
// user it has no sources (chat) or replaces it (batch). A turn without
// retrieval has nothing to cite, so any marker in its answer is invented and
// removed. Reasoning (<think>) content never counts as a citation.
func citeAnswer(answer string, retrieved []CitedSource) (content string, cited []CitedSource, uncited bool) {
	if len(retrieved) == 0 {
		return removeMarkers(answer), nil, false
	}
	byMarker := make(map[int]CitedSource, len(retrieved))
	for _, s := range retrieved {
		byMarker[s.Marker] = s
	}

	normalized := rewriteMarkers(answer, func(lead string, nums []int) string {
		var kept strings.Builder
		for _, n := range nums {
			if _, ok := byMarker[n]; ok {
				fmt.Fprintf(&kept, "[%d]", n)
			}
		}
		if kept.Len() == 0 {
			return ""
		}
		return lead + kept.String()
	})

	seen := make(map[int]bool)
	rewriteMarkers(StripThinkTags(normalized), func(_ string, nums []int) string {
		for _, n := range nums {
			if !seen[n] {
				seen[n] = true
				cited = append(cited, byMarker[n])
			}
		}
		return ""
	})

	return normalized, cited, len(cited) == 0
}

// stripMarkers removes every citation marker from text, for surfaces (batch
// answers) that carry their sources in a separate field.
func stripMarkers(text string) string {
	lines := strings.Split(removeMarkers(text), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// noSourcesNotice is shown under a chat answer that cited none of the
// documents retrieved for it.
const noSourcesNotice = "No relevant sources: this answer cites none of the retrieved documents."

// formatSourcesBlock renders an answer's cited sources for the terminal: one
// dimmed line per source with its marker, label, and link when it has one.
// Empty when there are no sources.
func formatSourcesBlock(sources []CitedSource) string {
	if len(sources) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(color.HiBlackString("Sources") + "\n")
	for _, s := range sources {
		line := fmt.Sprintf("  [%d] %s", s.Marker, s.Label)
		if s.Href != "" {
			line += " — " + s.Href
		}
		b.WriteString(color.HiBlackString(line) + "\n")
	}
	return b.String()
}

// ragCitationRules is the fixed citation instruction sent with retrieved
// context (see buildRAGPrompt) — whatever the system prompt, chat or batch — so
// the answer's markers can be resolved to the documents it relied on. It is
// deliberately not part of the system prompt: a turn without retrieved context
// must not see it, or the model copies its "[1]" example into answers that
// have nothing to cite.
const ragCitationRules = "Citation rules (mandatory, override any prior instruction):\n" +
	"- Each context chunk starts with a source marker such as [1]; chunks from the same document share the same marker.\n" +
	"- End every paragraph that uses information from the context with the marker(s) of the document(s) it relied on, e.g. \"... [1][3]\".\n" +
	"- Use only markers that appear in the context. Never invent a marker, and never cite a document for information it does not contain.\n" +
	"- Do not write a separate reference list; markers are resolved to their sources automatically."
