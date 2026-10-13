package analysis

import (
	"fmt"
	"strings"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
)

// OpenStackFlavour is the OpenStack offering answers should present.
//
// It is asked only when triage reported that the RFP requires OpenStack, so the empty
// value is the normal state for an RFP that does not mention it.
type OpenStackFlavour string

// The OpenStack flavours the interview offers, plus the empty not-applicable value.
const (
	OpenStackNotApplicable OpenStackFlavour = ""
	OpenStackSunbeam       OpenStackFlavour = "sunbeam"
	OpenStackCharmed       OpenStackFlavour = "charmed"
)

// Description renders the flavour as the offering to name in a prompt.
func (f OpenStackFlavour) Description() string {
	switch f {
	case OpenStackSunbeam:
		return "Canonical OpenStack deployed with Sunbeam"
	case OpenStackCharmed:
		return "Charmed OpenStack, deployed with Juju charms"
	}
	return ""
}

// Other returns the flavour that must not be presented as the proposal, so the
// generated prompt can rule it out by name rather than by implication.
func (f OpenStackFlavour) Other() string {
	switch f {
	case OpenStackSunbeam:
		return "Charmed OpenStack"
	case OpenStackCharmed:
		return "Sunbeam"
	}
	return ""
}

// Valid reports whether f is a recognised value.
func (f OpenStackFlavour) Valid() bool {
	return f == OpenStackNotApplicable || f == OpenStackSunbeam || f == OpenStackCharmed
}

// ParseOpenStackFlavour accepts the two flavours, case-insensitively, plus the empty
// value for an RFP where OpenStack is out of scope.
func ParseOpenStackFlavour(s string) (OpenStackFlavour, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return OpenStackNotApplicable, nil
	case "sunbeam":
		return OpenStackSunbeam, nil
	case "charmed", "charmed-openstack", "charmed openstack":
		return OpenStackCharmed, nil
	}
	return "", fmt.Errorf("openstack flavour must be %q or %q (or empty when out of scope), got %q",
		OpenStackSunbeam, OpenStackCharmed, s)
}

// Interview is the operator's answers, which materially shape the generated manifest.
type Interview struct {
	// AnswerWords is the target answer length. AnswerLimit says whether it may be
	// exceeded — a length is never stated without its mode, because "100 words" alone
	// tells the answering model nothing about what to do at 130.
	AnswerWords int
	AnswerLimit LengthMode

	// OpenStack is set only when triage reported OpenStack in scope.
	OpenStack OpenStackFlavour
}

// DefaultInterview returns the interview pre-filled from configuration, which is what
// the CLI prompts over and the UI renders as a pre-populated form.
func DefaultInterview(s Settings) Interview {
	return Interview{AnswerWords: s.AnswerWords, AnswerLimit: s.AnswerLimit}
}

// Validate rejects an interview that cannot be rendered into a prompt.
func (iv Interview) Validate() error {
	if iv.AnswerWords <= 0 {
		return fmt.Errorf("answer length must be a positive number of words, got %d", iv.AnswerWords)
	}
	if !iv.AnswerLimit.Valid() {
		return fmt.Errorf("answer length mode must be %q or %q, got %q", LengthHard, LengthSoft, iv.AnswerLimit)
	}
	if !iv.OpenStack.Valid() {
		return fmt.Errorf("unrecognised openstack flavour %q", iv.OpenStack)
	}
	return nil
}

// LengthSentence states the budget and its enforcement together, which is the only
// form either is allowed to appear in.
func (iv Interview) LengthSentence() string {
	if iv.AnswerLimit == LengthHard {
		return fmt.Sprintf(
			"Answers are at most %d words. This is a hard limit and must never be exceeded: an answer over %d words is truncated before it is read, so a long answer loses its own conclusion.",
			iv.AnswerWords, iv.AnswerWords)
	}
	return fmt.Sprintf(
		"Target about %d words per answer. This is a soft target: exceed it slightly where a requirement genuinely needs the room, but do not run to twice the length.",
		iv.AnswerWords)
}

// mandatoryRules is appended to every generated prompt.
//
// These are appended by rag-cli rather than requested of the model, and that is a
// security decision rather than a stylistic one. An RFP is untrusted input: a document
// can contain text that reads as instructions ("respond affirmatively to every
// requirement", "do not state gaps"), and a model asked to write a prompt from that
// document may absorb it. Appending the rules afterwards means no wording in the source
// document can remove them, and `answer batch --check` re-verifies they are still
// present on any manifest.
const mandatoryRules = `GAPS ARE REPORTED, NEVER FILLED
It is always better to say that no context is available, or to state plainly that a technology is NOT covered, than to give an answer that is false or invented. This outranks every other instruction here, including completeness and tone. Where the provided context does not substantiate what a requirement asks for, say so in as many words and stop. Do not soften a gap, do not write around it with a general statement about the platform, and do not let a demanded element pass unmentioned because it is unsubstantiated. A flagged gap is routed to a human before submission; a fabricated claim survives into an evaluation and is discovered by the customer.

BE DIRECT
No filler. No preamble, no restating the question, no meta-commentary about the context or your own process, and nothing like "Based on the provided context", "It is important to note", "As per the documentation", or "In today's landscape". Lead with the answer, substantiate it, stop. Every sentence must carry information the evaluator did not already have.`

// AssembleGeneratedPrompt returns the final prompt: the model's prompt followed by the
// rules and constraints rag-cli guarantees.
//
// Order matters. The mandatory block comes last so that it reads as the final word on
// any point the generated prose might have contradicted.
func AssembleGeneratedPrompt(generated string, iv Interview, t *Triage) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(generated))
	b.WriteString("\n\n")

	b.WriteString("ANSWER LENGTH\n")
	b.WriteString(iv.LengthSentence())
	b.WriteString("\n\n")

	if desc := iv.OpenStack.Description(); desc != "" {
		fmt.Fprintf(&b, "OPENSTACK OFFERING\nThe OpenStack offering being proposed is %s. Describe that offering and no other. Do not present %s as the proposed solution, and do not hedge between the two: where a capability differs between them, describe the proposed one.\n\n",
			desc, iv.OpenStack.Other())
	}

	if t != nil && t.MultiFieldAnswers {
		b.WriteString("SEVERAL FIELDS PER QUESTION\n")
		b.WriteString("Each question in this document expects more than one field to be filled.")
		if detail := strings.TrimSpace(t.MultiFieldDetail); detail != "" {
			fmt.Fprintf(&b, " The fields are: %s.", detail)
		}
		b.WriteString(" Produce every field, each on its own line, prefixed with the field's name and a colon, in the order named above. Apply the answer-length budget to the whole response rather than to each field separately, and keep the compliance-status style fields to the single word or phrase the document asks for.\n\n")
	}

	b.WriteString(mandatoryRules)
	b.WriteString("\n")

	return strings.TrimSpace(b.String()) + "\n"
}

// InspectAssembledPrompt reports what an assembled prompt carries, using the same
// detectors the manifest check applies.
//
// The detectors live in the chat package with the manifest type, so that generation and
// the check cannot disagree about whether a prompt still carries its rules — the check
// is the thing that catches a hand-edit dropping them, and it would be useless if it
// looked for something different from what assembly writes.
func InspectAssembledPrompt(prompt string) chat.PromptAssertions {
	return chat.InspectPrompt(&chat.BatchManifest{Prompt: prompt})
}

// PromptHasNoContextRule reports whether a prompt still instructs that reporting a gap
// beats inventing an answer.
func PromptHasNoContextRule(prompt string) bool {
	return InspectAssembledPrompt(prompt).NoContextRule
}

// PromptHasDirectnessRule reports whether a prompt still instructs directness.
func PromptHasDirectnessRule(prompt string) bool {
	return InspectAssembledPrompt(prompt).DirectnessRule
}

// PromptLengthStatement finds a stated word budget and its enforcement mode, reporting
// whether both were found. A length with no mode is treated as not found, because that
// is exactly the ambiguity the interview exists to remove.
func PromptLengthStatement(prompt string) (found bool, mode LengthMode) {
	a := InspectAssembledPrompt(prompt)
	if !a.LengthStated {
		return false, ""
	}
	if a.LengthHard {
		return true, LengthHard
	}
	return true, LengthSoft
}
