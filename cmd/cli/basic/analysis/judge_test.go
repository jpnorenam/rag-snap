package analysis

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
)

func TestJudgeSettingsDefaultToOn(t *testing.T) {
	s, err := LoadJudgeSettings(mapConfig{})
	if err != nil {
		t.Fatalf("LoadJudgeSettings: %v", err)
	}
	if !s.Enabled {
		t.Error("judging should be on when judge.enabled is unset")
	}
	if s.Reasoning != EffortMedium {
		t.Errorf("reasoning = %q, want medium", s.Reasoning)
	}
	if s.Concurrency != DefaultJudgeConcurrency {
		t.Errorf("concurrency = %d, want %d", s.Concurrency, DefaultJudgeConcurrency)
	}
}

func TestJudgeModelFallsBackToAnalysisModel(t *testing.T) {
	s, err := LoadJudgeSettings(mapConfig{ConfModel: "gemini-analysis"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Model != "gemini-analysis" {
		t.Errorf("model = %q, want the analysis model", s.Model)
	}

	s, err = LoadJudgeSettings(mapConfig{ConfModel: "gemini-analysis", ConfJudgeModel: "gemini-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Model != "gemini-flash" {
		t.Errorf("model = %q, want judge.model to win", s.Model)
	}
}

func TestJudgeSettingsRejectBadValues(t *testing.T) {
	for name, cfg := range map[string]mapConfig{
		"zero concurrency":  {ConfJudgeConcurrency: "0"},
		"text concurrency":  {ConfJudgeConcurrency: "many"},
		"reasoning none":    {ConfJudgeReasoning: "none"},
		"reasoning unknown": {ConfJudgeReasoning: "extreme"},
	} {
		if _, err := LoadJudgeSettings(cfg); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestValidateJudgeTellsOffFromUnusable(t *testing.T) {
	t.Setenv(envGeminiKey, "")
	off := ValidateJudge(JudgeSettings{Enabled: false})
	if !IsJudgeDisabled(off) {
		t.Errorf("a disabled judge should report IsJudgeDisabled, got %v", off)
	}

	err := ValidateJudge(JudgeSettings{Enabled: true})
	if err == nil || IsJudgeDisabled(err) {
		t.Fatalf("an enabled judge with nothing configured should be unusable, got %v", err)
	}
	for _, want := range []string{"answer judging is not configured", ConfJudgeModel, envGeminiKey} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q: %v", want, err)
		}
	}
}

func TestJudgeNeverUsesTheChatKey(t *testing.T) {
	t.Setenv(envGeminiKey, "")
	t.Setenv("CHAT_API_KEY", "bedrock-key")
	if _, err := NewJudge(JudgeSettings{Enabled: true, Model: "m"}, false); err == nil {
		t.Fatal("NewJudge must fail without GEMINI_API_KEY, whatever CHAT_API_KEY holds")
	}
}

func testJudge(t *testing.T, fs *fakeServer) *Judge {
	t.Helper()
	t.Setenv(envGeminiKey, testKey)
	j, err := newJudge(JudgeSettings{Enabled: true, Model: "test-model", Reasoning: EffortLow, Concurrency: 1}, fs.URL, false)
	if err != nil {
		t.Fatalf("newJudge: %v", err)
	}
	return j
}

func testInput() chat.JudgeInput {
	return chat.JudgeInput{
		Row: 2, ID: "N.32", Question: "Describe your OpenStack migration experience.",
		Answer:        "Canonical has migrated many clouds.",
		DomainContext: "Section N covers references.",
		SystemPrompt:  "Two to five sentences. Never name customers.",
		Evidence:      "[CANONICAL]\nMigration notes.\n(source: migrations.pdf, score: 0.9)",
		Words:         5, Sentences: 1,
	}
}

func TestJudgeReturnsAValidatedVerdict(t *testing.T) {
	fs := newFakeServer(t, fakeCompletion{content: `{"comment":"\"many clouds\" is vague; cite migrations.pdf.","confidence":3,"issues":["incomplete"]}`})
	got, err := testJudge(t, fs).Judge(context.Background(), testInput())
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if got.Confidence != 3 || got.Rating != chat.RatingYellow || got.Comment == "" || len(got.Issues) != 1 {
		t.Errorf("unexpected verdict: %+v", got)
	}

	body := fs.requests[0].rawBody()
	for _, want := range []string{"RULES THE ANSWER HAD TO FOLLOW", "Never name customers", "QUESTION N.32",
		"ANSWER (5 words, 1 sentence)", "migrations.pdf", "Section N covers references"} {
		if !strings.Contains(body, want) {
			t.Errorf("request should carry %q", want)
		}
	}
	if !strings.Contains(body, "answer_verdict") && !strings.Contains(body, "confidence") {
		t.Error("request should carry the verdict schema")
	}
}

func TestJudgeRequestPutsTheSharedPromptFirst(t *testing.T) {
	parts := judgeParts(testInput())
	if !strings.HasPrefix(parts[0].text, "RULES THE ANSWER HAD TO FOLLOW") {
		t.Errorf("the batch prompt must lead, so the prefix is cacheable across a run: %q", parts[0].text)
	}
	if !strings.HasPrefix(parts[len(parts)-1].text, "RETRIEVED CONTEXT") {
		t.Error("the per-answer evidence should come last")
	}
}

func TestValidateVerdict(t *testing.T) {
	for name, tc := range map[string]struct {
		in   verdictResponse
		fail bool
	}{
		"clean five":            {verdictResponse{Confidence: 5}, false},
		"five with stray notes": {verdictResponse{Confidence: 5, Comment: "fine", Issues: []string{"incomplete"}}, false},
		"zero":                  {verdictResponse{Confidence: 0, Comment: "x"}, true},
		"six":                   {verdictResponse{Confidence: 6, Comment: "x"}, true},
		"silent four":           {verdictResponse{Confidence: 4}, true},
		"unknown issue":         {verdictResponse{Confidence: 2, Comment: "x", Issues: []string{"vibes"}}, true},
	} {
		got, err := validateVerdict(tc.in)
		if tc.fail != (err != nil) {
			t.Errorf("%s: err = %v, want failure %v", name, err, tc.fail)
			continue
		}
		if err == nil && got.Confidence == 5 && (got.Comment != "" || got.Issues != nil) {
			t.Errorf("%s: a 5 must carry no comment or issues, got %+v", name, got)
		}
	}
}

func TestJudgeRetriesARateLimit(t *testing.T) {
	old := judgeBackoff
	judgeBackoff = 0
	t.Cleanup(func() { judgeBackoff = old })
	fs := newFakeServer(t,
		fakeCompletion{status: http.StatusTooManyRequests},
		fakeCompletion{status: http.StatusServiceUnavailable},
		fakeCompletion{content: `{"comment":"","confidence":5,"issues":[]}`},
	)
	got, err := testJudge(t, fs).Judge(context.Background(), testInput())
	if err != nil {
		t.Fatalf("Judge should succeed after retries: %v", err)
	}
	if got.Rating != chat.RatingGreen || fs.calls != 3 {
		t.Errorf("verdict %+v after %d calls", got, fs.calls)
	}
}

func TestJudgeGivesUpAfterBoundedRetries(t *testing.T) {
	old := judgeBackoff
	judgeBackoff = 0
	t.Cleanup(func() { judgeBackoff = old })
	queue := make([]fakeCompletion, 0, judgeRetries+2)
	for range judgeRetries + 2 {
		queue = append(queue, fakeCompletion{status: http.StatusTooManyRequests})
	}
	fs := newFakeServer(t, queue...)
	_, err := testJudge(t, fs).Judge(context.Background(), testInput())
	if err == nil {
		t.Fatal("expected the rate limit to surface")
	}
	if fs.calls != judgeRetries+1 {
		t.Errorf("made %d calls, want %d", fs.calls, judgeRetries+1)
	}
	if !strings.Contains(err.Error(), "after 3 retries") {
		t.Errorf("error should say it retried: %v", err)
	}
}

func TestJudgeDoesNotRetryATruncation(t *testing.T) {
	fs := newFakeServer(t, fakeCompletion{finishReason: "MAX_TOKENS", content: `{"comment":"`})
	if _, err := testJudge(t, fs).Judge(context.Background(), testInput()); err == nil {
		t.Fatal("a truncated verdict must fail")
	}
	if fs.calls != 1 {
		t.Errorf("a truncation was retried: %d calls", fs.calls)
	}
}

func TestConsistencyKeepsOnlyVerifiedRows(t *testing.T) {
	fs := newFakeServer(t, fakeCompletion{content: `{"findings":[
		{"rows":[{"row":2,"phrase":"36,000 packages"},{"row":5,"phrase":"\u201c25,000 Packages.\u201d"},{"row":99,"phrase":"x"}],"comment":"Row 2 says 36,000 packages, row 5 says 25,000."},
		{"rows":[{"row":99,"phrase":"x"}],"comment":"Names a row that does not exist."},
		{"rows":[{"row":2,"phrase":"36,000 packages"},{"row":3,"phrase":"40,000 packages"}],"comment":"Row 3's phrase is not in its answer."},
		{"rows":[{"row":2,"phrase":"36,000"},{"row":5,"phrase":"25,000"}],"comment":"  "}
	]}`})
	answers := []chat.ConsistencyInput{
		{Row: 2, ID: "A1", Question: "q", Answer: "The mirror holds 36,000 packages."},
		{Row: 3, ID: "A2", Question: "q", Answer: "x"},
		{Row: 5, ID: "A4", Question: "q", Answer: "About 25,000   packages are mirrored."},
	}
	got, err := testJudge(t, fs).Consistency(context.Background(), "rules", answers)
	if err != nil {
		t.Fatalf("Consistency: %v", err)
	}
	if len(got.Findings) != 1 || len(got.Findings[0].Rows) != 2 || got.Findings[0].Rows[0].Row != 2 || got.Findings[0].Rows[1].Row != 5 {
		t.Fatalf("findings = %+v, want one finding on rows 2 and 5", got.Findings)
	}
	if got.Discarded != 3 {
		t.Errorf("discarded = %d, want 3: the unknown row, the unquoted row and the empty comment", got.Discarded)
	}
	if body := fs.requests[0].rawBody(); !strings.Contains(body, "Row 5 [A4]") {
		t.Error("answers should be labelled by spreadsheet row")
	}
}

func TestQuoteIn(t *testing.T) {
	answer := "Ubuntu Pro extends security maintenance on Ubuntu LTS to 10 years, and to 15 years with the Legacy add-on."
	for phrase, want := range map[string]bool{
		"to 10 years":      true,
		"  TO 10\nyears, ": true,
		"\u201cto 15 years with the Legacy\u201d": true,
		"Ubuntu Pro extends … to 10 years":        true,
		"to 10 years ... Ubuntu Pro":              false, // fragments out of order
		"to 12 years":                             false,
		"":                                        false,
		"...":                                     false,
	} {
		if got := quoteIn(phrase, answer); got != want {
			t.Errorf("quoteIn(%q) = %v, want %v", phrase, got, want)
		}
	}
}

func TestConsistencySchemaQuotesEveryRow(t *testing.T) {
	findings := consistencySchema()["properties"].(map[string]any)["findings"].(map[string]any)
	rows := findings["items"].(map[string]any)["properties"].(map[string]any)["rows"].(map[string]any)
	item := rows["items"].(map[string]any)
	if fmt.Sprint(item["required"]) != "[phrase row]" {
		t.Errorf("each row must require its phrase and number, got %v", item["required"])
	}
	if !strings.Contains(consistencySystemPrompt, "copied exactly from that row's answer") {
		t.Error("the consistency prompt must ask for a quote per row")
	}
}

func TestAnswerFactsNameTheOpener(t *testing.T) {
	in := testInput()
	in.Opener, in.Sentences, in.Words = "Supported.", 5, 94
	if got := answerFacts(in); got != `94 words; opener "Supported.", then 5 sentences` {
		t.Errorf("answerFacts = %q", got)
	}
}

func TestRubricKeepsEditsOutOfRed(t *testing.T) {
	// From the probe: one deletable sentence earned a 2. The rubric must say an
	// edit is never red, except naming another customer.
	for _, want := range []string{
		"fixed by deleting or rewording a phrase is never below 3, except naming another customer",
		"Do not report markdown",
		"is not counted as a sentence",
		"looks malformed or implausible",
	} {
		if !strings.Contains(judgeSystemPrompt, want) {
			t.Errorf("judge prompt lacks %q", want)
		}
	}
}

func TestConsistencyPassReportsContradictionsOnly(t *testing.T) {
	// From an earlier full run: the pass re-reported per-row markdown and
	// customer-naming violations across 10 rows, capping each of them at 3.
	for _, want := range []string{"Report only contradictions", "Do not report rule violations of any kind"} {
		if !strings.Contains(consistencySystemPrompt, want) {
			t.Errorf("consistency prompt lacks %q", want)
		}
	}
	if strings.Contains(consistencySystemPrompt, "Repeated violations") {
		t.Error("the consistency pass must not report repeated rule violations")
	}
	if !strings.Contains(judgeSystemPrompt, "The customer the answers are written for is not another customer") {
		t.Error("naming the RFP's own customer must not count as other_customer")
	}
}
