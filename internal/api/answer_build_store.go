package api

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/rfp"
)

// buildStageTTL is how long parsed tables from a spreadsheet/CSV build stay
// available for a follow-up POST /1.0/answer/build/extract before expiring.
const buildStageTTL = 10 * time.Minute

// maxStagedBuilds caps how many parsed-table sets are retained at once; the
// oldest is evicted when the cap is exceeded. Parsed rows are small relative to
// the uploaded document, so this is a modest bound, not a tight one.
const maxStagedBuilds = 32

// stagedBuild holds the parsed tables from one spreadsheet/CSV build, kept in
// memory between the inspect pass (POST /1.0/answer/build) and the extract pass
// (POST /1.0/answer/build/extract) so the document is parsed once and never
// re-uploaded. Client-side lifecycle only — never persisted.
type stagedBuild struct {
	format  string // "xlsx" | "csv"
	tables  []rfp.SheetTable
	created time.Time
	expires time.Time
}

// buildStore is the in-memory registry of staged builds keyed by an opaque
// token. It is safe for concurrent use and self-prunes expired entries.
type buildStore struct {
	mu     sync.Mutex
	builds map[string]*stagedBuild
	now    func() time.Time // injectable for tests
}

func newBuildStore() *buildStore {
	return &buildStore{builds: make(map[string]*stagedBuild), now: time.Now}
}

// stage records parsed tables under a fresh token, pruning expired entries and
// evicting the oldest when the cap is exceeded. Returns the token.
func (s *buildStore) stage(format string, tables []rfp.SheetTable) (string, error) {
	token, err := newBuildToken()
	if err != nil {
		return "", err
	}
	now := s.now()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	if len(s.builds) >= maxStagedBuilds {
		s.evictOldestLocked()
	}
	s.builds[token] = &stagedBuild{
		format:  format,
		tables:  tables,
		created: now,
		expires: now.Add(buildStageTTL),
	}
	return token, nil
}

// get returns the staged build for a token, or ok=false when the token is
// unknown or expired.
func (s *buildStore) get(token string) (*stagedBuild, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.builds[token]
	if !ok {
		return nil, false
	}
	if !s.now().Before(b.expires) {
		delete(s.builds, token)
		return nil, false
	}
	return b, true
}

// consume removes a token after a successful extract so a build token is not
// reused for repeated runs.
func (s *buildStore) consume(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.builds, token)
}

func (s *buildStore) pruneLocked(now time.Time) {
	for token, b := range s.builds {
		if !now.Before(b.expires) {
			delete(s.builds, token)
		}
	}
}

func (s *buildStore) evictOldestLocked() {
	var oldestToken string
	var oldest time.Time
	for token, b := range s.builds {
		if oldestToken == "" || b.created.Before(oldest) {
			oldestToken = token
			oldest = b.created
		}
	}
	if oldestToken != "" {
		delete(s.builds, oldestToken)
	}
}

func newBuildToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
