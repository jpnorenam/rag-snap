package basic

import (
	"strings"
	"testing"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
)

func searchView(source string, score float64, matched bool) searchHitView {
	return searchHitView{Score: score, Where: "base: default", Label: "canonical", SourceID: source, Content: "content of " + source, Matched: matched}
}

func TestFormatSearchHitViews(t *testing.T) {
	notice := func(n int) string { return knowledge.SimilarNotice(n) + " Use --show-similar to show them." }

	cases := []struct {
		name        string
		hits        []searchHitView
		showSimilar bool
		want        []string // substrings, in order
		absent      []string
	}{
		{
			name:   "matches only",
			hits:   []searchHitView{searchView("a", 5, true), searchView("b", 1, true)},
			want:   []string{"Result 1 (score: 5.0000", "Source: a", "Result 2", "Source: b", "Total: 2 results"},
			absent: []string{"similar", knowledge.SimilarSeparator()},
		},
		{
			name:   "matches plus hidden similar",
			hits:   []searchHitView{searchView("a", 5, true), searchView("c", -2, false)},
			want:   []string{"Source: a", "Total: 1 results", notice(1)},
			absent: []string{"Source: c", knowledge.SimilarSeparator()},
		},
		{
			name:   "no matches plus hidden similar",
			hits:   []searchHitView{searchView("c", -2, false), searchView("d", -5, false)},
			want:   []string{knowledge.NoMatchesMessage("kafka"), notice(2)},
			absent: []string{"Source: c", "Total:"},
		},
		{
			name:        "show similar",
			hits:        []searchHitView{searchView("a", 5, true), searchView("c", -2, false)},
			showSimilar: true,
			want:        []string{"Result 1 ", "Source: a", knowledge.SimilarSeparator(), "Result 2 (score: -2.0000", "Source: c"},
			absent:      []string{"Use --show-similar"},
		},
		{
			name:   "empty",
			hits:   nil,
			want:   []string{knowledge.NoMatchesMessage("kafka")},
			absent: []string{"similar"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := formatSearchHitViews("kafka", tc.hits, tc.showSimilar)
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
}
