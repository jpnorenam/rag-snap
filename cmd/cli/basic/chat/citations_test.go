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

func hitFor(key, label, content string) knowledge.SearchHit {
	return knowledge.SearchHit{
		Index:    "rag-snap-context-docs",
		SourceID: key,
		Label:    knowledge.LabelCanonical,
		Content:  content,
		Citation: knowledge.Citation{Key: key, Label: label, Base: "docs"},
	}
}

// A local source ingested by URL and the kapa.ai page for the same URL share a
// source id, but are different citations and must not share a marker.
func TestFormatContextSeparatesSameKeyFromDifferentBases(t *testing.T) {
	url := "https://ubuntu.com/pro"
	local := hitFor(url, "Ubuntu Pro (local)", "local chunk")
	kapa := hitFor(url, "Ubuntu Pro", "kapa chunk")
	kapa.Citation.Base = "kapa"
	_, retrieved := formatContext([]knowledge.SearchHit{local, kapa}, NewCitationRegistry())
	if len(retrieved) != 2 || retrieved[0].Base != "docs" || retrieved[1].Base != "kapa" {
		t.Errorf("retrieved = %+v, want separate local and kapa sources", retrieved)
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

func keys(sources []CitedSource) []string {
	out := make([]string, len(sources))
	for i, s := range sources {
		out[i] = s.Key
	}
	return out
}

func TestCiteAnswer(t *testing.T) {
	cases := []struct {
		name        string
		answer      string
		retrieved   []CitedSource
		wantAnswer  string
		wantKeys    []string // cited documents, in answer-marker order
		wantUncited bool
	}{
		{
			name:       "invented marker dropped",
			answer:     "Pro covers FIPS. [1][7]",
			retrieved:  retrievedSources("a", "b"),
			wantAnswer: "Pro covers FIPS. [1]",
			wantKeys:   []string{"a"},
		},
		{
			name:       "markers are renumbered in first-cited order",
			answer:     "First point. [2]\n\nSecond point. [3][2]\n\nIn short. [1]",
			retrieved:  retrievedSources("a", "b", "c"),
			wantAnswer: "First point. [1]\n\nSecond point. [2][1]\n\nIn short. [3]",
			wantKeys:   []string{"b", "c", "a"},
		},
		{
			name:       "list forms are normalized and renumbered",
			answer:     "One. [3, 1]\n\nTwo. [1;2]",
			retrieved:  retrievedSources("a", "b", "c"),
			wantAnswer: "One. [1][2]\n\nTwo. [2][3]",
			wantKeys:   []string{"c", "a", "b"},
		},
		{
			name:       "a document repeated within a run is cited once",
			answer:     "One. [2, 2]",
			retrieved:  retrievedSources("a", "b"),
			wantAnswer: "One. [1]",
			wantKeys:   []string{"b"},
		},
		{
			name:       "legacy S-prefixed markers are renumbered",
			answer:     "One. [S2, S1] Two. [S1]",
			retrieved:  retrievedSources("a", "b"),
			wantAnswer: "One. [1][2] Two. [2]",
			wantKeys:   []string{"b", "a"},
		},
		{
			name:       "code and indexing are not markers",
			answer:     "Use `arr[1]` or\n```\nx = [1, 2]\n```\nsee my_items[2], obj.items[2] and f(x)[2]. [2]",
			retrieved:  retrievedSources("a", "b"),
			wantAnswer: "Use `arr[1]` or\n```\nx = [1, 2]\n```\nsee my_items[2], obj.items[2] and f(x)[2]. [1]",
			wantKeys:   []string{"b"},
		},
		{
			name:       "a marker glued to a prose word is a citation",
			answer:     "It supports SR-IOV[1]. Ubuntu 22.04[2] and (FIPS)[3] too; see the end.[1, 2]",
			retrieved:  retrievedSources("a", "b", "c"),
			wantAnswer: "It supports SR-IOV[1]. Ubuntu 22.04[2] and (FIPS)[3] too; see the end.[1][2]",
			wantKeys:   []string{"a", "b", "c"},
		},
		{
			name:       "bracketed numbers citing nothing retrieved are prose",
			answer:     "Configure option [7] in the BIOS menu. [1]",
			retrieved:  retrievedSources("a"),
			wantAnswer: "Configure option [7] in the BIOS menu. [1]",
			wantKeys:   []string{"a"},
		},
		{
			name:       "a marker followed by (…) is a marker, not link text",
			answer:     "See [2](https://evil.example).",
			retrieved:  retrievedSources("a", "b"),
			wantAnswer: "See [1](https://evil.example).",
			wantKeys:   []string{"b"},
		},
		{
			name:       "years and large numbers are not markers",
			answer:     "Released in [2024]. [2]",
			retrieved:  retrievedSources("a", "b"),
			wantAnswer: "Released in [2024]. [1]",
			wantKeys:   []string{"b"},
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
			wantAnswer:  "Ubuntu Pro covers FIPS. [9]",
			wantUncited: true,
		},
		{
			name:        "markers inside reasoning do not count and are left alone",
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
			if len(cited) != 0 || len(tc.wantKeys) != 0 {
				if !reflect.DeepEqual(keys(cited), tc.wantKeys) {
					t.Errorf("cited documents = %v, want %v", keys(cited), tc.wantKeys)
				}
			}
			for i, s := range cited {
				if s.Marker != i+1 {
					t.Errorf("cited markers = %v, want 1..%d", markers(cited), len(cited))
					break
				}
			}
		})
	}
}

func TestStripMarkers(t *testing.T) {
	sources := retrievedSources("a", "b", "c")
	for in, want := range map[string]string{
		"The solution provides FIPS 140-3 certified modules. [1][2]":    "The solution provides FIPS 140-3 certified modules.",
		"Canonical offers support [1]. It also offers training [2, 3].": "Canonical offers support. It also offers training.",
		"No markers here.": "No markers here.",
		"Configure option [12] in the BIOS menu. [1]": "Configure option [12] in the BIOS menu.",
		"Supports SR-IOV[1].":                         "Supports SR-IOV.",
		"Line one [1]  \nLine two [2]":                "Line one  \nLine two",
	} {
		if got := stripMarkers(in, sources); got != want {
			t.Errorf("stripMarkers(%q) = %q, want %q", in, got, want)
		}
	}
}

// The citation rules travel with retrieved context only: a session's system
// prompt is exactly the resolved prompt, so a turn without retrieval never sees
// the rules' "[1]" example.
func TestCitationRulesStayOutOfSystemPrompt(t *testing.T) {
	ls, err := NewLiveSession(BackendConfig{BaseURL: "http://127.0.0.1:1"}, "model", nil, nil, "", nil, "CUSTOM VARIANT", 0.3, false)
	if err != nil {
		t.Fatalf("NewLiveSession: %v", err)
	}
	if ls.conv.system != "CUSTOM VARIANT" {
		t.Errorf("system prompt = %q, want the resolved prompt unchanged", ls.conv.system)
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
