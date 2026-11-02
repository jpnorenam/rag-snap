package chat

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
)

func TestCitationRegistryStableNumbering(t *testing.T) {
	reg := NewCitationRegistry()
	if n := reg.Number("guide.pdf"); n != 1 {
		t.Fatalf("first document = %d, want 1", n)
	}
	if n := reg.Number("faq.md"); n != 2 {
		t.Fatalf("second document = %d, want 2", n)
	}
	// A later turn re-retrieves guide.pdf alongside a new document.
	if n := reg.Number("guide.pdf"); n != 1 {
		t.Errorf("re-retrieved document = %d, want 1", n)
	}
	if n := reg.Number("new.md"); n != 3 {
		t.Errorf("new document = %d, want 3", n)
	}
}

func TestCitationRegistrySeed(t *testing.T) {
	reg := NewCitationRegistry()
	reg.Seed([]CitedSource{
		{Marker: 2, Citation: knowledge.Citation{Key: "guide.pdf"}},
		{Marker: 1, Citation: knowledge.Citation{Key: "faq.md"}},
	})
	if n := reg.Number("guide.pdf"); n != 2 {
		t.Errorf("previously cited document = %d, want 2", n)
	}
	if n := reg.Number("new.md"); n != 3 {
		t.Errorf("new document after resume = %d, want 3", n)
	}
}

func hitFor(key, label, content string) knowledge.SearchHit {
	return knowledge.SearchHit{
		Index:    "rag-snap-context-docs",
		SourceID: key,
		Label:    knowledge.LabelCanonical,
		Content:  content,
		Citation: knowledge.Citation{Key: key, Label: label},
	}
}

func TestFormatContextSharesMarkerPerDocument(t *testing.T) {
	hits := []knowledge.SearchHit{
		hitFor("guide.pdf", "Install Guide", "chunk one"),
		hitFor("faq.md", "faq.md", "chunk two"),
		hitFor("guide.pdf", "Install Guide", "chunk three"),
	}
	text, retrieved := formatContext(hits, NewCitationRegistry())

	if got := strings.Count(text, "[1] [CANONICAL] Install Guide\n"); got != 2 {
		t.Errorf("guide.pdf chunks prefixed with [1] %d times, want 2:\n%s", got, text)
	}
	if !strings.Contains(text, "[2] [CANONICAL] faq.md\nchunk two") {
		t.Errorf("faq.md chunk not prefixed with [2]:\n%s", text)
	}
	if strings.Contains(text, "score") {
		t.Errorf("context still carries scores:\n%s", text)
	}
	if len(retrieved) != 2 || retrieved[0].Key != "guide.pdf" || retrieved[1].Key != "faq.md" {
		t.Errorf("retrieved = %+v, want guide.pdf then faq.md", retrieved)
	}
}

func retrievedSources(keys ...string) []CitedSource {
	out := make([]CitedSource, len(keys))
	for i, k := range keys {
		out[i] = CitedSource{Marker: i + 1, Citation: knowledge.Citation{Key: k, Label: k}}
	}
	return out
}

func markers(sources []CitedSource) []int {
	out := make([]int, len(sources))
	for i, s := range sources {
		out[i] = s.Marker
	}
	return out
}

func TestCiteAnswer(t *testing.T) {
	cases := []struct {
		name        string
		answer      string
		retrieved   []CitedSource
		wantAnswer  string
		wantMarkers []int
		wantUncited bool
	}{
		{
			name:        "invented marker dropped",
			answer:      "Pro covers FIPS. [1][7]",
			retrieved:   retrievedSources("a", "b"),
			wantAnswer:  "Pro covers FIPS. [1]",
			wantMarkers: []int{1},
		},
		{
			name:        "only cited documents are sources",
			answer:      "First point. [2]",
			retrieved:   retrievedSources("a", "b", "c"),
			wantAnswer:  "First point. [2]",
			wantMarkers: []int{2},
		},
		{
			name:        "list forms are normalized, first-cited order",
			answer:      "One. [3, 1]\n\nTwo. [1;2]",
			retrieved:   retrievedSources("a", "b", "c"),
			wantAnswer:  "One. [3][1]\n\nTwo. [1][2]",
			wantMarkers: []int{3, 1, 2},
		},
		{
			name:        "legacy S-prefixed markers are renumbered",
			answer:      "One. [S2, S1] Two. [S1]",
			retrieved:   retrievedSources("a", "b"),
			wantAnswer:  "One. [2][1] Two. [1]",
			wantMarkers: []int{2, 1},
		},
		{
			name:        "code and indexing are not markers",
			answer:      "Use `arr[1]` or\n```\nx = [1, 2]\n```\nsee items[2]. [1]",
			retrieved:   retrievedSources("a", "b"),
			wantAnswer:  "Use `arr[1]` or\n```\nx = [1, 2]\n```\nsee items[2]. [1]",
			wantMarkers: []int{1},
		},
		{
			name:        "a marker followed by (…) is a marker, not link text",
			answer:      "See [2](https://evil.example).",
			retrieved:   retrievedSources("a", "b"),
			wantAnswer:  "See [2](https://evil.example).",
			wantMarkers: []int{2},
		},
		{
			name:        "years and large numbers are not markers",
			answer:      "Released in [2024]. [2]",
			retrieved:   retrievedSources("a", "b"),
			wantAnswer:  "Released in [2024]. [2]",
			wantMarkers: []int{2},
		},
		{
			name:        "nothing cited is kept and flagged uncited",
			answer:      "Ubuntu Pro covers FIPS.",
			retrieved:   retrievedSources("a"),
			wantAnswer:  "Ubuntu Pro covers FIPS.",
			wantUncited: true,
		},
		{
			name:        "only invented markers is flagged uncited",
			answer:      "Ubuntu Pro covers FIPS. [9]",
			retrieved:   retrievedSources("a"),
			wantAnswer:  "Ubuntu Pro covers FIPS.",
			wantUncited: true,
		},
		{
			name:        "markers inside reasoning do not count",
			answer:      "<think>use [1]</think>Ubuntu Pro covers FIPS.",
			retrieved:   retrievedSources("a"),
			wantAnswer:  "<think>use [1]</think>Ubuntu Pro covers FIPS.",
			wantUncited: true,
		},
		{
			name:       "no retrieval leaves the answer untouched",
			answer:     "Hello! How can I help?",
			retrieved:  nil,
			wantAnswer: "Hello! How can I help?",
		},
		{
			name:       "no retrieval removes markers, which cite nothing",
			answer:     "Kafka is a distributed log. [1]",
			retrieved:  nil,
			wantAnswer: "Kafka is a distributed log.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, cited, uncited := citeAnswer(tc.answer, tc.retrieved)
			if got != tc.wantAnswer {
				t.Errorf("answer = %q, want %q", got, tc.wantAnswer)
			}
			if uncited != tc.wantUncited {
				t.Errorf("uncited = %v, want %v", uncited, tc.wantUncited)
			}
			if gotMarkers := markers(cited); len(cited) != 0 || len(tc.wantMarkers) != 0 {
				if !reflect.DeepEqual(gotMarkers, tc.wantMarkers) {
					t.Errorf("cited markers = %v, want %v", gotMarkers, tc.wantMarkers)
				}
			}
		})
	}
}

func TestStripMarkers(t *testing.T) {
	for in, want := range map[string]string{
		"The solution provides FIPS 140-3 certified modules. [1][2]":    "The solution provides FIPS 140-3 certified modules.",
		"Canonical offers support [1]. It also offers training [2, 3].": "Canonical offers support. It also offers training.",
		"No markers here.": "No markers here.",
	} {
		if got := stripMarkers(in); got != want {
			t.Errorf("stripMarkers(%q) = %q, want %q", in, got, want)
		}
	}
}

// The citation rules travel with retrieved context only: a session's system
// prompt is exactly the resolved prompt, so a turn without retrieval never sees
// the rules' "[1]" example.
func TestCitationRulesStayOutOfSystemPrompt(t *testing.T) {
	ls, err := NewLiveSession("http://127.0.0.1:1", "model", nil, "", nil, "CUSTOM VARIANT", 0.3, false)
	if err != nil {
		t.Fatalf("NewLiveSession: %v", err)
	}
	if ls.systemPrompt != "CUSTOM VARIANT" {
		t.Errorf("system prompt = %q, want the resolved prompt unchanged", ls.systemPrompt)
	}
}

func TestCitationRulesAccompanyRetrievedContext(t *testing.T) {
	cited := buildRAGPrompt("[1] [CANONICAL] guide\nchunk", "", "", "q?", true)
	if !strings.Contains(cited, ragCitationRules) {
		t.Errorf("prompt with retrieved context lacks the citation rules:\n%s", cited)
	}
	if strings.Index(cited, ragCitationRules) > strings.Index(cited, "Question:") {
		t.Errorf("citation rules should follow the context and precede the question:\n%s", cited)
	}
	empty := buildRAGPrompt("No relevant context was retrieved for this query.", "", "", "q?", false)
	if strings.Contains(empty, "Citation rules") {
		t.Errorf("empty-context prompt carries the citation rules:\n%s", empty)
	}
}
