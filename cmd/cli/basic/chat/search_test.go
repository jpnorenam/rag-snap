package chat

import (
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/internal/apiclient"
)

func TestParseSearchArgs(t *testing.T) {
	tests := []struct {
		name string
		args string
		want searchArgs
		ok   bool
	}{
		{"terms only", "ceph osd", searchArgs{k: defaultRAGTopK, terms: "ceph osd"}, true},
		{"flag before terms", "--show-similar kafka", searchArgs{k: defaultRAGTopK, showSimilar: true, terms: "kafka"}, true},
		{"flag between terms", "ceph --show-similar osd", searchArgs{k: defaultRAGTopK, showSimilar: true, terms: "ceph osd"}, true},
		{"flag after terms", "kafka --show-similar", searchArgs{k: defaultRAGTopK, showSimilar: true, terms: "kafka"}, true},
		{"with -k", "-k 5 --show-similar kafka", searchArgs{k: 5, showSimilar: true, terms: "kafka"}, true},
		{"with -k=", "kafka -k=3", searchArgs{k: 3, terms: "kafka"}, true},
		{"flag without terms", "--show-similar", searchArgs{}, false},
		{"flag and -k without terms", "-k 5 --show-similar", searchArgs{}, false},
		{"flag with a value", "--show-similar=true kafka", searchArgs{}, false},
		{"invalid -k", "-k 0 kafka", searchArgs{}, false},
		{"empty", "", searchArgs{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseSearchArgs(tt.args)
			if ok != tt.ok || got != tt.want {
				t.Errorf("parseSearchArgs(%q) = (%+v, %v), want (%+v, %v)", tt.args, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func boolPtr(b bool) *bool { return &b }

// TestSearchOutputs renders the same hits arriving from direct mode and from
// the daemon, so both /search modes agree on the split as well as the wording.
func TestSearchOutputs(t *testing.T) {
	noColor := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = noColor })
	notice := func(n int) string { return knowledge.SimilarNotice(n) + " Use --show-similar to show them." }
	index := knowledge.FullIndexName("default")

	type hit struct {
		source  string
		score   float64
		matched bool
	}
	cases := []struct {
		name        string
		hits        []hit
		showSimilar bool
		want        []string // substrings, in order
		absent      []string
	}{
		{
			name:   "matches only",
			hits:   []hit{{"a", 5, true}, {"b", 1, true}},
			want:   []string{"[1] score 5.0000", "source: a", "[2] score 1.0000", "source: b"},
			absent: []string{"similar"},
		},
		{
			name:   "matches plus hidden similar",
			hits:   []hit{{"a", 5, true}, {"c", -2, false}},
			want:   []string{"source: a", notice(1)},
			absent: []string{"source: c", knowledge.SimilarSeparator()},
		},
		{
			name:   "no matches plus hidden similar",
			hits:   []hit{{"c", -2, false}, {"d", -5, false}},
			want:   []string{knowledge.NoMatchesMessage("kafka"), notice(2)},
			absent: []string{"source: c"},
		},
		{
			name:        "show similar",
			hits:        []hit{{"a", 5, true}, {"c", -2, false}},
			showSimilar: true,
			want:        []string{"[1] score 5.0000", "source: a", knowledge.SimilarSeparator(), "[2] score -2.0000", "source: c"},
			absent:      []string{"Use --show-similar"},
		},
	}

	for _, tc := range cases {
		local := make([]knowledge.SearchHit, len(tc.hits))
		remote := make([]apiclient.SearchHit, len(tc.hits))
		for i, h := range tc.hits {
			local[i] = knowledge.SearchHit{Index: index, Score: h.score, SourceID: h.source, Label: "canonical", Matched: h.matched}
			remote[i] = apiclient.SearchHit{Base: "default", Score: h.score, SourceID: h.source, Label: "canonical", Matched: boolPtr(h.matched)}
		}
		outputs := map[string]string{
			"direct": formatSearchOutput("kafka", directSearchHits(local), tc.showSimilar),
			"daemon": formatSearchOutput("kafka", remoteSearchHits(remote), tc.showSimilar),
		}
		for mode, out := range outputs {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				rest := out
				for _, w := range tc.want {
					i := strings.Index(rest, w)
					if i < 0 {
						t.Fatalf("missing %q (in order) in output:\n%s", w, out)
					}
					rest = rest[i+len(w):]
				}
				for _, a := range tc.absent {
					if strings.Contains(out, a) {
						t.Errorf("unexpected %q in output:\n%s", a, out)
					}
				}
			})
		}
		if outputs["direct"] != outputs["daemon"] {
			t.Errorf("%s: direct and daemon /search output differ:\n--- direct\n%s\n--- daemon\n%s", tc.name, outputs["direct"], outputs["daemon"])
		}
	}
}
