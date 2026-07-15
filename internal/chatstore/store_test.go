package chatstore

import (
	"os"
	"path/filepath"
	"testing"
)

func sampleTurns() []Turn {
	return []Turn{
		{Role: "user", Content: "How do I rotate the OpenSearch admin password?"},
		{Role: "assistant", Content: "<think>recall docs</think>Use the securityadmin tool."},
	}
}

func TestSaveAndGetRoundTrip(t *testing.T) {
	s := New(t.TempDir())

	saved, err := s.Save(Chat{Model: "m1", Bases: []string{"default", "docs"}, Turns: sampleTurns()})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if saved.ID == "" {
		t.Fatal("expected a generated id")
	}
	if saved.CreatedAt.IsZero() || saved.UpdatedAt.IsZero() {
		t.Fatal("expected timestamps to be set")
	}

	got, err := s.Get(saved.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Turns) != 2 || got.Model != "m1" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if len(got.Bases) != 2 || got.Bases[0] != "default" || got.Bases[1] != "docs" {
		t.Fatalf("bases not preserved: %v", got.Bases)
	}
	if got.Turns[1].Content != "Use the securityadmin tool." {
		t.Fatalf("think content not stripped on save: %q", got.Turns[1].Content)
	}
}

func TestSaveDerivesTitleFromFirstUserTurn(t *testing.T) {
	s := New(t.TempDir())
	saved, err := s.Save(Chat{Turns: sampleTurns()})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if saved.Title != "How do I rotate the OpenSearch admin password?" {
		t.Fatalf("unexpected derived title: %q", saved.Title)
	}
}

func TestSaveExplicitTitleWins(t *testing.T) {
	s := New(t.TempDir())
	saved, err := s.Save(Chat{Title: "release planning", Turns: sampleTurns()})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if saved.Title != "release planning" {
		t.Fatalf("explicit title not used: %q", saved.Title)
	}
}

func TestSaveUpdatesInPlace(t *testing.T) {
	s := New(t.TempDir())
	first, err := s.Save(Chat{Turns: sampleTurns()})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	turns := append(sampleTurns(), Turn{Role: "user", Content: "and to reset it?"})
	second, err := s.Save(Chat{ID: first.ID, Turns: turns})
	if err != nil {
		t.Fatalf("Save (update): %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("id changed on update: %s -> %s", first.ID, second.ID)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatal("created_at not preserved on update")
	}
	if second.Title != first.Title {
		t.Fatalf("title changed on untitled re-save: %q -> %q", first.Title, second.Title)
	}

	list, err := s.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected one chat after update-in-place, got %d", len(list))
	}
	if list[0].TurnCount != 3 {
		t.Fatalf("expected 3 turns after update, got %d", list[0].TurnCount)
	}
}

func TestReSaveWithNewTitleRenames(t *testing.T) {
	s := New(t.TempDir())
	first, _ := s.Save(Chat{Turns: sampleTurns()})
	second, err := s.Save(Chat{ID: first.ID, Title: "renamed", Turns: sampleTurns()})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if second.Title != "renamed" {
		t.Fatalf("rename did not apply: %q", second.Title)
	}
}

func TestListSearchFilter(t *testing.T) {
	s := New(t.TempDir())
	if _, err := s.Save(Chat{Title: "tika extraction", Turns: []Turn{{Role: "user", Content: "hello"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(Chat{Title: "bedrock setup", Turns: []Turn{{Role: "user", Content: "configure aws"}}}); err != nil {
		t.Fatal(err)
	}

	// Match on title.
	got, err := s.List("TIKA")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Title != "tika extraction" {
		t.Fatalf("title search mismatch: %+v", got)
	}

	// Match on transcript content.
	got, err = s.List("aws")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Title != "bedrock setup" {
		t.Fatalf("content search mismatch: %+v", got)
	}

	// No match.
	got, err = s.List("nonexistent")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no matches, got %d", len(got))
	}
}

func TestListSkipsCorruptFiles(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	good, _ := s.Save(Chat{Turns: sampleTurns()})

	// Write a corrupt file that must be skipped, not fatal.
	if err := os.WriteFile(filepath.Join(dir, "deadbeef.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := s.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ID != good.ID {
		t.Fatalf("expected only the good chat, got %+v", got)
	}
}

func TestDelete(t *testing.T) {
	s := New(t.TempDir())
	saved, _ := s.Save(Chat{Turns: sampleTurns()})

	if err := s.Delete(saved.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(saved.ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
	if err := s.Delete(saved.ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound deleting twice, got %v", err)
	}
}

func TestSaveRejectsEmptyTranscript(t *testing.T) {
	s := New(t.TempDir())
	if _, err := s.Save(Chat{}); err != ErrEmpty {
		t.Fatalf("expected ErrEmpty for no turns, got %v", err)
	}
	if _, err := s.Save(Chat{Turns: []Turn{{Role: "user", Content: "  "}}}); err != ErrEmpty {
		t.Fatalf("expected ErrEmpty for whitespace-only turn, got %v", err)
	}
}

func TestSaveAfterDeleteRecreatesUnderSameID(t *testing.T) {
	s := New(t.TempDir())
	first, _ := s.Save(Chat{Turns: sampleTurns()})
	if err := s.Delete(first.ID); err != nil {
		t.Fatal(err)
	}
	again, err := s.Save(Chat{ID: first.ID, Turns: sampleTurns()})
	if err != nil {
		t.Fatalf("Save after delete: %v", err)
	}
	if again.ID != first.ID {
		t.Fatalf("expected same id, got %s want %s", again.ID, first.ID)
	}
}

func TestUnavailableStore(t *testing.T) {
	s := New("")
	if _, err := s.Save(Chat{Turns: sampleTurns()}); err != ErrUnavailable {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
	// Reads degrade to empty rather than error.
	if got, err := s.List(""); err != nil || len(got) != 0 {
		t.Fatalf("expected empty list with no error, got %v %v", got, err)
	}
}

func TestStripThink(t *testing.T) {
	cases := map[string]string{
		"<think>reasoning</think>answer":      "answer",
		"a<think>x</think>b<think>y</think>c": "abc",
		"no tags here":                        "no tags here",
		"<think>unclosed reasoning only":      "",
		"prefix <think>mid</think> suffix":    "prefix  suffix",
	}
	for in, want := range cases {
		if got := StripThink(in); got != want {
			t.Errorf("StripThink(%q) = %q, want %q", in, got, want)
		}
	}
}
