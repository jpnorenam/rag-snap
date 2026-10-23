package processing

import (
	"strings"
	"testing"
)

// No chunk may exceed the size limit by more than the overlap, whatever shape
// the table takes: a one-line "table" (what a flattened document looked like)
// or a table with a row too long to fit beside its header.
func TestChunkMarkdownTablesRespectSize(t *testing.T) {
	opts := ChunkOptions{Size: DefaultChunkSize, Overlap: DefaultChunkOverlap}
	limit := opts.Size + opts.Overlap + 1

	tests := []struct {
		name string
		text string
	}{
		{"single-line table", "| a | b | " + strings.Repeat("word ", 2000)},
		{"oversized row", "| h1 | h2 |\n|---|---|\n| short | row |\n| " + strings.Repeat("long cell text ", 300) + "| x |\n| last | row |"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks := ChunkMarkdown(tt.text, "s", opts)
			if len(chunks) < 2 {
				t.Fatalf("got %d chunks, want the table split", len(chunks))
			}
			for i, c := range chunks {
				if len(c.Content) > limit {
					t.Errorf("chunk %d is %d bytes, limit %d", i, len(c.Content), limit)
				}
			}
		})
	}
}

// Rows of a split table each carry the header so a chunk stays readable alone.
func TestChunkMarkdownOversizedRowKeepsHeader(t *testing.T) {
	text := "| h1 | h2 |\n|---|---|\n| " + strings.Repeat("long cell text ", 300) + "| x |"
	for i, c := range ChunkMarkdown(text, "s", ChunkOptions{Size: DefaultChunkSize, Overlap: DefaultChunkOverlap}) {
		if !strings.HasPrefix(c.Content, "| h1 | h2 |\n|---|---|\n") {
			t.Errorf("chunk %d is missing the table header: %.40q", i, c.Content)
		}
	}
}
