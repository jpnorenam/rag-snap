package processing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, name string, content []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadTextSourceRouting(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		content []byte
		wantOK  bool
	}{
		{"markdown", "README.md", []byte("# Title\n\nBody\n"), true},
		{"markdown upper-case extension", "NOTES.MD", []byte("# Title\n"), true},
		{"plain text", "LICENSE.txt", []byte("Some licence text\n"), true},
		{"pdf goes to tika", "guide.pdf", []byte("%PDF-1.7"), false},
		{"html goes to tika", "page.html", []byte("<p>hi</p>"), false},
		{"non-UTF-8 markdown goes to tika", "latin1.md", []byte("caf\xe9\n"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok, err := readTextSource(writeTemp(t, tt.file, tt.content))
			if err != nil {
				t.Fatal(err)
			}
			if ok != tt.wantOK {
				t.Errorf("readTextSource(%s) ok = %v, want %v", tt.file, ok, tt.wantOK)
			}
		})
	}
}

func TestReadTextSourceNormalizes(t *testing.T) {
	in := "\xef\xbb\xbf# Title\r\n\r\nText ![][image1] and ![logo](data:image/png;base64,iVBORw0KGgo=) end.\r\n\r\n" +
		"[image1]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUg+/AAA=>\r\n"
	got, ok, err := readTextSource(writeTemp(t, "doc.md", []byte(in)))
	if err != nil || !ok {
		t.Fatalf("readTextSource: ok=%v err=%v", ok, err)
	}
	want := "# Title\n\nText ![][image1] and ![logo]() end.\n\n"
	if got != want {
		t.Errorf("normalized content:\n got %q\nwant %q", got, want)
	}
}

// A Google Docs Markdown export opens with a title table. Read through Tika it
// collapsed to one line and was indexed as a single chunk of the whole file;
// read directly, its structure must survive into normally sized chunks.
func TestTableFirstMarkdownChunksNormally(t *testing.T) {
	var b strings.Builder
	b.WriteString("| Deployment Guide |  |\n| :---- | :---- |\n| Version: | 1.0 |\n\n# Known issues\n\n")
	for i := 0; i < 200; i++ {
		b.WriteString("Paragraph about MicroCloud and MicroCeph deployment steps on the infra nodes.\n\n")
	}
	b.WriteString("[image1]: <data:image/png;base64," + strings.Repeat("QUFB", 50000) + ">\n")

	content, ok, err := readTextSource(writeTemp(t, "guide.md", []byte(b.String())))
	if err != nil || !ok {
		t.Fatalf("readTextSource: ok=%v err=%v", ok, err)
	}
	chunks := ChunkMarkdown(content, "guide", ChunkOptions{Size: DefaultChunkSize, Overlap: DefaultChunkOverlap})
	if len(chunks) < 10 {
		t.Fatalf("got %d chunks, want the document split into many", len(chunks))
	}
	if !strings.HasPrefix(chunks[0].Content, "| Deployment Guide |") {
		t.Errorf("first chunk should be the intact title table, got %.80q", chunks[0].Content)
	}
	for i, c := range chunks {
		if len(c.Content) > DefaultChunkSize+DefaultChunkOverlap+1 {
			t.Errorf("chunk %d is %d bytes, over the size limit", i, len(c.Content))
		}
		if strings.Contains(c.Content, "base64") {
			t.Errorf("chunk %d still contains inline image data", i)
		}
	}
}
