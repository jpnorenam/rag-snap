package chat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeKeywords(t *testing.T) {
	tests := []struct {
		name      string
		generated string
		leading   [][]string
		want      string
	}{
		{
			"no leading tiers returns the generated query untouched",
			"ceph storage replication",
			nil,
			"ceph storage replication",
		},
		{
			"empty leading tiers return the generated query untouched",
			"ceph storage replication",
			[][]string{{}, {}},
			"ceph storage replication",
		},
		{
			"question keywords lead the generated ones",
			"virtual machine create",
			[][]string{{"openstack", "nova"}},
			"openstack nova virtual machine create",
		},
		{
			"three tiers keep question, then domain, then generated",
			"network interface passthrough",
			[][]string{{"sriov"}, {"epa", "numa"}},
			"sriov epa numa network interface passthrough",
		},
		{
			"domain keywords apply with no question keywords",
			"network interface",
			[][]string{{}, {"epa", "numa"}},
			"epa numa network interface",
		},
		{
			"case-differing duplicate collapses to its first position",
			"ceph replication",
			[][]string{{"Ceph"}, {"CEPH", "rados"}},
			"Ceph rados replication",
		},
		{
			"duplicate within a tier collapses",
			"storage",
			[][]string{{"ceph", "ceph"}},
			"ceph storage",
		},
		{
			"generated duplicate of a domain keyword is dropped",
			"numa topology",
			[][]string{nil, {"numa"}},
			"numa topology",
		},
		{
			"empty generated query yields the leading tiers alone",
			"",
			[][]string{{"sriov"}, {"epa"}},
			"sriov epa",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeKeywords(tt.generated, tt.leading...)
			if got != tt.want {
				t.Errorf("mergeKeywords(%q, %v) = %q, want %q", tt.generated, tt.leading, got, tt.want)
			}
		})
	}
}

func TestMergeKeywordsAcceptsKeywordList(t *testing.T) {
	// The call sites pass KeywordList values (question keywords, domain
	// keywords) into a []string variadic; this pins that down.
	question := KeywordList{"sriov"}
	domain := KeywordList{"epa"}
	if got, want := mergeKeywords("passthrough", question, domain), "sriov epa passthrough"; got != want {
		t.Errorf("mergeKeywords = %q, want %q", got, want)
	}
}

// preDomainRoutingTurn is the exact user turn the batch pipeline produced before
// domain routing existed. A question with neither a domain nor an id must still
// produce it byte for byte, or every existing manifest changes meaning.
func preDomainRoutingTurn(ragContext, question string) string {
	return fmt.Sprintf("Context:\n%s\n\nQuestion: %s", ragContext, question)
}

func TestBuildRAGPromptNoDomainNoID(t *testing.T) {
	const ragContext = "[CANONICAL] Ceph replicates objects across OSDs."
	const question = "Describe the storage replication approach."

	got := buildRAGPrompt(ragContext, "", "", question)
	if want := preDomainRoutingTurn(ragContext, question); got != want {
		t.Errorf("buildRAGPrompt with no domain and no id =\n%q\nwant\n%q", got, want)
	}
}

func TestBuildRAGPrompt(t *testing.T) {
	const ragContext = "[CANONICAL] chunk text"

	tests := []struct {
		name          string
		domainContext string
		id            string
		want          string
	}{
		{
			"no domain, no id",
			"", "",
			"Context:\n[CANONICAL] chunk text\n\nQuestion: SR-IOV",
		},
		{
			"id only",
			"", "C4",
			"Context:\n[CANONICAL] chunk text\n\nQuestion [C4]: SR-IOV",
		},
		{
			"domain only",
			"Enhanced Platform Awareness", "",
			"Context:\n[CANONICAL] chunk text\n\n" +
				"Requirement domain: Enhanced Platform Awareness. Answer within this domain.\n\n" +
				"Question: SR-IOV",
		},
		{
			"domain and id",
			"Enhanced Platform Awareness", "C4",
			"Context:\n[CANONICAL] chunk text\n\n" +
				"Requirement domain: Enhanced Platform Awareness. Answer within this domain.\n\n" +
				"Question [C4]: SR-IOV",
		},
		{
			"domain context already sentence-terminated is not double-punctuated",
			"Enhanced Platform Awareness.", "C4",
			"Context:\n[CANONICAL] chunk text\n\n" +
				"Requirement domain: Enhanced Platform Awareness. Answer within this domain.\n\n" +
				"Question [C4]: SR-IOV",
		},
		{
			"question-mark terminator is left alone",
			"Which server hardware?", "J1.3",
			"Context:\n[CANONICAL] chunk text\n\n" +
				"Requirement domain: Which server hardware? Answer within this domain.\n\n" +
				"Question [J1.3]: SR-IOV",
		},
		{
			"whitespace-only domain and id are treated as absent",
			"   ", "  ",
			"Context:\n[CANONICAL] chunk text\n\nQuestion: SR-IOV",
		},
		{
			"multi-line domain context is kept as written",
			"Security baseline deliverables:\nhardening and audit.", "GIS3",
			"Context:\n[CANONICAL] chunk text\n\n" +
				"Requirement domain: Security baseline deliverables:\nhardening and audit. Answer within this domain.\n\n" +
				"Question [GIS3]: SR-IOV",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildRAGPrompt(ragContext, tt.domainContext, tt.id, "SR-IOV")
			if got != tt.want {
				t.Errorf("buildRAGPrompt =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestBatchSystemPromptUnaffectedByDomains(t *testing.T) {
	prompts := PromptConfig{
		SourceRules:        "SOURCE RULES",
		AnswerSystemPrompt: "ANSWER SYSTEM PROMPT",
	}

	withoutDomains := &BatchManifest{Version: "1"}
	withDomains := &BatchManifest{
		Version: "1",
		Domains: []Domain{{Match: "C*", Context: "Enhanced Platform Awareness"}},
	}

	// The domain reaches the turn, never the system prompt: adding a domains
	// block must not change the prefix sent with every question.
	got := batchSystemPrompt(withDomains, prompts)
	if want := batchSystemPrompt(withoutDomains, prompts); got != want {
		t.Errorf("system prompt changed when a domains block was added:\n%q\nwant\n%q", got, want)
	}
	if got != prompts.AnswerSystemPrompt {
		t.Errorf("system prompt = %q, want the configured answer system prompt", got)
	}
	if strings.Contains(got, "Enhanced Platform Awareness") {
		t.Error("system prompt leaked a domain context")
	}

	// A custom manifest prompt still gets the non-negotiable source rules
	// appended, unchanged by this capability.
	custom := &BatchManifest{Version: "1", Prompt: "CUSTOM", Domains: withDomains.Domains}
	if got, want := batchSystemPrompt(custom, prompts), "CUSTOM\n\nSOURCE RULES"; got != want {
		t.Errorf("custom system prompt = %q, want %q", got, want)
	}
}

func TestLoadBatchManifestDomains(t *testing.T) {
	write := func(t *testing.T, body string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "manifest.yaml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("writing manifest: %v", err)
		}
		return path
	}

	t.Run("valid block is read", func(t *testing.T) {
		m, err := LoadBatchManifest(write(t, `version: "1"
domains:
  - match: "C*"
    context: Enhanced Platform Awareness
    keywords: [sriov, numa]
  - match: "J1.*"
    keywords:
      - vendor
questions:
  - id: C4
    source: EPA Sheet
    question: SR-IOV
`))
		if err != nil {
			t.Fatalf("LoadBatchManifest: %v", err)
		}
		if len(m.Domains) != 2 {
			t.Fatalf("read %d domains, want 2", len(m.Domains))
		}
		if m.Domains[0].Match != "C*" || m.Domains[0].Context != "Enhanced Platform Awareness" {
			t.Errorf("domain 0 = %+v, want match C* with its context", m.Domains[0])
		}
		if len(m.Domains[0].Keywords) != 2 {
			t.Errorf("domain 0 keywords = %v, want two entries", m.Domains[0].Keywords)
		}
		if len(m.Domains[1].Keywords) != 1 || m.Domains[1].Keywords[0] != "vendor" {
			t.Errorf("domain 1 keywords = %v, want [vendor] from the block sequence", m.Domains[1].Keywords)
		}
		// The field the decoder used to drop.
		if m.Questions[0].Source != "EPA Sheet" {
			t.Errorf("question source = %q, want %q", m.Questions[0].Source, "EPA Sheet")
		}
	})

	t.Run("no domains block", func(t *testing.T) {
		m, err := LoadBatchManifest(write(t, `version: "1"
questions:
  - question: SR-IOV
`))
		if err != nil {
			t.Fatalf("LoadBatchManifest: %v", err)
		}
		if m.Domains != nil {
			t.Errorf("Domains = %v, want nil", m.Domains)
		}
	})

	t.Run("invalid block is rejected at load time", func(t *testing.T) {
		_, err := LoadBatchManifest(write(t, `version: "1"
domains:
  - match: "C*"
questions:
  - question: SR-IOV
`))
		if err == nil {
			t.Fatal("LoadBatchManifest accepted an entry with neither context nor keywords")
		}
	})

	t.Run("duplicate patterns are rejected at load time", func(t *testing.T) {
		_, err := LoadBatchManifest(write(t, `version: "1"
domains:
  - match: "C*"
    context: first
  - match: "C*"
    context: second
questions:
  - question: SR-IOV
`))
		if err == nil {
			t.Fatal("LoadBatchManifest accepted duplicate match patterns")
		}
	})
}
