package apiclient

import (
	"encoding/json"
	"testing"
)

// TestSearchHitIsMatched decodes the three shapes a daemon can send: matched
// true, matched false, and no matched field (a daemon that predates it).
func TestSearchHitIsMatched(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"matched true", `{"score": 3.1, "matched": true}`, true},
		{"matched false", `{"score": -2.4, "matched": false}`, false},
		{"field absent", `{"score": -2.4}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var h SearchHit
			if err := json.Unmarshal([]byte(tc.body), &h); err != nil {
				t.Fatalf("decoding: %v", err)
			}
			if got := h.IsMatched(); got != tc.want {
				t.Errorf("IsMatched() = %v, want %v", got, tc.want)
			}
		})
	}
}
