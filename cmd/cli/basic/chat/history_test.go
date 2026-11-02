package chat

import (
	"testing"
	"time"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/internal/chatstore"
	"github.com/openai/openai-go/v3"
)

func TestHistoryToTurnsDropsSystem(t *testing.T) {
	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("system prompt"),
		openai.UserMessage("hello"),
		openai.AssistantMessage("hi there"),
	}
	turns := historyToTurns(msgs, nil)
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

func TestSourcesSurviveSaveAndResume(t *testing.T) {
	guide := CitedSource{Marker: 2, Citation: knowledge.Citation{Key: "guide.pdf", Label: "Install Guide", Href: "https://example.com/guide.pdf", Base: "docs", Tag: "canonical"}}
	faq := CitedSource{Marker: 1, Citation: knowledge.Citation{Key: "faq.md", Label: "faq.md", Base: "docs", Tag: "canonical"}}
	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("system"),
		openai.UserMessage("q1"),
		openai.AssistantMessage("a1 [1]"),
		openai.UserMessage("hi"),
		openai.AssistantMessage("hello"),
		openai.UserMessage("q2"),
		openai.AssistantMessage("a2 [2][1]"),
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
	reg := session.citationRegistry()
	if n := reg.Number("guide.pdf"); n != 2 {
		t.Errorf("re-retrieved guide.pdf = [%d], want [2]", n)
	}
	if n := reg.Number("new.md"); n != 3 {
		t.Errorf("new document after resume = [%d], want [3]", n)
	}

	// A second save from the resumed session writes the same sources back.
	again := historyToTurns(turnsToHistory("system", got.Turns), session.AnswerSources)
	if len(again[5].Sources) != 2 {
		t.Errorf("re-saved last answer sources = %+v, want 2", again[5].Sources)
	}
}
