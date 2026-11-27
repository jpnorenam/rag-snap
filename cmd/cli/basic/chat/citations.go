package chat

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/internal/citation"
)

// CitedSource is a cited document as presented to a user: the marker number
// it carries in the answer plus its resolved citation (see citation.Source).
type CitedSource = citation.Source

// CitationRegistry numbers the documents in one turn's retrieved context, in
// order of first appearance, so chunks of one document share a context marker.
// Context numbers are what the model sees; answers are renumbered from them in
// first-cited order (see citeAnswer), so they never reach the user.
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

// citationKey is the identity a hit is numbered under: its document key (the
// source id when the citation was never resolved), qualified by the base it
// came from. Kapa.ai hits and sources ingested with --url both use the URL as
// their source id, so without the base a local copy and the kapa.ai page of
// the same URL would share one marker and one citation.
func citationKey(hit knowledge.SearchHit) string {
	key := hit.Citation.Key
	if key == "" {
		key = hit.SourceID
	}
	return registryKey(hit.Citation.Base, key)
}

func registryKey(base, key string) string {
	return base + ":" + key
}

// markerGroup matches one run of citation groups the model may emit: "[1]",
// "[1][3]", and the list forms "[1, 2]", "[1,2]", "[1; 2]". An "S" before a
// number ("[S1]") is tolerated, since models (and chats saved before markers
// were renumbered) sometimes write it. Leading horizontal whitespace is
// captured so a run removed entirely does not leave a dangling space. A run is
// one unit: "[1][7]" with only [1] retrieved is a citation of [1] with an
// invented [7], not a citation plus a literal "[7]".
var markerGroup = regexp.MustCompile(`([ \t]*)((?:\[\s*[Ss]?\d{1,4}(?:\s*[,;]\s*[Ss]?\d{1,4})*\s*\][ \t]*)*\[\s*[Ss]?\d{1,4}(?:\s*[,;]\s*[Ss]?\d{1,4})*\s*\])`)

var markerNumber = regexp.MustCompile(`\d+`)

// codeSpan matches the spans whose brackets are never citations: reasoning
// (<think>) blocks, fenced code blocks, and inline code ("arr[1]", "[1, 2]").
var codeSpan = regexp.MustCompile("(?s)<think>.*?(?:</think>|$)|```.*?(?:```|$)|`[^`\n]*`")

// rewriteMarkers calls fn for every run of citation groups in text's prose and
// splices in what it returns; raw is the run as written, lead its leading
// whitespace. A bracketed number is a citation only outside code, and a marker
// glued to the preceding word counts too ("supports SR-IOV[1].", "version
// 2[3]") unless that word looks like code (see gluedToCode). A marker followed
// by "(…)" is still a marker, never link text, so a citation cannot be dressed
// up as a link to an arbitrary URL (the UI renders it the same way).
func rewriteMarkers(text string, fn func(raw, lead string, nums []int) string) string {
	var out strings.Builder
	prose := func(seg string) {
		last := 0
		for _, m := range markerGroup.FindAllStringSubmatchIndex(seg, -1) {
			start, end := m[0], m[1]
			lead := seg[m[2]:m[3]]
			if lead == "" && gluedToCode(seg[:start]) {
				continue
			}
			found := markerNumber.FindAllString(seg[m[4]:m[5]], -1)
			nums := make([]int, 0, len(found))
			for _, num := range found {
				n, _ := strconv.Atoi(num)
				nums = append(nums, n)
			}
			out.WriteString(seg[last:start])
			out.WriteString(fn(seg[start:end], lead, nums))
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

// gluedToCode reports whether the token a marker is glued to (the text after
// the last whitespace or "]" in before) looks like a code identifier rather
// than a word: it contains "_", a "(" after its first character ("f(x)[1]"),
// or a "." followed by a letter ("obj.items[1]"). Prose such as "SR-IOV",
// "22.04", "sentence." or "(SR-IOV)" does not.
func gluedToCode(before string) bool {
	tok := before[strings.LastIndexAny(before, " \t\n]")+1:]
	for i := 0; i < len(tok); i++ {
		switch c := tok[i]; {
		case c == '_':
			return true
		case c == '(' && i > 0:
			return true
		case c == '.' && i+1 < len(tok) && isLetter(tok[i+1]):
			return true
		}
	}
	return false
}

func isLetter(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// removeMarkers drops every citation group from text.
func removeMarkers(text string) string {
	return rewriteMarkers(text, func(string, string, []int) string { return "" })
}

// citeAnswer validates an answer's citation markers against the documents
// retrieved for the turn and renumbers them for the reader: the first document
// the answer cites becomes [1], the next new one [2], and so on, whatever its
// number in the context the model saw. In a run of markers that cites at least
// one retrieved document, markers for documents not retrieved are dropped and
// list forms are normalized to "[1][2]". A run citing no retrieved document is
// left as written: it may be prose ("option [2]", "Table [12]") rather than a
// citation. The cited sources are returned in answer-marker order (1, 2, …).
// uncited reports that context was retrieved but the answer cites none of it:
// the answer is still returned, and the caller flags it as unsourced. A turn
// without retrieval has nothing to cite, so any marker in its answer is copied
// or invented and removed. Reasoning (<think>) content is left untouched and
// never counts as a citation.
func citeAnswer(answer string, retrieved []CitedSource) (content string, cited []CitedSource, uncited bool) {
	if len(retrieved) == 0 {
		return removeMarkers(answer), nil, false
	}
	byMarker := make(map[int]CitedSource, len(retrieved))
	for _, s := range retrieved {
		byMarker[s.Marker] = s
	}
	local := make(map[int]int)

	content = rewriteMarkers(answer, func(raw, lead string, nums []int) string {
		var kept strings.Builder
		inRun := make(map[int]bool)
		for _, n := range nums {
			src, ok := byMarker[n]
			if !ok {
				continue
			}
			m, seen := local[n]
			if !seen {
				m = len(cited) + 1
				local[n] = m
				cited = append(cited, CitedSource{Marker: m, Citation: src.Citation})
			}
			if !inRun[m] {
				inRun[m] = true
				fmt.Fprintf(&kept, "[%d]", m)
			}
		}
		if kept.Len() == 0 {
			return raw
		}
		return lead + kept.String()
	})
	return content, cited, len(cited) == 0
}

// stripMarkers removes the citation markers that cite one of sources from
// text, for surfaces (batch answers) that carry their sources in a separate
// field. Bracketed numbers that cite nothing ("see Table [12]") are prose and
// stay, and only the whitespace a removed marker leaves behind is trimmed, so
// Markdown hard line breaks survive.
func stripMarkers(text string, sources []CitedSource) string {
	valid := make(map[int]bool, len(sources))
	for _, s := range sources {
		valid[s.Marker] = true
	}
	stripped := rewriteMarkers(text, func(raw, _ string, nums []int) string {
		for _, n := range nums {
			if valid[n] {
				return ""
			}
		}
		return raw
	})
	return strings.TrimSpace(stripped)
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
