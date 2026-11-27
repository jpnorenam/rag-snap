package chat

import (
	"testing"
	"time"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/internal/chatstore"
)

func TestHistoryToTurns(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "hello"},
		{Role: RoleAssistant, Content: "hi there"},
	}
	turns := historyToTurns(msgs, nil)
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
	got := turnsToHistory(historyToTurns(want, nil))
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

func TestSourcesSurviveSaveAndResume(t *testing.T) {
	guide := CitedSource{Marker: 2, Citation: knowledge.Citation{Key: "guide.pdf", Label: "Install Guide", Href: "https://example.com/guide.pdf", Base: "docs", Tag: "canonical"}}
	faq := CitedSource{Marker: 1, Citation: knowledge.Citation{Key: "faq.md", Label: "faq.md", Base: "docs", Tag: "canonical"}}
	msgs := []Message{
		{Role: RoleUser, Content: "q1"},
		{Role: RoleAssistant, Content: "a1 [1]"},
		{Role: RoleUser, Content: "hi"},
		{Role: RoleAssistant, Content: "hello"},
		{Role: RoleUser, Content: "q2"},
		{Role: RoleAssistant, Content: "a2 [2][1]"},
	}
	answerSources := [][]CitedSource{{faq}, nil, {guide, faq}}

	store := chatstore.New(t.TempDir())
	saved, err := store.Save(chatstore.Chat{Turns: historyToTurns(msgs, answerSources)})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Get(saved.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if n := len(got.Turns[1].Sources); n != 1 || got.Turns[1].Sources[0].Key != "faq.md" {
		t.Errorf("first answer sources = %+v, want faq.md", got.Turns[1].Sources)
	}
	if len(got.Turns[0].Sources) != 0 || len(got.Turns[3].Sources) != 0 {
		t.Errorf("user turn or uncited answer carries sources: %+v / %+v", got.Turns[0].Sources, got.Turns[3].Sources)
	}
	if s := got.Turns[5].Sources; len(s) != 2 || s[0].Href != "https://example.com/guide.pdf" || s[0].Marker != 2 {
		t.Errorf("last answer sources = %+v, want guide.pdf [2] with its link, then faq.md", s)
	}

	session := &Session{}
	restoreCitations(session, got.Turns)
	if len(session.AnswerSources) != 3 {
		t.Fatalf("restored %d answer source lists, want 3", len(session.AnswerSources))
	}
	// A second save from the resumed session writes the same sources back.
	again := historyToTurns(turnsToHistory(got.Turns), session.AnswerSources)
	if len(again[5].Sources) != 2 {
		t.Errorf("re-saved last answer sources = %+v, want 2", again[5].Sources)
	}
}
