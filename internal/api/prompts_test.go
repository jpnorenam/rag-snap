package api

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
)

// newTestPromptStore returns a store rooted at a temp $SNAP_COMMON, so the store
// can be exercised outside a snap.
func newTestPromptStore(t *testing.T) *promptStore {
	t.Helper()
	t.Setenv("SNAP_COMMON", t.TempDir())
	return newPromptStore()
}

// TestPromptStoreDefaultsWhenEmpty checks that a store with no file resolves to
// the built-in defaults and reports nothing as customized.
func TestPromptStoreDefaultsWhenEmpty(t *testing.T) {
	store := newTestPromptStore(t)

	views := store.views()
	if len(views) != 3 {
		t.Fatalf("expected 3 prompts, got %d", len(views))
	}

	// The fixed order matches the CLI's `prompt init` select.
	want := []string{"chat_system_prompt", "answer_system_prompt", "source_rules"}
	for i, v := range views {
		if v.Name != want[i] {
			t.Errorf("prompt %d: name = %q, want %q", i, v.Name, want[i])
		}
		if v.Customized {
			t.Errorf("prompt %q: customized = true, want false", v.Name)
		}
		if v.Value != v.Default {
			t.Errorf("prompt %q: value should equal the default when uncustomized", v.Name)
		}
		if v.Default == "" {
			t.Errorf("prompt %q: default is empty", v.Name)
		}
	}

	if got := store.resolve(); got != chat.DefaultPrompts() {
		t.Error("resolve() should return the built-in defaults for an empty store")
	}
}

// TestPromptStoreSetAndPersist checks that an override is stored, reported as
// customized, fed into the resolved config, and survives reopening the store
// (i.e. a daemon restart).
func TestPromptStoreSetAndPersist(t *testing.T) {
	t.Setenv("SNAP_COMMON", t.TempDir())
	store := newPromptStore()

	const custom = "Answer only in haiku."
	view, err := store.set(promptChatSystem, custom)
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if !view.Customized {
		t.Error("customized = false after set, want true")
	}
	if view.Value != custom {
		t.Errorf("value = %q, want %q", view.Value, custom)
	}
	if view.Default != chat.DefaultPrompts().ChatSystemPrompt {
		t.Error("default should still carry the built-in text after a customization")
	}

	// A fresh store over the same $SNAP_COMMON stands in for a daemon restart.
	reopened := newPromptStore()
	if got := reopened.resolve().ChatSystemPrompt; got != custom {
		t.Errorf("after restart: ChatSystemPrompt = %q, want %q", got, custom)
	}
	// Prompts that were never customized still resolve to their defaults.
	if got := reopened.resolve().SourceRules; got != chat.DefaultPrompts().SourceRules {
		t.Error("after restart: an uncustomized prompt should resolve to its default")
	}
}

// TestPromptStoreReset checks that reset drops the override (restoring the
// default) and that resetting an uncustomized prompt is a no-op.
func TestPromptStoreReset(t *testing.T) {
	store := newTestPromptStore(t)

	if _, err := store.set(promptAnswerSystem, "Be terse."); err != nil {
		t.Fatalf("set: %v", err)
	}

	view, err := store.reset(promptAnswerSystem)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if view.Customized {
		t.Error("customized = true after reset, want false")
	}
	if view.Value != chat.DefaultPrompts().AnswerSystemPrompt {
		t.Error("reset should restore the built-in default as the effective value")
	}

	// Reset is idempotent.
	if _, err := store.reset(promptAnswerSystem); err != nil {
		t.Fatalf("reset of an uncustomized prompt should be a no-op, got: %v", err)
	}
}

// TestPromptStoreRejectsEmpty checks that clearing an editor cannot silently
// reset a prompt: an empty or whitespace-only value is rejected and the stored
// value is left untouched.
func TestPromptStoreRejectsEmpty(t *testing.T) {
	store := newTestPromptStore(t)

	const custom = "Keep me."
	if _, err := store.set(promptSourceRules, custom); err != nil {
		t.Fatalf("set: %v", err)
	}

	for _, value := range []string{"", "   ", "\n\t "} {
		if _, err := store.set(promptSourceRules, value); !errors.Is(err, errEmptyPrompt) {
			t.Errorf("set(%q): error = %v, want errEmptyPrompt", value, err)
		}
	}

	if got := store.resolve().SourceRules; got != custom {
		t.Errorf("a rejected set must not change the stored value: got %q, want %q", got, custom)
	}
}

// TestPromptStoreValueEqualToDefaultClearsOverride checks that saving the
// default text verbatim leaves the prompt uncustomized, so the customized flag
// never lies and a future release's improved default is not shadowed by a stale
// identical copy.
func TestPromptStoreValueEqualToDefaultClearsOverride(t *testing.T) {
	store := newTestPromptStore(t)

	view, err := store.set(promptChatSystem, chat.DefaultPrompts().ChatSystemPrompt)
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if view.Customized {
		t.Error("customized = true after saving the default verbatim, want false")
	}

	// The override must not be persisted either.
	data, err := os.ReadFile(filepath.Join(os.Getenv("SNAP_COMMON"), promptsRelPath))
	if err != nil {
		t.Fatalf("reading store: %v", err)
	}
	var stored chat.PromptConfig
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("decoding store: %v", err)
	}
	if stored.ChatSystemPrompt != "" {
		t.Error("a value equal to the default should be stored as an empty override")
	}
}

// TestPromptStoreUnknownName checks that only the three templates are addressable.
func TestPromptStoreUnknownName(t *testing.T) {
	store := newTestPromptStore(t)

	if _, err := store.view("nope"); !errors.Is(err, errUnknownPrompt) {
		t.Errorf("view: error = %v, want errUnknownPrompt", err)
	}
	if _, err := store.set("nope", "x"); !errors.Is(err, errUnknownPrompt) {
		t.Errorf("set: error = %v, want errUnknownPrompt", err)
	}
	if _, err := store.reset("nope"); !errors.Is(err, errUnknownPrompt) {
		t.Errorf("reset: error = %v, want errUnknownPrompt", err)
	}
}

// TestPromptStoreCorruptFileFallsBackToDefaults checks that a hand-edited or
// truncated store cannot take chat down: it resolves to the built-in defaults
// and reports as uncustomized rather than erroring.
func TestPromptStoreCorruptFileFallsBackToDefaults(t *testing.T) {
	common := t.TempDir()
	t.Setenv("SNAP_COMMON", common)

	path := filepath.Join(common, promptsRelPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating store dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"chat_system_prompt": "trunca`), 0o600); err != nil {
		t.Fatalf("writing corrupt store: %v", err)
	}

	store := newPromptStore()
	if got := store.resolve(); got != chat.DefaultPrompts() {
		t.Error("a corrupt store should resolve to the built-in defaults")
	}
	for _, v := range store.views() {
		if v.Customized {
			t.Errorf("prompt %q: customized = true over a corrupt store, want false", v.Name)
		}
	}

	// A subsequent write must still succeed, replacing the corrupt file.
	if _, err := store.set(promptChatSystem, "Recovered."); err != nil {
		t.Fatalf("set over a corrupt store: %v", err)
	}
	if got := newPromptStore().resolve().ChatSystemPrompt; got != "Recovered." {
		t.Errorf("after recovery: ChatSystemPrompt = %q, want %q", got, "Recovered.")
	}
}

// TestPromptStoreWritesAreAtomic checks that a save leaves no temporary files
// behind (the temp+rename dance must clean up after itself).
func TestPromptStoreWritesAreAtomic(t *testing.T) {
	common := t.TempDir()
	t.Setenv("SNAP_COMMON", common)

	store := newPromptStore()
	if _, err := store.set(promptChatSystem, "Custom."); err != nil {
		t.Fatalf("set: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(common, "ragd"))
	if err != nil {
		t.Fatalf("reading store dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".prompts-") {
			t.Errorf("temporary file %q left behind after save", e.Name())
		}
	}
}
