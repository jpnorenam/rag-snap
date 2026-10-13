package chat

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// CheckSeverity distinguishes a manifest that will not run, or carries a reference that
// cannot contribute, from one that will run but probably does not do what was intended.
type CheckSeverity string

const (
	// CheckDefect is a finding with no legitimate use: the manifest would fail to run,
	// or it names a retrieval target that contributes nothing.
	CheckDefect CheckSeverity = "defect"
	// CheckWarning is a finding that may be deliberate. A pattern reserved for a
	// section with no questions yet is the canonical example.
	CheckWarning CheckSeverity = "warning"
)

// Check finding codes, stable enough for a UI to key styling or a script to grep on.
const (
	CheckNoQuestions      = "no-questions"
	CheckEmptyQuestion    = "empty-question"
	CheckDuplicateID      = "duplicate-question-id"
	CheckMissingID        = "missing-question-id"
	CheckDomainsInvalid   = "domains-invalid"
	CheckDomainZeroMatch  = "domain-matches-nothing"
	CheckQuestionUnrouted = "question-unrouted"
	CheckUnknownKB        = "unknown-knowledge-base"
	CheckUnknownKapaGroup = "unknown-kapa-source-group"
	CheckPromptNoContext  = "prompt-missing-no-context-rule"
	CheckPromptDirectness = "prompt-missing-directness-rule"
	CheckPromptNoLength   = "prompt-missing-length-statement"
)

// CheckFinding is one thing the check noticed.
type CheckFinding struct {
	Severity CheckSeverity `json:"severity"`
	Code     string        `json:"code"`
	Message  string        `json:"message"`
	// Subject is the question id, domain pattern, or knowledge-base name the finding
	// is about, so a UI can point at it without parsing the message.
	Subject string `json:"subject,omitempty"`
}

// DomainMatchCount is how many questions one domains entry would route.
type DomainMatchCount struct {
	Match string `json:"match"`
	Count int    `json:"count"`
}

// PromptAssertions records what a manifest's inline prompt still carries.
//
// Applicable is false for a manifest with no inline prompt, or one deferring to a
// stored variant through prompt_ref: there is nothing to assert about a prompt that is
// not in the file, and reporting that as a failure would be misleading.
type PromptAssertions struct {
	Applicable     bool `json:"applicable"`
	NoContextRule  bool `json:"no_context_rule"`
	DirectnessRule bool `json:"directness_rule"`
	LengthStated   bool `json:"length_stated"`
	// LengthHard is meaningful only when LengthStated: true for a hard limit, false
	// for a soft target.
	LengthHard bool `json:"length_hard"`
}

// CheckRefs supplies the retrieval targets that actually exist, for resolving a
// manifest's selections.
//
// A nil *CheckRefs means the reference checks were not performed — the knowledge store
// was unreachable — which the result reports as such rather than as passing. Silence
// about a check that never ran is exactly the kind of false reassurance this whole
// command exists to remove.
type CheckRefs struct {
	KnowledgeBases []string

	// KapaGroups holds the source groups that exist, and KapaChecked says whether
	// kapa was consulted at all. The two are separate because an empty list means
	// something quite different in each case: consulted-and-empty makes every named
	// group a defect, while not-consulted must report nothing. The daemon does not
	// wire kapa retrieval, so it always passes KapaChecked false, and conflating the
	// two would flag every kapa group in a valid manifest.
	KapaGroups  []string
	KapaChecked bool
}

// CheckResult is the outcome of checking one manifest.
type CheckResult struct {
	Questions     int                `json:"questions"`
	Domains       int                `json:"domains"`
	DomainMatches []DomainMatchCount `json:"domain_matches"`
	Findings      []CheckFinding     `json:"findings"`
	// ReferencesChecked is false when the knowledge store could not be consulted, and
	// KapaChecked when kapa was not. Each is reported separately so a client can say
	// which checks actually ran rather than implying all of them passed.
	ReferencesChecked bool             `json:"references_checked"`
	KapaChecked       bool             `json:"kapa_checked"`
	Prompt            PromptAssertions `json:"prompt"`
}

// Defects returns the findings that would stop a run or contribute nothing.
func (r CheckResult) Defects() []CheckFinding { return r.bySeverity(CheckDefect) }

// Warnings returns the findings that may be deliberate.
func (r CheckResult) Warnings() []CheckFinding { return r.bySeverity(CheckWarning) }

func (r CheckResult) bySeverity(s CheckSeverity) []CheckFinding {
	var out []CheckFinding
	for _, f := range r.Findings {
		if f.Severity == s {
			out = append(out, f)
		}
	}
	return out
}

// HasDefects reports whether the manifest carries any defect. It is what a command's
// exit status is derived from: warnings alone must not fail a script.
func (r CheckResult) HasDefects() bool {
	return len(r.Defects()) > 0
}

// Clean reports whether the check found nothing at all.
func (r CheckResult) Clean() bool { return len(r.Findings) == 0 }

// CheckManifest inspects a manifest without running it and without calling any model.
//
// Routing is resolved with the run's own CompileDomains and Resolve rather than a
// reimplementation of glob matching, because a check that disagrees with the run is
// worse than no check.
func CheckManifest(m *BatchManifest, refs *CheckRefs) CheckResult {
	res := CheckResult{
		Questions:         len(m.Questions),
		Domains:           len(m.Domains),
		ReferencesChecked: refs != nil,
		KapaChecked:       refs != nil && refs.KapaChecked,
		Prompt:            InspectPrompt(m),
	}

	res.checkStructure(m)
	res.checkRouting(m)
	res.checkReferences(m, refs)
	res.checkPrompt()

	return res
}

func (r *CheckResult) add(severity CheckSeverity, code, subject, format string, args ...any) {
	r.Findings = append(r.Findings, CheckFinding{
		Severity: severity,
		Code:     code,
		Subject:  subject,
		Message:  fmt.Sprintf(format, args...),
	})
}

// checkStructure reports what the run would reject, plus what it tolerates at the
// operator's expense.
func (r *CheckResult) checkStructure(m *BatchManifest) {
	if len(m.Questions) == 0 {
		r.add(CheckDefect, CheckNoQuestions, "", "the manifest carries no questions, so a run would be rejected")
		return
	}

	seen := map[string]int{}
	for i, q := range m.Questions {
		if strings.TrimSpace(q.Question) == "" {
			r.add(CheckDefect, CheckEmptyQuestion, q.ID,
				"question %d has empty question text, which a run rejects", i+1)
		}
		id := strings.TrimSpace(q.ID)
		if id == "" {
			r.add(CheckWarning, CheckMissingID, "",
				"question %d has no id; results are keyed by id, so this answer is harder to match back to the source document", i+1)
			continue
		}
		seen[strings.ToUpper(id)]++
	}

	// Reported once per duplicated id rather than once per occurrence, and sorted so
	// the output is stable between runs.
	var dupes []string
	for id, count := range seen {
		if count > 1 {
			dupes = append(dupes, id)
		}
	}
	sort.Strings(dupes)
	for _, id := range dupes {
		r.add(CheckDefect, CheckDuplicateID, id,
			"question id %q appears %d times; results are keyed by id, so these answers would collide", id, seen[id])
	}
}

// checkRouting resolves every question through the run's own resolver and reports what
// each domains entry would actually route.
func (r *CheckResult) checkRouting(m *BatchManifest) {
	if len(m.Domains) == 0 {
		// A manifest declaring no domains is not defective on that account.
		return
	}

	set, err := CompileDomains(m.Domains)
	if err != nil {
		r.add(CheckDefect, CheckDomainsInvalid, "",
			"the domains block is invalid and would fail a run: %v", err)
		return
	}

	counts := map[string]int{}
	var unrouted []string
	for _, q := range m.Questions {
		if d := set.Resolve(q.ID, q.Source); d != nil {
			counts[d.Match]++
			continue
		}
		id := q.ID
		if id == "" {
			id = strings.TrimSpace(q.Question)
			if len(id) > 40 {
				id = id[:40] + "…"
			}
		}
		unrouted = append(unrouted, id)
	}

	for _, d := range m.Domains {
		r.DomainMatches = append(r.DomainMatches, DomainMatchCount{Match: d.Match, Count: counts[d.Match]})
		if counts[d.Match] == 0 {
			r.add(CheckWarning, CheckDomainZeroMatch, d.Match,
				"domain %q matches none of this manifest's questions, so it contributes nothing; "+
					"this may be intentional for a section reserved for a later revision, but check the pattern against the ids in use", d.Match)
		}
	}

	// An unrouted question in an otherwise routed manifest is usually an oversight, so
	// it is worth saying — but it is a warning, since answering without a domain
	// context is legitimate.
	if len(unrouted) > 0 {
		subject := ""
		if len(unrouted) == 1 {
			subject = unrouted[0]
		}
		r.add(CheckWarning, CheckQuestionUnrouted, subject,
			"%d of %d questions match no domain and will be answered with no requirement context: %s",
			len(unrouted), len(m.Questions), summariseList(unrouted, 8))
	}
}

// checkReferences resolves the manifest's retrieval targets against what exists.
func (r *CheckResult) checkReferences(m *BatchManifest, refs *CheckRefs) {
	if refs == nil {
		return
	}

	for _, kb := range m.KnowledgeBases {
		if containsFold(refs.KnowledgeBases, kb) {
			continue
		}
		msg := fmt.Sprintf("knowledge base %q does not exist, so it contributes nothing to any answer", kb)
		// A typo gives the operator no other signal at all — the run succeeds and
		// simply retrieves less — so a near match is worth naming.
		if near, ok := nearestName(kb, refs.KnowledgeBases); ok {
			msg += fmt.Sprintf("; did you mean %q?", near)
		}
		r.add(CheckDefect, CheckUnknownKB, kb, "%s", msg)
	}

	// Only when kapa was actually consulted: an unconsulted kapa cannot tell us that a
	// group is missing, and reporting one as absent would be a fabricated finding.
	if !refs.KapaChecked {
		return
	}
	for _, g := range m.KapaSourceGroups {
		if !containsFold(refs.KapaGroups, g) {
			r.add(CheckDefect, CheckUnknownKapaGroup, g,
				"kapa source group %q does not exist in this project, so it contributes nothing to any answer", g)
		}
	}
}

// checkPrompt reports the prompt assertions as warnings. A hand-written prompt may
// legitimately express these differently or omit a word budget entirely, so none of
// them is a defect.
func (r *CheckResult) checkPrompt() {
	if !r.Prompt.Applicable {
		return
	}
	if !r.Prompt.NoContextRule {
		r.add(CheckWarning, CheckPromptNoContext, "",
			"the prompt carries no instruction preferring \"no context available\" over a fabricated answer")
	}
	if !r.Prompt.DirectnessRule {
		r.add(CheckWarning, CheckPromptDirectness, "",
			"the prompt carries no instruction to be direct and avoid filler")
	}
	if !r.Prompt.LengthStated {
		r.add(CheckWarning, CheckPromptNoLength, "",
			"the prompt states no answer length, or states one without saying whether it is a hard limit or a soft target")
	}
}

// Phrasings that evidence each prompt assertion.
//
// These are calibrated against the hand-written prompts in real use, not against the
// wording this codebase generates. A detector that only recognised generated phrasing
// would warn on almost every hand-written manifest, and an assertion that fires
// constantly teaches operators to ignore it — which is worse than not checking.
var (
	noContextPhrasings = []string{
		"no context is available", "no context available", "not covered",
		"does not contain enough information", "not enough information",
		"flag it as a gap", "state the gap",
	}
	directnessPhrasings = []string{
		"no filler", "be direct", "avoid filler",
		"no preamble", "meta-commentary", "lead with the answer",
	}
	// Enforcement signals, which say what happens at the boundary.
	hardLengthPhrasings = []string{
		"hard limit", "never be exceeded", "never exceed", "must not exceed",
		"at most", "maximum", "binding", "no more than",
	}
	softLengthPhrasings = []string{
		"soft target", "target about", "target length", "approximately", "aim for", "around",
	}
)

// wordCountRe matches an explicit word budget, e.g. "100 words", "at most 420 words".
// A number is required: without one, a stray "maximum" elsewhere in a long prompt would
// read as a budget that is not there, and a false positive here suppresses the very
// warning the check exists to raise.
var wordCountRe = regexp.MustCompile(`\b\d+\s*[- ]?words?\b`)

// InspectPrompt reports what a manifest's inline prompt still carries.
//
// These live with the manifest type rather than with the code that generates a prompt,
// because both the generator and the check need them and they describe a property of a
// manifest, not of generation.
func InspectPrompt(m *BatchManifest) PromptAssertions {
	prompt := strings.TrimSpace(m.Prompt)
	if prompt == "" || strings.TrimSpace(m.PromptRef) != "" {
		return PromptAssertions{Applicable: false}
	}
	lower := strings.ToLower(prompt)

	a := PromptAssertions{
		Applicable:     true,
		NoContextRule:  containsAny(lower, noContextPhrasings...),
		DirectnessRule: containsAny(lower, directnessPhrasings...),
	}

	// A budget counts only when its enforcement is stated too. "100 words" alone tells
	// the answering model nothing about what to do at 130, which is precisely the
	// ambiguity worth reporting. Hard is checked first: a prompt saying both "target
	// about 400 words" and "never exceed it" is a hard limit.
	if wordCountRe.MatchString(lower) {
		switch {
		case containsAny(lower, hardLengthPhrasings...):
			a.LengthStated, a.LengthHard = true, true
		case containsAny(lower, softLengthPhrasings...):
			a.LengthStated, a.LengthHard = true, false
		}
	}
	return a
}

func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

func containsFold(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}

// summariseList renders at most limit entries, noting how many were elided.
func summariseList(items []string, limit int) string {
	if len(items) <= limit {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:limit], ", ") + fmt.Sprintf(" and %d more", len(items)-limit)
}

// nearestName returns the closest candidate to name when it is close enough to be a
// plausible typo, using edit distance against a threshold that scales with length.
func nearestName(name string, candidates []string) (string, bool) {
	best, bestDist := "", -1
	for _, c := range candidates {
		d := editDistance(strings.ToLower(name), strings.ToLower(c))
		if bestDist < 0 || d < bestDist {
			best, bestDist = c, d
		}
	}
	if best == "" {
		return "", false
	}
	// A third of the name's length, at least one and at most three edits. Loose enough
	// to catch a transposition or a wrong character, tight enough that two unrelated
	// base names are not offered as suggestions for each other.
	threshold := len(name) / 3
	if threshold < 1 {
		threshold = 1
	}
	if threshold > 3 {
		threshold = 3
	}
	if bestDist <= threshold {
		return best, true
	}
	return "", false
}

// editDistance is the Levenshtein distance between a and b, computed with two rows
// rather than a full matrix since only the distance is needed.
func editDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	if len(ar) == 0 {
		return len(br)
	}
	if len(br) == 0 {
		return len(ar)
	}

	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = min3(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
