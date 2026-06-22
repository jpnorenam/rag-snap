package chat

import "testing"

func TestSyntaxHint(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantHint string
		wantOK   bool
	}{
		{"completed command", "/search", "[-k N] <query>", true},
		{"trailing space", "/search ", "[-k N] <query>", true},
		{"multiple trailing spaces", "/search   ", "[-k N] <query>", true},
		{"query started", "/search ceph", "", false},
		{"partial name", "/sea", "", false},
		{"command without args", "/use-knowledge", "", false},
		{"bare slash", "/", "", false},
		{"plain text", "hello", "", false},
		{"empty", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotHint, gotOK := syntaxHint(tt.input)
			if gotHint != tt.wantHint || gotOK != tt.wantOK {
				t.Errorf("syntaxHint(%q) = (%q, %v), want (%q, %v)",
					tt.input, gotHint, gotOK, tt.wantHint, tt.wantOK)
			}
		})
	}
}
