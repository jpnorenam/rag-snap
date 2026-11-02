package processing

import "testing"

func TestIsTempFileName(t *testing.T) {
	for name, want := range map[string]bool{
		"rag-crawl-123456.html":   true,
		"rag-github-42.md":        true,
		"ragd-upload-7.pdf":       true,
		"rag-crawl-notes.html":    false,
		"service-description.pdf": false,
		"":                        false,
	} {
		if got := IsTempFileName(name); got != want {
			t.Errorf("IsTempFileName(%q) = %v, want %v", name, got, want)
		}
	}
}
