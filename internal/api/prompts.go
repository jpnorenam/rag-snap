package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/canonical/go-snapctl/env"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
)

// promptsRelPath is the prompt store location under $SNAP_COMMON, alongside the
// socket and the localhost token. $SNAP_COMMON (not $SNAP_DATA) so customized
// prompts survive a snap refresh and are not reverted with a revision rollback.
const promptsRelPath = "ragd/prompts.json"

// promptName is one of the three fixed prompt templates.
type promptName string

const (
	promptChatSystem   promptName = "chat_system_prompt"
	promptAnswerSystem promptName = "answer_system_prompt"
	promptSourceRules  promptName = "source_rules"
)

// promptOrder is the canonical order prompts are listed in, matching the order
// of the CLI's `prompt init` select.
var promptOrder = []promptName{promptChatSystem, promptAnswerSystem, promptSourceRules}

// Errors distinguishing a bad prompt name from a bad prompt value, so handlers
// can map them to 404 and 400 respectively.
var (
	errUnknownPrompt = errors.New("unknown prompt")
	errEmptyPrompt   = errors.New("prompt value is empty")
)

// promptView is the client view of one prompt: its effective value, the built-in
// default it falls back to, and whether an override is stored. Returning the
// default alongside the value is what lets a client show the default for diffing
// and offer a meaningful reset without a second request.
type promptView struct {
	Name       string `json:"name"`
	Value      string `json:"value"`
	Default    string `json:"default"`
	Customized bool   `json:"customized"`
}

// promptStore is the daemon-owned store of prompt overrides, persisted as JSON
// under $SNAP_COMMON. It stores *overrides only*: a prompt with no stored value
// always resolves to the built-in default of the running release, so an improved
// default ships to users who never customized that prompt.
//
// The CLI's own ~/.config/rag-cli/prompts.json is a different, client-local file
// the daemon cannot read (distinct $HOME under strict confinement); it remains
// the fallback for daemonless CLI runs only.
type promptStore struct {
	mu sync.Mutex
	// path is the store file. It is empty when the path could not be resolved,
	// in which case the store degrades to read-only built-in defaults rather
	// than failing prompt resolution (and so chat/answer keep working).
	path string
}

// newPromptStore resolves the store path, creating its parent directory. A
// resolution failure is logged and degrades the store to defaults-only.
func newPromptStore() *promptStore {
	path, err := promptsPath()
	if err != nil {
		log.Printf("prompt store unavailable (%v); using built-in defaults", err)
		return &promptStore{}
	}
	return &promptStore{path: path}
}

// promptsPath resolves the store path under $SNAP_COMMON (temp-dir fallback
// off-snap, as for the token) and ensures its parent directory exists.
func promptsPath() (string, error) {
	base := env.SnapCommon()
	if base == "" {
		// Outside a snap (local dev / tests), fall back to a temp dir.
		base = os.TempDir()
	}
	path := filepath.Join(base, promptsRelPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("creating prompt store directory: %w", err)
	}
	return path, nil
}

// resolve returns the effective prompt configuration: stored overrides with
// every unset field filled in from the built-in defaults. This is what chat
// sessions and batch operations are seeded with.
func (p *promptStore) resolve() chat.PromptConfig {
	p.mu.Lock()
	defer p.mu.Unlock()
	return withDefaults(p.loadLocked())
}

// views returns all three prompts in the canonical order.
func (p *promptStore) views() []promptView {
	p.mu.Lock()
	defer p.mu.Unlock()

	overrides := p.loadLocked()
	views := make([]promptView, 0, len(promptOrder))
	for _, name := range promptOrder {
		views = append(views, viewOf(name, overrides))
	}
	return views
}

// view returns a single prompt, or errUnknownPrompt if the name is not one of
// the three templates.
func (p *promptStore) view(name promptName) (promptView, error) {
	if !validPrompt(name) {
		return promptView{}, errUnknownPrompt
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return viewOf(name, p.loadLocked()), nil
}

// set stores an override for a prompt and returns the resulting view.
//
// An empty or whitespace-only value is rejected (errEmptyPrompt): resetting is
// an explicit DELETE, never a side effect of clearing an editor. A value equal
// to the built-in default clears the override instead of storing a copy, so the
// customized flag never lies and a future release's improved default is not
// shadowed by a stale identical copy.
func (p *promptStore) set(name promptName, value string) (promptView, error) {
	if !validPrompt(name) {
		return promptView{}, errUnknownPrompt
	}
	if strings.TrimSpace(value) == "" {
		return promptView{}, errEmptyPrompt
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	overrides := p.loadLocked()
	if value == defaultOf(name) {
		value = ""
	}
	setField(&overrides, name, value)

	if err := p.saveLocked(overrides); err != nil {
		return promptView{}, err
	}
	return viewOf(name, overrides), nil
}

// reset drops a prompt's override so it resolves to the built-in default again.
// Resetting a prompt that is not customized is a no-op.
func (p *promptStore) reset(name promptName) (promptView, error) {
	if !validPrompt(name) {
		return promptView{}, errUnknownPrompt
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	overrides := p.loadLocked()
	if field(overrides, name) == "" {
		// Already at the default: nothing to write.
		return viewOf(name, overrides), nil
	}
	setField(&overrides, name, "")

	if err := p.saveLocked(overrides); err != nil {
		return promptView{}, err
	}
	return viewOf(name, overrides), nil
}

// loadLocked reads the stored overrides. A missing store is the normal
// uncustomized case. A store that cannot be read or parsed must not take chat
// down: it is logged and treated as "no overrides", so prompts resolve to the
// built-in defaults and report as not customized.
//
// The caller must hold p.mu.
func (p *promptStore) loadLocked() chat.PromptConfig {
	if p.path == "" {
		return chat.PromptConfig{}
	}

	data, err := os.ReadFile(p.path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("reading prompt store %s: %v; using built-in defaults", p.path, err)
		}
		return chat.PromptConfig{}
	}

	var overrides chat.PromptConfig
	if err := json.Unmarshal(data, &overrides); err != nil {
		log.Printf("prompt store %s is not valid JSON: %v; using built-in defaults", p.path, err)
		return chat.PromptConfig{}
	}
	return overrides
}

// saveLocked writes the overrides atomically: a temp file in the same directory
// (so the rename cannot cross filesystems) is written owner-only and renamed
// over the store, so a crash mid-write can never leave a torn file behind.
//
// The caller must hold p.mu.
func (p *promptStore) saveLocked(overrides chat.PromptConfig) error {
	if p.path == "" {
		return errors.New("prompt store is unavailable: cannot persist prompts")
	}

	data, err := json.MarshalIndent(overrides, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding prompts: %w", err)
	}

	dir := filepath.Dir(p.path)
	tmp, err := os.CreateTemp(dir, ".prompts-*.json")
	if err != nil {
		return fmt.Errorf("creating temporary prompt file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		// No-op once the rename succeeded; cleans up on any failure path.
		_ = os.Remove(tmpName)
	}()

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("securing temporary prompt file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing prompts: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing prompts: %w", err)
	}
	if err := os.Rename(tmpName, p.path); err != nil {
		return fmt.Errorf("replacing prompt store: %w", err)
	}
	return nil
}

// validPrompt reports whether name is one of the three templates.
func validPrompt(name promptName) bool {
	for _, n := range promptOrder {
		if n == name {
			return true
		}
	}
	return false
}

// promptNames lists the valid prompt names, for error messages.
func promptNames() string {
	names := make([]string, len(promptOrder))
	for i, n := range promptOrder {
		names[i] = string(n)
	}
	return strings.Join(names, ", ")
}

// viewOf builds the view of one prompt from the stored overrides.
func viewOf(name promptName, overrides chat.PromptConfig) promptView {
	override := field(overrides, name)
	def := defaultOf(name)

	value := override
	if value == "" {
		value = def
	}
	return promptView{
		Name:       string(name),
		Value:      value,
		Default:    def,
		Customized: override != "",
	}
}

// withDefaults fills every unset override with its built-in default.
func withDefaults(overrides chat.PromptConfig) chat.PromptConfig {
	effective := chat.DefaultPrompts()
	for _, name := range promptOrder {
		if v := field(overrides, name); v != "" {
			setField(&effective, name, v)
		}
	}
	return effective
}

// defaultOf returns a prompt's built-in default text.
func defaultOf(name promptName) string {
	return field(chat.DefaultPrompts(), name)
}

// field reads one prompt out of a PromptConfig by name.
func field(cfg chat.PromptConfig, name promptName) string {
	switch name {
	case promptChatSystem:
		return cfg.ChatSystemPrompt
	case promptAnswerSystem:
		return cfg.AnswerSystemPrompt
	case promptSourceRules:
		return cfg.SourceRules
	}
	return ""
}

// setField writes one prompt into a PromptConfig by name.
func setField(cfg *chat.PromptConfig, name promptName, value string) {
	switch name {
	case promptChatSystem:
		cfg.ChatSystemPrompt = value
	case promptAnswerSystem:
		cfg.AnswerSystemPrompt = value
	case promptSourceRules:
		cfg.SourceRules = value
	}
}
