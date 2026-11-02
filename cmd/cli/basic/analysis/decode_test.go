package analysis

import (
	"encoding/json"
	"strings"
	"testing"
)

func mustExtract(t *testing.T, in string) string {
	t.Helper()
	got, err := extractJSONBlock(in)
	if err != nil {
		t.Fatalf("extractJSONBlock: %v", err)
	}
	if !json.Valid([]byte(got)) {
		t.Fatalf("extracted payload is not valid JSON:\n%s", got)
	}
	return got
}

func TestExtractBareJSON(t *testing.T) {
	// The normal case for a schema-constrained response.
	const in = `{"questions":[{"id":"C1","question":"SR-IOV"}]}`
	if got := mustExtract(t, in); got != in {
		t.Errorf("got %q, want the input unchanged", got)
	}
}

func TestExtractFencedJSON(t *testing.T) {
	in := "```json\n{\"questions\":[{\"id\":\"C1\",\"question\":\"SR-IOV\"}]}\n```"
	got := mustExtract(t, in)
	if !strings.Contains(got, `"C1"`) {
		t.Errorf("fenced payload not recovered: %s", got)
	}
}

func TestExtractBareFenceWithoutLanguage(t *testing.T) {
	in := "```\n{\"questions\":[]}\n```"
	if got := mustExtract(t, in); !strings.Contains(got, "questions") {
		t.Errorf("bare-fence payload not recovered: %s", got)
	}
}

// TestExtractSurvivesBackticksInsideThePayload is the regression this extractor exists
// for. Closing at the first "```" cut the payload mid-structure whenever a
// triple-backtick appeared inside it — which happens because question and context
// strings are lifted from a document that may contain one, and because the worked
// example in the grounding assets is itself a fenced block the model has just been
// shown. Downstream this surfaced as an opaque YAML "unexpected end of stream".
func TestExtractSurvivesBackticksInsideThePayload(t *testing.T) {
	payload := `{"questions":[` +
		`{"id":"C1","question":"Describe support for fenced code, e.g. ` + "```" + `yaml blocks"},` +
		`{"id":"C2","question":"CPU pinning"}` +
		`]}`
	in := "```json\n" + payload + "\n```"

	got := mustExtract(t, in)

	var out questionsResponse
	if err := json.Unmarshal([]byte(got), &out); err != nil {
		t.Fatalf("decoding the recovered payload: %v", err)
	}
	// Both questions must survive; the old extractor lost everything after the inner
	// backticks.
	if len(out.Questions) != 2 {
		t.Fatalf("recovered %d questions, want 2 — the payload was cut at an inner fence", len(out.Questions))
	}
	if out.Questions[1].ID != "C2" {
		t.Errorf("second question = %q, want C2", out.Questions[1].ID)
	}
}

func TestExtractJSONWrappedInProse(t *testing.T) {
	// A model that ignores "return only JSON" and explains itself first.
	in := `Here is the manifest you asked for:
{"questions":[{"id":"C1","question":"SR-IOV"}]}
Let me know if you need changes.`
	got := mustExtract(t, in)
	if strings.Contains(got, "Here is") || strings.Contains(got, "Let me know") {
		t.Errorf("prose was not stripped: %s", got)
	}
}

func TestExtractPrefersTheWholeTextWhenItIsAlreadyValid(t *testing.T) {
	// A valid payload that merely *contains* a fence marker in a string must not be
	// re-sliced: the whole text parses, so it is taken as-is.
	in := `{"questions":[{"id":"C1","question":"Explain ` + "```" + ` fences"}]}`
	if got := mustExtract(t, in); got != in {
		t.Errorf("a valid payload should be returned unchanged, got: %s", got)
	}
}

func TestExtractReturnsBestEffortForTruncatedPayload(t *testing.T) {
	// Genuinely cut off: nothing can be recovered, but the caller needs the payload to
	// build a diagnostic, so an error here would throw away the evidence.
	const cut = `{"questions":[{"id":"C1","question":"SR-IOV"},{"id":"C2","`
	got, err := extractJSONBlock(cut)
	if err != nil {
		t.Fatalf("a truncated payload should still return its content, got error: %v", err)
	}
	if json.Valid([]byte(got)) {
		t.Fatal("a truncated payload must not be reported as valid")
	}
	if !strings.Contains(got, "SR-IOV") {
		t.Errorf("the partial payload should be preserved as evidence, got: %s", got)
	}
}

func TestExtractEmptyIsAnError(t *testing.T) {
	if _, err := extractJSONBlock("   \n  "); err == nil {
		t.Fatal("empty content should be an error")
	}
}

func TestGreedyFencedSpanKeepsAnEmbeddedFence(t *testing.T) {
	// The pairing that survives a triple-backtick inside the payload.
	inner, ok := greedyFencedSpan("```json\nA ``` B\n```")
	if !ok {
		t.Fatal("expected a fenced span")
	}
	if !strings.Contains(inner, "A ``` B") {
		t.Errorf("span should cover the embedded fence, got %q", inner)
	}
}

func TestFencedBlocksEnumeratesEveryBlock(t *testing.T) {
	// A document may carry several blocks — the grounding assets do — so they must be
	// enumerated rather than run together.
	in := "prose\n```json\n{\"a\":1}\n```\nmore prose\n```json\n{\"b\":2}\n```\n"
	blocks := fencedBlocks(in)
	if len(blocks) != 2 {
		t.Fatalf("got %d blocks, want 2: %q", len(blocks), blocks)
	}
	if !strings.Contains(blocks[0], `"a"`) || !strings.Contains(blocks[1], `"b"`) {
		t.Errorf("blocks came back wrong: %q", blocks)
	}
}

func TestExtractPicksTheFirstValidBlockOfSeveral(t *testing.T) {
	in := "Here is the manifest:\n```json\n{\"questions\":[{\"id\":\"C1\",\"question\":\"SR-IOV\"}]}\n```\n" +
		"And an aside:\n```json\n{\"unrelated\":true}\n```\n"
	got := mustExtract(t, in)
	if !strings.Contains(got, "C1") {
		t.Errorf("should have taken the first block, got: %s", got)
	}
	if strings.Contains(got, "unrelated") {
		t.Errorf("blocks must not be run together, got: %s", got)
	}
}
