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
		{"save command", "/save", "[title]", true},
		{"save with title started", "/save notes", "", false},
		{"history command has no args", "/history", "", false},
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

func TestSlashCommandsRegistered(t *testing.T) {
	want := map[string]bool{
		cmdUseKnowledge: false,
		cmdSearch:       false,
		cmdSave:         false,
		cmdHistory:      false,
	}
	for _, c := range slashCommands {
		if _, ok := want[c.name]; ok {
			want[c.name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("slash command %q is not registered", name)
		}
	}
}
