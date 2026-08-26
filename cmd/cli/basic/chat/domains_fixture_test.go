package chat

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// fixturePath is the shared Go/TS resolution fixture, relative to this package.
// The TypeScript resolver (ui/lib/domains.ts) asserts the same file, so a change
// to specificity or the source fallback applied to only one implementation fails
// the other's tests.
const fixturePath = "../../../../testdata/domain_resolution.yaml"

// resolutionFixture mirrors testdata/domain_resolution.yaml. Domain is reused as
// the entry type so the fixture is decoded into the same struct a manifest is.
type resolutionFixture struct {
	Groups []struct {
		Name    string   `yaml:"name"`
		Domains []Domain `yaml:"domains"`
		Cases   []struct {
			Name   string `yaml:"name"`
			ID     string `yaml:"id"`
			Source string `yaml:"source"`
			Want   string `yaml:"want"`
		} `yaml:"cases"`
	} `yaml:"groups"`
}

func loadResolutionFixture(t *testing.T) resolutionFixture {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(fixturePath))
	if err != nil {
		t.Fatalf("reading the shared resolution fixture: %v", err)
	}
	var fixture resolutionFixture
	if err := yaml.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("decoding the shared resolution fixture: %v", err)
	}
	if len(fixture.Groups) == 0 {
		t.Fatal("the shared resolution fixture has no groups")
	}
	return fixture
}

func TestDomainSetResolveSharedFixture(t *testing.T) {
	fixture := loadResolutionFixture(t)

	for _, group := range fixture.Groups {
		t.Run(group.Name, func(t *testing.T) {
			set, err := CompileDomains(group.Domains)
			if err != nil {
				t.Fatalf("CompileDomains(%+v): %v", group.Domains, err)
			}
			if len(group.Cases) == 0 {
				t.Fatal("fixture group has no cases")
			}
			for _, tc := range group.Cases {
				t.Run(tc.Name, func(t *testing.T) {
					got := set.Resolve(tc.ID, tc.Source)
					if tc.Want == "" {
						if got != nil {
							t.Fatalf("Resolve(%q, %q) = %q, want no domain", tc.ID, tc.Source, got.Match)
						}
						return
					}
					if got == nil {
						t.Fatalf("Resolve(%q, %q) = no domain, want %q", tc.ID, tc.Source, tc.Want)
					}
					if got.Match != tc.Want {
						t.Errorf("Resolve(%q, %q) = %q, want %q", tc.ID, tc.Source, got.Match, tc.Want)
					}
				})
			}
		})
	}
}
