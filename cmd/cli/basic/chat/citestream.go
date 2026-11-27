package chat

import (
	"regexp"
	"strings"
)

// citationStream renumbers an answer's citation markers while it streams, so
// every surface shows the numbers the saved answer will carry (see
// citeAnswer). It wraps a StreamFunc: answer text is held back while it could
// still change meaning — a marker run that may continue ("[1", "[1][", a
// trailing space before a possible marker) or an inline code span not yet
// closed — and the rest is passed on rewritten. Reasoning and warning tokens
// pass through untouched.
//
// Released text is computed by running citeAnswer over everything received up
// to the hold point, so the stream and the final answer are the same function
// of the same text: citeAnswer is prefix-stable as long as the hold point
// never falls inside a marker run or an open code span, which is what
// holdPoint guarantees.
type citationStream struct {
	retrieved []CitedSource
	emit      StreamFunc
	// onCited, when set, receives the sources cited so far each time the
	// answer cites a new document, before the text carrying its marker.
	onCited func([]CitedSource) error

	raw     strings.Builder
	emitted int
	nCited  int
}

func newCitationStream(retrieved []CitedSource, emit StreamFunc, onCited func([]CitedSource) error) *citationStream {
	return &citationStream{retrieved: retrieved, emit: emit, onCited: onCited}
}

// Emit is the StreamFunc to hand the backend.
func (s *citationStream) Emit(kind TokenKind, content string) error {
	if kind == TokenThink {
		// A delta closing a reasoning block can carry the start of the answer
		// with it; only the reasoning part passes through as reasoning.
		if i := strings.Index(content, "</think>"); i >= 0 {
			head, tail := content[:i+len("</think>")], content[i+len("</think>"):]
			if err := s.emit(TokenThink, head); err != nil {
				return err
			}
			if tail == "" {
				return nil
			}
			return s.answer(tail, false)
		}
	}
	if kind != TokenAnswer {
		return s.emit(kind, content)
	}
	return s.answer(content, false)
}

// Flush releases whatever answer text is still held back. Call it once the
// stream has ended.
func (s *citationStream) Flush() error {
	return s.answer("", true)
}

func (s *citationStream) answer(delta string, final bool) error {
	s.raw.WriteString(delta)
	raw := s.raw.String()
	cut := len(raw)
	if !final {
		cut = holdPoint(raw)
	}
	out, cited, _ := citeAnswer(raw[:cut], s.retrieved)
	if len(cited) > s.nCited {
		s.nCited = len(cited)
		if s.onCited != nil {
			if err := s.onCited(cited); err != nil {
				return err
			}
		}
	}
	if len(out) <= s.emitted {
		return nil
	}
	next := out[s.emitted:]
	s.emitted = len(out)
	return s.emit(TokenAnswer, next)
}

// pendingRun matches the end of text that may still grow into, or extend, a
// citation run: horizontal whitespace, complete groups, and a group not yet
// closed.
var pendingRun = regexp.MustCompile(`[ \t]*(?:\[\s*[Ss]?\d{1,4}(?:\s*[,;]\s*[Ss]?\d{1,4})*\s*\][ \t]*)*(?:\[[\s\dSs,;]{0,40})?$`)

// holdPoint returns how much of raw can be released: everything before a
// trailing pending marker run, and before an inline code span still open on
// the last line (its closing backtick would turn markers into code).
func holdPoint(raw string) int {
	cut := len(raw)
	if loc := pendingRun.FindStringIndex(raw); loc != nil {
		cut = loc[0]
	}
	lineStart := strings.LastIndexByte(raw[:cut], '\n') + 1
	if line := raw[lineStart:cut]; strings.Count(line, "`")%2 == 1 {
		cut = lineStart + strings.LastIndexByte(line, '`')
	}
	return cut
}
