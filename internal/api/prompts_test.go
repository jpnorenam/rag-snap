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

// TestPromptStoreDefaultsWhenEmpty checks that a store with no files resolves to
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
	// Generation slots report no active variant and no stored variants.
	if views[0].Active != "" || len(views[0].Variants) != 0 {
		t.Errorf("fresh generation slot should have no active variant and no variants, got active=%q variants=%v", views[0].Active, views[0].Variants)
	}

	if got := store.resolve(); got != chat.DefaultPrompts() {
		t.Error("resolve() should return the built-in defaults for an empty store")
	}
}

// TestPromptStoreLegacySetCreatesCustomVariant checks the back-compat PUT path:
// customizing a generation slot from the default state creates and activates a
// `custom` variant, and the change survives a restart.
func TestPromptStoreLegacySetCreatesCustomVariant(t *testing.T) {
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
	if view.Active != legacyCustomVariant {
		t.Errorf("active variant = %q, want %q", view.Active, legacyCustomVariant)
	}
	if view.Default != chat.DefaultPrompts().ChatSystemPrompt {
		t.Error("default should still carry the built-in text after a customization")
	}

	// A fresh store over the same $SNAP_COMMON stands in for a daemon restart.
	reopened := newPromptStore()
	if got := reopened.resolve().ChatSystemPrompt; got != custom {
		t.Errorf("after restart: ChatSystemPrompt = %q, want %q", got, custom)
	}
	if got := reopened.resolve().SourceRules; got != chat.DefaultPrompts().SourceRules {
		t.Error("after restart: an uncustomized prompt should resolve to its default")
	}
}

// TestPromptStoreResetClearsPointerKeepsVariant checks that DELETE returns the
// slot to the default while leaving stored variants in place.
func TestPromptStoreResetClearsPointerKeepsVariant(t *testing.T) {
	store := newTestPromptStore(t)

	if _, err := store.createVariant(promptAnswerSystem, "terse", "Be terse."); err != nil {
		t.Fatalf("createVariant: %v", err)
	}
	if _, err := store.activate(promptAnswerSystem, "terse"); err != nil {
		t.Fatalf("activate: %v", err)
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
	// The variant is preserved and still listed.
	if len(view.Variants) != 1 || view.Variants[0] != "terse" {
		t.Errorf("reset should preserve stored variants, got %v", view.Variants)
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
// default text verbatim over the legacy `custom` variant leaves the prompt
// uncustomized, so the customized flag never lies.
func TestPromptStoreValueEqualToDefaultClearsOverride(t *testing.T) {
	store := newTestPromptStore(t)

	// Customize first so a `custom` variant is active.
	if _, err := store.set(promptChatSystem, "Something custom."); err != nil {
		t.Fatalf("set custom: %v", err)
	}
	// Now put the default back verbatim.
	view, err := store.set(promptChatSystem, chat.DefaultPrompts().ChatSystemPrompt)
	if err != nil {
		t.Fatalf("set default: %v", err)
	}
	if view.Customized {
		t.Error("customized = true after saving the default verbatim, want false")
	}
	if view.Active != "" {
		t.Errorf("active variant = %q after clearing, want empty", view.Active)
	}
	// The custom variant record is gone.
	if _, err := store.getVariant(promptChatSystem, legacyCustomVariant); !errors.Is(err, errUnknownVariant) {
		t.Errorf("custom variant should be deleted, getVariant err = %v", err)
	}
}

// TestPromptStoreUnknownName checks that only the three slots are addressable.
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

// TestVariantLifecycle exercises create → save (append) → activate → delete and
// the version history along the way.
func TestVariantLifecycle(t *testing.T) {
	store := newTestPromptStore(t)

	// Create.
	v, err := store.createVariant(promptChatSystem, "presales-call", "v1 text")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if v.Version != 1 || v.Value != "v1 text" || v.Active {
		t.Errorf("create view = %+v, want version 1, v1 text, not active", v)
	}

	// Save twice: distinct values append, identical value is a no-op.
	if _, err := store.saveVariant(promptChatSystem, "presales-call", "v2 text"); err != nil {
		t.Fatalf("save v2: %v", err)
	}
	if v, err = store.saveVariant(promptChatSystem, "presales-call", "v2 text"); err != nil {
		t.Fatalf("save v2 again: %v", err)
	}
	if v.Version != 2 {
		t.Errorf("identical save changed head to version %d, want 2 (no-op)", v.Version)
	}
	versions, err := store.versions(promptChatSystem, "presales-call")
	if err != nil {
		t.Fatalf("versions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions))
	}

	// Activate: it drives resolution and its provenance ref carries the version.
	if _, err := store.activate(promptChatSystem, "presales-call"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	value, ref, err := store.resolveSlot(promptChatSystem, "")
	if err != nil {
		t.Fatalf("resolveSlot active: %v", err)
	}
	if value != "v2 text" || ref != "presales-call@2" {
		t.Errorf("active resolution = (%q, %q), want (v2 text, presales-call@2)", value, ref)
	}

	// The active variant cannot be deleted.
	if err := store.deleteVariant(promptChatSystem, "presales-call"); !errors.Is(err, errVariantActive) {
		t.Errorf("delete active: err = %v, want errVariantActive", err)
	}
	// Deactivate, then delete succeeds.
	if _, err := store.activate(promptChatSystem, ""); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if err := store.deleteVariant(promptChatSystem, "presales-call"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.getVariant(promptChatSystem, "presales-call"); !errors.Is(err, errUnknownVariant) {
		t.Errorf("after delete: getVariant err = %v, want errUnknownVariant", err)
	}
}

// TestVariantRestoreAppendsHead checks that restoring an earlier version appends
// a new head with that content, leaving history linear.
func TestVariantRestoreAppendsHead(t *testing.T) {
	store := newTestPromptStore(t)

	if _, err := store.createVariant(promptChatSystem, "v", "one"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.saveVariant(promptChatSystem, "v", "two"); err != nil {
		t.Fatalf("save two: %v", err)
	}
	if _, err := store.saveVariant(promptChatSystem, "v", "three"); err != nil {
		t.Fatalf("save three: %v", err)
	}

	view, err := store.restoreVersion(promptChatSystem, "v", 1)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if view.Version != 4 || view.Value != "one" {
		t.Errorf("restore head = version %d value %q, want version 4 value one", view.Version, view.Value)
	}
	versions, _ := store.versions(promptChatSystem, "v")
	if len(versions) != 4 {
		t.Errorf("expected 4 versions after restore, got %d", len(versions))
	}

	// An unknown version is a not-found.
	if _, err := store.restoreVersion(promptChatSystem, "v", 99); !errors.Is(err, errUnknownVersion) {
		t.Errorf("restore unknown version: err = %v, want errUnknownVersion", err)
	}
}

// TestVariantNameValidation checks the reserved and invalid name rejections.
func TestVariantNameValidation(t *testing.T) {
	store := newTestPromptStore(t)

	if _, err := store.createVariant(promptChatSystem, "default", "x"); !errors.Is(err, errReservedName) {
		t.Errorf("create default: err = %v, want errReservedName", err)
	}
	for _, bad := range []string{"", "Bad", "../escape", "has space", "under_score"} {
		if _, err := store.createVariant(promptChatSystem, bad, "x"); !errors.Is(err, errInvalidName) {
			t.Errorf("create %q: err = %v, want errInvalidName", bad, err)
		}
	}

	// A duplicate create is a conflict.
	if _, err := store.createVariant(promptChatSystem, "dup", "x"); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := store.createVariant(promptChatSystem, "dup", "y"); !errors.Is(err, errVariantExists) {
		t.Errorf("duplicate create: err = %v, want errVariantExists", err)
	}
}

// TestVariantsRejectedOnSourceRules checks that the guardrail slot exposes no
// variants.
func TestVariantsRejectedOnSourceRules(t *testing.T) {
	store := newTestPromptStore(t)

	if _, err := store.listVariants(promptSourceRules); !errors.Is(err, errNoVariants) {
		t.Errorf("listVariants source_rules: err = %v, want errNoVariants", err)
	}
	if _, err := store.createVariant(promptSourceRules, "x", "y"); !errors.Is(err, errNoVariants) {
		t.Errorf("createVariant source_rules: err = %v, want errNoVariants", err)
	}
	if _, err := store.activate(promptSourceRules, "x"); !errors.Is(err, errNoVariants) {
		t.Errorf("activate source_rules: err = %v, want errNoVariants", err)
	}
	// But an unknown slot is errUnknownPrompt, not errNoVariants.
	if _, err := store.listVariants("nope"); !errors.Is(err, errUnknownPrompt) {
		t.Errorf("listVariants unknown: err = %v, want errUnknownPrompt", err)
	}
}

// TestResolveSlotExplicitSelection checks that an explicit selection overrides
// the active pointer and that an unknown selection is rejected.
func TestResolveSlotExplicitSelection(t *testing.T) {
	store := newTestPromptStore(t)

	if _, err := store.createVariant(promptChatSystem, "presales", "presales text"); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Activate a *different* selection to prove the explicit one wins.
	if _, err := store.createVariant(promptChatSystem, "other", "other text"); err != nil {
		t.Fatalf("create other: %v", err)
	}
	if _, err := store.activate(promptChatSystem, "other"); err != nil {
		t.Fatalf("activate: %v", err)
	}

	value, ref, err := store.resolveSlot(promptChatSystem, "presales")
	if err != nil {
		t.Fatalf("resolveSlot: %v", err)
	}
	if value != "presales text" || ref != "presales@1" {
		t.Errorf("explicit resolution = (%q, %q), want (presales text, presales@1)", value, ref)
	}

	if _, _, err := store.resolveSlot(promptChatSystem, "nope"); !errors.Is(err, errUnknownVariant) {
		t.Errorf("resolveSlot unknown: err = %v, want errUnknownVariant", err)
	}
}

// TestPromptStoreMigratesLegacyOverrides checks the one-way migration: a legacy
// single-file override store becomes activated `custom`/`override` variants, the
// effective values are preserved, the legacy file is renamed aside, and the
// migration does not re-run.
func TestPromptStoreMigratesLegacyOverrides(t *testing.T) {
	common := t.TempDir()
	t.Setenv("SNAP_COMMON", common)

	// Write a legacy prompts.json under $SNAP_COMMON/ragd (the old location).
	legacyDir := filepath.Join(common, "ragd")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacyPath := filepath.Join(legacyDir, "prompts.json")
	legacy := chat.PromptConfig{
		ChatSystemPrompt: "legacy chat override",
		SourceRules:      "legacy source rules",
		// AnswerSystemPrompt left empty: it must resolve to the default.
	}
	data, _ := json.MarshalIndent(legacy, "", "  ")
	if err := os.WriteFile(legacyPath, data, 0o600); err != nil {
		t.Fatalf("writing legacy store: %v", err)
	}

	store := newPromptStore()
	got := store.resolve()
	if got.ChatSystemPrompt != "legacy chat override" {
		t.Errorf("migrated chat prompt = %q, want the legacy override", got.ChatSystemPrompt)
	}
	if got.SourceRules != "legacy source rules" {
		t.Errorf("migrated source rules = %q, want the legacy override", got.SourceRules)
	}
	if got.AnswerSystemPrompt != chat.DefaultPrompts().AnswerSystemPrompt {
		t.Error("an unset legacy field should resolve to the built-in default")
	}

	// The chat override is now a `custom` variant, active.
	views := store.views()
	if views[0].Active != legacyCustomVariant {
		t.Errorf("migrated chat slot active = %q, want %q", views[0].Active, legacyCustomVariant)
	}

	// The legacy file is renamed aside and does not re-migrate.
	if _, err := os.Stat(legacyPath); !errors.Is(err, os.ErrNotExist) {
		t.Error("legacy file should be renamed aside after migration")
	}
	if _, err := os.Stat(legacyPath + ".migrated"); err != nil {
		t.Errorf("migrated legacy file should be preserved at .migrated: %v", err)
	}
	// A fresh store over the same dir finds no legacy file: still customized.
	if got := newPromptStore().resolve().ChatSystemPrompt; got != "legacy chat override" {
		t.Errorf("after restart: chat prompt = %q, want the migrated override", got)
	}
}

// TestPromptStoreCorruptRecordDegradesAlone checks that one unparseable variant
// record does not take chat down: its slot resolves to the built-in default and
// reports uncustomized, while a healthy slot keeps working.
func TestPromptStoreCorruptRecordDegradesAlone(t *testing.T) {
	common := t.TempDir()
	t.Setenv("SNAP_COMMON", common)

	store := newPromptStore()
	// A healthy answer-slot customization.
	if _, err := store.set(promptAnswerSystem, "Healthy answer prompt."); err != nil {
		t.Fatalf("set answer: %v", err)
	}
	// A chat customization we then corrupt on disk, leaving the active pointer.
	if _, err := store.set(promptChatSystem, "Chat prompt."); err != nil {
		t.Fatalf("set chat: %v", err)
	}
	corruptPath := filepath.Join(common, promptsStoreRelDir, string(promptChatSystem), legacyCustomVariant+".json")
	if err := os.WriteFile(corruptPath, []byte(`{"versions": [trunca`), 0o600); err != nil {
		t.Fatalf("corrupting record: %v", err)
	}

	reopened := newPromptStore()
	got := reopened.resolve()
	if got.ChatSystemPrompt != chat.DefaultPrompts().ChatSystemPrompt {
		t.Error("a corrupt record should degrade its slot to the built-in default")
	}
	if got.AnswerSystemPrompt != "Healthy answer prompt." {
		t.Error("a healthy slot must keep working alongside a corrupt one")
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

	slotDir := filepath.Join(common, promptsStoreRelDir, string(promptChatSystem))
	entries, err := os.ReadDir(slotDir)
	if err != nil {
		t.Fatalf("reading slot dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".prompts-") {
			t.Errorf("temporary file %q left behind after save", e.Name())
		}
	}
}
