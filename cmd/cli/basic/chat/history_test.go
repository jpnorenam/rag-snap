package chat

import (
	"testing"
	"time"

	"github.com/jpnorenam/rag-snap/internal/chatstore"
)

func TestHistoryToTurns(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "hello"},
		{Role: RoleAssistant, Content: "hi there"},
	}
	turns := historyToTurns(msgs)
	if len(turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(turns))
	}
	if turns[0].Role != "user" || turns[0].Content != "hello" {
		t.Errorf("turn 0 = %+v", turns[0])
	}
	if turns[1].Role != "assistant" || turns[1].Content != "hi there" {
		t.Errorf("turn 1 = %+v", turns[1])
	}
}

func TestTurnsToHistoryDropsUnknownRoles(t *testing.T) {
	turns := []chatstore.Turn{
		{Role: "system", Content: "a stored system prompt must never be replayed"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}
	msgs := turnsToHistory(turns)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 turns (system dropped), got %d", len(msgs))
	}
	if msgs[0] != (Message{Role: RoleUser, Content: "hello"}) {
		t.Errorf("first message is not the user turn: %+v", msgs[0])
	}
	if msgs[1] != (Message{Role: RoleAssistant, Content: "hi there"}) {
		t.Errorf("second message is not the assistant turn: %+v", msgs[1])
	}
}

// Saved turns are backend-neutral: a chat saved under one chat.api must resume
// unchanged under the other.
func TestHistoryRoundTrip(t *testing.T) {
	want := []Message{
		{Role: RoleUser, Content: "what is MAAS?"},
		{Role: RoleAssistant, Content: "Metal as a Service."},
		{Role: RoleUser, Content: "and its storage?"},
	}
	got := turnsToHistory(historyToTurns(want))
	if len(got) != len(want) {
		t.Fatalf("round trip changed the length: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("message %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestRelativeTime(t *testing.T) {
	now := time.Now()
	cases := []struct {
		in   time.Time
		want string
	}{
		{now.Add(-30 * time.Second), "just now"},
		{now.Add(-5 * time.Minute), "5m ago"},
		{now.Add(-3 * time.Hour), "3h ago"},
		{now.Add(-50 * time.Hour), "2d ago"},
		{time.Time{}, "unknown"},
	}
	for _, c := range cases {
		if got := relativeTime(c.in); got != c.want {
			t.Errorf("relativeTime(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
