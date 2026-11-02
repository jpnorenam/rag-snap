package analysis

import (
	"embed"
	"fmt"
	"strings"
)

// assetsFS holds the grounding payload sent with every analysis request.
//
// These are embedded rather than read from disk or from documentation so that the
// contract the model is given ships with the binary that consumes its output. A
// contract on disk could drift from the code, and a model told the wrong contract
// produces a manifest that fails to load — or worse, one that loads and routes
// nothing.
//
//go:embed assets/manifest-contract.md assets/manifest-example.md
var assetsFS embed.FS

// ManifestContract is the field-by-field explanation of the manifest format and the
// domain resolution rules, for a model that has never seen rag-cli.
func ManifestContract() string {
	return mustAsset("assets/manifest-contract.md")
}

// ManifestExample is a worked example of a complete manifest. The contract describes
// the format; the example shows it, which is what a model actually generalises from.
func ManifestExample() string {
	return mustAsset("assets/manifest-example.md")
}

func mustAsset(name string) string {
	data, err := assetsFS.ReadFile(name)
	if err != nil {
		// Unreachable: the file is embedded at build time, so a failure here means
		// the binary was built without it, which a test guards against.
		panic(fmt.Sprintf("analysis: embedded asset %q missing: %v", name, err))
	}
	return string(data)
}

// groundingPreamble introduces the two assets in the system turn.
const groundingPreamble = `You generate manifests for rag-cli, a retrieval-augmented generation tool that answers RFP/RFI documents. The contract below defines the format; the worked example shows it. Follow both exactly.`

// Grounding returns the full grounding text: the preamble, the contract, and the
// worked example, in the order a request sends them.
func Grounding() string {
	var b strings.Builder
	b.WriteString(groundingPreamble)
	b.WriteString("\n\n")
	b.WriteString(ManifestContract())
	b.WriteString("\n\n")
	b.WriteString(ManifestExample())
	return b.String()
}
