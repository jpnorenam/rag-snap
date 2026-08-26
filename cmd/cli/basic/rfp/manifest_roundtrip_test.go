// This is an external test package (rfp_test) so it can import the chat package,
// which imports rfp — the manifest a build writes has to be read back by the
// reader that actually runs it, not by a stand-in.
package rfp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/rfp"
)

func builtManifest() *rfp.Manifest {
	return &rfp.Manifest{
		Version: "1.0",
		Questions: []rfp.Question{
			{ID: "C1", Question: "Does the platform support SR-IOV?", Source: "EPA Sheet"},
			{ID: "J1.3", Question: "Which server hardware is supported?"},
			{ID: "1", Question: "Describe the GIS deliverables.", Source: "GIS Deliverables"},
		},
	}
}

// TestBuiltManifestStubIsInert is the guard on the stub's whole premise: a
// freshly built manifest must run exactly as it did before the stub existed, so
// the batch reader has to see no domains at all.
func TestBuiltManifestStubIsInert(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := rfp.WriteManifest(path, builtManifest()); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	m, err := chat.LoadBatchManifest(path)
	if err != nil {
		t.Fatalf("LoadBatchManifest on a built manifest: %v", err)
	}
	if len(m.Domains) != 0 {
		t.Errorf("built manifest parsed %d domains, want 0: %+v", len(m.Domains), m.Domains)
	}
	if len(m.Questions) != 3 {
		t.Fatalf("built manifest parsed %d questions, want 3", len(m.Questions))
	}
	// The stub is comment text, so it must not have disturbed the encoded body.
	if m.Version != "1.0" {
		t.Errorf("version = %q, want 1.0", m.Version)
	}
	if got := m.Questions[0]; got.ID != "C1" || got.Source != "EPA Sheet" {
		t.Errorf("question 0 = %+v, want id C1 from the EPA Sheet", got)
	}
	if got := m.Questions[2].Source; got != "GIS Deliverables" {
		t.Errorf("question 2 source = %q, want %q", got, "GIS Deliverables")
	}

	// And nothing resolves, so no question's prompt or retrieval changes.
	domains, err := chat.CompileDomains(m.Domains)
	if err != nil {
		t.Fatalf("CompileDomains on a built manifest: %v", err)
	}
	for _, q := range m.Questions {
		if d := domains.Resolve(q.ID, q.Source); d != nil {
			t.Errorf("question %q resolved to domain %q; want none", q.ID, d.Match)
		}
	}
}

// TestBuiltManifestStubUncommentedIsRejected pins the preamble's promise that an
// uncommented but unfilled entry fails loudly instead of being silently ignored.
func TestBuiltManifestStubUncommentedIsRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")
	if err := rfp.WriteManifest(path, builtManifest()); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading built manifest: %v", err)
	}

	// Uncomment the block lines the way an operator would, leaving the prose
	// lines of the preamble as comments.
	lines := strings.Split(string(data), "\n")
	uncommented := 0
	for i, line := range lines {
		if strings.HasPrefix(line, "# domains:") || strings.HasPrefix(line, "#   ") {
			lines[i] = strings.TrimPrefix(line, "# ")
			uncommented++
		}
	}
	if uncommented == 0 {
		t.Fatal("found no stub block lines to uncomment; the stub format changed")
	}

	edited := filepath.Join(dir, "edited.yaml")
	if err := os.WriteFile(edited, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatalf("writing edited manifest: %v", err)
	}
	if _, err := chat.LoadBatchManifest(edited); err == nil {
		t.Error("an uncommented, unfilled stub was accepted; it must be rejected rather than ignored")
	}
}
