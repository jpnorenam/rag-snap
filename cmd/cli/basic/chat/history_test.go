package chat

import (
	"testing"
	"time"

	"github.com/jpnorenam/rag-snap/internal/chatstore"
	"github.com/openai/openai-go/v3"
)

func TestHistoryToTurnsDropsSystem(t *testing.T) {
	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("system prompt"),
		openai.UserMessage("hello"),
		openai.AssistantMessage("hi there"),
	}
	turns := historyToTurns(msgs)
	if len(turns) != 2 {
		t.Fatalf("expected 2 turns (system dropped), got %d", len(turns))
	}
	if turns[0].Role != "user" || turns[0].Content != "hello" {
		t.Errorf("turn 0 = %+v", turns[0])
	}
	if turns[1].Role != "assistant" || turns[1].Content != "hi there" {
		t.Errorf("turn 1 = %+v", turns[1])
	}
}

func TestTurnsToHistoryRebuildsWithFreshSystem(t *testing.T) {
	turns := []chatstore.Turn{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}
	msgs := turnsToHistory("fresh system", turns)
	if len(msgs) != 3 {
		t.Fatalf("expected system + 2 turns, got %d", len(msgs))
	}
	if msgs[0].OfSystem == nil || msgs[0].OfSystem.Content.OfString.Or("") != "fresh system" {
		t.Errorf("first message is not the fresh system prompt: %+v", msgs[0])
	}
	if msgs[1].OfUser == nil || msgs[1].OfUser.Content.OfString.Or("") != "hello" {
		t.Errorf("second message is not the user turn: %+v", msgs[1])
	}
	if msgs[2].OfAssistant == nil || msgs[2].OfAssistant.Content.OfString.Or("") != "hi there" {
		t.Errorf("third message is not the assistant turn: %+v", msgs[2])
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
