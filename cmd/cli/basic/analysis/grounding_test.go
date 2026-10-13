package analysis

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
)

// yamlFieldNames returns the yaml tag names of a struct's exported fields, which are
// the manifest's real field names.
func yamlFieldNames(t *testing.T, v any) []string {
	t.Helper()
	rt := reflect.TypeOf(v)
	var out []string
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name != "" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// The manifest fields analysis asks the model to produce. Each must be documented in
// the embedded contract, because a field the model is not told about is a field it
// will not emit.
var modelGeneratedFields = map[string]bool{
	"version":            true,
	"knowledge_bases":    true,
	"kapa_source_groups": true,
	"prompt":             true,
	"domains":            true,
	"questions":          true,
}

// Manifest fields the model is deliberately NOT asked for, with the reason. Listing
// them explicitly is what makes the drift test meaningful: a new field added to the
// manifest struct fails the test until someone decides which list it belongs in.
var deliberatelyNotGenerated = map[string]string{
	"model":      "the run resolves the answering model itself; analysis must not pin it",
	"prompt_ref": "names a stored prompt variant, mutually exclusive with the inline prompt analysis generates",
}

func TestContractDocumentsEveryManifestField(t *testing.T) {
	contract := ManifestContract()

	for _, field := range yamlFieldNames(t, chat.BatchManifest{}) {
		switch {
		case modelGeneratedFields[field]:
			if !strings.Contains(contract, "`"+field+"`") {
				t.Errorf("manifest field %q is asked of the model but is not documented in the contract", field)
			}
		case deliberatelyNotGenerated[field] != "":
			// Not documented on purpose. Nothing to assert beyond the
			// classification existing.
		default:
			t.Errorf("manifest field %q is classified in neither modelGeneratedFields nor "+
				"deliberatelyNotGenerated — decide whether the model should produce it, and "+
				"document it in assets/manifest-contract.md if so", field)
		}
	}
}

func TestContractDocumentsEveryQuestionAndDomainField(t *testing.T) {
	contract := ManifestContract()

	for _, field := range yamlFieldNames(t, chat.BatchQuestion{}) {
		if !strings.Contains(contract, "`"+field+"`") {
			t.Errorf("question field %q is not documented in the contract", field)
		}
	}
	for _, field := range yamlFieldNames(t, chat.Domain{}) {
		if !strings.Contains(contract, "`"+field+"`") {
			t.Errorf("domain field %q is not documented in the contract", field)
		}
	}
}

func TestContractStatesTheDomainResolutionRules(t *testing.T) {
	contract := strings.ToLower(ManifestContract())

	// These are the rules a model gets wrong by default, each verified against
	// chat/domains.go. Losing any of them from the contract produces patterns that
	// silently route nothing, which is this feature's worst failure mode.
	for _, rule := range []string{
		"case-insensitive",   // patterns are uppercased before matching
		"literal",            // specificity is the non-wildcard character count
		"bare run of digits", // the only case where source is consulted
		"same pattern",       // duplicate patterns are rejected
	} {
		if !strings.Contains(contract, rule) {
			t.Errorf("the contract no longer states the domain rule covering %q", rule)
		}
	}
}

func TestContractStatesTheNoContextGuarantee(t *testing.T) {
	contract := strings.ToLower(ManifestContract())
	// The model must know it cannot cause an ungrounded answer, so it does not write
	// a prompt that tries to.
	if !strings.Contains(contract, "does not call the model at all") {
		t.Error("the contract no longer explains that retrieval finding nothing short-circuits the model call")
	}
	if !strings.Contains(contract, "source rules are appended") {
		t.Error("the contract no longer warns that source rules are appended to the prompt")
	}
}

func TestExampleIsValidJSONMatchingTheManifestShape(t *testing.T) {
	// The example is what the model generalises from, so a malformed one teaches
	// the wrong shape. Parsing it as a manifest proves it is at least loadable.
	manifest := decodeExampleManifest(t)

	if manifest.Version != "1.0" {
		t.Errorf("example version = %q, want \"1.0\"", manifest.Version)
	}
	if len(manifest.Questions) == 0 {
		t.Fatal("example carries no questions")
	}
	if len(manifest.KnowledgeBases) == 0 {
		t.Error("example selects no knowledge bases")
	}
	if strings.TrimSpace(manifest.Prompt) == "" {
		t.Error("example carries no prompt")
	}
	// The example must not pin fields analysis is not allowed to generate.
	if manifest.Model != "" {
		t.Error("example pins a model, which analysis must never do")
	}
	if manifest.PromptRef != "" {
		t.Error("example sets prompt_ref, which is mutually exclusive with an inline prompt")
	}
}

func TestExampleDomainsCompileAndRouteAsDocumented(t *testing.T) {
	manifest := decodeExampleManifest(t)

	// The example's domains must survive the same validation a real run applies —
	// otherwise the worked example teaches a manifest that fails to load.
	set, err := chat.CompileDomains(manifest.Domains)
	if err != nil {
		t.Fatalf("example domains do not compile: %v", err)
	}

	// Every pattern except the one the example explicitly documents as reserved must
	// match at least one of the example's own questions.
	const reserved = "GIS*"
	matched := map[string]int{}
	for _, q := range manifest.Questions {
		if d := set.Resolve(q.ID, q.Source); d != nil {
			matched[d.Match]++
		}
	}
	for _, d := range manifest.Domains {
		if d.Match == reserved {
			if matched[d.Match] != 0 {
				t.Errorf("the example's reserved pattern %q unexpectedly matches a question", reserved)
			}
			continue
		}
		if matched[d.Match] == 0 {
			t.Errorf("example domain %q matches none of the example's questions, "+
				"which is precisely the mistake the contract warns against", d.Match)
		}
	}

	// The specificity rule is the one worth asserting behaviourally: J1.* must win
	// for a J1 id even though a broader pattern could also have been written.
	if d := set.Resolve("J1.1", "J. Hardware"); d == nil || d.Match != "J1.*" {
		got := "<nil>"
		if d != nil {
			got = d.Match
		}
		t.Errorf("J1.1 resolved to %s, want J1.*", got)
	}
	// A single-question entry must beat its section's prefix entry.
	if d := set.Resolve("E4", "E. VM Lifecycle"); d == nil || d.Match != "E4" {
		got := "<nil>"
		if d != nil {
			got = d.Match
		}
		t.Errorf("E4 resolved to %s, want the single-question entry E4", got)
	}
}

func TestExampleQuestionIDsAreUnique(t *testing.T) {
	manifest := decodeExampleManifest(t)
	seen := map[string]bool{}
	for _, q := range manifest.Questions {
		if seen[q.ID] {
			t.Errorf("example reuses question id %q; results are keyed by id", q.ID)
		}
		seen[q.ID] = true
	}
}

// decodeExampleManifest pulls the JSON block out of the worked example and decodes it
// the same way a model's response is decoded, so the example is verified through the
// real code path rather than a test-only parser.
func decodeExampleManifest(t *testing.T) *chat.BatchManifest {
	t.Helper()
	block, err := extractJSONBlock(ManifestExample())
	if err != nil {
		t.Fatalf("extracting the example's JSON block: %v", err)
	}
	manifest, err := DecodeManifestJSON([]byte(block))
	if err != nil {
		t.Fatalf("decoding the example manifest: %v", err)
	}
	return manifest
}

func TestGroundingCarriesBothAssets(t *testing.T) {
	g := Grounding()
	if !strings.Contains(g, "The answer-batch manifest contract") {
		t.Error("grounding is missing the contract")
	}
	if !strings.Contains(g, "A worked example") {
		t.Error("grounding is missing the worked example")
	}
}

// TestEveryExampleJSONBlockIsValid guards the whole asset, not just the first block.
//
// The example is what the model generalises from, so a malformed block teaches the wrong
// shape — and a block added later would otherwise go unchecked.
func TestEveryExampleJSONBlockIsValid(t *testing.T) {
	// The asset also carries plain-text illustration blocks (a source cell shown as it
	// appears in the document), so only the JSON-looking ones are validated.
	var jsonBlocks int
	for i, block := range fencedBlocks(ManifestExample()) {
		trimmed := strings.TrimSpace(block)
		if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
			continue
		}
		jsonBlocks++
		if !json.Valid([]byte(trimmed)) {
			t.Errorf("example JSON block %d is not valid JSON:\n%s", i+1, trimmed)
		}
	}
	if jsonBlocks < 2 {
		t.Fatalf("expected at least two JSON blocks in the example, found %d", jsonBlocks)
	}
}

// TestContractRequiresSplittingAndExpansion guards the two instructions whose absence
// caused a real regression: a manifest of 15 whole cells where 39 split questions were
// needed, with the shorthand left unexpanded.
func TestContractRequiresSplittingAndExpansion(t *testing.T) {
	contract := strings.ToLower(ManifestContract())

	for _, phrase := range []string{
		"one question per entry", // a cell holding several questions becomes several
		"make each question answerable",
		"stand on its own", // why expansion is needed at all
		"named capability", // the compliance-matrix exception that must survive
	} {
		if !strings.Contains(contract, phrase) {
			t.Errorf("the contract no longer covers %q — without it, cells are left whole "+
				"and shorthand is left unexpanded", phrase)
		}
	}

	// The id guidance has to permit derived ids, or a document with no per-question ids
	// gets section titles as ids, which collide and cannot be routed.
	if !strings.Contains(contract, "r02-a") {
		t.Error("the contract no longer shows how to derive suffixed ids for a document with none")
	}
	if !strings.Contains(contract, "do **not** use a section title as an id") {
		t.Error("the contract no longer forbids using a section title as an id")
	}
}
