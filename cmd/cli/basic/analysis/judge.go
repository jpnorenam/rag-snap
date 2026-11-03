package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
	"github.com/jpnorenam/rag-snap/pkg/storage"
	"google.golang.org/genai"
)

// Answer-judge configuration keys, seeded as `package` keys by the install hook and
// by post-refresh.
const (
	// ConfJudgeEnabled turns judging off for every run when false. It defaults to
	// true: the Gemini agreement covers sending answers and knowledge-base chunks,
	// and judging is meant to be the normal path.
	ConfJudgeEnabled = "judge.enabled"

	// ConfJudgeModel is the Gemini model that judges. Empty falls back to
	// analysis.model, so an install that already analyses needs nothing more.
	ConfJudgeModel = "judge.model"

	// ConfJudgeReasoning is the reasoning effort of each judge call.
	ConfJudgeReasoning = "judge.reasoning"

	// ConfJudgeConcurrency bounds the judge calls in flight at once. It is
	// configuration because the right value depends on the key's quota tier.
	ConfJudgeConcurrency = "judge.concurrency"
)

// DefaultJudgeConcurrency is conservative for paid-tier preview-model rate limits.
const DefaultJudgeConcurrency = 4

// judgeMaxOutputTokens is the per-answer ceiling. A verdict is a short object; the
// ceiling exists for the reasoning, which Gemini counts against it.
const judgeMaxOutputTokens = 16384

// consistencyMaxOutputTokens is the consistency call's ceiling. It reasons over a
// whole run, so it gets more room than one verdict.
const consistencyMaxOutputTokens = 32768

// judgeRetries is how many times a rate-limited or transient-server-error call is
// retried before its row is marked not judged.
const judgeRetries = 3

// judgeBackoff is the first retry delay, doubled per attempt. A variable so tests
// need not sleep.
var judgeBackoff = 2 * time.Second

// JudgeSettings is the resolved judge configuration. Like Settings, it carries no key.
type JudgeSettings struct {
	Enabled     bool
	Model       string
	Reasoning   Effort
	Concurrency int
}

// LoadJudgeSettings reads the judge keys, applying their defaults. It does not read
// the environment; ValidateJudge does that.
func LoadJudgeSettings(cfg storage.Config) (JudgeSettings, error) {
	s := JudgeSettings{
		Enabled:     configBool(cfg, ConfJudgeEnabled, true),
		Model:       configString(cfg, ConfJudgeModel),
		Concurrency: DefaultJudgeConcurrency,
	}
	if s.Model == "" {
		s.Model = configString(cfg, ConfModel)
	}

	var err error
	if s.Reasoning, err = loadEffort(cfg, ConfJudgeReasoning); err != nil {
		return JudgeSettings{}, err
	}
	if s.Reasoning == EffortUnset {
		s.Reasoning = EffortMedium
	}

	if raw := configString(cfg, ConfJudgeConcurrency); raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil || n <= 0 {
			return JudgeSettings{}, fmt.Errorf("config key %q must be a positive whole number, got %q", ConfJudgeConcurrency, raw)
		}
		s.Concurrency = n
	}
	return s, nil
}

// ValidateJudge reports whether judging can run, collecting every missing
// prerequisite. A disabled judge is reported as such, so callers can tell "off" from
// "on but unusable" with IsJudgeDisabled.
func ValidateJudge(s JudgeSettings) error {
	if !s.Enabled {
		return errJudgeDisabled
	}
	var reasons []string
	if s.Model == "" {
		reasons = append(reasons, fmt.Sprintf("no Gemini model — set %s, or %s for analysis and judging alike", ConfJudgeModel, ConfModel))
	}
	if resolveKey() == "" {
		reasons = append(reasons, fmt.Sprintf("no API key — set %s in the environment", envGeminiKey))
	}
	if len(reasons) > 0 {
		return &NotConfiguredError{Feature: "answer judging", Reasons: reasons}
	}
	return nil
}

var errJudgeDisabled = fmt.Errorf("answer judging is turned off (%s=false)", ConfJudgeEnabled)

// IsJudgeDisabled reports whether err says judging is turned off by configuration,
// which is a choice rather than a fault and needs no notice.
func IsJudgeDisabled(err error) bool { return errors.Is(err, errJudgeDisabled) }

// Judge assesses batch answers with Gemini. It implements chat.AnswerJudge.
type Judge struct {
	client   *Client
	settings JudgeSettings
}

var _ chat.AnswerJudge = (*Judge)(nil)

// NewJudge builds a judge from resolved settings, through the same pinned client
// analysis uses.
func NewJudge(s JudgeSettings, verbose bool) (*Judge, error) {
	return newJudge(s, geminiBaseURL, verbose)
}

func newJudge(s JudgeSettings, baseURL string, verbose bool) (*Judge, error) {
	if err := ValidateJudge(s); err != nil {
		return nil, err
	}
	api, err := newGenaiClient(baseURL)
	if err != nil {
		return nil, err
	}
	client := &Client{settings: Settings{Enabled: true, Model: s.Model}, api: api, verbose: verbose}
	return &Judge{client: client, settings: s}, nil
}

// Settings returns the resolved judge configuration. Safe to print.
func (j *Judge) Settings() JudgeSettings { return j.settings }

// Model is the model every judge call uses.
func (j *Judge) Model() string { return j.settings.Model }

// verdictResponse is the judge schema's shape.
type verdictResponse struct {
	Comment    string   `json:"comment"`
	Confidence int      `json:"confidence"`
	Issues     []string `json:"issues"`
}

// Judge assesses one answer against its own evidence.
func (j *Judge) Judge(ctx context.Context, in chat.JudgeInput) (chat.Judgment, error) {
	raw, err := j.withRetry(ctx, func() ([]byte, error) {
		raw, _, err := j.client.complete(ctx, "judge", judgeSystemPrompt, judgeParts(in),
			"answer_verdict", verdictSchema(), j.settings.Reasoning, judgeMaxOutputTokens)
		return raw, err
	})
	if err != nil {
		return chat.Judgment{}, err
	}
	var resp verdictResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return chat.Judgment{}, fmt.Errorf("reading the verdict: %w", err)
	}
	return validateVerdict(resp)
}

// validateVerdict turns a schema-valid response into a verdict, refusing anything
// the rubric does not allow rather than coercing it.
func validateVerdict(r verdictResponse) (chat.Judgment, error) {
	if r.Confidence < 1 || r.Confidence > 5 {
		return chat.Judgment{}, fmt.Errorf("the judge returned confidence %d, outside 1–5", r.Confidence)
	}
	comment := strings.TrimSpace(r.Comment)
	if r.Confidence < 5 && comment == "" {
		return chat.Judgment{}, fmt.Errorf("the judge scored %d without saying what is wrong", r.Confidence)
	}
	var issues []string
	for _, is := range r.Issues {
		if !slices.Contains(chat.JudgeIssues, is) {
			return chat.Judgment{}, fmt.Errorf("the judge returned unknown issue %q", is)
		}
		if !slices.Contains(issues, is) {
			issues = append(issues, is)
		}
	}
	if r.Confidence == 5 {
		// A perfect score carries nothing to act on, by definition of the rubric.
		comment, issues = "", nil
	}
	return chat.Judgment{
		Confidence: r.Confidence,
		Rating:     chat.RatingFor(r.Confidence),
		Issues:     issues,
		Comment:    comment,
	}, nil
}

// consistencyResponse is the consistency schema's shape.
type consistencyResponse struct {
	Findings []struct {
		Rows []struct {
			Row    int    `json:"row"`
			Phrase string `json:"phrase"`
		} `json:"rows"`
		Comment string `json:"comment"`
	} `json:"findings"`
}

// Consistency compares every answer of a run with the others. A row is kept in a
// finding only if it is in the run and its quoted phrase is in its answer; a finding
// left with fewer than two rows, or with no comment, is discarded and counted.
func (j *Judge) Consistency(ctx context.Context, systemPrompt string, answers []chat.ConsistencyInput) (chat.ConsistencyReport, error) {
	raw, err := j.withRetry(ctx, func() ([]byte, error) {
		raw, _, err := j.client.complete(ctx, "consistency", consistencySystemPrompt, consistencyParts(systemPrompt, answers),
			"answer_consistency", consistencySchema(), j.settings.Reasoning, consistencyMaxOutputTokens)
		return raw, err
	})
	if err != nil {
		return chat.ConsistencyReport{}, err
	}
	var resp consistencyResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return chat.ConsistencyReport{}, fmt.Errorf("reading the consistency findings: %w", err)
	}

	answerOf := make(map[int]string, len(answers))
	for _, a := range answers {
		answerOf[a.Row] = a.Answer
	}
	var out chat.ConsistencyReport
	for _, f := range resp.Findings {
		var rows []chat.ConsistencyRow
		for _, r := range f.Rows {
			answer, ok := answerOf[r.Row]
			if !ok || !quoteIn(r.Phrase, answer) ||
				slices.ContainsFunc(rows, func(x chat.ConsistencyRow) bool { return x.Row == r.Row }) {
				continue
			}
			rows = append(rows, chat.ConsistencyRow{Row: r.Row, Phrase: strings.TrimSpace(r.Phrase)})
		}
		comment := strings.TrimSpace(f.Comment)
		if len(rows) < 2 || comment == "" {
			out.Discarded++
			continue
		}
		out.Findings = append(out.Findings, chat.ConsistencyFinding{Rows: rows, Comment: comment})
	}
	return out, nil
}

// quoteIn reports whether a quoted phrase occurs in an answer, tolerating what models
// do to a quote: case, runs of whitespace, curly quotes, quotes and punctuation around
// it, and an ellipsis standing for an omitted middle, whose fragments must then occur
// in order.
func quoteIn(phrase, answer string) bool {
	text := normaliseQuote(answer)
	p := strings.ReplaceAll(normaliseQuote(phrase), "…", "...")
	found := false
	pos := 0
	for _, frag := range strings.Split(p, "...") {
		frag = strings.Trim(frag, quoteTrim)
		if frag == "" {
			continue
		}
		i := strings.Index(text[pos:], frag)
		if i < 0 {
			return false
		}
		pos += i + len(frag)
		found = true
	}
	return found
}

// quoteTrim is what is stripped from the ends of a quote or a quote fragment.
const quoteTrim = " \"'.,;:!?()[]"

var quoteReplacer = strings.NewReplacer("\u2018", "'", "\u2019", "'", "\u201c", "\"", "\u201d", "\"")

func normaliseQuote(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(quoteReplacer.Replace(s))), " ")
}

// withRetry retries a call that hit the rate limit or a transient server error, with
// jittered exponential backoff. Truncated, blocked and malformed responses are not
// retried: the same request would most likely fail the same way.
func (j *Judge) withRetry(ctx context.Context, call func() ([]byte, error)) ([]byte, error) {
	delay := judgeBackoff
	for attempt := 0; ; attempt++ {
		raw, err := call()
		if err == nil || attempt == judgeRetries || !retryable(err) {
			if err != nil && attempt > 0 && retryable(err) {
				err = fmt.Errorf("%w (after %d retries)", err, attempt)
			}
			return raw, err
		}
		wait := delay + time.Duration(rand.Int64N(int64(delay)/2+1))
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		delay *= 2
	}
}

func retryable(err error) bool {
	var quota *QuotaError
	if errors.As(err, &quota) {
		return true
	}
	var apiErr genai.APIError
	return errors.As(err, &apiErr) && apiErr.Code >= 500
}
