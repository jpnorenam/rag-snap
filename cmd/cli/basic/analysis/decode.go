package analysis

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
	"gopkg.in/yaml.v3"
)

// DecodeManifestJSON parses a model's JSON response into the manifest type the run
// actually consumes.
//
// It decodes with the YAML decoder on purpose. JSON is a subset of YAML 1.2, so the
// yaml tags already on chat.BatchManifest apply unchanged — which means analysis needs
// no third JSON-tagged mirror of the manifest to drift from the other two, and the
// custom unmarshalers on the real type (KeywordList, which accepts either a list or a
// comma-separated string) are exercised exactly as they are for a hand-written
// manifest. A manifest that decodes here is a manifest the run can load.
func DecodeManifestJSON(data []byte) (*chat.BatchManifest, error) {
	var manifest chat.BatchManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parsing model response: %w", err)
	}
	return &manifest, nil
}

// extractJSONBlock pulls the JSON payload out of a model response.
//
// It tries candidates in order of trustworthiness and returns the first that actually
// parses, rather than slicing on the first delimiter it happens to find:
//
//  1. The whole text, which is what a schema-constrained response normally is.
//  2. The contents of a fenced block, closed at the LAST fence rather than the first.
//  3. The span from the first "{" to the last "}", for a payload wrapped in prose.
//
// Closing at the last fence matters. Taking the first one truncates the payload
// whenever a triple-backtick appears inside the JSON — and it can, since question and
// context strings are lifted from a document that may contain one, and the worked
// example in the grounding assets is itself a fenced block the model has just been
// shown. The result was a payload cut off mid-structure, reported downstream as an
// opaque "unexpected end of stream".
//
// Validating each candidate is what makes the ordering safe: a wrong guess is
// discarded rather than passed on.
func extractJSONBlock(text string) (string, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", fmt.Errorf("no JSON found")
	}
	if json.Valid([]byte(trimmed)) {
		return trimmed, nil
	}

	var best string
	consider := func(candidate string) bool {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return false
		}
		if json.Valid([]byte(candidate)) {
			best = candidate
			return true
		}
		// Kept as a fallback so the caller's diagnostic reports the most
		// JSON-looking thing available rather than the whole prose reply.
		if best == "" || len(candidate) > len(best) {
			best = candidate
		}
		return false
	}

	// Each fenced block in turn. A document may carry several (the grounding assets
	// do), so the blocks are tried individually before the greedy span below.
	for _, block := range fencedBlocks(trimmed) {
		if consider(block) {
			return best, nil
		}
	}
	// The greedy span: from the first fence opener to the LAST fence. This is what
	// recovers a single block whose payload contains a triple-backtick of its own —
	// the individual-segment pass above cuts such a payload in half, this does not.
	if inner, ok := greedyFencedSpan(trimmed); ok && consider(inner) {
		return best, nil
	}
	if start, end := strings.Index(trimmed, "{"), strings.LastIndex(trimmed, "}"); start >= 0 && end > start {
		if consider(trimmed[start : end+1]) {
			return best, nil
		}
	}

	// Nothing parsed. Return the best candidate anyway: the caller validates and
	// builds a diagnostic from it, which is more useful than an error with no payload.
	if best == "" {
		best = trimmed
	}
	return best, nil
}

// fencedBlocks returns the contents of each fenced code block, in order.
//
// Segments are delimited by "```", with any language tag on the opening line stripped.
// A document may legitimately carry several blocks — the grounding assets do — so they
// are enumerated rather than assumed to be one.
func fencedBlocks(text string) []string {
	parts := strings.Split(text, "```")
	if len(parts) < 3 {
		return nil
	}
	var out []string
	// Odd indices are inside a fence; even indices are the prose between them.
	for i := 1; i < len(parts); i += 2 {
		out = append(out, stripLanguageTag(parts[i]))
	}
	return out
}

// greedyFencedSpan returns everything between the first fence opener and the last fence.
//
// This is the pairing that survives a triple-backtick inside the payload: splitting on
// every "```" cuts such a payload in half, while spanning to the last fence keeps it
// whole. It is tried after the per-block pass, because for a document of several blocks
// it would run them together.
func greedyFencedSpan(text string) (string, bool) {
	first := strings.Index(text, "```")
	last := strings.LastIndex(text, "```")
	if first < 0 || last <= first {
		return "", false
	}
	return stripLanguageTag(text[first+3 : last]), true
}

// stripLanguageTag removes a leading "json" (or any other bare word) left on the opening
// fence line.
func stripLanguageTag(block string) string {
	newline := strings.IndexByte(block, '\n')
	if newline < 0 {
		return block
	}
	if first := strings.TrimSpace(block[:newline]); first != "" && !strings.ContainsAny(first, "{}[]\"") {
		return block[newline+1:]
	}
	return block
}
