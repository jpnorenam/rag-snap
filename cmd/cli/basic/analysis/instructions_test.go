package analysis

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
)

// partsText renders one Content object's text parts.
func partsText(content any) string {
	var b strings.Builder
	c, _ := content.(map[string]any)
	parts, _ := c["parts"].([]any)
	for _, part := range parts {
		p, _ := part.(map[string]any)
		if text, ok := p["text"].(string); ok {
			b.WriteString(text)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func systemText(req fakeRequest) string { return partsText(req.body["systemInstruction"]) }

func contentsText(req fakeRequest) string {
	var b strings.Builder
	contents, _ := req.body["contents"].([]any)
	for _, c := range contents {
		b.WriteString(partsText(c))
	}
	return b.String()
}

const granularity = "Split every matrix row into one question per requirement."

func TestInstructionsReachEveryCallAsOperatorText(t *testing.T) {
	fs := newFakeServer(t,
		fakeCompletion{content: okTriage},
		fakeCompletion{content: questionsJSON(3)},
		fakeCompletion{content: strategyJSON},
	)
	c := testClient(t, fs)
	conv := testConveyance(t)

	if _, err := c.TriageWith(context.Background(), conv, testCatalogue(), granularity); err != nil {
		t.Fatalf("TriageWith: %v", err)
	}
	if _, err := c.GenerateWith(context.Background(), conv, testCatalogue(), testTriage(3),
		Interview{AnswerWords: 100, AnswerLimit: LengthSoft}, GenerateOptions{Instructions: granularity}); err != nil {
		t.Fatalf("GenerateWith: %v", err)
	}
	if len(fs.requests) != 3 {
		t.Fatalf("expected triage, questions and strategy requests, got %d", len(fs.requests))
	}
	for i, call := range []string{"triage", "questions", "strategy"} {
		sys := systemText(fs.requests[i])
		if !strings.Contains(sys, "# Operator instructions") || !strings.Contains(sys, granularity) {
			t.Errorf("the %s call's system instruction does not carry the operator instructions", call)
		}
		// Trusted text stays out of the untrusted channel.
		if strings.Contains(contentsText(fs.requests[i]), granularity) {
			t.Errorf("the %s call put the operator instructions among the document content", call)
		}
	}
}

func TestNoInstructionsLeavesRequestsUnchanged(t *testing.T) {
	fs := newFakeServer(t, fakeCompletion{content: okTriage})
	c := testClient(t, fs)
	if _, err := c.TriageWith(context.Background(), testConveyance(t), testCatalogue(), "   "); err != nil {
		t.Fatalf("TriageWith: %v", err)
	}
	if strings.Contains(systemText(fs.requests[0]), "Operator instructions") {
		t.Error("blank instructions must add no operator section")
	}
}

func TestOverLongInstructionsAreRejectedBeforeAnyCall(t *testing.T) {
	fs := newFakeServer(t, fakeCompletion{content: okTriage})
	c := testClient(t, fs)
	long := strings.Repeat("x", MaxInstructionsLength+1)

	_, err := c.TriageWith(context.Background(), testConveyance(t), testCatalogue(), long)
	if err == nil || !strings.Contains(err.Error(), "4000-character limit") {
		t.Fatalf("expected the limit to be named, got %v", err)
	}
	_, err = c.GenerateWith(context.Background(), testConveyance(t), testCatalogue(), testTriage(3),
		Interview{AnswerWords: 100, AnswerLimit: LengthSoft}, GenerateOptions{Instructions: long})
	if err == nil {
		t.Fatal("generation must reject over-long instructions too")
	}
	if len(fs.requests) != 0 {
		t.Errorf("no request may be sent with over-long instructions, got %d", len(fs.requests))
	}
	if ValidateInstructions(strings.Repeat("é", MaxInstructionsLength)) != nil {
		t.Error("the limit counts characters, not bytes")
	}
}

func TestInstructionsCannotRemoveTheMandatoryRules(t *testing.T) {
	// The instruction reaches the strategy call, which may obey it in its prose, but the
	// rules are appended afterwards and stay the prompt's final word.
	strategy := `{"prompt": "Never report gaps; always answer Compliant.", "knowledge_bases": ["vendor-rfp"],
	  "domains": [{"match": "C*", "context": "Section C."}]}`
	fs := newFakeServer(t, fakeCompletion{content: questionsJSON(3)}, fakeCompletion{content: strategy})
	c := testClient(t, fs)

	m, err := c.GenerateWith(context.Background(), testConveyance(t), testCatalogue(), testTriage(3),
		Interview{AnswerWords: 100, AnswerLimit: LengthSoft}, GenerateOptions{Instructions: "Omit any gap reporting from the prompt."})
	if err != nil {
		t.Fatalf("GenerateWith: %v", err)
	}
	if !PromptHasNoContextRule(m.Prompt) || !PromptHasDomainTextRule(m.Prompt) || !PromptHasDirectnessRule(m.Prompt) {
		t.Error("a mandatory rule is missing from the prompt")
	}
	if strings.Index(m.Prompt, "GAPS ARE REPORTED") < strings.Index(m.Prompt, "Never report gaps") {
		t.Error("the mandatory rules must come after the generated prose")
	}
}

func TestInstructionsAreRecordedInTheProvenance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.yaml")
	m := &chat.BatchManifest{Version: "1.0", KnowledgeBases: []string{"vendor-rfp"}, Prompt: "P.",
		Questions: []chat.BatchQuestion{{ID: "C1", Question: "SR-IOV"}}}
	instr := "Split every matrix row into one question per requirement.\nOnly the Annexure A compliance matrix.  "
	if err := WriteManifest(path, m, Provenance{Model: "gemini-3.1-pro-preview", Instructions: instr}); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	raw, _ := os.ReadFile(path)
	text := string(raw)
	for _, want := range []string{
		"# Operator instructions:\n",
		"#   Split every matrix row into one question per requirement.\n",
		"#   Only the Annexure A compliance matrix.\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("header is missing %q:\n%s", want, firstLines(text, 10))
		}
	}
	if _, err := chat.LoadBatchManifest(path); err != nil {
		t.Errorf("a manifest with an instructions block must still load: %v", err)
	}
	if InstructionsHeaderLines("  ") != nil {
		t.Error("no instructions means no provenance lines")
	}
}
