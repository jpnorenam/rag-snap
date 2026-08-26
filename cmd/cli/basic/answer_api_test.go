package basic

import (
	"encoding/json"
	"testing"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
)

// TestBatchManifestBodyCarriesDomainsAndSource guards the CLI's daemon-backed
// path against the drop that questions[].source suffered: every routing field a
// loaded manifest carries must reach the posted JSON, or `answer batch` answers
// differently with the daemon running than without it. The asserted JSON is the
// wire shape internal/api decodes in TestBatchManifestRequestRoundTrip.
func TestBatchManifestBodyCarriesDomainsAndSource(t *testing.T) {
	manifest := &chat.BatchManifest{
		Version: "1.0",
		Domains: []chat.Domain{
			{Match: "C*", Context: "Enhanced Platform Awareness", Keywords: chat.KeywordList{"sriov", "numa"}},
			{Match: "GIS*", Keywords: chat.KeywordList{"hardening"}},
		},
		Questions: []chat.BatchQuestion{
			{ID: "C4", Question: "SR-IOV", Source: "EPA Sheet", Keywords: chat.KeywordList{"passthrough"}},
			{ID: "17", Question: "Create VM", Source: "GIS Deliverables"},
		},
	}

	data, err := json.Marshal(batchManifestBody(manifest, 0.1))
	if err != nil {
		t.Fatalf("marshaling body: %v", err)
	}

	var wire struct {
		Domains []struct {
			Match    string   `json:"match"`
			Context  string   `json:"context"`
			Keywords []string `json:"keywords"`
		} `json:"domains"`
		Questions []struct {
			ID     string `json:"id"`
			Source string `json:"source"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("decoding posted body: %v", err)
	}

	if len(wire.Domains) != 2 {
		t.Fatalf("posted body carries %d domains, want 2; body=%s", len(wire.Domains), data)
	}
	if got := wire.Domains[0]; got.Match != "C*" || got.Context != "Enhanced Platform Awareness" {
		t.Errorf("posted domain[0] = %+v, want match C* with its context", got)
	}
	if got := wire.Domains[0].Keywords; len(got) != 2 || got[0] != "sriov" {
		t.Errorf("posted domain[0] keywords = %v, want [sriov numa]", got)
	}
	if got := wire.Domains[1]; got.Match != "GIS*" || got.Context != "" {
		t.Errorf("posted domain[1] = %+v, want the keywords-only GIS* entry", got)
	}
	if got := wire.Questions[0].Source; got != "EPA Sheet" {
		t.Errorf("posted question[0] source = %q, want %q", got, "EPA Sheet")
	}
	if got := wire.Questions[1].Source; got != "GIS Deliverables" {
		t.Errorf("posted question[1] source = %q, want %q", got, "GIS Deliverables")
	}
}

// TestBatchManifestBodyOmitsAbsentRouting keeps a manifest without routing on the
// wire shape it has today, so an existing manifest posts byte-identically.
func TestBatchManifestBodyOmitsAbsentRouting(t *testing.T) {
	manifest := &chat.BatchManifest{
		Version:   "1.0",
		Questions: []chat.BatchQuestion{{Question: "SR-IOV"}},
	}
	data, err := json.Marshal(batchManifestBody(manifest, 0.1))
	if err != nil {
		t.Fatalf("marshaling body: %v", err)
	}
	const want = `{"version":"1.0","temperature":0.1,"questions":[{"question":"SR-IOV"}]}`
	if string(data) != want {
		t.Errorf("posted body = %s\nwant %s", data, want)
	}
}

// TestBatchResultsFromDecodesDomain confirms the results the daemon publishes in
// its operation metadata decode with their recorded domain, so the JSON file the
// daemon-backed path writes carries the same provenance as the direct path.
func TestBatchResultsFromDecodesDomain(t *testing.T) {
	results := []chat.BatchResult{
		{ID: "C4", Question: "SR-IOV", Answer: "yes", Domain: "C*"},
		{ID: "A9", Question: "Other", Answer: "no"},
	}
	// Round-trip through the untyped shape operation metadata arrives as.
	data, err := json.Marshal(map[string]any{"results": results})
	if err != nil {
		t.Fatalf("marshaling metadata: %v", err)
	}
	var meta struct {
		Results []chat.BatchResult `json:"results"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("decoding metadata: %v", err)
	}
	if got := meta.Results[0].Domain; got != "C*" {
		t.Errorf("decoded domain = %q, want %q", got, "C*")
	}
	if got := meta.Results[1].Domain; got != "" {
		t.Errorf("unrouted result decoded domain = %q, want empty", got)
	}

	// And the written results file carries it.
	out, err := json.Marshal(chat.BatchOutput{Results: meta.Results})
	if err != nil {
		t.Fatalf("marshaling output: %v", err)
	}
	if !json.Valid(out) {
		t.Fatal("output is not valid JSON")
	}
	var file struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(out, &file); err != nil {
		t.Fatalf("decoding output: %v", err)
	}
	if got := file.Results[0]["domain"]; got != "C*" {
		t.Errorf("results file domain = %v, want C*", got)
	}
	if _, present := file.Results[1]["domain"]; present {
		t.Error("unrouted result wrote a domain key; want it omitted")
	}
}
