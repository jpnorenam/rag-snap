package analysis

import (
	"context"
	"errors"
	"fmt"
	"regexp"
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
	// Ids alone let the strategy call route by the document's own clause numbering and
	// attach a context to the wrong question, so each id must arrive with its text.
	for _, line := range []string{"- C1: Requirement 1", "- C3: Requirement 3"} {
		if !strings.Contains(body, line) {
			t.Errorf("the strategy call was not given %q", line)
		}
	}
}

func TestTrimAtWordKeepsWordsWholeAndMarksTheCut(t *testing.T) {
	long := strings.Repeat("requirement ", 40) // 480 bytes
	got := trimAtWord(long, 50)
	if !strings.HasSuffix(got, " …") || len(got) > 50+len(" …") {
		t.Errorf("trimAtWord = %q, want at most 50 bytes plus a cut marker", got)
	}
	if strings.Contains(strings.TrimSuffix(got, " …"), "requirem ") || strings.HasSuffix(strings.TrimSuffix(got, " …"), "requir") {
		t.Errorf("trimAtWord split a word: %q", got)
	}
	if got := trimAtWord("Does the\n  provider   support it?", 300); got != "Does the provider support it?" {
		t.Errorf("whitespace should collapse to single spaces, got %q", got)
	}
}

func TestExtractionInstructionsCoverEveryDemandColumn(t *testing.T) {
	// Both passes must count on the same basis, or a missed evidence column is missed
	// by both and the shortfall check has nothing to compare.
	if !strings.Contains(questionsInstruction, "EVERY COLUMN THAT MAKES A DEMAND") {
		t.Error("the questions instruction lost the every-demand-column rule")
	}
	if !strings.Contains(triageInstruction, "evidence, validation-proof, acceptance-criteria or deliverables column") {
		t.Error("the triage instruction does not count demands in secondary columns")
	}

	fs := newFakeServer(t, fakeCompletion{content: questionsJSON(3)}, fakeCompletion{content: strategyJSON})
	c := testClient(t, fs)
	if _, err := c.Generate(context.Background(), testConveyance(t), testCatalogue(), testTriage(3),
		Interview{AnswerWords: 100, AnswerLimit: LengthSoft}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(requestText(t, fs.requests[0]), "EVERY COLUMN THAT MAKES A DEMAND") {
		t.Error("the questions request did not carry the every-demand-column rule")
	}
}

func TestExtractionNeverSplitsByAnswerFieldOrInventsRangedIDs(t *testing.T) {
	// The live MASCOM regression: with the every-column rule alone, a high-effort
	// extraction emitted one question per answer field (compliance, product, licence,
	// cross-reference, comment) and gave individual ids to numbers inside ranged rows.
	for _, want := range []string{"ANSWER FIELDS ARE NOT QUESTIONS", "RANGED ROWS KEEP THEIR RANGE", `"FR_11-15-a"`} {
		if !strings.Contains(questionsInstruction, want) {
			t.Errorf("the questions instruction is missing %q", want)
		}
	}
	if !strings.Contains(triageInstruction, "answer-field columns the respondent fills in") {
		t.Error("triage must not count answer-field columns as questions, or the estimate inflates to match the regression")
	}
}

func TestShortfallSectionColumnFitsTheLongestName(t *testing.T) {
	err := &ShortfallError{Expected: 220, Produced: 77, FinishReason: "STOP", Effort: EffortMedium, Sections: []SectionCount{
		{Name: "A.1 Functional Requirements Compliance Matrix", Estimated: 100, Produced: 62},
		{Name: "A.2 Non-Functional Requirements Compliance Matrix", Estimated: 120, Produced: 15},
	}}
	var cols []int
	for _, line := range strings.Split(err.Error(), "\n") {
		if i := strings.Index(line, "produced "); i >= 0 && strings.HasPrefix(line, "  A.") {
			cols = append(cols, i)
		}
	}
	if len(cols) != 2 || cols[0] != cols[1] {
		t.Errorf("the produced column should align across sections, got offsets %v:\n%s", cols, err.Error())
	}
}

func TestStrategyIsToldContextsDirectAndNeverAssert(t *testing.T) {
	if !strings.Contains(strategyInstruction, "A context DIRECTS and never ASSERTS") {
		t.Error("the strategy instruction lost the directs-not-asserts rule")
	}
	if !strings.Contains(ManifestContract(), "### What a domain `context` may say") {
		t.Error("the contract lost its section on what a domain context may say")
	}
}

func TestWorkedExampleNamesAreAllPlaceholders(t *testing.T) {
	// A realistic example name was copied into a live generation ("vendor-rfp-answers").
	// Every retrieval target the example names must be visibly a placeholder.
	ex := ManifestExample()
	re := regexp.MustCompile(`"(knowledge_bases|kapa_source_groups)":\s*\[([^\]]*)\]`)
	lists := re.FindAllStringSubmatch(ex, -1)
	if len(lists) == 0 {
		t.Fatal("the worked example names no knowledge bases at all; this test no longer guards anything")
	}
	for _, l := range lists {
		for _, name := range regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(l[2], -1) {
			if !IsExampleName(name[1]) {
				t.Errorf("%s entry %q in the worked example is not an example- placeholder", l[1], name[1])
			}
		}
	}
	if !strings.Contains(testCatalogue().Prompt(), exampleNamePrefix) {
		t.Error("the catalogue prompt should say example names are never selectable")
	}
}

func TestCopiedExampleNameIsDiagnosedAsCopied(t *testing.T) {
	strategy := `{
	  "prompt": "A prompt.",
	  "knowledge_bases": ["vendor-rfp", "example-kb-past-answers"],
	  "domains": [{"match": "C*", "context": "Section C."}]
	}`
	fs := newFakeServer(t, fakeCompletion{content: questionsJSON(5)}, fakeCompletion{content: strategy})
	c := testClient(t, fs)

	_, err := c.Generate(context.Background(), testConveyance(t), testCatalogue(), testTriage(5),
		Interview{AnswerWords: 100, AnswerLimit: LengthSoft})
	if err == nil {
		t.Fatal("a knowledge base copied from the example must fail generation")
	}
	if !strings.Contains(err.Error(), "example-kb-past-answers (copied from the worked example") {
		t.Errorf("the error should say the name was copied from the example, got: %v", err)
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
	// The call finished normally, so the message says so and does not send the
	// operator after the reasoning budget — the advice that was backwards for MASCOM.
	msg := err.Error()
	for _, want := range []string{"finished normally", `"STOP"`, "By section:", "C. Section", "re-run the extraction at reasoning \"medium\""} {
		if !strings.Contains(msg, want) {
			t.Errorf("shortfall message should contain %q, got:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "lower "+ConfReasoningQuestions) {
		t.Errorf("a clean-finish shortfall must not prescribe lowering reasoning effort:\n%s", msg)
	}
	if len(shortfall.Questions) != 40 || shortfall.Effort != EffortLow {
		t.Errorf("the error must carry the 40 produced questions and the effort used, got %d and %q",
			len(shortfall.Questions), shortfall.Effort)
	}
}

func TestAcceptingAShortfallSkipsTheQuestionsCall(t *testing.T) {
	// Only a strategy response is queued: a second questions call would consume it and
	// fail the strategy parse, so this proves the accepted questions were used as-is.
	fs := newFakeServer(t, fakeCompletion{content: strategyJSON})
	c := testClient(t, fs)

	accepted := []chat.BatchQuestion{{ID: "C1", Question: "SR-IOV", Source: "C. Section"}, {ID: "C2", Question: "NUMA", Source: "C. Section"}}
	m, err := c.GenerateWith(context.Background(), testConveyance(t), testCatalogue(), testTriage(150),
		Interview{AnswerWords: 100, AnswerLimit: LengthSoft}, GenerateOptions{AcceptedQuestions: accepted})
	if err != nil {
		t.Fatalf("GenerateWith: %v", err)
	}
	if fs.calls != 1 {
		t.Errorf("expected only the strategy call, got %d calls", fs.calls)
	}
	if !strings.Contains(requestText(t, fs.requests[0]), "Design the answering strategy") {
		t.Error("the one call made should be the strategy call")
	}
	if len(m.Questions) != 2 || m.Questions[1].ID != "C2" {
		t.Errorf("the manifest should carry exactly the accepted questions, got %+v", m.Questions)
	}
}

func TestRerunOverridesTheQuestionsEffortOnly(t *testing.T) {
	fs := newFakeServer(t, fakeCompletion{content: questionsJSON(3)}, fakeCompletion{content: strategyJSON})
	c := testClient(t, fs) // questions effort low, strategy high

	if _, err := c.GenerateWith(context.Background(), testConveyance(t), testCatalogue(), testTriage(3),
		Interview{AnswerWords: 100, AnswerLimit: LengthSoft}, GenerateOptions{QuestionsEffort: EffortMedium}); err != nil {
		t.Fatalf("GenerateWith: %v", err)
	}
	if got := thinkingLevel(t, fs.requests[0]); got != "MEDIUM" {
		t.Errorf("re-run questions call thinkingLevel = %v, want MEDIUM", got)
	}
	if got := thinkingLevel(t, fs.requests[1]); got != "HIGH" {
		t.Errorf("strategy call thinkingLevel = %v, want its own HIGH, unaffected by the override", got)
	}
}

func TestAttributeSectionsByLongestPrefix(t *testing.T) {
	tr := &Triage{Sections: []TriageSection{
		{Name: "Functional", IDPrefix: "FR_*", EstimatedQuestions: 95},
		{Name: "Non-functional", IDPrefix: "NFR_", EstimatedQuestions: 115},
		{Name: "Narrative", EstimatedQuestions: 10}, // no prefix: unknown, not guessed
	}}
	qs := []chat.BatchQuestion{{ID: "FR_01-a"}, {ID: "fr_02"}, {ID: "NFR_01-a"}, {ID: "NFR_02-b"}, {ID: "NFR_03"}, {ID: "X1"}}
	got := attributeSections(tr, qs)

	want := []SectionCount{
		{Name: "Functional", IDPrefix: "FR_", Estimated: 95, Produced: 2},
		{Name: "Non-functional", IDPrefix: "NFR_", Estimated: 115, Produced: 3},
		{Name: "Narrative", Estimated: 10, Produced: -1},
		{Name: "(matching no section's id prefix)", Produced: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("attributeSections = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("section %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if attributeSections(nil, qs) != nil {
		t.Error("no triage means no breakdown")
	}
}

func TestAttributeSectionsFallsBackToSource(t *testing.T) {
	// The live MASCOM case: triage listed the sections with no id prefixes, but extraction
	// wrote each section's title into "source".
	tr := &Triage{Sections: []TriageSection{
		{Name: "A.1 Functional Requirements Compliance Matrix", EstimatedQuestions: 100},
		{Name: "A.2 Non-Functional Requirements Compliance Matrix", EstimatedQuestions: 130},
	}}
	qs := []chat.BatchQuestion{
		{ID: "FR_01-a", Source: "A.1 Functional Requirements Compliance Matrix / FR_01 Hypervisor"},
		{ID: "FR_02-a", Source: "a.1 functional requirements compliance matrix"},
		{ID: "NFR_01-a", Source: "A.2 Non-Functional Requirements Compliance Matrix / NFR_01 Performance"},
	}
	got := attributeSections(tr, qs)
	if len(got) != 2 || got[0].Produced != 2 || got[1].Produced != 1 {
		t.Errorf("attributeSections = %+v, want 2 and 1 attributed by source", got)
	}
}

func TestShortfallWithoutSectionEstimatesSaysSo(t *testing.T) {
	err := &ShortfallError{Expected: 230, Produced: 146, FinishReason: "STOP", Effort: EffortMedium,
		Sections: []SectionCount{{Name: "A.1", Produced: 80}, {Name: "A.2", Produced: 66}}}
	msg := err.Error()
	if !strings.Contains(msg, "no per-section estimates") || strings.Contains(msg, "points at unsplit rows") {
		t.Errorf("without estimates the message must not offer a comparison it cannot make:\n%s", msg)
	}
}

func TestTriageSchemaRequiresWhatTheBreakdownNeeds(t *testing.T) {
	sections := triageSchema()["properties"].(map[string]any)["sections"].(map[string]any)
	items := sections["items"].(map[string]any)
	req := fmt.Sprint(items["required"])
	for _, f := range []string{"id_prefix", "estimated_questions"} {
		if !strings.Contains(req, f) {
			t.Errorf("triage sections must require %q, got required=%s", f, req)
		}
	}
	// Both found live: Gemini rejects maxItems on this schema with a bare 400, and an
	// unused per-section summary was the field a whitespace runaway filled.
	if _, has := sections["maxItems"]; has {
		t.Error("Gemini rejects maxItems on the triage schema; it must not be sent")
	}
	if _, has := items["properties"].(map[string]any)["summary"]; has {
		t.Error("the unused per-section summary is a runaway surface and must not be in the schema")
	}
	if !strings.Contains(operatorSection("x"), "do not restate or quote them") {
		t.Error("the operator section must ask the model not to echo the instructions")
	}
}

func TestNextEffortLadder(t *testing.T) {
	for from, want := range map[Effort]Effort{
		EffortMinimal: EffortLow, EffortLow: EffortMedium, EffortMedium: EffortHigh,
		EffortUnset: EffortHigh, EffortHigh: EffortUnset,
	} {
		if got := NextEffort(from); got != want {
			t.Errorf("NextEffort(%q) = %q, want %q", from, got, want)
		}
	}
}

func TestTruncationIsNeverAShortfall(t *testing.T) {
	// Only a clean finish can be accepted; a cut-off response must never be.
	fs := newFakeServer(t, fakeCompletion{content: `{"questions":[`, finishReason: "MAX_TOKENS", thoughtTokens: 60000, answerTokens: 100})
	c := testClient(t, fs)
	_, err := c.Generate(context.Background(), testConveyance(t), testCatalogue(), testTriage(150),
		Interview{AnswerWords: 100, AnswerLimit: LengthSoft})
	if !IsTruncated(err) || IsShortfall(err) {
		t.Errorf("a truncated questions call must be a TruncatedError and never a ShortfallError, got %T: %v", err, err)
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
