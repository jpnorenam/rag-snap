package chat

import (
	"regexp"
	"strings"
	"unicode"
)

// markdownPatterns are the markdown constructs an RFP answer must not carry: pasted
// into a spreadsheet cell they show as literal characters. Single-asterisk italics are
// deliberately absent — a lone "*" is too often a footnote mark or a multiplication.
var markdownPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{"bold", regexp.MustCompile(`\*\*[^*\n]+\*\*|__[^_\n]+__`)},
	{"backticks", regexp.MustCompile("`")},
	{"heading", regexp.MustCompile(`(?m)^\s{0,3}#{1,6}\s`)},
	{"bullet list", regexp.MustCompile(`(?m)^\s*[-*+•]\s+\S`)},
	{"numbered list", regexp.MustCompile(`(?m)^\s*\d+[.)]\s+\S`)},
}

// markdownFindings names each markdown construct present in s.
func markdownFindings(s string) []string {
	var found []string
	for _, p := range markdownPatterns {
		if p.re.MatchString(s) {
			found = append(found, p.name)
		}
	}
	return found
}

// maxOpenerWords bounds what counts as an opener: "Supported.", "Not supported.",
// "Within scope.", "No hidden licensing." A longer first sentence is the answer.
const maxOpenerWords = 3

// splitOpener separates a short leading declaration from the rest of the answer, so
// a sentence limit is measured on the answer proper. It returns "" and the whole
// answer when there is no such opener.
func splitOpener(s string) (opener, rest string) {
	t := strings.TrimSpace(s)
	end := strings.IndexAny(t, ".!?")
	if end < 0 || end+1 >= len(t) || !unicode.IsSpace(rune(t[end+1])) {
		return "", s
	}
	first := t[:end+1]
	if n := countWords(first); n == 0 || n > maxOpenerWords {
		return "", s
	}
	return first, strings.TrimSpace(t[end+1:])
}

// countWords counts whitespace-separated words.
func countWords(s string) int { return len(strings.Fields(s)) }

// countSentences counts sentence-ending punctuation followed by whitespace or the end
// of the text, so a version such as "1.32" or a decimal does not end a sentence. Text
// with words but no terminator is one sentence. Abbreviations ("e.g. ") overcount; the
// figure is an approximation the judge is told it is, not a measurement.
func countSentences(s string) int {
	rs := []rune(strings.TrimSpace(s))
	n := 0
	for i, r := range rs {
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		// Only the last mark of a run such as "..." or "?!" is followed by a space,
		// so a run counts once.
		if i+1 == len(rs) || unicode.IsSpace(rs[i+1]) {
			n++
		}
	}
	if n == 0 && countWords(s) > 0 {
		return 1
	}
	return n
}
