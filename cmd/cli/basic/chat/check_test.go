package chat

import (
	"strings"
	"testing"
)

func findingCodes(r CheckResult) []string {
	out := make([]string, 0, len(r.Findings))
	for _, f := range r.Findings {
		out = append(out, f.Code)
	}
	return out
}

func hasCode(r CheckResult, code string) bool {
	for _, f := range r.Findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

func findingFor(t *testing.T, r CheckResult, code string) CheckFinding {
	t.Helper()
	for _, f := range r.Findings {
		if f.Code == code {
			return f
		}
	}
	t.Fatalf("no finding with code %q; got %v", code, findingCodes(r))
	return CheckFinding{}
}

// goodPrompt carries all three assertions, so a test targeting something else is not
// tripped up by prompt warnings.
const goodPrompt = "Answer directly with no filler. Answers are at most 100 words; " +
	"this is a hard limit and must never be exceeded. Where no context is available, say so."

func TestCheckCleanManifest(t *testing.T) {
	m := &BatchManifest{
		Version:        "1.0",
		KnowledgeBases: []string{"vendor-rfp"},
		Prompt:         goodPrompt,
		Domains:        []Domain{{Match: "C*", Context: "Section C."}},
		Questions: []BatchQuestion{
			{ID: "C1", Question: "SR-IOV", Source: "C"},
			{ID: "C2", Question: "CPU pinning", Source: "C"},
		},
	}
	r := CheckManifest(m, &CheckRefs{KnowledgeBases: []string{"vendor-rfp"}})

	if !r.Clean() {
		t.Errorf("expected a clean manifest, got findings: %v", findingCodes(r))
	}
	if r.HasDefects() {
		t.Error("a clean manifest must not report defects")
	}
	if r.Questions != 2 || r.Domains != 1 {
		t.Errorf("counts = %d questions / %d domains, want 2/1", r.Questions, r.Domains)
	}
	if len(r.DomainMatches) != 1 || r.DomainMatches[0].Count != 2 {
		t.Errorf("domain match counts = %+v, want C* matching 2", r.DomainMatches)
	}
	if !r.ReferencesChecked {
		t.Error("references should be reported as checked when refs were supplied")
	}
}

func TestCheckZeroMatchDomainIsAWarningNotADefect(t *testing.T) {
	// This is load-bearing: manifests in this repository deliberately reserve patterns
	// for sections that carry no questions yet. Failing on them would make the check
	// useless on the very manifests it has to work with.
	m := &BatchManifest{
		Version: "1.0",
		Prompt:  goodPrompt,
		Domains: []Domain{
			{Match: "C*", Context: "Section C."},
			{Match: "GIS*", Context: "Reserved for a later revision."},
		},
		Questions: []BatchQuestion{{ID: "C1", Question: "SR-IOV", Source: "C"}},
	}
	r := CheckManifest(m, nil)

	f := findingFor(t, r, CheckDomainZeroMatch)
	if f.Severity != CheckWarning {
		t.Errorf("zero-match severity = %q, want %q", f.Severity, CheckWarning)
	}
	if f.Subject != "GIS*" {
		t.Errorf("finding subject = %q, want the offending pattern", f.Subject)
	}
	// The wording has to leave room for the legitimate case, or an operator will
	// "fix" a deliberately reserved pattern.
	if !strings.Contains(f.Message, "may be intentional") {
		t.Errorf("message should allow for a deliberate reservation, got: %s", f.Message)
	}
	if r.HasDefects() {
		t.Errorf("a reserved pattern must not make a manifest defective: %v", findingCodes(r))
	}
}

func TestCheckReportsPerDomainMatchCounts(t *testing.T) {
	m := &BatchManifest{
		Version: "1.0",
		Prompt:  goodPrompt,
		Domains: []Domain{
			{Match: "J*", Context: "All of J."},
			{Match: "J1.*", Context: "Just J1."},
		},
		Questions: []BatchQuestion{
			{ID: "J1.1", Question: "a"},
			{ID: "J1.2", Question: "b"},
			{ID: "J2.1", Question: "c"},
		},
	}
	r := CheckManifest(m, nil)

	counts := map[string]int{}
	for _, dm := range r.DomainMatches {
		counts[dm.Match] = dm.Count
	}
	// Specificity: J1.* takes both J1 rows, leaving J* only the J2 row. The check must
	// reflect the run's own resolution rather than counting raw pattern matches.
	if counts["J1.*"] != 2 {
		t.Errorf("J1.* matched %d, want 2", counts["J1.*"])
	}
	if counts["J*"] != 1 {
		t.Errorf("J* matched %d, want 1 (J1.* is more specific)", counts["J*"])
	}
}

func TestCheckReportsUnroutedQuestions(t *testing.T) {
	m := &BatchManifest{
		Version:   "1.0",
		Prompt:    goodPrompt,
		Domains:   []Domain{{Match: "C*", Context: "Section C."}},
		Questions: []BatchQuestion{{ID: "C1", Question: "a"}, {ID: "Z9", Question: "b"}},
	}
	r := CheckManifest(m, nil)

	f := findingFor(t, r, CheckQuestionUnrouted)
	if f.Severity != CheckWarning {
		t.Errorf("unrouted severity = %q, want a warning", f.Severity)
	}
	if !strings.Contains(f.Message, "Z9") {
		t.Errorf("message should name the unrouted question, got: %s", f.Message)
	}
}

func TestCheckIgnoresRoutingForAManifestWithNoDomains(t *testing.T) {
	m := &BatchManifest{
		Version:   "1.0",
		Prompt:    goodPrompt,
		Questions: []BatchQuestion{{ID: "1", Question: "a"}},
	}
	r := CheckManifest(m, nil)

	if hasCode(r, CheckQuestionUnrouted) {
		t.Error("a manifest with no domains must not report unrouted questions")
	}
	if !r.Clean() {
		t.Errorf("declaring no domains is not a defect: %v", findingCodes(r))
	}
}

func TestCheckStructuralDefects(t *testing.T) {
	tests := []struct {
		name     string
		manifest *BatchManifest
		wantCode string
		severity CheckSeverity
	}{
		{
			name:     "no questions",
			manifest: &BatchManifest{Version: "1.0", Prompt: goodPrompt},
			wantCode: CheckNoQuestions,
			severity: CheckDefect,
		},
		{
			name: "empty question text",
			manifest: &BatchManifest{Version: "1.0", Prompt: goodPrompt,
				Questions: []BatchQuestion{{ID: "C1", Question: "  "}}},
			wantCode: CheckEmptyQuestion,
			severity: CheckDefect,
		},
		{
			name: "duplicate ids",
			manifest: &BatchManifest{Version: "1.0", Prompt: goodPrompt,
				Questions: []BatchQuestion{{ID: "C1", Question: "a"}, {ID: "C1", Question: "b"}}},
			wantCode: CheckDuplicateID,
			severity: CheckDefect,
		},
		{
			name: "missing id",
			manifest: &BatchManifest{Version: "1.0", Prompt: goodPrompt,
				Questions: []BatchQuestion{{Question: "a"}}},
			wantCode: CheckMissingID,
			severity: CheckWarning,
		},
		{
			name: "invalid domains entry",
			manifest: &BatchManifest{Version: "1.0", Prompt: goodPrompt,
				Domains:   []Domain{{Match: "C*"}},
				Questions: []BatchQuestion{{ID: "C1", Question: "a"}}},
			wantCode: CheckDomainsInvalid,
			severity: CheckDefect,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := CheckManifest(tc.manifest, nil)
			f := findingFor(t, r, tc.wantCode)
			if f.Severity != tc.severity {
				t.Errorf("severity = %q, want %q", f.Severity, tc.severity)
			}
		})
	}
}

func TestCheckDuplicateIDsAreCaseInsensitiveAndReportedOnce(t *testing.T) {
	m := &BatchManifest{
		Version: "1.0", Prompt: goodPrompt,
		Questions: []BatchQuestion{
			{ID: "c1", Question: "a"}, {ID: "C1", Question: "b"}, {ID: "C1", Question: "c"},
		},
	}
	r := CheckManifest(m, nil)

	count := 0
	for _, f := range r.Findings {
		if f.Code == CheckDuplicateID {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected one duplicate-id finding for three colliding ids, got %d", count)
	}
}

func TestCheckUnknownKnowledgeBaseIsADefectWithASuggestion(t *testing.T) {
	m := &BatchManifest{
		Version:        "1.0",
		Prompt:         goodPrompt,
		KnowledgeBases: []string{"canonical-k9s"},
		Questions:      []BatchQuestion{{ID: "C1", Question: "a"}},
	}
	r := CheckManifest(m, &CheckRefs{KnowledgeBases: []string{"canonical-k8s", "ubuntu-pro"}})

	f := findingFor(t, r, CheckUnknownKB)
	// A defect, not a warning: unlike a reserved pattern it has no legitimate use.
	if f.Severity != CheckDefect {
		t.Errorf("severity = %q, want %q", f.Severity, CheckDefect)
	}
	// A typo gives the operator no other signal, so the near match earns its place.
	if !strings.Contains(f.Message, "canonical-k8s") {
		t.Errorf("message should suggest the near match, got: %s", f.Message)
	}
}

func TestCheckDoesNotSuggestAnUnrelatedName(t *testing.T) {
	m := &BatchManifest{
		Version:        "1.0",
		Prompt:         goodPrompt,
		KnowledgeBases: []string{"something-entirely-different"},
		Questions:      []BatchQuestion{{ID: "C1", Question: "a"}},
	}
	r := CheckManifest(m, &CheckRefs{KnowledgeBases: []string{"canonical-k8s"}})

	f := findingFor(t, r, CheckUnknownKB)
	if strings.Contains(f.Message, "did you mean") {
		t.Errorf("an unrelated name must not get a suggestion, got: %s", f.Message)
	}
}

func TestCheckUnknownKapaGroupIsADefect(t *testing.T) {
	m := &BatchManifest{
		Version:          "1.0",
		Prompt:           goodPrompt,
		KapaSourceGroups: []string{"made-up"},
		Questions:        []BatchQuestion{{ID: "C1", Question: "a"}},
	}
	r := CheckManifest(m, &CheckRefs{KapaGroups: []string{"real-id"}, KapaChecked: true})

	if f := findingFor(t, r, CheckUnknownKapaGroup); f.Severity != CheckDefect {
		t.Errorf("severity = %q, want %q", f.Severity, CheckDefect)
	}
	if !r.KapaChecked {
		t.Error("KapaChecked should be true when kapa was consulted")
	}
}

func TestCheckDoesNotFlagKapaGroupsWhenKapaWasNotConsulted(t *testing.T) {
	// The daemon does not wire kapa retrieval, so it passes KapaChecked false with an
	// empty list. Conflating that with "kapa has no groups" would report every kapa
	// group in a perfectly valid manifest as a defect — a fabricated finding.
	m := &BatchManifest{
		Version:          "1.0",
		Prompt:           goodPrompt,
		KnowledgeBases:   []string{"vendor-rfp"},
		KapaSourceGroups: []string{"aaaa-bbbb", "cccc-dddd"},
		Questions:        []BatchQuestion{{ID: "C1", Question: "a"}},
	}
	r := CheckManifest(m, &CheckRefs{KnowledgeBases: []string{"vendor-rfp"}})

	if hasCode(r, CheckUnknownKapaGroup) {
		t.Errorf("kapa groups must not be flagged when kapa was never consulted: %v", findingCodes(r))
	}
	if r.KapaChecked {
		t.Error("KapaChecked should be false when kapa was not consulted")
	}
	// The knowledge-base half still ran.
	if !r.ReferencesChecked {
		t.Error("knowledge base references should still be reported as checked")
	}
}

func TestCheckReportsReferenceChecksAsNotPerformedWhenUnavailable(t *testing.T) {
	// The knowledge store being unreachable must not read as "the references passed".
	m := &BatchManifest{
		Version:        "1.0",
		Prompt:         goodPrompt,
		KnowledgeBases: []string{"anything-at-all"},
		Domains:        []Domain{{Match: "C*", Context: "Section C."}},
		Questions:      []BatchQuestion{{ID: "C1", Question: "a"}},
	}
	r := CheckManifest(m, nil)

	if r.ReferencesChecked {
		t.Error("ReferencesChecked must be false when no refs were supplied")
	}
	if hasCode(r, CheckUnknownKB) {
		t.Error("no reference findings should be produced when the checks did not run")
	}
	// The other checks still ran.
	if r.Questions != 1 || len(r.DomainMatches) != 1 {
		t.Error("structural and routing checks should still have been performed")
	}
}

func TestCheckPromptAssertions(t *testing.T) {
	base := func(prompt string) *BatchManifest {
		return &BatchManifest{
			Version: "1.0", Prompt: prompt,
			Questions: []BatchQuestion{{ID: "C1", Question: "a"}},
		}
	}

	t.Run("all present", func(t *testing.T) {
		r := CheckManifest(base(goodPrompt), nil)
		if !r.Prompt.Applicable || !r.Prompt.NoContextRule || !r.Prompt.DirectnessRule || !r.Prompt.LengthStated {
			t.Errorf("expected every assertion found, got %+v", r.Prompt)
		}
		if !r.Prompt.LengthHard {
			t.Error("a hard limit should be detected as hard")
		}
	})

	t.Run("missing no-context rule is a warning", func(t *testing.T) {
		r := CheckManifest(base("Be direct, no filler. Target about 100 words."), nil)
		f := findingFor(t, r, CheckPromptNoContext)
		if f.Severity != CheckWarning {
			t.Errorf("severity = %q, want a warning: a hand-written prompt may word this differently", f.Severity)
		}
	})

	t.Run("length without a mode is not a length", func(t *testing.T) {
		r := CheckManifest(base("Where no context is available say so. Be direct. Keep to 100 words."), nil)
		if r.Prompt.LengthStated {
			t.Error("a word count with no enforcement mode must not count as a length statement")
		}
		if !hasCode(r, CheckPromptNoLength) {
			t.Error("expected a warning about the missing length statement")
		}
	})

	t.Run("not applicable with no inline prompt", func(t *testing.T) {
		r := CheckManifest(base(""), nil)
		if r.Prompt.Applicable {
			t.Error("prompt assertions must be not-applicable for a manifest with no inline prompt")
		}
		for _, code := range []string{CheckPromptNoContext, CheckPromptDirectness, CheckPromptNoLength} {
			if hasCode(r, code) {
				t.Errorf("must not report %q for a manifest with no inline prompt", code)
			}
		}
	})

	t.Run("not applicable with prompt_ref", func(t *testing.T) {
		m := base("")
		m.PromptRef = "telco-v2"
		r := CheckManifest(m, nil)
		if r.Prompt.Applicable {
			t.Error("prompt assertions must be not-applicable when a stored variant is named")
		}
	})
}

func TestCheckSeveritySeparation(t *testing.T) {
	// Warnings-only must be distinguishable from both clean and defective, because the
	// common case — a reserved pattern — is warnings-only and must not read as failure.
	m := &BatchManifest{
		Version:   "1.0",
		Prompt:    goodPrompt,
		Domains:   []Domain{{Match: "NOPE*", Context: "Reserved."}},
		Questions: []BatchQuestion{{ID: "C1", Question: "a"}},
	}
	r := CheckManifest(m, nil)

	if r.Clean() {
		t.Error("a manifest with warnings is not clean")
	}
	if r.HasDefects() {
		t.Error("a manifest with only warnings must not report defects")
	}
	if len(r.Warnings()) == 0 {
		t.Error("expected warnings")
	}
}

// TestInspectPromptRecognisesHandWrittenPhrasings guards the detectors' calibration.
//
// These phrasings are taken from prompts in real use, not from the wording this codebase
// generates. The detectors were originally tuned to generated wording and warned on
// almost every hand-written manifest as a result — and an assertion that fires
// constantly teaches operators to ignore it, which is worse than not checking at all.
func TestInspectPromptRecognisesHandWrittenPhrasings(t *testing.T) {
	directness := []string{
		"No preamble, no restating the label, no meta-commentary.",
		"Lead with the answer, substantiate, stop.",
		"No filler.",
		"Be direct.",
	}
	for _, prompt := range directness {
		t.Run("directness/"+prompt[:min(24, len(prompt))], func(t *testing.T) {
			if !InspectPrompt(&BatchManifest{Prompt: prompt}).DirectnessRule {
				t.Errorf("should have detected a directness instruction in %q", prompt)
			}
		})
	}

	noContext := []string{
		"Where the context substantiates nothing, reply that it does not contain enough information.",
		"State plainly that the technology is NOT covered.",
		"Say that no context is available rather than inventing an answer.",
		"Flag it as a gap rather than filling it.",
	}
	for _, prompt := range noContext {
		t.Run("no-context/"+prompt[:min(24, len(prompt))], func(t *testing.T) {
			if !InspectPrompt(&BatchManifest{Prompt: prompt}).NoContextRule {
				t.Errorf("should have detected a no-context instruction in %q", prompt)
			}
		})
	}

	lengths := []struct {
		name     string
		prompt   string
		wantHard bool
	}{
		{"word budgets are binding", "Word budgets are binding. Write to within ten percent of the 420 words and never exceed it.", true},
		{"at most", "Answers are at most 100 words.", true},
		{"maximum", "450 words maximum for this section.", true},
		{"hard limit", "120 words. This is a hard limit and must never be exceeded.", true},
		{"target length", "Target length: 180 words.", false},
		{"target about", "Target about 150 words per answer; this is a soft target.", false},
	}
	for _, tc := range lengths {
		t.Run("length/"+tc.name, func(t *testing.T) {
			a := InspectPrompt(&BatchManifest{Prompt: tc.prompt})
			if !a.LengthStated {
				t.Fatalf("should have detected a length statement in %q", tc.prompt)
			}
			if a.LengthHard != tc.wantHard {
				t.Errorf("LengthHard = %v, want %v for %q", a.LengthHard, tc.wantHard, tc.prompt)
			}
		})
	}
}

func TestInspectPromptRequiresANumberForALengthStatement(t *testing.T) {
	// A stray enforcement word in a long prompt must not read as a budget that is not
	// there: a false positive here suppresses the very warning worth raising.
	for _, prompt := range []string{
		"Provide the maximum detail available about the platform's words and phrasing.",
		"Never exceed your authority when describing support commitments.",
	} {
		if InspectPrompt(&BatchManifest{Prompt: prompt}).LengthStated {
			t.Errorf("should not have read a length statement from %q", prompt)
		}
	}
}

func TestInspectPromptPrefersHardWhenBothAreStated(t *testing.T) {
	// "Target about 400 words ... never exceed it" is a hard limit.
	a := InspectPrompt(&BatchManifest{
		Prompt: "Target about 400 words for this section, and never exceed it.",
	})
	if !a.LengthStated || !a.LengthHard {
		t.Errorf("a stated target with a hard boundary is a hard limit, got %+v", a)
	}
}

func TestEditDistanceAndNearestName(t *testing.T) {
	if d := editDistance("canonical-k9s", "canonical-k8s"); d != 1 {
		t.Errorf("editDistance = %d, want 1", d)
	}
	if d := editDistance("", "abc"); d != 3 {
		t.Errorf("editDistance from empty = %d, want 3", d)
	}
	if got, ok := nearestName("ubunto-pro", []string{"ubuntu-pro", "maas"}); !ok || got != "ubuntu-pro" {
		t.Errorf("nearestName = (%q, %v), want ubuntu-pro", got, ok)
	}
	if _, ok := nearestName("zzz", []string{"ubuntu-pro"}); ok {
		t.Error("an unrelated name should get no suggestion")
	}
}
