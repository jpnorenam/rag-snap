package analysis

import (
	"context"
	"fmt"
	"strings"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
	"gopkg.in/yaml.v3"
)

// shortfallFraction is how far below the triage estimate a question count may fall
// before generation is treated as incomplete.
//
// It is a fraction rather than an absolute because the estimate is itself approximate:
// a triage that guessed 150 and an extraction that found 140 is agreement, while one
// that found 40 is a call that stopped early. Two thirds is deliberately forgiving —
// the check exists to catch a call that returned a fraction of the document, not to
// second-guess a careful extraction.
const shortfallFraction = 2.0 / 3.0

// questionsResponse is the questions call's payload. It carries yaml tags because the
// response is decoded with the YAML decoder — JSON being a subset of YAML — so that
// chat.BatchQuestion's own tags and custom unmarshalers apply unchanged.
type questionsResponse struct {
	Questions []chat.BatchQuestion `yaml:"questions"`
}

// strategyResponse is the strategy call's payload: everything about a manifest that is
// not the question list.
type strategyResponse struct {
	Prompt           string        `yaml:"prompt"`
	KnowledgeBases   []string      `yaml:"knowledge_bases"`
	KapaSourceGroups []string      `yaml:"kapa_source_groups"`
	Domains          []chat.Domain `yaml:"domains"`
}

func questionsSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"questions": map[string]any{
				"type":        "array",
				"description": "Every answerable question in the document, in document order, one question per entry. A cell holding several questions yields several entries.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":       map[string]any{"type": "string", "description": "Short, routable identifier. The document's own where it has one; otherwise a section code suffixed per question (R02-a, R02-b). Never a long section title."},
						"question": map[string]any{"type": "string", "description": "ONE self-contained question, readable with no document and no section header in view. Split a cell holding several questions into several entries; expand shorthand into a complete question."},
						"source":   map[string]any{"type": "string", "description": "The full section, sheet or row title it came from. Always set this when the question was derived from a larger cell."},
					},
					"required": []any{"question"},
				},
			},
		},
		"required": []any{"questions"},
	}
}

func strategySchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "One system prompt for the whole RFP, written as instructions to the model that will answer each question in isolation.",
			},
			"knowledge_bases": map[string]any{
				"type":        "array",
				"description": "Knowledge base names, chosen only from the supplied catalogue.",
				"items":       map[string]any{"type": "string"},
			},
			"kapa_source_groups": map[string]any{
				"type":        "array",
				"description": "Source group identifiers, chosen only from the supplied catalogue. Omit when none apply.",
				"items":       map[string]any{"type": "string"},
			},
			"domains": map[string]any{
				"type":        "array",
				"description": "Requirement-domain routing. Each pattern must match at least one of the question ids.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"match":    map[string]any{"type": "string", "description": "Glob matched against question ids."},
						"context":  map[string]any{"type": "string", "description": "Prose injected into the turn for every matching question."},
						"keywords": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					},
					"required": []any{"match", "context"},
				},
			},
		},
		"required": []any{"prompt", "knowledge_bases", "domains"},
	}
}

const questionsInstruction = `Extract every question or requirement from the attached documents that needs an answer.

This is an extraction pass. Do not write a prompt, choose knowledge bases, or design domains — another pass does that.

Follow the contract's "questions[]" section exactly. The three rules that matter most here:

- ONE QUESTION PER ENTRY. A cell, row or bullet list often packs several distinct questions together, separated by bullet markers (_ - •), newlines, semicolons, or a "?" mid-text. Each becomes its own entry. A cell left whole produces one crammed answer that addresses its first clause and neglects the rest, so splitting is not tidying — it decides whether those requirements get answered at all. Do not emit an entry for a header that merely labels the cells beneath it.

- MAKE EACH QUESTION STAND ALONE. The answering model sees one question with no document, no neighbouring rows and no section header. Where the source is shorthand ("_ control over encryption keys."), expand it into a complete question meaning exactly what the shorthand meant. Add only what the shorthand implied; never invent a requirement, and never split one question into two by inventing a second clause. The exception is a compliance matrix whose rows are single named capabilities ("SR-IOV", "CPU Pinning") — there the term is the requirement, so leave it alone.

- IDS MUST BE SHORT AND ROUTABLE. Use the document's ids where they exist. Where they do not, assign a short section code and suffix each question within it (R02-a, R02-b, R02-c), and put the full section title in "source". Never use a long section title as an id.

Work through the document in order, section by section, so nothing is missed.`

const strategyInstruction = `Design the answering strategy for the attached documents: the system prompt, the retrieval targets, and the requirement-domain routing.

Do NOT extract the questions — another pass does that. You are given the question ids that pass found, so that your domains patterns match ids that actually exist.

Produce:

1. "prompt" — one system prompt for the whole RFP, following the "Writing the prompt" section of the contract. Ground it in what these documents actually say: their evaluation criteria, stated incumbent environment, terminology and length limits.

   This is the single biggest lever on answer quality and the part most often underdone. A prompt of a few short paragraphs is not enough. Write it as a briefing document with a titled section for each of: who is answering and to whom; exactly which products and versions are being proposed, and which competing or incumbent ones must never be described as the proposal; what the input to each turn looks like and how to treat it; the answer shape, including whether markdown and line breaks are safe for where the answer is pasted; what counts as evidence and which named components, interfaces and configuration surfaces to cite; the specifics that must never be invented (versions, dates, figures, customer names, headcounts, certifications); how to handle a third-party product named in a requirement; and how to treat commercial or licensing requirements. Expect several hundred words at minimum, and considerably more for a document with distinct sections — a thorough prompt for a substantial RFP runs to a thousand words or more. Length is not the goal; covering every one of those points concretely, in this RFP's own terms, is.

2. "knowledge_bases" — the retrieval targets, chosen only from the catalogue.

3. "kapa_source_groups" — source group identifiers from the catalogue, or omit entirely.

4. "domains" — one entry per section of the RFP, each with a pattern matching that section's question ids and a context that tells the answering model what that section is really asking about and what counts as evidence in it. Check every pattern against the question ids you were given. Where a single question needs context the rest of its section does not, give it its own entry with a pattern matching only that id.

Do not include instructions about answer length, gap reporting, or filler in the prompt: those are appended afterwards and will be duplicated if you write them yourself.`

// Generate runs the second phase and returns a validated manifest.
//
// Generation is two calls over the same staged documents: questions (long, mechanical)
// and strategy (shorter, synthetic). The split is for the reasoning budget rather than
// the output size — the largest manifest in this repository is under a third of the
// output ceiling, so size was never the constraint. What the split buys is that each
// call gets its own ceiling covering its own reasoning, and each can carry its own
// reasoning effort: low for exhaustive extraction, high for synthesis.
func (c *Client) Generate(
	ctx context.Context,
	conv *Conveyance,
	cat *Catalogue,
	t *Triage,
	iv Interview,
) (*chat.BatchManifest, error) {
	if err := iv.Validate(); err != nil {
		return nil, err
	}

	questions, err := c.generateQuestions(ctx, conv, t)
	if err != nil {
		return nil, err
	}

	strategy, err := c.generateStrategy(ctx, conv, cat, t, iv, questions)
	if err != nil {
		return nil, err
	}

	manifest := &chat.BatchManifest{
		Version:          "1.0",
		KnowledgeBases:   cat.FilterKnowledgeBases(strategy.KnowledgeBases),
		KapaSourceGroups: cat.FilterKapaGroups(strategy.KapaSourceGroups),
		Prompt:           AssembleGeneratedPrompt(strategy.Prompt, iv, t),
		Domains:          strategy.Domains,
		Questions:        questions,
	}

	// The selections are validated against the catalogue that was actually sent,
	// rather than trusted because the model was told to use it. An uncatalogued name
	// retrieves nothing and reports no error at run time, so it has to be caught here.
	badKBs, badGroups := cat.ValidateSelections(strategy.KnowledgeBases, strategy.KapaSourceGroups)
	if len(badKBs) > 0 || len(badGroups) > 0 {
		var parts []string
		if len(badKBs) > 0 {
			parts = append(parts, fmt.Sprintf("knowledge base(s) %s", strings.Join(badKBs, ", ")))
		}
		if len(badGroups) > 0 {
			parts = append(parts, fmt.Sprintf("kapa source group(s) %s", strings.Join(badGroups, ", ")))
		}
		return nil, fmt.Errorf(
			"the model selected %s, which do not exist; they were dropped, so re-run analysis rather than answering against an incomplete selection",
			strings.Join(parts, " and "))
	}
	if len(manifest.KnowledgeBases) == 0 {
		return nil, &IncompleteError{Call: "strategy", Missing: []string{"any usable knowledge base"}}
	}

	// The manifest must load under the reader a run uses, checked here rather than
	// discovered when the operator tries to run it.
	if err := ValidateManifest(manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

func (c *Client) generateQuestions(ctx context.Context, conv *Conveyance, t *Triage) ([]chat.BatchQuestion, error) {
	system := Grounding()

	instruction := questionsInstruction
	if t != nil && t.CompoundCells {
		instruction += "\n\nTriage found that cells in this document pack several questions together"
		if sep := strings.TrimSpace(t.Separator); sep != "" {
			instruction += fmt.Sprintf(", separated by %s", sep)
		}
		instruction += ". Split every one of them, and expect to produce materially more entries than the document has rows."
	}
	if t != nil && t.EstimatedQuestions > 0 {
		instruction += fmt.Sprintf("\n\nA triage pass over these same documents counted about %d questions. Treat that as the number to account for: if you produce far fewer, you have stopped early.", t.EstimatedQuestions)
	}
	if t != nil && len(t.Sections) > 0 {
		var b strings.Builder
		b.WriteString("\n\nThe sections triage found, with the id prefix each uses:\n")
		for _, s := range t.Sections {
			fmt.Fprintf(&b, "- %s", s.Name)
			if s.IDPrefix != "" {
				fmt.Fprintf(&b, " (ids beginning %s)", s.IDPrefix)
			}
			if s.EstimatedQuestions > 0 {
				fmt.Fprintf(&b, " — about %d questions", s.EstimatedQuestions)
			}
			b.WriteString("\n")
		}
		instruction += b.String()
	}

	parts := append([]part{textPart(instruction)}, documentParts(conv)...)

	data, err := c.complete(ctx, "questions", system, parts, "rfp_questions", questionsSchema(), c.settings.ReasoningQuestions, maxOutputTokens)
	if err != nil {
		return nil, err
	}

	var resp questionsResponse
	if err := yaml.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing questions response: %w", err)
	}

	questions := dropEmptyQuestions(resp.Questions)
	if len(questions) == 0 {
		return nil, &IncompleteError{Call: "questions", Missing: []string{"any questions"}}
	}

	// The count comparison is the only cheap detector for a call that quietly returned
	// a fraction of the RFP, which is why triage estimates a count at all.
	if t != nil && t.EstimatedQuestions > 0 {
		if float64(len(questions)) < float64(t.EstimatedQuestions)*shortfallFraction {
			return nil, &ShortfallError{Expected: t.EstimatedQuestions, Produced: len(questions)}
		}
	}
	return questions, nil
}

func (c *Client) generateStrategy(
	ctx context.Context,
	conv *Conveyance,
	cat *Catalogue,
	t *Triage,
	iv Interview,
	questions []chat.BatchQuestion,
) (*strategyResponse, error) {
	system := Grounding() + "\n\n" + cat.Prompt()

	var b strings.Builder
	b.WriteString(strategyInstruction)

	if desc := iv.OpenStack.Description(); desc != "" {
		fmt.Fprintf(&b, "\n\nThe OpenStack offering being proposed is %s. Write the prompt around that offering, and do not present %s as the proposal.",
			desc, iv.OpenStack.Other())
	}
	if t != nil && t.MultiFieldAnswers {
		b.WriteString("\n\nEach question in this document expects more than one field to be filled. Take that into account when describing the answer shape, but do not specify the field format itself — that is appended afterwards.")
	}
	if t != nil && strings.TrimSpace(t.Notes) != "" {
		fmt.Fprintf(&b, "\n\nTriage noted: %s", strings.TrimSpace(t.Notes))
	}

	b.WriteString("\n\n" + questionIDDigest(questions))

	parts := append([]part{textPart(b.String())}, documentParts(conv)...)

	data, err := c.complete(ctx, "strategy", system, parts, "rfp_strategy", strategySchema(), c.settings.ReasoningStrategy, maxOutputTokens)
	if err != nil {
		return nil, err
	}

	var resp strategyResponse
	if err := yaml.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing strategy response: %w", err)
	}

	var missing []string
	if strings.TrimSpace(resp.Prompt) == "" {
		missing = append(missing, "a prompt")
	}
	if len(resp.KnowledgeBases) == 0 {
		missing = append(missing, "knowledge base selections")
	}
	if len(missing) > 0 {
		return nil, &IncompleteError{Call: "strategy", Missing: missing}
	}
	return &resp, nil
}

// questionIDDigest lists the question ids and sources the strategy call must write its
// patterns against.
//
// Handing the ids over is what makes a generated pattern land. A pattern designed
// against imagined ids matches nothing, and a zero-match pattern fails silently: the run
// succeeds and the answers simply arrive without their context.
func questionIDDigest(questions []chat.BatchQuestion) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The %d question ids the extraction pass produced, which your domains patterns must match:\n\n", len(questions))

	// Grouped by source so the section structure is visible, which is what a domains
	// list is organised around.
	bySource := map[string][]string{}
	var order []string
	for _, q := range questions {
		src := q.Source
		if src == "" {
			src = "(no source)"
		}
		if _, seen := bySource[src]; !seen {
			order = append(order, src)
		}
		id := q.ID
		if id == "" {
			id = "(no id)"
		}
		bySource[src] = append(bySource[src], id)
	}
	for _, src := range order {
		ids := bySource[src]
		fmt.Fprintf(&b, "- %s: %s\n", src, strings.Join(ids, ", "))
	}
	return b.String()
}

// dropEmptyQuestions removes entries with no question text, which the manifest reader
// would reject outright.
func dropEmptyQuestions(in []chat.BatchQuestion) []chat.BatchQuestion {
	out := make([]chat.BatchQuestion, 0, len(in))
	for _, q := range in {
		q.ID = strings.TrimSpace(q.ID)
		q.Question = strings.TrimSpace(q.Question)
		q.Source = strings.TrimSpace(q.Source)
		if q.Question == "" {
			continue
		}
		out = append(out, q)
	}
	return out
}

// ValidateManifest checks a generated manifest against what the run's own reader
// requires, so a manifest is written only after it has been validated.
func ValidateManifest(m *chat.BatchManifest) error {
	if m.Version == "" {
		return &IncompleteError{Call: "generation", Missing: []string{"a version"}}
	}
	if len(m.Questions) == 0 {
		return &IncompleteError{Call: "generation", Missing: []string{"any questions"}}
	}
	for i, q := range m.Questions {
		if strings.TrimSpace(q.Question) == "" {
			return fmt.Errorf("generated question %d has empty question text", i+1)
		}
	}
	// The same compilation a run performs, so a malformed domains block fails here
	// rather than when the operator starts a run.
	if _, err := chat.CompileDomains(m.Domains); err != nil {
		return fmt.Errorf("generated domains are invalid: %w", err)
	}
	if !PromptHasNoContextRule(m.Prompt) {
		return fmt.Errorf("the assembled prompt lost the no-context rule, which must never happen: this is a bug in prompt assembly, not in the model response")
	}
	return nil
}
