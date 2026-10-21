package api

import (
	"fmt"
	"sync"
	"time"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/analysis"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
)

// analysisStageTTL is how long a conveyed document set stays available for a follow-up
// generation call before expiring.
//
// It is shorter than a person might expect from a wizard because the staged entry holds
// the document content itself, not a reference to it — see stagedAnalysis. Ten minutes
// matches the build store, which is long enough to review a triage and answer three
// questions.
const analysisStageTTL = 10 * time.Minute

// Bounds on what the store will hold.
//
// These exist because a base64-encoded PDF is roughly a third larger than the file, and
// the daemon keeps it resident for the whole session. Unlike the build store — which
// holds parsed rows, small relative to the upload — this one is a real memory consumer,
// so it is bounded by bytes rather than only by entry count, and a request that would
// breach the total is rejected before anything is conveyed.
const (
	maxStagedAnalyses      = 8
	maxAnalysisRequestSize = 48 << 20  // per request, matching the converter's default
	maxAnalysisTotalSize   = 192 << 20 // across all live sessions
)

// stagedAnalysis holds one conveyed document set between the triage pass and the
// generation pass.
//
// The content is held rather than referenced because conveyance produces base64 PDF
// bytes and converted Markdown, not a remote file handle: there is no server-side upload
// to point at. Re-conveying on the second call was rejected because on this path the
// upload arrived over the API and was never on disk, so it cannot be re-read.
type stagedAnalysis struct {
	conveyance *analysis.Conveyance
	triage     *analysis.Triage
	catalogue  *analysis.Catalogue
	size       int64
	created    time.Time
	expires    time.Time

	// instructions are the operator's analysis instructions, kept so generation applies
	// the same ones triage did without the client sending them again.
	instructions string

	// shortfall is the most recent generation's shortfall, retained so the operator can
	// accept the questions it produced — or re-run the extraction — without re-uploading.
	// It is nil until a generation falls short.
	shortfall *stagedShortfall
}

// stagedShortfall is what a shortfall left behind for the operator's decision.
type stagedShortfall struct {
	questions []chat.BatchQuestion
	expected  int
	produced  int
	effort    analysis.Effort
	size      int64
}

// questionsSize approximates the bytes a question list holds, for the store's bound.
func questionsSize(qs []chat.BatchQuestion) int64 {
	var n int64
	for _, q := range qs {
		n += int64(len(q.ID) + len(q.Question) + len(q.Source))
		for _, k := range q.Keywords {
			n += int64(len(k))
		}
	}
	return n
}

// analysisStore is the in-memory registry of staged analyses keyed by an opaque token.
// Safe for concurrent use, self-pruning, never persisted.
type analysisStore struct {
	mu      sync.Mutex
	entries map[string]*stagedAnalysis
	total   int64
	now     func() time.Time // injectable for tests
}

func newAnalysisStore() *analysisStore {
	return &analysisStore{entries: make(map[string]*stagedAnalysis), now: time.Now}
}

// checkRequestSize reports whether a conveyed set is small enough to accept, so an
// over-large upload is refused before it is conveyed rather than after.
func checkAnalysisRequestSize(size int64) error {
	if size > maxAnalysisRequestSize {
		return fmt.Errorf("the documents total %s, above the %s limit for one analysis request",
			humanSize(size), humanSize(maxAnalysisRequestSize))
	}
	return nil
}

// stage records a conveyed set under a fresh token.
//
// It prunes expired entries and evicts oldest-first until both the entry cap and the
// total-bytes cap are satisfied, then rejects the request if it still does not fit —
// which can only happen for a request larger than the whole budget.
func (s *analysisStore) stage(conv *analysis.Conveyance, triage *analysis.Triage, cat *analysis.Catalogue) (string, error) {
	return s.stageWith(conv, triage, cat, "")
}

// stageWith is stage with the operator's instructions, which count against the store's
// byte bound like the documents.
func (s *analysisStore) stageWith(conv *analysis.Conveyance, triage *analysis.Triage, cat *analysis.Catalogue, instructions string) (string, error) {
	size := conv.TotalBytes() + int64(len(instructions))
	if err := checkAnalysisRequestSize(size); err != nil {
		return "", err
	}

	token, err := newBuildToken()
	if err != nil {
		return "", err
	}
	now := s.now()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)

	for len(s.entries) >= maxStagedAnalyses || (s.total+size > maxAnalysisTotalSize && len(s.entries) > 0) {
		s.evictOldestLocked()
	}
	if s.total+size > maxAnalysisTotalSize {
		return "", fmt.Errorf("the documents total %s, which does not fit the %s the daemon holds for analysis sessions",
			humanSize(size), humanSize(maxAnalysisTotalSize))
	}

	s.entries[token] = &stagedAnalysis{
		conveyance:   conv,
		triage:       triage,
		catalogue:    cat,
		instructions: instructions,
		size:         size,
		created:      now,
		expires:      now.Add(analysisStageTTL),
	}
	s.total += size
	return token, nil
}

// get returns the staged analysis for a token, or ok=false when the token is unknown or
// expired.
func (s *analysisStore) get(token string) (*stagedAnalysis, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[token]
	if !ok {
		return nil, false
	}
	if !s.now().Before(e.expires) {
		s.removeLocked(token)
		return nil, false
	}
	return e, true
}

// retainShortfall records a shortfall on a live session, replacing any earlier one. The
// produced questions are counted against the store's byte bound like the documents.
// It reports false when the session has expired meanwhile, in which case there is
// nothing to decide and the client will be told the session expired.
func (s *analysisStore) retainShortfall(token string, sf *analysis.ShortfallError) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[token]
	if !ok || !s.now().Before(e.expires) {
		return false
	}
	retained := &stagedShortfall{
		questions: sf.Questions,
		expected:  sf.Expected,
		produced:  sf.Produced,
		effort:    sf.Effort,
		size:      questionsSize(sf.Questions),
	}
	delta := retained.size
	if e.shortfall != nil {
		delta -= e.shortfall.size
	}
	e.shortfall = retained
	e.size += delta
	s.total += delta
	return true
}

// shortfallFor returns the shortfall retained on a session, if any.
func (s *analysisStore) shortfallFor(token string) (*stagedShortfall, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[token]
	if !ok || e.shortfall == nil {
		return nil, false
	}
	return e.shortfall, true
}

// consume releases a token and the memory it held. Generation is the end of the
// session, so the documents are dropped rather than left resident for a TTL.
func (s *analysisStore) consume(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeLocked(token)
}

// expiresAt returns the expiry of a staged entry, for reporting to a client so it can
// tell the operator their session has a lifetime.
func (s *analysisStore) expiresAt(token string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[token]
	if !ok {
		return time.Time{}, false
	}
	return e.expires, true
}

func (s *analysisStore) removeLocked(token string) {
	if e, ok := s.entries[token]; ok {
		s.total -= e.size
		if s.total < 0 {
			s.total = 0
		}
		delete(s.entries, token)
	}
}

func (s *analysisStore) pruneLocked(now time.Time) {
	for token, e := range s.entries {
		if !now.Before(e.expires) {
			s.removeLocked(token)
			_ = e
		}
	}
}

func (s *analysisStore) evictOldestLocked() {
	var oldestToken string
	var oldest time.Time
	for token, e := range s.entries {
		if oldestToken == "" || e.created.Before(oldest) {
			oldestToken = token
			oldest = e.created
		}
	}
	if oldestToken != "" {
		s.removeLocked(oldestToken)
	}
}

// humanSize renders a byte count for an operator-facing message.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	for _, u := range []string{"KiB", "MiB", "GiB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, u)
		}
	}
	return fmt.Sprintf("%.1f TiB", value/unit)
}
