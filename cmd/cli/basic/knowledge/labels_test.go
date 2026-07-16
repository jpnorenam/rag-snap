package knowledge

import "testing"

func TestValidateLabel(t *testing.T) {
	valid := []string{"canonical", "kapa-canonical", "internal", "a", "0", "x-1-y", "abcdefghijklmnopqrstuvwxyz012345"}
	for _, l := range valid {
		if err := ValidateLabel(l); err != nil {
			t.Errorf("ValidateLabel(%q) = %v, want nil", l, err)
		}
	}

	invalid := []string{
		"",
		"-leading",
		"UPPER",
		"has space",
		"has_underscore",
		"tag]breakout",
		"line\nbreak",
		"abcdefghijklmnopqrstuvwxyz0123456", // 33 chars
	}
	for _, l := range invalid {
		if err := ValidateLabel(l); err == nil {
			t.Errorf("ValidateLabel(%q) = nil, want error", l)
		}
	}
}

func TestInferLabelFromIndex(t *testing.T) {
	cases := map[string]string{
		KapaIndexName:                    LabelKapa,
		"rag-snap-context-default":       LabelCanonical,
		"rag-snap-context-ceph-upstream": LabelUpstream,
		"rag-snap-context-UPSTREAM-docs": LabelUpstream,
		"rag-snap-context-partner":       LabelCanonical,
	}
	for index, want := range cases {
		if got := InferLabelFromIndex(index); got != want {
			t.Errorf("InferLabelFromIndex(%q) = %q, want %q", index, got, want)
		}
	}
}

func TestResolveLabel(t *testing.T) {
	// A stored label always wins, even over the upstream naming convention.
	if got := ResolveLabel("rag-snap-context-ceph-upstream", "internal"); got != "internal" {
		t.Errorf("ResolveLabel with stored label = %q, want %q", got, "internal")
	}
	// Without a stored label, the legacy inference applies.
	if got := ResolveLabel("rag-snap-context-ceph-upstream", ""); got != LabelUpstream {
		t.Errorf("ResolveLabel fallback = %q, want %q", got, LabelUpstream)
	}
	if got := ResolveLabel("rag-snap-context-default", ""); got != LabelCanonical {
		t.Errorf("ResolveLabel fallback = %q, want %q", got, LabelCanonical)
	}
}

func TestLabelTag(t *testing.T) {
	cases := map[string]string{
		"canonical":      "[CANONICAL]",
		"kapa-canonical": "[KAPA-CANONICAL]",
		"internal":       "[INTERNAL]",
	}
	for label, want := range cases {
		if got := LabelTag(label); got != want {
			t.Errorf("LabelTag(%q) = %q, want %q", label, got, want)
		}
	}
}
