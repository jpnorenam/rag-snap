package chat

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
)

// captureStdout returns what fn prints to stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()
	fn()
	_ = w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}

func TestSearchKapaOnly(t *testing.T) {
	kapa := knowledge.NewKapaClientAt(stubKapa(t, "never"), "proj", "key")
	session := &Session{KapaClient: kapa, ActiveKapaGroups: []string{"g-maas"}}

	out := captureStdout(t, func() { handleSearch("commission servers", session) })
	if !strings.Contains(out, "kapa content") || !strings.Contains(out, "KAPA-CANONICAL") {
		t.Errorf("output = %q, want the kapa.ai hit tagged KAPA-CANONICAL", out)
	}
}

func TestSearchNeedsASource(t *testing.T) {
	out := captureStdout(t, func() { handleSearch("anything", &Session{}) })
	if !strings.Contains(out, "/use-knowledge") || !strings.Contains(out, "/use-kapa") {
		t.Errorf("output = %q, want guidance naming /use-knowledge and /use-kapa", out)
	}
}

func TestSearchKapaFailureIsReported(t *testing.T) {
	kapa := knowledge.NewKapaClientAt(stubKapa(t, "FAILKAPA"), "proj", "key")
	session := &Session{KapaClient: kapa, ActiveKapaGroups: []string{"g-maas"}}

	out := captureStdout(t, func() { handleSearch("FAILKAPA query", session) })
	if !strings.Contains(out, "Warning: kapa.ai search failed") {
		t.Errorf("output = %q, want a kapa.ai failure warning", out)
	}
	if !strings.Contains(out, "No results found.") {
		t.Errorf("output = %q, want the (empty) local results still shown", out)
	}
}
