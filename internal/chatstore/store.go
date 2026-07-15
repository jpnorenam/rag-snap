// Package chatstore is a local, file-backed store of saved chat conversations.
// Each chat is one JSON file keyed by id under a directory, so a corrupt record
// can never take down the whole store and deletes are a single unlink. It has no
// dependency on the inference/OpenAI types: callers convert their in-memory
// history to the neutral Turn slice before saving.
//
// Two processes mount it at different directories under strict confinement: the
// daemon under $SNAP_COMMON/ragd/chats/ (shared by the UI and the CLI's remote
// mode) and the daemonless CLI under the user config dir. The implementation is
// identical; only the path differs.
package chatstore

import (
	"crypto/rand"
	"encoding/hex"
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
)

// Errors distinguishing the not-found and empty-transcript cases so callers can
// map them to 404 / a friendly rejection.
var (
	// ErrNotFound is returned by Get/Delete for an unknown id.
	ErrNotFound = errors.New("chat not found")
	// ErrEmpty is returned by Save when the transcript has no turns.
	ErrEmpty = errors.New("nothing to save: the conversation is empty")
	// ErrUnavailable is returned when the store directory could not be resolved,
	// so persistence is impossible while reads degrade to an empty store.
	ErrUnavailable = errors.New("chat store is unavailable: cannot persist chats")
)

// titleMaxRunes bounds a derived title so a long first prompt does not become an
// unwieldy title.
const titleMaxRunes = 60

// idPattern is the shape of a generated id; validated on lookup so an id taken
// from a request path can never escape the store directory.
var idPattern = regexp.MustCompile(`^[a-f0-9]{8,}$`)

// Turn is one exchange in a saved conversation: a user prompt or an assistant
// reply, as plain text. Reasoning/<think> spans are stripped from assistant
// content before it reaches the store.
type Turn struct {
	Role    string `json:"role"` // "user" or "assistant"
	Content string `json:"content"`
}

// Chat is a saved conversation with everything needed to resume it.
type Chat struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Model     string    `json:"model"`
	Bases     []string  `json:"bases"`
	Turns     []Turn    `json:"turns"`
}

// Summary is the transcript-free view returned by List: enough to render a
// history row and resume, without the full conversation.
type Summary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Model     string    `json:"model"`
	Bases     []string  `json:"bases"`
	TurnCount int       `json:"turn_count"`
}

// Store persists chats as one JSON file per id under dir. When dir is empty the
// store degrades to read-only-empty (List/Get succeed with nothing) and Save
// fails with ErrUnavailable, so a store that cannot be located never breaks chat.
type Store struct {
	mu  sync.Mutex
	dir string
}

// New builds a store rooted at dir. It does not touch the filesystem; the
// directory is created lazily on the first Save.
func New(dir string) *Store {
	return &Store{dir: dir}
}

// Save persists a chat and returns the stored record. When in.ID is empty a new
// id and created_at are assigned; otherwise the record is updated in place,
// preserving the original created_at (recreating it if the file is gone). The
// title is used verbatim when non-empty, else kept from the existing record, else
// derived from the first user turn. An empty transcript is rejected with ErrEmpty.
func (s *Store) Save(in Chat) (Chat, error) {
	if !hasTurns(in.Turns) {
		return Chat{}, ErrEmpty
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dir == "" {
		return Chat{}, ErrUnavailable
	}

	now := time.Now().UTC()
	out := in
	out.Turns = strippedTurns(in.Turns)

	var existing *Chat
	if in.ID != "" {
		if c, err := s.getLocked(in.ID); err == nil {
			existing = &c
		}
	}

	switch {
	case out.ID == "":
		id, err := newID()
		if err != nil {
			return Chat{}, err
		}
		out.ID = id
		out.CreatedAt = now
	case existing != nil:
		out.CreatedAt = existing.CreatedAt
	default:
		// Pinned id with no file behind it (e.g. the record was deleted): recreate.
		out.CreatedAt = now
	}
	out.UpdatedAt = now

	if strings.TrimSpace(out.Title) == "" {
		if existing != nil && existing.Title != "" {
			out.Title = existing.Title
		} else {
			out.Title = deriveTitle(out.Turns)
		}
	}

	if err := s.writeLocked(out); err != nil {
		return Chat{}, err
	}
	return out, nil
}

// List returns chat summaries newest-first by updated_at. When search is
// non-empty only chats whose title or any turn content contains it
// (case-insensitively) are returned. Files that fail to parse are logged and
// skipped rather than failing the whole listing.
func (s *Store) List(search string) ([]Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	chats, err := s.loadAllLocked()
	if err != nil {
		return nil, err
	}

	needle := strings.ToLower(strings.TrimSpace(search))
	summaries := make([]Summary, 0, len(chats))
	for _, c := range chats {
		if needle != "" && !matches(c, needle) {
			continue
		}
		summaries = append(summaries, summaryOf(c))
	}
	sort.SliceStable(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})
	return summaries, nil
}

// Get returns the full chat for id, or ErrNotFound.
func (s *Store) Get(id string) (Chat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getLocked(id)
}

// Delete removes the chat for id, or returns ErrNotFound.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dir == "" || !idPattern.MatchString(id) {
		return ErrNotFound
	}
	err := os.Remove(s.pathFor(id))
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	return err
}

// getLocked reads one chat. The caller must hold s.mu.
func (s *Store) getLocked(id string) (Chat, error) {
	if s.dir == "" || !idPattern.MatchString(id) {
		return Chat{}, ErrNotFound
	}
	data, err := os.ReadFile(s.pathFor(id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Chat{}, ErrNotFound
		}
		return Chat{}, err
	}
	var c Chat
	if err := json.Unmarshal(data, &c); err != nil {
		return Chat{}, fmt.Errorf("chat %s is not valid JSON: %w", id, err)
	}
	return c, nil
}

// loadAllLocked reads every chat file, skipping (and logging) unparseable ones.
// A missing directory is the normal empty-store case. The caller must hold s.mu.
func (s *Store) loadAllLocked() ([]Chat, error) {
	if s.dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	chats := make([]Chat, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			log.Printf("reading chat store file %s: %v; skipping", e.Name(), err)
			continue
		}
		var c Chat
		if err := json.Unmarshal(data, &c); err != nil {
			log.Printf("chat store file %s is not valid JSON: %v; skipping", e.Name(), err)
			continue
		}
		chats = append(chats, c)
	}
	return chats, nil
}

// writeLocked persists a chat atomically (temp file in the same directory,
// written owner-only, renamed over the target). The caller must hold s.mu.
func (s *Store) writeLocked(c Chat) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("creating chat store directory: %w", err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding chat: %w", err)
	}

	tmp, err := os.CreateTemp(s.dir, ".chat-*.json")
	if err != nil {
		return fmt.Errorf("creating temporary chat file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("securing temporary chat file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing chat: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing chat: %w", err)
	}
	if err := os.Rename(tmpName, s.pathFor(c.ID)); err != nil {
		return fmt.Errorf("replacing chat file: %w", err)
	}
	return nil
}

func (s *Store) pathFor(id string) string {
	return filepath.Join(s.dir, id+".json")
}

// StripThink removes <think>…</think> reasoning spans (including an unclosed
// trailing <think>) from s, collapsing the whitespace they leave behind. History
// only needs the final answer to continue a conversation.
func StripThink(s string) string {
	for {
		open := strings.Index(s, "<think>")
		if open < 0 {
			break
		}
		end := strings.Index(s[open:], "</think>")
		if end < 0 {
			// Unclosed: drop everything from the tag onward.
			s = s[:open]
			break
		}
		s = s[:open] + s[open+end+len("</think>"):]
	}
	return strings.TrimSpace(s)
}

// summaryOf projects a chat to its transcript-free summary.
func summaryOf(c Chat) Summary {
	return Summary{
		ID:        c.ID,
		Title:     c.Title,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
		Model:     c.Model,
		Bases:     c.Bases,
		TurnCount: len(c.Turns),
	}
}

// matches reports whether the chat's title or any turn content contains the
// (already lowercased) needle.
func matches(c Chat, needle string) bool {
	if strings.Contains(strings.ToLower(c.Title), needle) {
		return true
	}
	for _, t := range c.Turns {
		if strings.Contains(strings.ToLower(t.Content), needle) {
			return true
		}
	}
	return false
}

// deriveTitle builds a title from the first user turn: whitespace-collapsed and
// truncated. Falls back to a generic label when there is no user turn.
func deriveTitle(turns []Turn) string {
	for _, t := range turns {
		if t.Role != "user" {
			continue
		}
		title := strings.Join(strings.Fields(t.Content), " ")
		if title == "" {
			continue
		}
		return truncateRunes(title, titleMaxRunes)
	}
	return "Untitled chat"
}

func truncateRunes(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return strings.TrimSpace(string(r[:limit])) + "…"
}

// strippedTurns returns turns with <think> spans removed from assistant content.
func strippedTurns(turns []Turn) []Turn {
	out := make([]Turn, len(turns))
	for i, t := range turns {
		if t.Role == "assistant" {
			t.Content = StripThink(t.Content)
		}
		out[i] = t
	}
	return out
}

// hasTurns reports whether turns holds at least one non-empty turn.
func hasTurns(turns []Turn) bool {
	for _, t := range turns {
		if strings.TrimSpace(t.Content) != "" {
			return true
		}
	}
	return false
}

// newID returns a random opaque hex id.
func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generating chat id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
