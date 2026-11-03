package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeJudge scores by question id, sleeps a varying amount so verdicts complete out
// of order, and records how many calls were ever in flight at once.
type fakeJudge struct {
	mu          sync.Mutex
	inFlight    int
	maxInFlight int
	calls       atomic.Int32
	inputs      []JudgeInput

	consistency    []ConsistencyFinding
	discarded      int
	consistencyErr error
	consistencyIn  []ConsistencyInput

	block chan struct{} // when set, Judge waits on it or the context
}

func (f *fakeJudge) Model() string { return "fake-judge" }

func (f *fakeJudge) Judge(ctx context.Context, in JudgeInput) (Judgment, error) {
	f.calls.Add(1)
	f.mu.Lock()
	f.inFlight++
	f.maxInFlight = max(f.maxInFlight, f.inFlight)
	f.inputs = append(f.inputs, in)
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
	}()

	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return Judgment{}, ctx.Err()
		}
	}
	// Earlier rows sleep longer, so completion order is the reverse of answer order.
	time.Sleep(time.Duration(10-in.Row) * 3 * time.Millisecond)

	switch in.ID {
	case "FAIL":
		return Judgment{}, errors.New("gemini unreachable")
	case "RED":
		return Judgment{Confidence: 2, Rating: RatingRed, Issues: []string{IssueOtherCustomer}, Comment: `Remove "the Customer A RFI".`}, nil
	}
	return Judgment{Confidence: 5, Rating: RatingGreen}, nil
}

func (f *fakeJudge) Consistency(_ context.Context, _ string, answers []ConsistencyInput) (ConsistencyReport, error) {
	f.consistencyIn = answers
	return ConsistencyReport{Findings: f.consistency, Discarded: f.discarded}, f.consistencyErr
}

// rows builds a finding's rows with placeholder quotes; the quotes are verified in
// package analysis, so here only the row numbers matter.
func rows(nums ...int) []ConsistencyRow {
	out := make([]ConsistencyRow, len(nums))
	for i, n := range nums {
		out[i] = ConsistencyRow{Row: n, Phrase: fmt.Sprintf("phrase %d", n)}
	}
	return out
}

// judgedBatch runs a batch against a fake Anthropic backend: questions containing
// NO-CONTEXT retrieve nothing, questions containing MARKDOWN get a bold answer.
func judgedBatch(t *testing.T, ids []string, judge *BatchJudge, hooks BatchHooks) (*BatchOutput, error) {
	t.Helper()
	return judgedBatchCtx(context.Background(), t, ids, judge, hooks)
}

func judgedBatchCtx(ctx context.Context, t *testing.T, ids []string, judge *BatchJudge, hooks BatchHooks) (*BatchOutput, error) {
	t.Helper()
	isolateAnthropicEnv(t)
	f := newFakeServer(t, func(w http.ResponseWriter, body map[string]any) {
		system, _ := json.Marshal(body["system"])
		messages, _ := json.Marshal(body["messages"])
		switch {
		case strings.Contains(string(system), "RAG query optimizer"):
			writeMessageStream(w, "", `{"anchors":[],"expansion":[]}`, "end_turn", "")
		case strings.Contains(string(messages), "MARKDOWN"):
			writeMessageStream(w, "", "The platform is **fully** supported.", "end_turn", "")
		default:
			writeMessageStream(w, "", "The platform is supported. It ships with Ubuntu.", "end_turn", "")
		}
	})
	orig := batchRetrieve
	t.Cleanup(func() { batchRetrieve = orig })
	batchRetrieve = func(_ *Session, query, _ string, _ bool) string {
		if strings.Contains(query, "NO-CONTEXT") {
			return ""
		}
		return "[CANONICAL]\nchunk for " + query
	}

	manifest := &BatchManifest{Model: "anthropic.claude-sonnet-5-5"}
	for _, id := range ids {
		manifest.Questions = append(manifest.Questions, BatchQuestion{ID: id, Question: id + " question"})
	}
	return RunBatch(ctx, BackendConfig{API: APIAnthropic, BaseURL: f.URL}, nil, nil, "", manifest, DefaultPrompts(), 0.1, judge, hooks, false)
}

func TestRunBatchJudgesEveryAnswerInOrder(t *testing.T) {
	judge := &fakeJudge{consistency: []ConsistencyFinding{{Rows: rows(2, 3), Comment: "Rows 2 and 3 disagree on support."}}}
	var answeredSnapshot []BatchResult
	var judgedCalls int
	hooks := BatchHooks{
		OnAnswered: func(out *BatchOutput) { answeredSnapshot = out.Results },
		OnJudged:   func(_ int, _ Judgment, done, total int) { judgedCalls++; _ = done; _ = total },
	}
	ids := []string{"OK1", "OK2", "NO-CONTEXT", "MARKDOWN", "RED", "FAIL"}
	out, err := judgedBatch(t, ids, &BatchJudge{Judge: judge, Concurrency: 3}, hooks)
	if err != nil {
		t.Fatalf("RunBatch: %v", err)
	}

	if len(answeredSnapshot) != len(ids) {
		t.Fatalf("OnAnswered saw %d results, want %d", len(answeredSnapshot), len(ids))
	}
	for _, r := range answeredSnapshot {
		if r.Judgment != nil {
			t.Error("OnAnswered must carry the answers without verdicts")
		}
	}
	if judgedCalls != len(ids) {
		t.Errorf("OnJudged fired %d times, want %d", judgedCalls, len(ids))
	}
	if got := judge.calls.Load(); got != int32(len(ids)-1) {
		t.Errorf("judge called %d times; the gap must be judged locally", got)
	}

	byID := map[string]*Judgment{}
	for i, r := range out.Results {
		if r.ID != ids[i] {
			t.Fatalf("result %d is %q, want %q: order changed", i, r.ID, ids[i])
		}
		if r.Judgment == nil {
			t.Fatalf("result %s has no judgment", r.ID)
		}
		byID[r.ID] = r.Judgment
	}

	// Consistency names rows 2 and 3 (OK1, OK2): each gets a note naming the other,
	// is capped at 3, and records the 5 it had.
	for id, other := range map[string]string{"OK1": "row 3", "OK2": "row 2"} {
		j := byID[id]
		if j.Confidence != 3 || j.Rating != RatingYellow || j.PreConsistencyConfidence != 5 ||
			!strings.Contains(j.Comment, "Consistency: conflicts with "+other+" — Rows 2 and 3 disagree") {
			t.Errorf("%s = %+v, want a note naming %s, capped at 3 from 5", id, j, other)
		}
	}
	if j := byID["NO-CONTEXT"]; j.Rating != RatingRed || j.Issues[0] != IssueGap {
		t.Errorf("gap verdict = %+v", j)
	}
	if j := byID["MARKDOWN"]; j.Confidence != 4 || !strings.Contains(j.Comment, "bold") {
		t.Errorf("markdown verdict = %+v, want capped at 4 naming the bold", j)
	}
	if j := byID["RED"]; j.Rating != RatingRed {
		t.Errorf("red verdict = %+v", j)
	}
	if j := byID["FAIL"]; j.Judged() || !strings.Contains(j.Error, "gemini unreachable") {
		t.Errorf("failed verdict = %+v, want not judged with the reason", j)
	}

	s := out.Judge
	if s == nil || s.Model != "fake-judge" || !s.Consistency {
		t.Fatalf("summary = %+v", s)
	}
	if s.Green+s.Yellow+s.Red+s.NotJudged != len(ids) || s.Yellow != 3 || s.Red != 2 || s.NotJudged != 1 {
		t.Errorf("summary counts = %+v", s)
	}
	for _, in := range judge.consistencyIn {
		if in.ID == "NO-CONTEXT" {
			t.Error("the gap must not reach the consistency pass")
		}
	}
}

func TestRunBatchJudgeGetsTheEvidenceAndFacts(t *testing.T) {
	judge := &fakeJudge{}
	if _, err := judgedBatch(t, []string{"OK1"}, &BatchJudge{Judge: judge, Concurrency: 1}, BatchHooks{}); err != nil {
		t.Fatal(err)
	}
	in := judge.inputs[0]
	if in.Row != 2 || !strings.Contains(in.Evidence, "chunk for OK1") || in.SystemPrompt == "" {
		t.Errorf("judge input = %+v", in)
	}
	if in.Words != 8 || in.Sentences != 2 {
		t.Errorf("counts = %d words, %d sentences; want 8 and 2", in.Words, in.Sentences)
	}
}

func TestRunBatchRespectsJudgeConcurrency(t *testing.T) {
	judge := &fakeJudge{}
	ids := []string{"A", "B", "C", "D", "E", "F", "G"}
	if _, err := judgedBatch(t, ids, &BatchJudge{Judge: judge, Concurrency: 2}, BatchHooks{}); err != nil {
		t.Fatal(err)
	}
	if judge.maxInFlight > 2 {
		t.Errorf("%d judge calls in flight, bound is 2", judge.maxInFlight)
	}
}

func TestConsistencyFailureKeepsVerdicts(t *testing.T) {
	judge := &fakeJudge{consistencyErr: errors.New("quota")}
	out, err := judgedBatch(t, []string{"OK1", "OK2"}, &BatchJudge{Judge: judge, Concurrency: 2}, BatchHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Judge.Consistency || out.Judge.ConsistencyError != "quota" || out.Judge.Green != 2 {
		t.Errorf("summary = %+v, want two greens and a reported consistency failure", out.Judge)
	}
}

func TestUnjudgedRunKeepsItsShape(t *testing.T) {
	out, err := judgedBatch(t, []string{"OK1", "NO-CONTEXT"}, nil, BatchHooks{
		OnAnswered: func(*BatchOutput) { t.Error("OnAnswered fired on an unjudged run") },
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), `"judgment"`) || strings.Contains(string(raw), `"judge"`) {
		t.Errorf("unjudged results carry verdict keys: %s", raw)
	}
}

func TestCancellingDuringJudgingStopsTheRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	judge := &fakeJudge{block: make(chan struct{})}
	hooks := BatchHooks{OnAnswered: func(*BatchOutput) { cancel() }}
	_, err := judgedBatchCtx(ctx, t, []string{"A", "B"}, &BatchJudge{Judge: judge, Concurrency: 1}, hooks)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestApplyConsistencyIgnoresUnknownRows(t *testing.T) {
	results := []BatchResult{
		{Judgment: &Judgment{Confidence: 5, Rating: RatingGreen}},
		{Judgment: &Judgment{Confidence: 5, Rating: RatingGreen}},
	}
	kept, dropped := applyConsistency(results, []ConsistencyFinding{
		{Rows: rows(9), Comment: "nowhere"},
		{Rows: rows(2, 9), Comment: "one side outside the run"},
	})
	if len(kept) != 0 || dropped != 2 {
		t.Errorf("kept %v, dropped %d; want both dropped", kept, dropped)
	}
	for _, r := range results {
		if r.Judgment.Comment != "" || r.Judgment.Confidence != 5 {
			t.Errorf("a finding without two rows in the run changed a verdict: %+v", r.Judgment)
		}
	}
}

func greenResults(n int) []BatchResult {
	out := make([]BatchResult, n)
	for i := range out {
		out[i] = BatchResult{ID: fmt.Sprintf("Q%d", i+1), Judgment: &Judgment{Confidence: 5, Rating: RatingGreen}}
	}
	return out
}

func TestApplyConsistencyKeepsABroadFindingOffTheRows(t *testing.T) {
	// A telco RFP run: one remark about customer names over 284 rows was pasted into
	// 284 comments and capped all of them. It must now touch none.
	results := greenResults(300)
	var all []int
	for r := 2; r < 2+284; r++ {
		all = append(all, r)
	}
	kept, dropped := applyConsistency(results, []ConsistencyFinding{
		{Rows: rows(all...), Comment: "Repeated violation of the rules against naming other customers."},
		{Rows: rows(263, 47), Comment: "S3 replication: row 47 says 2026, row 263 says available."},
	})
	if dropped != 0 || len(kept) != 2 || !kept[0].Broad || kept[1].Broad {
		t.Fatalf("kept %+v, dropped %d", kept, dropped)
	}
	if kept[1].Rows[0].Row != 47 {
		t.Error("a finding's rows should be sorted")
	}
	for i, r := range results {
		row := SpreadsheetRow(i)
		applied := row == 47 || row == 263
		if applied != (r.Judgment.Confidence == 3) || strings.Contains(r.Judgment.Comment, "customers") {
			t.Fatalf("row %d = %+v; only rows 47 and 263 should change, and only by the S3 finding", row, r.Judgment)
		}
	}

	exactly := greenResults(10)
	kept, _ = applyConsistency(exactly, []ConsistencyFinding{{Rows: rows(2, 3, 4, 5, 6, 7, 8, 9), Comment: "eight rows"}})
	if kept[0].Broad || exactly[7].Judgment.Confidence != 3 {
		t.Errorf("a finding over exactly %d rows is still applied", ConsistencyBroadRows)
	}
}

func TestApplyConsistencyRecordsOnlyALoweredConfidence(t *testing.T) {
	results := []BatchResult{
		{Judgment: &Judgment{Confidence: 2, Rating: RatingRed, Comment: "unsupported"}},
		{Judgment: &Judgment{Confidence: 4, Rating: RatingYellow}},
		{Judgment: &Judgment{Confidence: 5, Rating: RatingGreen}},
	}
	applyConsistency(results, []ConsistencyFinding{{Rows: rows(2, 3, 4), Comment: "they differ"}})
	if j := results[0].Judgment; j.Confidence != 2 || j.PreConsistencyConfidence != 0 || !strings.Contains(j.Comment, "conflicts with rows 3, 4") {
		t.Errorf("a red row keeps its score and gets the note: %+v", j)
	}
	if j := results[1].Judgment; j.Confidence != 3 || j.PreConsistencyConfidence != 4 {
		t.Errorf("a 4 lowered to 3 records the 4: %+v", j)
	}
	raw, _ := json.Marshal(results[0].Judgment)
	if strings.Contains(string(raw), "pre_consistency_confidence") {
		t.Errorf("an unlowered verdict must not carry the field: %s", raw)
	}
}

func TestRunSummaryCarriesTheFindingsOnce(t *testing.T) {
	judge := &fakeJudge{
		consistency: []ConsistencyFinding{{Rows: rows(2, 3), Comment: "Rows 2 and 3 disagree on support."}},
		discarded:   2,
	}
	out, err := judgedBatch(t, []string{"OK1", "OK2"}, &BatchJudge{Judge: judge, Concurrency: 2}, BatchHooks{})
	if err != nil {
		t.Fatal(err)
	}
	s := out.Judge
	if len(s.ConsistencyFindings) != 1 || s.ConsistencyDiscarded != 2 {
		t.Errorf("summary = %+v, want one finding and two discarded", s)
	}
	text := FormatJudgeSummary(out)
	if strings.Count(text, "Rows 2 and 3 disagree on support.") != 3 {
		// once in the Consistency section, once in each yellow row's comment
		t.Errorf("summary text:\n%s", text)
	}
	if !strings.Contains(text, "\nConsistency:\n  rows 2, 3  Rows 2 and 3 disagree") || !strings.Contains(text, "2 finding(s) discarded") {
		t.Errorf("summary is missing the consistency section:\n%s", text)
	}
}

func TestFormatJudgeSummaryAbbreviatesABroadFinding(t *testing.T) {
	var all []int
	for r := 2; r < 2+20; r++ {
		all = append(all, r)
	}
	out := &BatchOutput{Judge: &JudgeSummary{Model: "m", Consistency: true,
		ConsistencyFindings: []ConsistencyFinding{{Rows: rows(all...), Comment: "names customers", Broad: true}}}}
	text := FormatJudgeSummary(out)
	if !strings.Contains(text, "rows 2, 3, 4, 5, 6, 7, 8, 9 … and 12 more (20 rows — too broad to be a contradiction, not applied to rows)  names customers") {
		t.Errorf("summary text:\n%s", text)
	}
}

func TestRatingFor(t *testing.T) {
	want := map[int]Rating{5: RatingGreen, 4: RatingYellow, 3: RatingYellow, 2: RatingRed, 1: RatingRed}
	for c, r := range want {
		if got := RatingFor(c); got != r {
			t.Errorf("RatingFor(%d) = %s, want %s", c, got, r)
		}
	}
}

func TestMarkdownFindings(t *testing.T) {
	for in, want := range map[string]string{
		"Plain prose, version 1.32.":  "",
		"It is **fully** supported.":  "bold",
		"Run `snap install`.":         "backticks",
		"## Heading\ntext":            "heading",
		"Options:\n- one\n- two":      "bullet list",
		"Steps:\n1. first\n2. second": "numbered list",
		"A 2 * 3 grid, footnoted*.":   "",
	} {
		got := strings.Join(markdownFindings(in), ",")
		if got != want {
			t.Errorf("markdownFindings(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCountSentences(t *testing.T) {
	for in, want := range map[string]int{
		"":                                    0,
		"No terminator":                       1,
		"Supported. Version 1.32 is current.": 2,
		"Really?! Yes, it is.":                2,
		"Wait... then go.":                    2,
	} {
		if got := countSentences(in); got != want {
			t.Errorf("countSentences(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestFormatJudgeSummaryUsesSpreadsheetRows(t *testing.T) {
	out := &BatchOutput{
		Judge: &JudgeSummary{Model: "m", Green: 1, Yellow: 1, Red: 1, NotJudged: 1, Consistency: true},
		Results: []BatchResult{
			{ID: "A", Judgment: &Judgment{Confidence: 5, Rating: RatingGreen}},
			{ID: "B", Judgment: &Judgment{Confidence: 3, Rating: RatingYellow, Comment: "verify the figure"}},
			{ID: "C", Judgment: &Judgment{Confidence: 1, Rating: RatingRed, Comment: "gap"}},
			{Judgment: &Judgment{Error: "judge call failed: x"}},
		},
	}
	got := FormatJudgeSummary(out)
	for _, want := range []string{
		"1 green · 1 yellow · 1 red · 1 not judged",
		"row 2 is the first answer",
		"row 4 [C]  gap",
		"row 3 [B]  verify the figure",
		"row 5  judge call failed: x",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "row 2 [A]") {
		t.Error("green rows should not be listed")
	}
	if strings.Index(got, "Red:") > strings.Index(got, "Yellow:") {
		t.Error("red rows should be listed before yellow")
	}
	if FormatJudgeSummary(&BatchOutput{}) != "" {
		t.Error("an unjudged run has no summary")
	}
}

// The probe run (batch-results-20261022-083445.json) had N.5 and N.10 rated green:
// retrieval found context, the answering model wrote the fixed refusal itself, and
// the judge called the refusal correct. The row still needs a human answer.
func TestModelWrittenGapIsRedWhateverTheJudgeSays(t *testing.T) {
	j := applyLocalChecks(Judgment{Confidence: 5, Rating: RatingGreen}, noContextAnswer)
	if j.Rating != RatingRed || j.Confidence != 1 || !slices.Contains(j.Issues, IssueGap) || j.Comment != declinedComment {
		t.Errorf("verdict = %+v, want red gap with the declined comment", j)
	}

	missed := applyLocalChecks(Judgment{Confidence: 1, Rating: RatingRed, Issues: []string{IssueMissedContext}, Comment: "answer is in k8s.pdf"}, noContextAnswer)
	if slices.Contains(missed.Issues, IssueGap) || missed.Comment != "answer is in k8s.pdf" {
		t.Errorf("a missed_context finding must survive: %+v", missed)
	}
}

func TestRunBatchMakesModelWrittenGapsRed(t *testing.T) {
	isolateAnthropicEnv(t)
	f := newFakeServer(t, func(w http.ResponseWriter, body map[string]any) {
		system, _ := json.Marshal(body["system"])
		if strings.Contains(string(system), "RAG query optimizer") {
			writeMessageStream(w, "", `{"anchors":[],"expansion":[]}`, "end_turn", "")
			return
		}
		writeMessageStream(w, "", noContextAnswer, "end_turn", "")
	})
	orig := batchRetrieve
	t.Cleanup(func() { batchRetrieve = orig })
	batchRetrieve = func(*Session, string, string, bool) string { return "[CANONICAL]\nunrelated chunk" }

	judge := &fakeJudge{} // rates everything 5
	manifest := &BatchManifest{Model: "m", Questions: []BatchQuestion{{ID: "N.5", Question: "Linux certifications?"}}}
	out, err := RunBatch(context.Background(), BackendConfig{API: APIAnthropic, BaseURL: f.URL}, nil, nil, "", manifest,
		DefaultPrompts(), 0.1, &BatchJudge{Judge: judge, Concurrency: 1}, BatchHooks{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if judge.calls.Load() != 1 {
		t.Error("a model-written gap should still reach the judge, which may find missed context")
	}
	if j := out.Results[0].Judgment; j.Rating != RatingRed {
		t.Errorf("verdict = %+v, want red", j)
	}
}

func TestSplitOpener(t *testing.T) {
	for in, want := range map[string][2]string{
		// G8 in the probe: five sentences after the required opener, not six.
		"Supported. One. Two. Three. Four. Five.":         {"Supported.", "One. Two. Three. Four. Five."},
		"No hidden licensing. Canonical does not charge.": {"No hidden licensing.", "Canonical does not charge."},
		// H1: a long first sentence is the answer, not an opener.
		"Subscription-based; there is no perpetual licence. More.": {"", "Subscription-based; there is no perpetual licence. More."},
		"Supported.":           {"", "Supported."},
		"Version 1.32 is LTS.": {"", "Version 1.32 is LTS."},
	} {
		opener, rest := splitOpener(in)
		if opener != want[0] || rest != want[1] {
			t.Errorf("splitOpener(%q) = %q, %q; want %q, %q", in, opener, rest, want[0], want[1])
		}
	}
	if _, rest := splitOpener("Supported. One. Two. Three. Four. Five."); countSentences(rest) != 5 {
		t.Error("the answer proper should count five sentences")
	}
}
