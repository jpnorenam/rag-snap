package chat

import "testing"

// TestSystemPromptFor pins the fallback rule: a customized prompt is always
// honoured, and only the RAG-specific built-in default is swapped for the
// generic assistant prompt when retrieval is unavailable.
func TestSystemPromptFor(t *testing.T) {
	defaults := DefaultPrompts()
	customized := defaults
	customized.ChatSystemPrompt = "You are a laconic assistant."

	cases := []struct {
		name               string
		cfg                PromptConfig
		retrievalAvailable bool
		want               string
	}{
		{"default with retrieval", defaults, true, defaults.ChatSystemPrompt},
		{"default without retrieval falls back", defaults, false, fallbackChatSystemPrompt},
		{"customized with retrieval", customized, true, customized.ChatSystemPrompt},
		{"customized without retrieval is honoured", customized, false, customized.ChatSystemPrompt},
	}
	for _, tc := range cases {
		if got := SystemPromptFor(tc.cfg, tc.retrievalAvailable); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
