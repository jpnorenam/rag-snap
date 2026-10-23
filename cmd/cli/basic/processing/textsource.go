package processing

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// textSourceExtensions are the file types read directly instead of through Tika.
// Tika treats these as plain text and wraps the whole file in a single <p>, so
// the HTML-to-Markdown pass collapses every newline and the structure-aware
// chunker sees one line: headings, tables, lists and code blocks are lost, and
// a file whose first line is a table becomes a single oversized chunk.
var textSourceExtensions = map[string]bool{
	".md":       true,
	".markdown": true,
	".mdx":      true,
	".txt":      true,
}

var (
	// dataURIPattern matches a base64 data URI, as Google Docs and other
	// exporters inline images into Markdown.
	dataURIPattern = regexp.MustCompile(`data:[\w.+-]+/[\w.+-]+;base64,[A-Za-z0-9+/=]+`)
	// emptyRefDefPattern matches a reference-style link definition left empty
	// once its data URI is removed, e.g. "[image1]: <>".
	emptyRefDefPattern = regexp.MustCompile(`(?m)^[ \t]*\[[^\]\n]+\]:[ \t]*(<>)?[ \t]*$\n?`)
)

// readTextSource returns the content of a Markdown or plain-text file ready for
// ChunkMarkdown, or ok=false when the file should go through Tika instead: a
// different extension, or bytes that are not valid UTF-8 (Tika detects the
// encoding).
func readTextSource(filePath string) (content string, ok bool, err error) {
	if !textSourceExtensions[strings.ToLower(filepath.Ext(filePath))] {
		return "", false, nil
	}

	raw, err := os.ReadFile(filePath)
	if err != nil {
		return "", false, fmt.Errorf("reading %s: %w", filepath.Base(filePath), err)
	}
	if !utf8.Valid(raw) {
		return "", false, nil
	}

	return normalizeText(string(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")))), true, nil
}

// normalizeText converts line endings to "\n" (the chunker splits on "\n\n")
// and strips inline base64 images, which carry no text and would otherwise be
// indexed as hundreds of chunks of noise.
func normalizeText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = dataURIPattern.ReplaceAllString(s, "")
	s = emptyRefDefPattern.ReplaceAllString(s, "")
	return s
}
