package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/canonical/go-snapctl/env"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
)

// promptsStoreRelDir is the prompt store root under $SNAP_COMMON, alongside the
// socket and the localhost token. $SNAP_COMMON (not $SNAP_DATA) so customized
// prompts survive a snap refresh and are not reverted with a revision rollback.
const promptsStoreRelDir = "ragd/prompts"

// legacyPromptsRelName is the single-file override store used before variants
// existed. It is migrated once into the per-variant layout and then renamed
// aside; it lives next to the new store directory under $SNAP_COMMON/ragd.
const legacyPromptsRelName = "prompts.json"

// promptName is one of the three fixed prompt slots.
type promptName string

const (
	promptChatSystem   promptName = "chat_system_prompt"
	promptAnswerSystem promptName = "answer_system_prompt"
	promptSourceRules  promptName = "source_rules"
)

// promptOrder is the canonical order prompts are listed in, matching the order
// of the CLI's `prompt init` select.
var promptOrder = []promptName{promptChatSystem, promptAnswerSystem, promptSourceRules}

// reservedVariant is the variant name that always denotes the built-in default:
// it can never be created, edited, or deleted.
const reservedVariant = "default"

// legacyCustomVariant is the name given to an override adopted through the
// back-compatible PUT path (and to a migrated legacy override) on a generation
// slot: it is an ordinary variant, just one the daemon may create on the user's
// behalf so the pre-variants edit flow keeps working unchanged.
const legacyCustomVariant = "custom"

// sourceRulesVariant is the single internal variant name backing the
// `source_rules` override. That slot exposes no variant endpoints — it is the
// grounding guardrail — but reuses the same versioned record machinery so it
// gains rollback without gaining personas.
const sourceRulesVariant = "override"

// variantNamePattern bounds a variant name to a safe, lowercase, path-segment
// shape; validated on lookup so a name taken from a request path can never
// escape the store directory (mirrors chatstore's id validation).
var variantNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// Errors distinguishing the failure modes so handlers can map them to statuses.
var (
	errUnknownPrompt  = errors.New("unknown prompt")
	errEmptyPrompt    = errors.New("prompt value is empty")
	errUnknownVariant = errors.New("unknown variant")
	errInvalidName    = errors.New("invalid variant name")
	errReservedName   = errors.New("reserved variant name")
	errVariantExists  = errors.New("variant already exists")
	errVariantActive  = errors.New("variant is active")
	errNoVariants     = errors.New("prompt does not support variants")
	errUnknownVersion = errors.New("unknown version")
)

// promptVersion is one immutable saved value in a variant's history.
type promptVersion struct {
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	Value     string    `json:"value"`
}

// variantRecord is the persisted form of one named variant (and of the
// `source_rules` override). Versions are append-only; the last is the head.
type variantRecord struct {
	Name      string          `json:"name"`
	Slot      string          `json:"slot"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
	Versions  []promptVersion `json:"versions"`
}

// head returns the record's latest version, or false when it has none.
func (r variantRecord) head() (promptVersion, bool) {
	if len(r.Versions) == 0 {
		return promptVersion{}, false
	}
	return r.Versions[len(r.Versions)-1], true
}

// activePointers maps a slot to its active variant name. An absent slot resolves
// to the built-in default. Persisted as active.json under the store root.
type activePointers map[string]string

// promptView is the client view of one slot: its effective value, the built-in
// default it falls back to, and whether an override is active. Generation slots
// additionally carry the active variant name and the stored variant names, so a
// client can render the selector without a second request.
type promptView struct {
	Name       string `json:"name"`
	Value      string `json:"value"`
	Default    string `json:"default"`
	Customized bool   `json:"customized"`
	// Active and Variants are populated for generation slots only (omitted for
	// source_rules, which has no user-facing variants).
	Active   string   `json:"active,omitempty"`
	Variants []string `json:"variants,omitempty"`
}

// variantSummary is the transcript-free view of a variant returned by the
// variants listing.
type variantSummary struct {
	Name      string    `json:"name"`
	UpdatedAt time.Time `json:"updated_at"`
	Versions  int       `json:"versions"`
	Active    bool      `json:"active"`
}

// variantView is the full view of one variant: its head value and version plus
// metadata.
type variantView struct {
	Name      string    `json:"name"`
	Slot      string    `json:"slot"`
	Value     string    `json:"value"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Active    bool      `json:"active"`
}

// promptStore is the daemon-owned store of prompt customizations, persisted as
// one JSON file per variant under $SNAP_COMMON/ragd/prompts/ (a corrupt file
// loses one variant, not the store). It stores *overrides only*: a slot with no
// active variant always resolves to the built-in default of the running release,
// so an improved default ships to users who never customized that slot.
//
// The CLI's own ~/.config/rag-cli/prompts.json is a different, client-local file
// the daemon cannot read (distinct $HOME under strict confinement); it remains
// the fallback for daemonless CLI runs only.
type promptStore struct {
	mu sync.Mutex
	// root is the store directory. It is empty when the path could not be
	// resolved, in which case the store degrades to read-only built-in defaults
	// rather than failing prompt resolution (so chat/answer keep working).
	root string
	// migrated guards the one-time legacy migration within a process. Across
	// restarts the migration is naturally idempotent (the legacy file is renamed
	// aside), so this only avoids re-statting.
	migrated bool
}

// newPromptStore resolves the store root, creating it. A resolution failure is
// logged and degrades the store to defaults-only.
func newPromptStore() *promptStore {
	root, err := promptsRoot()
	if err != nil {
		log.Printf("prompt store unavailable (%v); using built-in defaults", err)
		return &promptStore{}
	}
	return &promptStore{root: root}
}

// promptsRoot resolves the store root under $SNAP_COMMON (temp-dir fallback
// off-snap, as for the token) and ensures it exists.
func promptsRoot() (string, error) {
	base := env.SnapCommon()
	if base == "" {
		// Outside a snap (local dev / tests), fall back to a temp dir.
		base = os.TempDir()
	}
	root := filepath.Join(base, promptsStoreRelDir)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("creating prompt store directory: %w", err)
	}
	return root, nil
}

// resolve returns the effective prompt configuration: each slot's active variant
// head with unset slots filled in from the built-in defaults. This is what chat
// sessions and batch operations are seeded with when no explicit selection is
// made.
func (p *promptStore) resolve() chat.PromptConfig {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureMigratedLocked()

	active := p.loadActiveLocked()
	cfg := chat.DefaultPrompts()
	for _, slot := range promptOrder {
		value, _ := p.effectiveLocked(slot, active)
		setField(&cfg, slot, value)
	}
	return cfg
}

// resolveSlot resolves one generation slot honouring an explicit variant
// selection over the active pointer. An empty selection uses the active pointer
// (or the built-in default); the reserved name "default" forces the built-in
// default regardless of the active pointer. A named selection that does not
// exist returns errUnknownVariant. ref is the provenance reference
// ("name@version", or empty for the built-in default).
func (p *promptStore) resolveSlot(slot promptName, selection string) (value, ref string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureMigratedLocked()

	if selection == "" {
		active := p.loadActiveLocked()
		value, ref = p.effectiveLocked(slot, active)
		return value, ref, nil
	}
	if selection == reservedVariant {
		// "default" is not a stored variant; it explicitly selects the built-in
		// default for this session, overriding whatever the slot has active.
		return defaultOf(slot), "", nil
	}
	if !validVariantName(selection) {
		return "", "", errUnknownVariant
	}
	rec, ok := p.loadRecordLocked(slot, selection)
	if !ok {
		return "", "", errUnknownVariant
	}
	head, ok := rec.head()
	if !ok {
		return "", "", errUnknownVariant
	}
	return head.Value, provenanceRef(selection, head.Version), nil
}

// views returns all three slots in the canonical order.
func (p *promptStore) views() []promptView {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureMigratedLocked()

	active := p.loadActiveLocked()
	views := make([]promptView, 0, len(promptOrder))
	for _, slot := range promptOrder {
		views = append(views, p.viewLocked(slot, active))
	}
	return views
}

// view returns a single slot, or errUnknownPrompt if the name is not one of the
// three slots.
func (p *promptStore) view(name promptName) (promptView, error) {
	if !validPrompt(name) {
		return promptView{}, errUnknownPrompt
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureMigratedLocked()
	return p.viewLocked(name, p.loadActiveLocked()), nil
}

// set writes a value through the slot's current selection, preserving the
// pre-variants edit semantics: a new version on the active variant when one is
// active; the versioned override for source_rules; and, when the built-in
// default is active on a generation slot, the creation and activation of a
// `custom` variant. An empty value is rejected (reset is an explicit DELETE). A
// value equal to the built-in default clears the legacy `custom` variant or the
// source_rules override instead of storing a copy, so the customized flag never
// lies.
func (p *promptStore) set(name promptName, value string) (promptView, error) {
	if !validPrompt(name) {
		return promptView{}, errUnknownPrompt
	}
	if strings.TrimSpace(value) == "" {
		return promptView{}, errEmptyPrompt
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureMigratedLocked()

	active := p.loadActiveLocked()
	cur := active[string(name)]
	def := defaultOf(name)

	if name == promptSourceRules {
		if value == def {
			// Clear the override → built-in default (history preserved on disk).
			delete(active, string(name))
			if err := p.saveActiveLocked(active); err != nil {
				return promptView{}, err
			}
			return p.viewLocked(name, active), nil
		}
		if err := p.appendVersionLocked(name, sourceRulesVariant, value); err != nil {
			return promptView{}, err
		}
		active[string(name)] = sourceRulesVariant
		if err := p.saveActiveLocked(active); err != nil {
			return promptView{}, err
		}
		return p.viewLocked(name, active), nil
	}

	// Generation slot.
	switch {
	case cur == "":
		if value == def {
			return p.viewLocked(name, active), nil // Already default: no-op.
		}
		if err := p.appendVersionLocked(name, legacyCustomVariant, value); err != nil {
			return promptView{}, err
		}
		active[string(name)] = legacyCustomVariant
	case cur == legacyCustomVariant && value == def:
		// The magic override put back to the default clears rather than storing a
		// default-valued version.
		if err := p.deleteRecordLocked(name, legacyCustomVariant); err != nil {
			return promptView{}, err
		}
		delete(active, string(name))
	default:
		// Write through to the active variant.
		if err := p.appendVersionLocked(name, cur, value); err != nil {
			return promptView{}, err
		}
	}
	if err := p.saveActiveLocked(active); err != nil {
		return promptView{}, err
	}
	return p.viewLocked(name, active), nil
}

// reset returns a slot to the built-in default: a generation slot's active
// pointer is cleared (stored variants preserved); source_rules' override is
// cleared (its history preserved on disk). Resetting a slot already at its
// default is a no-op.
func (p *promptStore) reset(name promptName) (promptView, error) {
	if !validPrompt(name) {
		return promptView{}, errUnknownPrompt
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureMigratedLocked()

	active := p.loadActiveLocked()
	if active[string(name)] == "" {
		return p.viewLocked(name, active), nil // Already default.
	}
	delete(active, string(name))
	if err := p.saveActiveLocked(active); err != nil {
		return promptView{}, err
	}
	return p.viewLocked(name, active), nil
}

// listVariants returns the summaries of a generation slot's variants in name
// order. A non-generation slot returns errNoVariants; an unknown slot returns
// errUnknownPrompt.
func (p *promptStore) listVariants(slot promptName) ([]variantSummary, error) {
	if err := p.requireGenerationSlot(slot); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureMigratedLocked()

	active := p.loadActiveLocked()
	names := p.listVariantNamesLocked(slot)
	out := make([]variantSummary, 0, len(names))
	for _, n := range names {
		rec, ok := p.loadRecordLocked(slot, n)
		if !ok {
			continue
		}
		out = append(out, variantSummary{
			Name:      n,
			UpdatedAt: rec.UpdatedAt,
			Versions:  len(rec.Versions),
			Active:    active[string(slot)] == n,
		})
	}
	return out, nil
}

// createVariant stores a new variant from an initial value, failing with
// errVariantExists if the name is taken. It does not activate the variant.
func (p *promptStore) createVariant(slot promptName, name, value string) (variantView, error) {
	if err := p.requireGenerationSlot(slot); err != nil {
		return variantView{}, err
	}
	if err := validateNewVariantName(name); err != nil {
		return variantView{}, err
	}
	if strings.TrimSpace(value) == "" {
		return variantView{}, errEmptyPrompt
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureMigratedLocked()

	if _, ok := p.loadRecordLocked(slot, name); ok {
		return variantView{}, errVariantExists
	}
	if err := p.appendVersionLocked(slot, name, value); err != nil {
		return variantView{}, err
	}
	return p.variantViewLocked(slot, name, p.loadActiveLocked())
}

// getVariant returns one variant's head value and metadata.
func (p *promptStore) getVariant(slot promptName, name string) (variantView, error) {
	if err := p.requireGenerationSlot(slot); err != nil {
		return variantView{}, err
	}
	if !validVariantName(name) {
		return variantView{}, errUnknownVariant
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureMigratedLocked()

	if _, ok := p.loadRecordLocked(slot, name); !ok {
		return variantView{}, errUnknownVariant
	}
	return p.variantViewLocked(slot, name, p.loadActiveLocked())
}

// saveVariant appends a new version to a variant, creating it if absent
// (upsert). A value byte-identical to the head is a no-op. An empty value is
// rejected.
func (p *promptStore) saveVariant(slot promptName, name, value string) (variantView, error) {
	if err := p.requireGenerationSlot(slot); err != nil {
		return variantView{}, err
	}
	if err := validateNewVariantName(name); err != nil {
		return variantView{}, err
	}
	if strings.TrimSpace(value) == "" {
		return variantView{}, errEmptyPrompt
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureMigratedLocked()

	if err := p.appendVersionLocked(slot, name, value); err != nil {
		return variantView{}, err
	}
	return p.variantViewLocked(slot, name, p.loadActiveLocked())
}

// deleteVariant removes a variant and its history. The active variant cannot be
// deleted (errVariantActive) — the client must activate another selection first.
func (p *promptStore) deleteVariant(slot promptName, name string) error {
	if err := p.requireGenerationSlot(slot); err != nil {
		return err
	}
	if !validVariantName(name) {
		return errUnknownVariant
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureMigratedLocked()

	if p.loadActiveLocked()[string(slot)] == name {
		return errVariantActive
	}
	if _, ok := p.loadRecordLocked(slot, name); !ok {
		return errUnknownVariant
	}
	return p.deleteRecordLocked(slot, name)
}

// versions returns a variant's full, ordered version history.
func (p *promptStore) versions(slot promptName, name string) ([]promptVersion, error) {
	if err := p.requireGenerationSlot(slot); err != nil {
		return nil, err
	}
	if !validVariantName(name) {
		return nil, errUnknownVariant
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureMigratedLocked()

	rec, ok := p.loadRecordLocked(slot, name)
	if !ok {
		return nil, errUnknownVariant
	}
	return rec.Versions, nil
}

// restoreVersion appends a new head version carrying an earlier version's
// content (history stays linear and immutable). Restoring the current head is a
// no-op. An unknown version number returns errUnknownVersion.
func (p *promptStore) restoreVersion(slot promptName, name string, version int) (variantView, error) {
	if err := p.requireGenerationSlot(slot); err != nil {
		return variantView{}, err
	}
	if !validVariantName(name) {
		return variantView{}, errUnknownVariant
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureMigratedLocked()

	rec, ok := p.loadRecordLocked(slot, name)
	if !ok {
		return variantView{}, errUnknownVariant
	}
	var value string
	found := false
	for _, v := range rec.Versions {
		if v.Version == version {
			value = v.Value
			found = true
			break
		}
	}
	if !found {
		return variantView{}, errUnknownVersion
	}
	// appendVersionLocked no-ops when value equals the head, so restoring the
	// current head naturally does nothing.
	if err := p.appendVersionLocked(slot, name, value); err != nil {
		return variantView{}, err
	}
	return p.variantViewLocked(slot, name, p.loadActiveLocked())
}

// activate points a generation slot's active pointer at a variant, or at the
// built-in default when name is empty. Activating an unknown variant returns
// errUnknownVariant and leaves the pointer unchanged.
func (p *promptStore) activate(slot promptName, name string) (promptView, error) {
	if err := p.requireGenerationSlot(slot); err != nil {
		return promptView{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureMigratedLocked()

	active := p.loadActiveLocked()
	if name == "" || name == reservedVariant {
		delete(active, string(slot))
	} else {
		if !validVariantName(name) {
			return promptView{}, errUnknownVariant
		}
		if _, ok := p.loadRecordLocked(slot, name); !ok {
			return promptView{}, errUnknownVariant
		}
		active[string(slot)] = name
	}
	if err := p.saveActiveLocked(active); err != nil {
		return promptView{}, err
	}
	return p.viewLocked(slot, active), nil
}

// --- locked helpers -------------------------------------------------------

// effectiveLocked returns a slot's effective value and provenance ref given the
// active pointers. A dangling pointer (variant deleted out from under it)
// degrades to the built-in default.
//
// The caller must hold p.mu.
func (p *promptStore) effectiveLocked(slot promptName, active activePointers) (value, ref string) {
	name := active[string(slot)]
	if name == "" {
		return defaultOf(slot), ""
	}
	rec, ok := p.loadRecordLocked(slot, name)
	if !ok {
		return defaultOf(slot), ""
	}
	head, ok := rec.head()
	if !ok {
		return defaultOf(slot), ""
	}
	return head.Value, provenanceRef(name, head.Version)
}

// viewLocked builds the view of one slot from the active pointers.
//
// The caller must hold p.mu.
func (p *promptStore) viewLocked(slot promptName, active activePointers) promptView {
	def := defaultOf(slot)
	value, _ := p.effectiveLocked(slot, active)
	v := promptView{
		Name:       string(slot),
		Value:      value,
		Default:    def,
		Customized: active[string(slot)] != "",
	}
	if isGenerationSlot(slot) {
		v.Active = active[string(slot)]
		v.Variants = p.listVariantNamesLocked(slot)
	}
	return v
}

// variantViewLocked builds the full view of one variant. The variant must exist.
//
// The caller must hold p.mu.
func (p *promptStore) variantViewLocked(slot promptName, name string, active activePointers) (variantView, error) {
	rec, ok := p.loadRecordLocked(slot, name)
	if !ok {
		return variantView{}, errUnknownVariant
	}
	head, ok := rec.head()
	if !ok {
		return variantView{}, errUnknownVariant
	}
	return variantView{
		Name:      name,
		Slot:      string(slot),
		Value:     head.Value,
		Version:   head.Version,
		CreatedAt: rec.CreatedAt,
		UpdatedAt: rec.UpdatedAt,
		Active:    active[string(slot)] == name,
	}, nil
}

// appendVersionLocked appends value as a new version of a variant, creating the
// record if absent. A value byte-identical to the head is a no-op.
//
// The caller must hold p.mu.
func (p *promptStore) appendVersionLocked(slot promptName, name, value string) error {
	if p.root == "" {
		return errors.New("prompt store is unavailable: cannot persist prompts")
	}
	now := time.Now().UTC()
	rec, ok := p.loadRecordLocked(slot, name)
	if !ok {
		rec = variantRecord{Name: name, Slot: string(slot), CreatedAt: now}
	}
	if head, ok := rec.head(); ok && head.Value == value {
		return nil // Identical head: no version spam.
	}
	next := 1
	if head, ok := rec.head(); ok {
		next = head.Version + 1
	}
	rec.Versions = append(rec.Versions, promptVersion{Version: next, CreatedAt: now, Value: value})
	rec.UpdatedAt = now
	return writeJSONAtomic(p.recordPath(slot, name), rec)
}

// loadActiveLocked reads the active pointers. A missing or unparseable file
// means "all defaults" rather than a failure.
//
// The caller must hold p.mu.
func (p *promptStore) loadActiveLocked() activePointers {
	ap := activePointers{}
	if p.root == "" {
		return ap
	}
	data, err := os.ReadFile(filepath.Join(p.root, "active.json"))
	if err != nil {
		return ap
	}
	if err := json.Unmarshal(data, &ap); err != nil {
		log.Printf("prompt active pointers are not valid JSON: %v; using built-in defaults", err)
		return activePointers{}
	}
	return ap
}

// saveActiveLocked persists the active pointers atomically.
//
// The caller must hold p.mu.
func (p *promptStore) saveActiveLocked(active activePointers) error {
	if p.root == "" {
		return errors.New("prompt store is unavailable: cannot persist prompts")
	}
	return writeJSONAtomic(filepath.Join(p.root, "active.json"), active)
}

// loadRecordLocked reads one variant record. A missing file is the normal
// uncustomized case. An unparseable file is logged and treated as absent, so one
// corrupt record degrades alone without taking the store down.
//
// The caller must hold p.mu.
func (p *promptStore) loadRecordLocked(slot promptName, name string) (variantRecord, bool) {
	if p.root == "" {
		return variantRecord{}, false
	}
	data, err := os.ReadFile(p.recordPath(slot, name))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("reading prompt variant %s/%s: %v; treating as absent", slot, name, err)
		}
		return variantRecord{}, false
	}
	var rec variantRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		log.Printf("prompt variant %s/%s is not valid JSON: %v; treating as absent", slot, name, err)
		return variantRecord{}, false
	}
	return rec, true
}

// deleteRecordLocked removes a variant record file.
//
// The caller must hold p.mu.
func (p *promptStore) deleteRecordLocked(slot promptName, name string) error {
	if p.root == "" {
		return nil
	}
	err := os.Remove(p.recordPath(slot, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// listVariantNamesLocked lists the stored variant names of a slot, in order.
//
// The caller must hold p.mu.
func (p *promptStore) listVariantNamesLocked(slot promptName) []string {
	if p.root == "" {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(p.root, string(slot)))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		name, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// recordPath is the on-disk path of a variant record. Names are validated before
// they reach here, so no path traversal is possible.
func (p *promptStore) recordPath(slot promptName, name string) string {
	return filepath.Join(p.root, string(slot), name+".json")
}

// ensureMigratedLocked migrates a legacy single-file override store into the
// per-variant layout exactly once. Each non-empty override becomes version 1 of
// a variant (a `custom` variant for a generation slot, the `override` for
// source_rules) and is activated. The legacy file is then renamed aside so the
// migration never re-runs and its content stays recoverable. Any failure is
// logged and degrades to whatever was migrated so far — never a daemon failure.
//
// The caller must hold p.mu.
func (p *promptStore) ensureMigratedLocked() {
	if p.migrated || p.root == "" {
		return
	}
	p.migrated = true

	legacyPath := filepath.Join(filepath.Dir(p.root), legacyPromptsRelName)
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		return // Nothing to migrate.
	}

	var old chat.PromptConfig
	if err := json.Unmarshal(data, &old); err != nil {
		log.Printf("legacy prompt store %s is not valid JSON: %v; skipping migration", legacyPath, err)
		p.renameLegacyAside(legacyPath)
		return
	}

	active := p.loadActiveLocked()
	migrate := func(slot promptName, value, variant string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		if err := p.appendVersionLocked(slot, variant, value); err != nil {
			log.Printf("migrating legacy %s override: %v; skipping", slot, err)
			return
		}
		active[string(slot)] = variant
	}
	migrate(promptChatSystem, old.ChatSystemPrompt, legacyCustomVariant)
	migrate(promptAnswerSystem, old.AnswerSystemPrompt, legacyCustomVariant)
	migrate(promptSourceRules, old.SourceRules, sourceRulesVariant)

	if err := p.saveActiveLocked(active); err != nil {
		log.Printf("saving migrated active pointers: %v", err)
		return
	}
	p.renameLegacyAside(legacyPath)
}

// renameLegacyAside moves the migrated legacy file out of the way so migration
// never re-runs; the pre-migration content remains recoverable at .migrated.
func (p *promptStore) renameLegacyAside(legacyPath string) {
	if err := os.Rename(legacyPath, legacyPath+".migrated"); err != nil {
		log.Printf("renaming migrated legacy prompt store: %v", err)
	}
}

// requireGenerationSlot rejects a non-generation slot: an unknown name is
// errUnknownPrompt, source_rules is errNoVariants (it has no user variants).
func (p *promptStore) requireGenerationSlot(slot promptName) error {
	if !validPrompt(slot) {
		return errUnknownPrompt
	}
	if !isGenerationSlot(slot) {
		return errNoVariants
	}
	return nil
}

// --- free helpers ---------------------------------------------------------

// writeJSONAtomic writes v as indented JSON to path via a temp file in the same
// directory renamed over the target, so a crash mid-write never leaves a torn
// file. The parent directory is created if needed.
func writeJSONAtomic(path string, v any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating prompt store directory: %w", err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding prompts: %w", err)
	}

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
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replacing prompt file: %w", err)
	}
	return nil
}

// provenanceRef formats a variant reference for provenance records, or "" for
// the built-in default.
func provenanceRef(name string, version int) string {
	if name == "" {
		return ""
	}
	return fmt.Sprintf("%s@%d", name, version)
}

// validPrompt reports whether name is one of the three slots.
func validPrompt(name promptName) bool {
	for _, n := range promptOrder {
		if n == name {
			return true
		}
	}
	return false
}

// isGenerationSlot reports whether the slot supports user-facing variants (the
// two generation prompts; not the source_rules guardrail).
func isGenerationSlot(name promptName) bool {
	return name == promptChatSystem || name == promptAnswerSystem
}

// validVariantName reports whether name is a syntactically valid variant name.
func validVariantName(name string) bool {
	return variantNamePattern.MatchString(name)
}

// validateNewVariantName rejects the reserved name and syntactically invalid
// names for a create/save.
func validateNewVariantName(name string) error {
	if name == reservedVariant {
		return errReservedName
	}
	if !validVariantName(name) {
		return errInvalidName
	}
	return nil
}

// promptNames lists the valid prompt slot names, for error messages.
func promptNames() string {
	names := make([]string, len(promptOrder))
	for i, n := range promptOrder {
		names[i] = string(n)
	}
	return strings.Join(names, ", ")
}

// defaultOf returns a slot's built-in default text.
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
