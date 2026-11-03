package chat

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// Rating is the reviewer-facing bucket a verdict falls into: green answers can be
// pasted as they are, yellow ones need an edit or a check, red ones are wrong or need
// a human answer. It is always derived from the confidence score, never asked of the
// model, so the two cannot disagree.
type Rating string

// The three ratings, from paste-as-is to needs-a-human.
const (
	RatingGreen  Rating = "green"
	RatingYellow Rating = "yellow"
	RatingRed    Rating = "red"
)

// RatingFor maps an anchored 1–5 confidence score onto its rating.
func RatingFor(confidence int) Rating {
	switch {
	case confidence >= 5:
		return RatingGreen
	case confidence >= 3:
		return RatingYellow
	default:
		return RatingRed
	}
}

// The issue categories a verdict may carry. They exist for filtering; the comment is
// what tells the reviewer what to do.
const (
	IssueUnsupportedClaim   = "unsupported_claim"
	IssueContradictsContext = "contradicts_context"
	IssueMissedContext      = "missed_context"
	IssueIncomplete         = "incomplete"
	IssueOffQuestion        = "off_question"
	IssueOtherCustomer      = "other_customer"
	IssueRuleViolation      = "rule_violation"
	IssueGap                = "gap"
)

// JudgeIssues is every category, in the order they are documented.
var JudgeIssues = []string{
	IssueUnsupportedClaim, IssueContradictsContext, IssueMissedContext, IssueIncomplete,
	IssueOffQuestion, IssueOtherCustomer, IssueRuleViolation, IssueGap,
}

// Judgment is the judge's verdict on one answer.
//
// A row the judge could not assess carries only Error: no confidence and no rating,
// so "not judged" can never be mistaken for a score.
type Judgment struct {
	Confidence int      `json:"confidence,omitempty"`
	Rating     Rating   `json:"rating,omitempty"`
	Issues     []string `json:"issues,omitempty"`
	Comment    string   `json:"comment,omitempty"`
	Error      string   `json:"error,omitempty"`

	// PreConsistencyConfidence is the confidence before the consistency pass lowered
	// it, set only when it did, so the pass's effect on a row can be seen and undone.
	PreConsistencyConfidence int `json:"pre_consistency_confidence,omitempty"`
}

// Judged reports whether the verdict carries a score.
func (j Judgment) Judged() bool { return j.Error == "" && j.Confidence > 0 }

// JudgeSummary is the run-level account of judging: which model judged, how many rows
// landed in each rating, and whether the cross-answer consistency pass completed.
type JudgeSummary struct {
	Model            string `json:"model"`
	Green            int    `json:"green"`
	Yellow           int    `json:"yellow"`
	Red              int    `json:"red"`
	NotJudged        int    `json:"not_judged"`
	Consistency      bool   `json:"consistency"`
	ConsistencyError string `json:"consistency_error,omitempty"`

	// ConsistencyFindings is every finding the pass kept, recorded once here rather
	// than copied into every row it names. ConsistencyDiscarded counts the findings
	// dropped because their quotes did not check out against the answers.
	ConsistencyFindings  []ConsistencyFinding `json:"consistency_findings,omitempty"`
	ConsistencyDiscarded int                  `json:"consistency_discarded,omitempty"`
}

// JudgeInput is everything the judge reads to assess one answer: the rules the
// answer had to follow, the question it answers, and the exact context it was
// grounded on. Words and Sentences are counted locally and given to the judge as
// facts, since models count badly.
type JudgeInput struct {
	Row           int
	ID            string
	Question      string
	Answer        string
	DomainContext string
	SystemPrompt  string
	Evidence      string
	Words         int
	// Sentences excludes Opener, a short declaration such as "Supported." that a
	// prompt may require before the answer proper. Counting it would push every
	// answer that obeys "open with Supported. … at most five sentences" one over.
	Sentences int
	Opener    string
}

// ConsistencyInput is one answer as the consistency pass sees it — without its
// evidence, which would not fit for a whole run and is not what the pass looks at.
type ConsistencyInput struct {
	Row      int
	ID       string
	Question string
	Answer   string
}

// ConsistencyRow is one side of a contradiction: a row and the phrase in its answer
// that conflicts.
type ConsistencyRow struct {
	Row    int    `json:"row"`
	Phrase string `json:"phrase"`
}

// ConsistencyFinding is one cross-answer problem, naming the rows it involves and
// quoting each. Broad marks a finding over more rows than a contradiction plausibly
// spans: it is kept for the reviewer to read once but never applied to rows.
type ConsistencyFinding struct {
	Rows    []ConsistencyRow `json:"rows"`
	Comment string           `json:"comment"`
	Broad   bool             `json:"broad,omitempty"`
}

// ConsistencyReport is what the consistency pass returns: the findings whose quotes
// were verified against the answers, and how many were discarded because they were not.
type ConsistencyReport struct {
	Findings  []ConsistencyFinding
	Discarded int
}

// ConsistencyBroadRows is the most rows a finding may name and still be applied to
// them. Real contradictions in reviewed runs spanned two or three rows; a remark over
// hundreds is not a contradiction, and applying it would cap every row it names.
const ConsistencyBroadRows = 8

// AnswerJudge assesses batch answers. The Gemini implementation lives in package
// analysis, which already depends on this package; RunBatch sees only this seam.
// Both methods must be safe for concurrent use.
type AnswerJudge interface {
	// Model names the model that judges, for the run summary.
	Model() string
	// Judge assesses one answer against its own evidence.
	Judge(ctx context.Context, in JudgeInput) (Judgment, error)
	// Consistency compares every answer of a run with the others. Every finding it
	// returns has had each row's quoted phrase verified against that row's answer.
	Consistency(ctx context.Context, systemPrompt string, answers []ConsistencyInput) (ConsistencyReport, error)
}

// BatchJudge configures judging for one RunBatch call. A nil *BatchJudge means the
// run is not judged at all, and its results carry no verdict fields.
type BatchJudge struct {
	Judge AnswerJudge
	// Concurrency bounds the judge calls in flight at once; values below 1 mean 1.
	Concurrency int
}

// SpreadsheetRow is the row a result lands on when the results are pasted under a
// header row: the first result is row 2. Reviewers work by row, so every
// reviewer-facing reference to a result uses this number.
func SpreadsheetRow(resultIndex int) int { return resultIndex + 2 }

// gapJudgment is the verdict for a fixed no-context answer. It is decided locally:
// there is nothing for a model to judge, and the remedy is always the same.
func gapJudgment() Judgment {
	return Judgment{
		Confidence: 1,
		Rating:     RatingRed,
		Issues:     []string{IssueGap},
		Comment: "No grounding context was retrieved for this question, so it needs a human answer " +
			"(or a re-run, if the knowledge bases should have covered it).",
	}
}

// declinedComment explains a model-written refusal the judge left without comment:
// context was retrieved, the answering model found nothing in it, and a judge may
// well call that refusal correct — but the row still has no answer to paste.
const declinedComment = "The answering model found nothing in the retrieved context to answer this, so it needs a human answer."

// applyGapRule makes a refusal red whatever the judge thought of it. A refusal can
// be the right behaviour and still leave the reviewer with an empty cell, and red
// is what tells them to fill it. The judge's own finding is kept when it has one:
// missed_context says the answer was there after all, which is worth more than
// "gap".
func applyGapRule(j Judgment) Judgment {
	j.Confidence = 1
	j.Rating = RatingRed
	if !slices.Contains(j.Issues, IssueMissedContext) && !slices.Contains(j.Issues, IssueGap) {
		j.Issues = append(j.Issues, IssueGap)
	}
	if strings.TrimSpace(j.Comment) == "" {
		j.Comment = declinedComment
	}
	return j
}

// isGapAnswer reports whether an answer is the fixed no-context response, whether
// RunBatch emitted it or the answering model wrote it, as the prompt tells it to.
func isGapAnswer(answer string) bool { return strings.TrimSpace(answer) == noContextAnswer }

// judgePool judges answers concurrently while the batch keeps answering. Verdicts are
// stored by result index, so their order never depends on which call finishes first.
type judgePool struct {
	ctx      context.Context
	judge    AnswerJudge
	sem      chan struct{}
	hooks    BatchHooks
	wg       sync.WaitGroup
	mu       sync.Mutex
	verdicts map[int]Judgment
	done     int
	total    int
}

func newJudgePool(ctx context.Context, cfg *BatchJudge, hooks BatchHooks) *judgePool {
	n := cfg.Concurrency
	if n < 1 {
		n = 1
	}
	return &judgePool{
		ctx:      ctx,
		judge:    cfg.Judge,
		sem:      make(chan struct{}, n),
		hooks:    hooks,
		verdicts: map[int]Judgment{},
	}
}

// record stores a verdict and reports progress. Hooks are called under the lock, so
// a hook never runs concurrently with another judge hook.
func (p *judgePool) record(idx int, j Judgment) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.verdicts[idx] = j
	p.done++
	p.hooks.judged(idx, j, p.done, p.total)
}

// local records a verdict decided without a model call.
func (p *judgePool) local(idx int, j Judgment) {
	p.mu.Lock()
	p.total++
	p.mu.Unlock()
	p.record(idx, j)
}

// submit queues one answer for judging and returns at once, so answering continues
// while the judge works.
func (p *judgePool) submit(idx int, in JudgeInput) {
	p.mu.Lock()
	p.total++
	p.mu.Unlock()

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		select {
		case p.sem <- struct{}{}:
		case <-p.ctx.Done():
			p.record(idx, Judgment{Error: "not judged: the run was cancelled"})
			return
		}
		defer func() { <-p.sem }()

		j, err := p.judge.Judge(p.ctx, in)
		if err != nil {
			p.record(idx, Judgment{Error: "judge call failed: " + err.Error()})
			return
		}
		p.record(idx, applyLocalChecks(j, in.Answer))
	}()
}

// wait blocks until every submitted answer has a verdict.
func (p *judgePool) wait() map[int]Judgment {
	p.wg.Wait()
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.verdicts
}

// applyLocalChecks folds the mechanical checks into a model verdict: markdown the
// prompt bans is recorded as a rule violation and caps the score at 4, because a
// spreadsheet cell shows it as literal asterisks however good the content is.
func applyLocalChecks(j Judgment, answer string) Judgment {
	if isGapAnswer(answer) {
		return applyGapRule(j)
	}
	marks := markdownFindings(answer)
	if len(marks) == 0 {
		return j
	}
	note := "Contains markdown (" + strings.Join(marks, ", ") + "), which pastes as literal characters; strip it."
	j.Comment = appendSentence(j.Comment, note)
	if !slices.Contains(j.Issues, IssueRuleViolation) {
		j.Issues = append(j.Issues, IssueRuleViolation)
	}
	j.Confidence = min(j.Confidence, 4)
	j.Rating = RatingFor(j.Confidence)
	return j
}

// applyConsistency applies the findings to the rows they name and returns the findings
// kept, with the number dropped. A row outside the run is dropped from its finding, and a
// finding left with fewer than two rows — a contradiction needs two sides — is dropped.
//
// A finding over more than ConsistencyBroadRows rows is kept but marked broad and
// touches no row. Every other finding adds to each row it names a note naming the
// other rows, and caps that row at 3 so a contradiction is never left green, recording
// the confidence the cap replaced.
func applyConsistency(results []BatchResult, findings []ConsistencyFinding) ([]ConsistencyFinding, int) {
	var kept []ConsistencyFinding
	dropped := 0
	for _, f := range findings {
		var rows []ConsistencyRow
		for _, r := range f.Rows {
			idx := r.Row - 2
			if idx < 0 || idx >= len(results) || results[idx].Judgment == nil ||
				slices.ContainsFunc(rows, func(x ConsistencyRow) bool { return x.Row == r.Row }) {
				continue
			}
			rows = append(rows, r)
		}
		comment := strings.TrimSpace(f.Comment)
		if len(rows) < 2 || comment == "" {
			dropped++
			continue
		}
		slices.SortFunc(rows, func(a, b ConsistencyRow) int { return a.Row - b.Row })
		f = ConsistencyFinding{Rows: rows, Comment: comment, Broad: len(rows) > ConsistencyBroadRows}
		kept = append(kept, f)
		if f.Broad {
			continue
		}
		for _, r := range rows {
			var others []string
			for _, o := range rows {
				if o.Row != r.Row {
					others = append(others, fmt.Sprint(o.Row))
				}
			}
			label := "row " + others[0]
			if len(others) > 1 {
				label = "rows " + strings.Join(others, ", ")
			}
			j := results[r.Row-2].Judgment
			j.Comment = appendSentence(j.Comment, fmt.Sprintf("Consistency: conflicts with %s — %s", label, comment))
			if j.Judged() && j.Confidence > 3 {
				if j.PreConsistencyConfidence == 0 {
					j.PreConsistencyConfidence = j.Confidence
				}
				j.Confidence = 3
				j.Rating = RatingFor(j.Confidence)
			}
		}
	}
	return kept, dropped
}

// summarise counts verdicts per rating.
func summarise(model string, results []BatchResult) *JudgeSummary {
	s := &JudgeSummary{Model: model}
	for _, r := range results {
		switch {
		case r.Judgment == nil || !r.Judgment.Judged():
			s.NotJudged++
		case r.Judgment.Rating == RatingGreen:
			s.Green++
		case r.Judgment.Rating == RatingYellow:
			s.Yellow++
		default:
			s.Red++
		}
	}
	return s
}

func appendSentence(base, add string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return add
	}
	return base + " " + add
}

// FormatJudgeSummary renders a judged run's review summary: counts per rating, then
// every red and yellow row by spreadsheet row number with its comment. It returns ""
// for a run that was not judged.
func FormatJudgeSummary(out *BatchOutput) string {
	if out == nil || out.Judge == nil {
		return ""
	}
	s := out.Judge
	var b strings.Builder
	fmt.Fprintf(&b, "Judge (%s): %d green · %d yellow · %d red · %d not judged\n",
		s.Model, s.Green, s.Yellow, s.Red, s.NotJudged)
	if !s.Consistency {
		reason := s.ConsistencyError
		if reason == "" {
			reason = "it did not run"
		}
		fmt.Fprintf(&b, "The cross-answer consistency pass did not complete: %s\n", reason)
	}
	b.WriteString("Rows are spreadsheet rows; row 2 is the first answer.\n")

	section := func(title string, keep func(*Judgment) bool, text func(*Judgment) string) {
		var lines []string
		for i, r := range out.Results {
			if r.Judgment == nil || !keep(r.Judgment) {
				continue
			}
			id := ""
			if r.ID != "" {
				id = " [" + r.ID + "]"
			}
			lines = append(lines, fmt.Sprintf("  row %d%s  %s", SpreadsheetRow(i), id, text(r.Judgment)))
		}
		if len(lines) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n%s:\n%s\n", title, strings.Join(lines, "\n"))
	}
	comment := func(j *Judgment) string { return j.Comment }
	section("Red", func(j *Judgment) bool { return j.Judged() && j.Rating == RatingRed }, comment)
	section("Yellow", func(j *Judgment) bool { return j.Judged() && j.Rating == RatingYellow }, comment)
	section("Not judged", func(j *Judgment) bool { return !j.Judged() }, func(j *Judgment) string { return j.Error })

	// Each finding once, here, rather than only inside every row it names.
	if len(s.ConsistencyFindings) > 0 || s.ConsistencyDiscarded > 0 {
		b.WriteString("\nConsistency:\n")
		for _, f := range s.ConsistencyFindings {
			note := ""
			if f.Broad {
				note = fmt.Sprintf(" (%d rows — too broad to be a contradiction, not applied to rows)", len(f.Rows))
			}
			fmt.Fprintf(&b, "  %s%s  %s\n", findingRows(f), note, f.Comment)
		}
		if s.ConsistencyDiscarded > 0 {
			fmt.Fprintf(&b, "  %d finding(s) discarded: their quoted phrases were not in the answers they named.\n", s.ConsistencyDiscarded)
		}
	}
	return b.String()
}

// findingRows labels a finding's rows, abbreviating a broad one so it stays one line.
func findingRows(f ConsistencyFinding) string {
	const shown = ConsistencyBroadRows
	labels := make([]string, 0, min(len(f.Rows), shown))
	for i, r := range f.Rows {
		if i == shown {
			break
		}
		labels = append(labels, fmt.Sprint(r.Row))
	}
	out := "rows " + strings.Join(labels, ", ")
	if len(f.Rows) > shown {
		out += fmt.Sprintf(" … and %d more", len(f.Rows)-shown)
	}
	return out
}
