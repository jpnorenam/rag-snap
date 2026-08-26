package rfp

import (
	"strings"
	"testing"
)

func TestIDPrefix(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{"C4", "C"},
		{"J1.3", "J"},
		{"GIS3", "GIS"},
		{"ADMIN", "ADMIN"},
		{"  T2  ", "T"},
		// Bare sequence numbers have no prefix to route on; these are the ids
		// the source fallback exists for.
		{"1", ""},
		{"42", ""},
		{"", ""},
	}

	for _, tt := range tests {
		if got := idPrefix(tt.id); got != tt.want {
			t.Errorf("idPrefix(%q) = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestDomainsStub(t *testing.T) {
	stub := domainsStub([]Question{
		{ID: "C1", Question: "a", Source: "EPA Sheet"},
		{ID: "C2", Question: "b", Source: "EPA Sheet"},
		{ID: "J1.3", Question: "c"},
		{ID: "1", Question: "d", Source: "GIS Deliverables"},
		{ID: "2", Question: "e", Source: "GIS Deliverables"},
		{ID: "3", Question: "f", Source: "GIS Deliverables"},
		// No prefix and no source: nothing to suggest, and nothing to crash on.
		{ID: "4", Question: "g"},
	})

	// Every line is a comment, so the stub cannot alter the manifest it precedes.
	for _, line := range strings.Split(strings.TrimSuffix(stub, "\n"), "\n") {
		if !strings.HasPrefix(line, "#") {
			t.Fatalf("stub line is not commented: %q", line)
		}
	}

	wantLines := []string{
		`#   - match: "C*"   # 2 question(s) by id prefix`,
		`#   - match: "J*"   # 1 question(s) by id prefix`,
		`#   - match: "GIS Deliverables"   # 3 question(s) by source`,
	}
	for _, want := range wantLines {
		if !strings.Contains(stub, want) {
			t.Errorf("stub is missing %q; got:\n%s", want, stub)
		}
	}

	// Candidates keep first-appearance order, so the stub reads in document order.
	c, j, gis := strings.Index(stub, `"C*"`), strings.Index(stub, `"J*"`), strings.Index(stub, `"GIS Deliverables"`)
	if !(c < j && j < gis) {
		t.Errorf("candidates out of document order: C at %d, J at %d, source at %d", c, j, gis)
	}

	// A prefixed question is suggested by prefix only — its source is not also
	// listed, because the resolver never falls back to source for such an id.
	if strings.Contains(stub, "EPA Sheet") {
		t.Errorf("stub suggested the source of a prefixed question:\n%s", stub)
	}

	// Placeholders are empty so an uncommented-but-unedited entry is rejected
	// rather than injecting placeholder prose into every prompt.
	if !strings.Contains(stub, `context: ""`) || !strings.Contains(stub, "keywords: []") {
		t.Errorf("stub placeholders are not empty:\n%s", stub)
	}
}

func TestDomainsStubNoCandidates(t *testing.T) {
	for _, questions := range [][]Question{
		nil,
		{{ID: "1", Question: "a"}, {ID: "2", Question: "b"}},
	} {
		stub := domainsStub(questions)
		if !strings.Contains(stub, "# domains:") {
			t.Errorf("stub omitted the domains key:\n%s", stub)
		}
		if !strings.Contains(stub, "no id prefixes or sources observed") {
			t.Errorf("stub gave no guidance for a document with no routing hints:\n%s", stub)
		}
	}
}
