package knowledge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The fixture is shared with the UI tests (ui/lib/searchSplit.test.ts): both
// clients must split and summarize every case identically.
const splitFixturePath = "../../../../testdata/search_split_cases.json"

type splitFixtureHit struct {
	ID      string  `json:"id"`
	Score   float64 `json:"score"`
	Matched *bool   `json:"matched"`
}

type splitFixtureCase struct {
	Name             string            `json:"name"`
	Query            string            `json:"query"`
	Hits             []splitFixtureHit `json:"hits"`
	Matched          []string          `json:"matched"`
	Similar          []string          `json:"similar"`
	NoMatchesMessage *string           `json:"no_matches_message"`
	SimilarNotice    *string           `json:"similar_notice"`
}

func loadSplitFixture(t *testing.T) []splitFixtureCase {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(splitFixturePath))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	var f struct {
		Cases []splitFixtureCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("decoding fixture: %v", err)
	}
	if len(f.Cases) == 0 {
		t.Fatal("fixture has no cases")
	}
	return f.Cases
}

func ids(hits []splitFixtureHit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.ID)
	}
	return out
}

// TestIsMatchAgreesWithFixture pins the server rule to the matched values the
// fixture (and so the UI tests) assume the server emits.
func TestIsMatchAgreesWithFixture(t *testing.T) {
	for _, tc := range loadSplitFixture(t) {
		for _, h := range tc.Hits {
			if h.Matched == nil {
				continue
			}
			if got := IsMatch(h.Score); got != *h.Matched {
				t.Errorf("%s: IsMatch(%v) = %v, fixture says matched=%v", tc.Name, h.Score, got, *h.Matched)
			}
		}
	}
	if IsMatch(0) {
		t.Error("IsMatch(0) = true, want false: the threshold is exclusive")
	}
}

func TestSplitByMatchFixture(t *testing.T) {
	for _, tc := range loadSplitFixture(t) {
		t.Run(tc.Name, func(t *testing.T) {
			matched, similar := SplitByMatch(tc.Hits, func(h splitFixtureHit) bool {
				return h.Matched == nil || *h.Matched
			})
			if got := ids(matched); !reflect.DeepEqual(got, tc.Matched) {
				t.Errorf("matched = %v, want %v", got, tc.Matched)
			}
			if got := ids(similar); !reflect.DeepEqual(got, tc.Similar) {
				t.Errorf("similar = %v, want %v", got, tc.Similar)
			}

			out := FormatSplit(tc.Query, "<matched>\n", "<similar>\n", len(matched), len(similar), false)
			checkMessage(t, out, "no-matches message", NoMatchesMessage(tc.Query), tc.NoMatchesMessage)
			checkMessage(t, out, "similar notice", SimilarNotice(len(similar)), tc.SimilarNotice)
		})
	}
}

// checkMessage asserts that msg is the fixture's expected string and appears
// in out, or that out omits it when the fixture expects no message.
func checkMessage(t *testing.T, out, what, msg string, want *string) {
	t.Helper()
	if want == nil {
		if strings.Contains(out, msg) {
			t.Errorf("%s %q shown, want none; output:\n%s", what, msg, out)
		}
		return
	}
	if msg != *want {
		t.Errorf("%s = %q, want %q", what, msg, *want)
	}
	if !strings.Contains(out, msg) {
		t.Errorf("%s %q missing from output:\n%s", what, msg, out)
	}
}

func TestFormatSplitShowSimilar(t *testing.T) {
	hidden := FormatSplit("q", "M\n", "S\n", 1, 2, false)
	if strings.Contains(hidden, "S\n") || !strings.Contains(hidden, "Use --show-similar to show them.") {
		t.Errorf("hidden output = %q", hidden)
	}

	shown := FormatSplit("q", "M\n", "S\n", 1, 2, true)
	want := "M\n\n" + SimilarSeparator() + "\nS\n"
	if shown != want {
		t.Errorf("shown output = %q, want %q", shown, want)
	}

	none := FormatSplit("q", "", "S\n", 0, 1, true)
	if !strings.HasPrefix(none, NoMatchesMessage("q")+"\n") || !strings.HasSuffix(none, "S\n") {
		t.Errorf("no-match output = %q", none)
	}
}

func TestCapHits(t *testing.T) {
	// Two indexes' top-2 lists, merged by search() into score order.
	merged := []SearchHit{
		{Index: "b", Score: 9}, {Index: "a", Score: 7}, {Index: "a", Score: 3}, {Index: "b", Score: 1},
	}
	got := capHits(merged, 2)
	if len(got) != 2 || got[0].Score != 9 || got[1].Score != 7 {
		t.Errorf("capHits = %+v, want the global top 2 (9, 7)", got)
	}
	if len(capHits(merged, 10)) != 4 {
		t.Error("capHits must not pad or drop when under the cap")
	}
}

// TestMatchThresholdIsPinnedToTheDefaultReranker fails when the reranker
// `knowledge init` registers changes, because MatchScoreThreshold assumes this
// model's output scale (a relevance logit with its boundary at 0). A model that
// emits probabilities would mark every hit as a match at threshold 0.
func TestMatchThresholdIsPinnedToTheDefaultReranker(t *testing.T) {
	if DefaultCrossEncoderName != "huggingface/cross-encoders/ms-marco-MiniLM-L-12-v2" || MatchScoreThreshold != 0 {
		t.Fatalf("the reranker (%s) or MatchScoreThreshold (%v) changed: check the new model's score scale and set the threshold to its decision boundary, then update this test",
			DefaultCrossEncoderName, MatchScoreThreshold)
	}
}
