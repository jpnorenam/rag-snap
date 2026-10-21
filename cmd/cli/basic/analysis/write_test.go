package analysis

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
)

func sampleManifest() *chat.BatchManifest {
	return &chat.BatchManifest{
		Version:        "1.0",
		KnowledgeBases: []string{"vendor-rfp"},
		Prompt: "You are a solutions engineer.\n\nANSWER SHAPE\nTwo to four sentences.\n" +
			"\nGAPS ARE REPORTED\nSay that no context is available rather than inventing.\n",
		Domains: []chat.Domain{{
			Match:    "C*",
			Context:  "Section C covers platform capabilities.",
			Keywords: chat.KeywordList{"SR-IOV"},
		}},
		Questions: []chat.BatchQuestion{{ID: "C1", Question: "SR-IOV", Source: "C. Section"}},
	}
}

func writeAndRead(t *testing.T, m *chat.BatchManifest, p Provenance) (string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "generated.yaml")
	if err := WriteManifest(path, m, p); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	return path, string(data)
}

func TestWriteManifestProducesALoadableFileWithProvenance(t *testing.T) {
	path, text := writeAndRead(t, sampleManifest(), Provenance{
		Model: "vendor/model-1",
		Documents: []DocumentReport{
			{Filename: "rfp.xlsx", Format: "xlsx", Path: PathConverted},
		},
	})

	// Provenance is readable from the file itself.
	for _, want := range []string{"--analyze", "vendor/model-1", "rfp.xlsx", "1 questions, 1 domains"} {
		if !strings.Contains(text, want) {
			t.Errorf("header is missing %q:\n%s", want, firstLines(text, 12))
		}
	}

	// And it loads through the reader a run uses, with no hand-editing.
	loaded, err := chat.LoadBatchManifest(path)
	if err != nil {
		t.Fatalf("the written manifest does not load: %v", err)
	}
	if len(loaded.Questions) != 1 || loaded.Questions[0].ID != "C1" {
		t.Errorf("questions did not round-trip: %+v", loaded.Questions)
	}
	if len(loaded.Domains) != 1 || loaded.Domains[0].Match != "C*" {
		t.Errorf("domains did not round-trip: %+v", loaded.Domains)
	}
	if !strings.Contains(loaded.Prompt, "ANSWER SHAPE") {
		t.Error("prompt did not round-trip")
	}
}

func TestWriteManifestRecordsAnAcceptedShortfall(t *testing.T) {
	_, text := writeAndRead(t, sampleManifest(), Provenance{
		Model:             "gemini-3.1-pro-preview",
		ShortfallAccepted: &ShortfallAcceptance{Expected: 168, Produced: 111},
	})
	want := "# Question count accepted by the operator below the triage estimate: 111 produced, about 168 estimated"
	if !strings.Contains(text, want) {
		t.Errorf("header is missing the acceptance line:\n%s", firstLines(text, 12))
	}

	_, plain := writeAndRead(t, sampleManifest(), Provenance{Model: "gemini-3.1-pro-preview"})
	if strings.Contains(plain, "accepted by the operator") {
		t.Error("a manifest generated without a shortfall must not claim one was accepted")
	}
}

func TestWriteManifestKeepsThePromptAsAReadableLiteralBlock(t *testing.T) {
	// The whole point: a two-thousand-word prompt has to stay legible and editable in
	// the file. A quoted scalar with escaped newlines would be neither.
	_, text := writeAndRead(t, sampleManifest(), Provenance{})

	if !strings.Contains(text, "prompt: |") {
		t.Errorf("prompt should be a literal block scalar:\n%s", text)
	}
	if strings.Contains(text, `\n`) {
		t.Errorf("the prompt was encoded with escaped newlines, which is unreadable:\n%s", text)
	}
}

func TestWriteManifestSurvivesTrailingWhitespaceFromTheModel(t *testing.T) {
	// A single line ending in a space makes the YAML encoder abandon the literal block
	// and emit one enormous quoted line instead. Models produce trailing spaces
	// routinely, so this is the difference between a file an operator will edit and one
	// they will not.
	m := sampleManifest()
	m.Prompt = "You are a solutions engineer.   \n\nANSWER SHAPE\t\nTwo to four sentences.  \n" +
		"\nGAPS\nSay that no context is available.  \n"
	m.Domains[0].Context = "Section C.   \nWith a trailing space.  "

	path, text := writeAndRead(t, m, Provenance{})

	if !strings.Contains(text, "prompt: |") {
		t.Errorf("prompt should still be a literal block after normalisation:\n%s", text)
	}
	if strings.Contains(text, `\n`) {
		t.Errorf("trailing whitespace forced an escaped scalar:\n%s", text)
	}
	// Content must survive the normalisation.
	loaded, err := chat.LoadBatchManifest(path)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if !strings.Contains(loaded.Prompt, "Two to four sentences.") {
		t.Error("prompt content was lost while stripping trailing whitespace")
	}
	if !strings.Contains(loaded.Domains[0].Context, "With a trailing space.") {
		t.Error("domain context was lost while stripping trailing whitespace")
	}
}

func TestWriteManifestWritesNoCommentedOutDomainsStub(t *testing.T) {
	// The extraction path emits a commented-out stub of candidate patterns. Here the
	// domains block is real, and a stub beneath it would be confusing.
	_, text := writeAndRead(t, sampleManifest(), Provenance{})

	if strings.Contains(text, "# domains:") {
		t.Errorf("a generated manifest must not carry the commented-out stub:\n%s", text)
	}
	if !strings.Contains(text, "\ndomains:\n") {
		t.Errorf("the real domains block is missing:\n%s", text)
	}
}

func TestStripTrailingSpaceCollapsesBlankRuns(t *testing.T) {
	got := stripTrailingSpace("a\n\n\n\nb\n\n\n")
	if got != "a\n\nb\n" {
		t.Errorf("stripTrailingSpace = %q, want %q", got, "a\n\nb\n")
	}
	if stripTrailingSpace("") != "" {
		t.Error("an empty string should stay empty")
	}
}

func TestNormaliseTrimsIdentifiers(t *testing.T) {
	m := &chat.BatchManifest{
		Questions: []chat.BatchQuestion{{ID: "  C1 ", Question: " SR-IOV ", Source: " C "}},
		Domains:   []chat.Domain{{Match: " C* ", Context: "x"}},
	}
	normalise(m)

	if m.Questions[0].ID != "C1" || m.Questions[0].Source != "C" {
		t.Errorf("identifiers not trimmed: %+v", m.Questions[0])
	}
	// A pattern with stray spaces would match nothing, so trimming it matters.
	if m.Domains[0].Match != "C*" {
		t.Errorf("domain match not trimmed: %q", m.Domains[0].Match)
	}
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
