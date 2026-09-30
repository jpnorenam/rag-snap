package basic

import (
	"io"
	"os"
	"regexp"
	"testing"
)

// TestPrintModelIDFormat pins the line format opensearch-on-aws.sh parses (model_id in
// internal/awssetup/assets/opensearch-on-aws.sh) to persist the IDs after `knowledge init`.
func TestPrintModelIDFormat(t *testing.T) {
	r, w, _ := os.Pipe()
	stdout := os.Stdout
	os.Stdout = w
	printModelID("Embedding", "knowledge.model.embedding", "aB3-x_9", false)
	printModelID("Rerank", "knowledge.model.rerank", "Zz9", false)
	w.Close()
	os.Stdout = stdout
	out, _ := io.ReadAll(r)

	for label, id := range map[string]string{"Embedding": "aB3-x_9", "Rerank": "Zz9"} {
		re := regexp.MustCompile(`(?m)^` + label + ` model ID: ([A-Za-z0-9_-]+)\s*$`)
		if m := re.FindStringSubmatch(string(out)); m == nil || m[1] != id {
			t.Errorf("%s line not in the parsed format:\n%s", label, out)
		}
	}
}
