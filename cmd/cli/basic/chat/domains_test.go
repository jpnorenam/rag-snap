package chat

import "testing"

func TestCompileDomainsValidation(t *testing.T) {
	tests := []struct {
		name    string
		domains []Domain
		wantErr bool
	}{
		{"nil list", nil, false},
		{"empty list", []Domain{}, false},
		{
			"context only",
			[]Domain{{Match: "C*", Context: "Enhanced Platform Awareness"}},
			false,
		},
		{
			"keywords only",
			[]Domain{{Match: "C*", Keywords: KeywordList{"sriov"}}},
			false,
		},
		{
			"context and keywords",
			[]Domain{{Match: "C*", Context: "EPA", Keywords: KeywordList{"sriov"}}},
			false,
		},
		{
			"neither context nor keywords",
			[]Domain{{Match: "C*"}},
			true,
		},
		{
			"whitespace-only context with no keywords",
			[]Domain{{Match: "C*", Context: "   "}},
			true,
		},
		{
			"empty match pattern",
			[]Domain{{Match: "", Context: "EPA"}},
			true,
		},
		{
			"duplicate match patterns",
			[]Domain{
				{Match: "C*", Context: "EPA"},
				{Match: "C*", Context: "Something else"},
			},
			true,
		},
		{
			"duplicate patterns differing only in case",
			[]Domain{
				{Match: "gis*", Context: "Security baseline"},
				{Match: "GIS*", Context: "Something else"},
			},
			true,
		},
		{
			"distinct patterns are fine",
			[]Domain{
				{Match: "C*", Context: "EPA"},
				{Match: "J1.*", Context: "Server manufacturer"},
			},
			false,
		},
		{
			"invalid glob pattern",
			[]Domain{{Match: "C[", Context: "EPA"}},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set, err := CompileDomains(tt.domains)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("CompileDomains(%+v) = nil error, want error", tt.domains)
				}
				return
			}
			if err != nil {
				t.Fatalf("CompileDomains(%+v) = %v, want nil error", tt.domains, err)
			}
			if set == nil {
				t.Fatal("CompileDomains returned a nil set with a nil error")
			}
			if set.Len() != len(tt.domains) {
				t.Errorf("set.Len() = %d, want %d", set.Len(), len(tt.domains))
			}
		})
	}
}

func TestCompileDomainsTrimsMatch(t *testing.T) {
	set, err := CompileDomains([]Domain{{Match: "  C*  ", Context: "EPA"}})
	if err != nil {
		t.Fatalf("CompileDomains: %v", err)
	}
	got := set.Resolve("C4", "")
	if got == nil {
		t.Fatal("Resolve(\"C4\") = nil, want the trimmed C* entry")
	}
	if got.Match != "C*" {
		t.Errorf("resolved Match = %q, want %q", got.Match, "C*")
	}
}

func TestDomainSetResolve(t *testing.T) {
	// A representative routing table: two overlapping prefixes, a lowercase
	// pattern, and one with an interior wildcard. No catch-all — see
	// TestDomainSetResolveCatchAll for that interaction.
	table := []Domain{
		{Match: "C*", Context: "Enhanced Platform Awareness"},
		{Match: "J*", Context: "Hardware"},
		{Match: "J1.*", Context: "Storage vendor"},
		{Match: "gis*", Context: "Security baseline"},
		{Match: "T?.A", Context: "Interior wildcard"},
	}

	tests := []struct {
		name   string
		id     string
		source string
		want   string // expected Match, "" for no domain
	}{
		{"specific prefix beats broader one", "J1.3", "", "J1.*"},
		{"broader prefix still applies", "J2.7", "", "J*"},
		{"lowercase pattern matches uppercase id", "GIS3", "", "gis*"},
		{"single-character wildcard", "T4.A", "", "T?.A"},
		{"bare numeric id falls back to source", "17", "C", "C*"},
		{"bare numeric id falls back on a sheet name", "42", "GIS Deliverables", "gis*"},
		{"id match is not re-matched against source", "C4", "J1.9", "C*"},
		{"non-numeric id does not fall back to source", "ADMIN-1", "C", ""},
		{"numeric id with unmatched source", "17", "Admin", ""},
		{"empty source with numeric id", "17", "", ""},
	}

	set, err := CompileDomains(table)
	if err != nil {
		t.Fatalf("CompileDomains: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := set.Resolve(tt.id, tt.source)
			if tt.want == "" {
				if got != nil {
					t.Fatalf("Resolve(%q, %q) = %q, want no domain", tt.id, tt.source, got.Match)
				}
				return
			}
			if got == nil {
				t.Fatalf("Resolve(%q, %q) = no domain, want %q", tt.id, tt.source, tt.want)
			}
			if got.Match != tt.want {
				t.Errorf("Resolve(%q, %q) = %q, want %q", tt.id, tt.source, got.Match, tt.want)
			}
		})
	}
}

func TestDomainSetResolveCatchAll(t *testing.T) {
	set, err := CompileDomains([]Domain{
		{Match: "*", Context: "General requirement"},
		{Match: "C*", Context: "Enhanced Platform Awareness"},
		{Match: "gis*", Context: "Security baseline"},
	})
	if err != nil {
		t.Fatalf("CompileDomains: %v", err)
	}

	tests := []struct {
		name   string
		id     string
		source string
		want   string
	}{
		{"catch-all applies when nothing else matches", "ZZ9", "", "*"},
		{"catch-all never outranks a specific pattern", "C4", "", "C*"},
		{"catch-all matches an empty id", "", "", "*"},
		// A catch-all matches a bare numeric id, so the id "matched something"
		// and source is never consulted. A routing table that wants sheet-name
		// fallback must therefore leave the catch-all out.
		{"catch-all preempts the source fallback", "17", "GIS Deliverables", "*"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := set.Resolve(tt.id, tt.source)
			if got == nil {
				t.Fatalf("Resolve(%q, %q) = no domain, want %q", tt.id, tt.source, tt.want)
			}
			if got.Match != tt.want {
				t.Errorf("Resolve(%q, %q) = %q, want %q", tt.id, tt.source, got.Match, tt.want)
			}
		})
	}
}

func TestDomainSetResolveNoMatch(t *testing.T) {
	// Without a catch-all, an unmatched question resolves to no domain and is
	// answered exactly as it is without routing.
	set, err := CompileDomains([]Domain{
		{Match: "C*", Context: "EPA"},
		{Match: "J1.*", Context: "Storage vendor"},
	})
	if err != nil {
		t.Fatalf("CompileDomains: %v", err)
	}

	for _, tc := range []struct{ id, source string }{
		{"A7", ""},
		{"", ""},
		{"17", "Admin"},
		{"J2.1", ""},
	} {
		if got := set.Resolve(tc.id, tc.source); got != nil {
			t.Errorf("Resolve(%q, %q) = %q, want no domain", tc.id, tc.source, got.Match)
		}
	}
}

func TestDomainSetResolveTieBreaksToDocumentOrder(t *testing.T) {
	// "A*" and "B*" both have one literal character; only one can match a given
	// id, so a real tie needs two patterns that match the same id. "C*" and
	// "?4" both match "C4" with one literal each.
	set, err := CompileDomains([]Domain{
		{Match: "C*", Context: "first"},
		{Match: "?4", Context: "second"},
	})
	if err != nil {
		t.Fatalf("CompileDomains: %v", err)
	}
	got := set.Resolve("C4", "")
	if got == nil {
		t.Fatal("Resolve(\"C4\") = no domain, want the first of the tied entries")
	}
	if got.Context != "first" {
		t.Errorf("resolved Context = %q, want %q (document order)", got.Context, "first")
	}

	// Reversing document order reverses the winner, confirming the tie is
	// broken by position and not by some property of the patterns.
	reversed, err := CompileDomains([]Domain{
		{Match: "?4", Context: "second"},
		{Match: "C*", Context: "first"},
	})
	if err != nil {
		t.Fatalf("CompileDomains: %v", err)
	}
	got = reversed.Resolve("C4", "")
	if got == nil {
		t.Fatal("Resolve(\"C4\") = no domain, want the first of the tied entries")
	}
	if got.Context != "second" {
		t.Errorf("resolved Context = %q, want %q (document order)", got.Context, "second")
	}
}

func TestDomainSetResolveEmptySet(t *testing.T) {
	// The no-domains path: both a compiled empty set and a nil pointer resolve
	// to no domain rather than erroring, so callers need no special case.
	set, err := CompileDomains(nil)
	if err != nil {
		t.Fatalf("CompileDomains(nil): %v", err)
	}
	if got := set.Resolve("C4", "C"); got != nil {
		t.Errorf("empty set Resolve = %q, want no domain", got.Match)
	}
	if set.Len() != 0 {
		t.Errorf("empty set Len() = %d, want 0", set.Len())
	}

	var nilSet *DomainSet
	if got := nilSet.Resolve("C4", "C"); got != nil {
		t.Errorf("nil set Resolve = %q, want no domain", got.Match)
	}
	if nilSet.Len() != 0 {
		t.Errorf("nil set Len() = %d, want 0", nilSet.Len())
	}
}

func TestDomainSetResolveKeywords(t *testing.T) {
	set, err := CompileDomains([]Domain{
		{Match: "C*", Keywords: KeywordList{"sriov", "numa"}},
	})
	if err != nil {
		t.Fatalf("CompileDomains: %v", err)
	}
	got := set.Resolve("C4", "")
	if got == nil {
		t.Fatal("Resolve(\"C4\") = no domain, want the keywords-only entry")
	}
	if got.Context != "" {
		t.Errorf("resolved Context = %q, want empty", got.Context)
	}
	if len(got.Keywords) != 2 || got.Keywords[0] != "sriov" || got.Keywords[1] != "numa" {
		t.Errorf("resolved Keywords = %v, want [sriov numa]", got.Keywords)
	}
}

func TestLiteralCount(t *testing.T) {
	tests := []struct {
		pattern string
		want    int
	}{
		{"*", 0},
		{"?", 0},
		{"C*", 1},
		{"J1.*", 3},
		{"GIS*", 3},
		{"T?.A", 3},
		{"EXACT", 5},
	}
	for _, tt := range tests {
		if got := literalCount(tt.pattern); got != tt.want {
			t.Errorf("literalCount(%q) = %d, want %d", tt.pattern, got, tt.want)
		}
	}
}

func TestIsBareSequence(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"17", true},
		{"0", true},
		{"", false},
		{"C4", false},
		{"17a", false},
		{"1.2", false},
		{" 17", false},
		{"-1", false},
	}
	for _, tt := range tests {
		if got := isBareSequence(tt.in); got != tt.want {
			t.Errorf("isBareSequence(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
