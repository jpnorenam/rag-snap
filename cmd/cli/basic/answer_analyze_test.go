package basic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveInstructions(t *testing.T) {
	if got, err := resolveInstructions("  Only the matrix.  ", ""); err != nil || got != "Only the matrix." {
		t.Errorf("flag text = %q, %v; want it trimmed", got, err)
	}

	path := filepath.Join(t.TempDir(), "instr.txt")
	if err := os.WriteFile(path, []byte("Split every row.\nOnly Annexure A.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveInstructions("", path); err != nil || got != "Split every row.\nOnly Annexure A." {
		t.Errorf("file text = %q, %v; want the whole file, trimmed", got, err)
	}

	if _, err := resolveInstructions("", filepath.Join(t.TempDir(), "missing.txt")); err == nil ||
		!strings.Contains(err.Error(), "--instructions-file") {
		t.Errorf("a missing file should name the flag, got %v", err)
	}
	if _, err := resolveInstructions(strings.Repeat("x", 4001), ""); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Errorf("over-long instructions should be rejected naming the limit, got %v", err)
	}
}
