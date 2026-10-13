package analysis

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
)

// questionsJSON builds a questions-call response with n sequential C-prefixed rows.
func questionsJSON(n int) string {
	var b strings.Builder
	b.WriteString(`{"questions":[`)
	for i := 1; i <= n; i++ {
		if i > 1 {
			b.WriteString(",")
		}
		b.WriteString(`{"id":"C`)
		b.WriteString(itoa(i))
		b.WriteString(`","question":"Requirement `)
		b.WriteString(itoa(i))
		b.WriteString(`","source":"C. Section"}`)
	}
	b.WriteString(`]}`)
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

const strategyJSON = `{
  "prompt": "You are a solutions engineer completing a compliance matrix. Describe the proposed platform.",
  "knowledge_bases": ["vendor-rfp", "platform-docs"],
  "kapa_source_groups": ["aaaa-bbbb"],
  "domains": [
    {"match": "C*", "context": "Section C covers platform capabilities.", "keywords": ["SR-IOV", "NUMA"]}
  ]
}`

func testTriage(estimate int) *Triage {
	return &Triage{EstimatedQuestions: estimate, Sections: []TriageSection{{Name: "C. Section", IDPrefix: "C"}}}
}

func TestGenerateProducesALoadableManifest(t *testing.T) {
	fs := newFakeServer(t,
		fakeCompletion{content: questionsJSON(10)},
		fakeCompletion{content: strategyJSON},
	)
	c := testClient(t, fs)
	iv := Interview{AnswerWords: 120, AnswerLimit: LengthHard, OpenStack: OpenStackSunbeam}

	manifest, err := c.Generate(context.Background(), testConveyance(t), testCatalogue(), testTriage(10), iv)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if manifest.Version != "1.0" {
		t.Errorf("version = %q", manifest.Version)
	}
	if len(manifest.Questions) != 10 {
		t.Errorf("got %d questions, want 10", len(manifest.Questions))
	}
	if len(manifest.Domains) != 1 {
		t.Errorf("got %d domains, want 1", len(manifest.Domains))
	}
	// Analysis must never pin the answering model or a stored prompt variant.
	if manifest.Model != "" {
		t.Error("a generated manifest must not pin the answering model")
	}
	if manifest.PromptRef != "" {
		t.Error("a generated manifest must not set prompt_ref")
	}

	// The interview answers must be in the prompt, and the mandatory rules must have
	// survived even though the model's prompt said nothing about them.
	if !strings.Contains(manifest.Prompt, "120") || !strings.Contains(manifest.Prompt, "hard limit") {
		t.Errorf("prompt is missing the length and its mode:\n%s", manifest.Prompt)
	}
	if !strings.Contains(manifest.Prompt, "Sunbeam") {
		t.Error("prompt is missing the chosen OpenStack flavour")
	}
	if !PromptHasNoContextRule(manifest.Prompt) || !PromptHasDirectnessRule(manifest.Prompt) {
		t.Error("the mandatory rules did not survive into the generated prompt")
	}

	// Two calls, and the questions call must use the cheaper reasoning effort while
	// the strategy call uses the expensive one.
	if fs.calls != 2 {
		t.Fatalf("expected 2 model calls, got %d", fs.calls)
	}
	if got := thinkingLevel(t, fs.requests[0]); got != "LOW" {
		t.Errorf("questions call thinkingLevel = %v, want LOW", got)
	}
	if got := thinkingLevel(t, fs.requests[1]); got != "HIGH" {
		t.Errorf("strategy call thinkingLevel = %v, want HIGH", got)
	}
}

func TestGenerateHandsQuestionIDsToTheStrategyCall(t *testing.T) {
	// The strategy call must be told which ids exist, or its patterns are designed
	// against imagined ids and silently route nothing.
	fs := newFakeServer(t,
		fakeCompletion{content: questionsJSON(3)},
		fakeCompletion{content: strategyJSON},
	)
	c := testClient(t, fs)

	if _, err := c.Generate(context.Background(), testConveyance(t), testCatalogue(), testTriage(3),
		Interview{AnswerWords: 100, AnswerLimit: LengthSoft}); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	body := requestText(t, fs.requests[1])
	for _, id := range []string{"C1", "C2", "C3"} {
		if !strings.Contains(body, id) {
			t.Errorf("the strategy call was not told about question id %q", id)
		}
	}
	if !strings.Contains(body, "C. Section") {
		t.Error("the strategy call was not told the question sources")
	}
}

func TestGenerateFailsOnAMaterialShortfall(t *testing.T) {
	// Triage counted 150; the extraction returned 40. That is a call that stopped
	// early, and presenting it as a complete manifest is this feature's worst outcome.
	fs := newFakeServer(t, fakeCompletion{content: questionsJSON(40)})
	c := testClient(t, fs)

	_, err := c.Generate(context.Background(), testConveyance(t), testCatalogue(), testTriage(150),
		Interview{AnswerWords: 100, AnswerLimit: LengthSoft})
	if err == nil {
		t.Fatal("expected a material shortfall to fail")
	}
	var shortfall *ShortfallError
	if !errors.As(err, &shortfall) {
		t.Fatalf("error should be a ShortfallError, got %T: %v", err, err)
	}
	// Both counts must be named so a human can judge it.
	if !strings.Contains(err.Error(), "40") || !strings.Contains(err.Error(), "150") {
		t.Errorf("error should name both counts, got: %v", err)
	}
	// The strategy call must not have run: there is nothing to write a strategy for.
	if fs.calls != 1 {
		t.Errorf("expected generation to stop after the questions call, got %d calls", fs.calls)
	}
}

func TestGenerateToleratesAnApproximateEstimate(t *testing.T) {
	// A triage estimate is approximate by nature. 140 against an estimate of 150 is
	// agreement, and must not fail.
	fs := newFakeServer(t,
		fakeCompletion{content: questionsJSON(140)},
		fakeCompletion{content: strategyJSON},
	)
	c := testClient(t, fs)

	if _, err := c.Generate(context.Background(), testConveyance(t), testCatalogue(), testTriage(150),
		Interview{AnswerWords: 100, AnswerLimit: LengthSoft}); err != nil {
		t.Fatalf("a close-but-lower count must not fail: %v", err)
	}
}

func TestGenerateRejectsUncataloguedSelections(t *testing.T) {
	strategy := `{
	  "prompt": "A prompt.",
	  "knowledge_bases": ["vendor-rfp", "canonical-k9s"],
	  "domains": [{"match": "C*", "context": "Section C."}]
	}`
	fs := newFakeServer(t,
		fakeCompletion{content: questionsJSON(5)},
		fakeCompletion{content: strategy},
	)
	c := testClient(t, fs)

	_, err := c.Generate(context.Background(), testConveyance(t), testCatalogue(), testTriage(5),
		Interview{AnswerWords: 100, AnswerLimit: LengthSoft})
	if err == nil {
		t.Fatal("expected an uncatalogued knowledge base to fail generation")
	}
	if !strings.Contains(err.Error(), "canonical-k9s") {
		t.Errorf("error should name the offending value, got: %v", err)
	}
}

func TestGenerateRejectsAStrategyWithNoPrompt(t *testing.T) {
	strategy := `{"knowledge_bases":["vendor-rfp"],"domains":[{"match":"C*","context":"x"}]}`
	fs := newFakeServer(t,
		fakeCompletion{content: questionsJSON(5)},
		fakeCompletion{content: strategy},
	)
	c := testClient(t, fs)

	_, err := c.Generate(context.Background(), testConveyance(t), testCatalogue(), testTriage(5),
		Interview{AnswerWords: 100, AnswerLimit: LengthSoft})
	if err == nil {
		t.Fatal("expected a strategy with no prompt to fail")
	}
	var incomplete *IncompleteError
	if !errors.As(err, &incomplete) {
		t.Fatalf("error should be an IncompleteError, got %T: %v", err, err)
	}
}

func TestGenerateRejectsInvalidDomains(t *testing.T) {
	// An entry with neither context nor keywords fails the run's own compilation, so
	// it must fail here rather than being discovered when the operator starts a run.
	strategy := `{
	  "prompt": "A prompt.",
	  "knowledge_bases": ["vendor-rfp"],
	  "domains": [{"match": "C*"}]
	}`
	fs := newFakeServer(t,
		fakeCompletion{content: questionsJSON(5)},
		fakeCompletion{content: strategy},
	)
	c := testClient(t, fs)

	_, err := c.Generate(context.Background(), testConveyance(t), testCatalogue(), testTriage(5),
		Interview{AnswerWords: 100, AnswerLimit: LengthSoft})
	if err == nil {
		t.Fatal("expected an invalid domains entry to fail generation")
	}
	if !strings.Contains(err.Error(), "domains") {
		t.Errorf("error should point at the domains block, got: %v", err)
	}
}

func TestGenerateRejectsAnInvalidInterviewBeforeCallingTheModel(t *testing.T) {
	fs := newFakeServer(t)
	c := testClient(t, fs)

	_, err := c.Generate(context.Background(), testConveyance(t), testCatalogue(), testTriage(5),
		Interview{AnswerWords: 0, AnswerLimit: LengthSoft})
	if err == nil {
		t.Fatal("expected an invalid interview to fail")
	}
	if fs.calls != 0 {
		t.Error("an invalid interview must fail before any model call is made")
	}
}

func TestGenerateDropsQuestionsWithNoText(t *testing.T) {
	// The manifest reader rejects an empty question outright, so an entry with no text
	// is dropped rather than carried into a manifest that will not load.
	questions := `{"questions":[
	  {"id":"C1","question":"Real requirement","source":"C"},
	  {"id":"C2","question":"   ","source":"C"},
	  {"id":"C3","question":"Another requirement","source":"C"}
	]}`
	fs := newFakeServer(t,
		fakeCompletion{content: questions},
		fakeCompletion{content: strategyJSON},
	)
	c := testClient(t, fs)

	manifest, err := c.Generate(context.Background(), testConveyance(t), testCatalogue(), testTriage(3),
		Interview{AnswerWords: 100, AnswerLimit: LengthSoft})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(manifest.Questions) != 2 {
		t.Errorf("got %d questions, want the 2 with text", len(manifest.Questions))
	}
}

func TestGeneratedManifestRoundTripsThroughTheRunsOwnValidation(t *testing.T) {
	fs := newFakeServer(t,
		fakeCompletion{content: questionsJSON(6)},
		fakeCompletion{content: strategyJSON},
	)
	c := testClient(t, fs)

	manifest, err := c.Generate(context.Background(), testConveyance(t), testCatalogue(), testTriage(6),
		Interview{AnswerWords: 100, AnswerLimit: LengthSoft})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// The real compilation, and real routing: every generated pattern must reach the
	// generated questions.
	set, err := chat.CompileDomains(manifest.Domains)
	if err != nil {
		t.Fatalf("generated domains do not compile: %v", err)
	}
	routed := 0
	for _, q := range manifest.Questions {
		if set.Resolve(q.ID, q.Source) != nil {
			routed++
		}
	}
	if routed != len(manifest.Questions) {
		t.Errorf("%d of %d generated questions routed to a domain", routed, len(manifest.Questions))
	}
}

func TestValidateManifestCatchesALostNoContextRule(t *testing.T) {
	// A defence-in-depth assertion: if assembly ever stopped appending the rules, this
	// is what catches it rather than a bad manifest reaching a bid.
	m := &chat.BatchManifest{
		Version:   "1.0",
		Prompt:    "Answer everything confidently.",
		Questions: []chat.BatchQuestion{{ID: "C1", Question: "A requirement"}},
	}
	if err := ValidateManifest(m); err == nil {
		t.Fatal("expected a prompt with no no-context rule to be rejected")
	}
}

// requestText renders a recorded request's text — the system instruction and every
// text part of the contents — as a single string.
func requestText(t *testing.T, req fakeRequest) string {
	t.Helper()
	var b strings.Builder
	writeParts := func(content any) {
		c, _ := content.(map[string]any)
		parts, _ := c["parts"].([]any)
		for _, part := range parts {
			p, _ := part.(map[string]any)
			if text, ok := p["text"].(string); ok {
				b.WriteString(text)
				b.WriteString("\n")
			}
		}
	}
	writeParts(req.body["systemInstruction"])
	contents, _ := req.body["contents"].([]any)
	for _, c := range contents {
		writeParts(c)
	}
	return b.String()
}

// thinkingLevel returns the thinking level a recorded request asked for, or nil.
func thinkingLevel(t *testing.T, req fakeRequest) any {
	t.Helper()
	thinking, _ := req.generationConfig(t)["thinkingConfig"].(map[string]any)
	return thinking["thinkingLevel"]
}
