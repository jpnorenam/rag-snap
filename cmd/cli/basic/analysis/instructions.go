package analysis

import (
	"fmt"
	"strings"
)

// MaxInstructionsLength bounds the operator's instructions, in characters. About a
// thousand tokens: far more than the one-to-three sentences that fixed every live case,
// and small beside the documents. Over the bound is an error, never a truncation —
// cutting an instruction short would silently change what it says.
const MaxInstructionsLength = 4000

// ValidateInstructions rejects instructions over the bound. The CLI, the daemon and the
// UI all apply this one rule, so they report the same limit in the same words.
func ValidateInstructions(s string) error {
	if n := len([]rune(strings.TrimSpace(s))); n > MaxInstructionsLength {
		return fmt.Errorf("the analysis instructions are %d characters, above the %d-character limit", n, MaxInstructionsLength)
	}
	return nil
}

// operatorSection renders the operator's instructions for a call's system instruction.
//
// They go in the system instruction because that is the trusted channel: the documents
// arrive as user content and are untrusted, so text in an RFP that imitates an instruction
// cannot claim this heading's authority. The precedence sentence names what instructions
// may and may not override. "One question per section" is meant to override the default
// splitting; "use these ids" must not license ids that break routing, and the catalogue
// and count checks run locally afterwards whatever is said here.
func operatorSection(instructions string) string {
	instructions = strings.TrimSpace(instructions)
	if instructions == "" {
		return ""
	}
	return "\n\n# Operator instructions\n\n" +
		"These come from the person running this analysis, not from the documents. Follow them: they take " +
		"precedence over the default extraction and strategy guidance above. They do not override the manifest " +
		"contract's rules (routable ids, catalogue-only selections, the output format). Apply them; do not " +
		"restate or quote them in your response.\n\n" +
		instructions
}
