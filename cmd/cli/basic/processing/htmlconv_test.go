package processing

import "testing"

func TestHTMLToMarkdownTitle(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{"empty Tika title is dropped", "<html><head><title>&#0;</title></head><body><p>Body</p></body></html>", "Body"},
		{"real title is kept", "<html><head><title>Ubuntu Pro</title></head><body><p>Body</p></body></html>", "Ubuntu Pro\n\n\n\nBody"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := HTMLToMarkdown(tt.html)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("HTMLToMarkdown = %q, want %q", got, tt.want)
			}
		})
	}
}
