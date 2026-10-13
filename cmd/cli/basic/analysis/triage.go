package analysis

import (
	"context"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// TriageSection is one section, sheet or part the triage found in the RFP.
type TriageSection struct {
	Name string `yaml:"name" json:"name"`
	// IDPrefix is the question-id prefix this section's requirements carry, when the
	// document uses one. It is the raw material for a domains pattern, so it is
	// asked for here rather than inferred later.
	IDPrefix string `yaml:"id_prefix,omitempty" json:"id_prefix,omitempty"`
	// EstimatedQuestions is this section's share of the total estimate.
	EstimatedQuestions int    `yaml:"estimated_questions,omitempty" json:"estimated_questions,omitempty"`
	Summary            string `yaml:"summary,omitempty" json:"summary,omitempty"`
}

// Triage is the cheap first pass: what the documents are, before any manifest is
// generated.
//
// It exists so the interview can be conditioned on the document's actual content —
// in particular so the OpenStack question is asked only when the RFP requires
// OpenStack — and so the operator gets an abandon point before the expensive call.
type Triage struct {
	Title  string `yaml:"title,omitempty" json:"title,omitempty"`
	Issuer string `yaml:"issuer,omitempty" json:"issuer,omitempty"`

	Sections []TriageSection `yaml:"sections,omitempty" json:"sections"`

	// EstimatedQuestions is the count generation is later measured against, which is
	// the only cheap detector for a generation call that quietly returned a fraction
	// of the RFP.
	EstimatedQuestions int `yaml:"estimated_questions" json:"estimated_questions"`

	// OpenStackInScope gates the OpenStack flavour interview question. Evidence is
	// requested alongside it so an operator can see why it was asked, or why not.
	OpenStackInScope  bool   `yaml:"openstack_in_scope" json:"openstack_in_scope"`
	OpenStackEvidence string `yaml:"openstack_evidence,omitempty" json:"openstack_evidence,omitempty"`

	// MultiFieldAnswers says each question expects more than one field to be filled
	// (a compliance column plus a remarks column, say), which changes what the
	// generated prompt has to instruct.
	MultiFieldAnswers bool   `yaml:"multi_field_answers" json:"multi_field_answers"`
	MultiFieldDetail  string `yaml:"multi_field_detail,omitempty" json:"multi_field_detail,omitempty"`

	// CompoundCells says a single cell or row packs several distinct questions
	// together, and Separator describes how they are delimited. The extraction pass is
	// told both, because a cell left whole produces one crammed answer that addresses
	// its first clause and neglects the rest.
	CompoundCells bool   `yaml:"compound_cells" json:"compound_cells"`
	Separator     string `yaml:"separator,omitempty" json:"separator,omitempty"`

	SuggestedKnowledgeBases   []string `yaml:"suggested_knowledge_bases,omitempty" json:"suggested_knowledge_bases"`
	SuggestedKapaSourceGroups []string `yaml:"suggested_kapa_source_groups,omitempty" json:"suggested_kapa_source_groups"`

	Notes string `yaml:"notes,omitempty" json:"notes,omitempty"`

	// Report and Model are filled in by rag-cli, not by the model: how each document
	// was conveyed, and which model produced this triage.
	Report []DocumentReport `yaml:"-" json:"report"`
	Model  string           `yaml:"-" json:"model"`
}

// triageSchema is the response schema for the triage call. It is kept shallow — one
// level of nesting — because schema dialects are a JSON Schema subset and deeply
// nested schemas may be rejected outright.
func triageSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title":  map[string]any{"type": "string", "description": "The RFP's title as stated in the document."},
			"issuer": map[string]any{"type": "string", "description": "The organisation issuing the RFP, as stated."},
			"sections": map[string]any{
				"type":        "array",
				"description": "Each section, sheet or part that groups requirements.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":                map[string]any{"type": "string"},
						"id_prefix":           map[string]any{"type": "string", "description": "The question-id prefix this section's rows use, if any (e.g. \"C\" for C1, C2)."},
						"estimated_questions": map[string]any{"type": "integer"},
						"summary":             map[string]any{"type": "string"},
					},
					"required": []any{"name"},
				},
			},
			"estimated_questions": map[string]any{
				"type":        "integer",
				"description": "Total ANSWERABLE QUESTIONS across the whole document, not the number of rows. A cell packing three questions counts as three. This number is compared against the later extraction to detect an incomplete one, so counting rows would hide a badly under-split extraction.",
			},
			"openstack_in_scope": map[string]any{
				"type":        "boolean",
				"description": "True only if the RFP requires OpenStack to be addressed.",
			},
			"openstack_evidence": map[string]any{"type": "string", "description": "Brief quote or reference supporting openstack_in_scope."},
			"multi_field_answers": map[string]any{
				"type":        "boolean",
				"description": "True if each question expects more than one field to be filled (for example a compliance status column and a separate remarks column).",
			},
			"multi_field_detail":           map[string]any{"type": "string", "description": "Name the fields each question expects, if more than one."},
			"suggested_knowledge_bases":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"suggested_kapa_source_groups": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"notes":                        map[string]any{"type": "string", "description": "Anything about the document that will matter when writing the prompt: length limits, evaluation criteria, terminology, the stated incumbent environment."},
		},
		"required": []any{"estimated_questions", "openstack_in_scope", "multi_field_answers"},
	}
}

const triageInstruction = `Read the attached documents and report what they are. Do NOT generate a manifest yet.

This is a triage pass. Its purpose is to tell a human operator what was found, and to gather the facts a later generation pass needs:

- Identify the RFP and who issued it.
- List every section, sheet or part that groups requirements, with the question-id prefix its rows use where the document uses one.
- Count the ANSWERABLE QUESTIONS, not the rows. A single cell or row frequently packs several distinct questions together, separated by bullet markers (_ - •), newlines, or a "?" mid-text, and each of those is one answerable question. A sheet of 15 rows whose cells each hold two or three questions has roughly 35-45 questions, not 15. Count as carefully as you can and do not round to a comfortable number: this count is compared against a later extraction to detect whether that extraction was cut short, so counting rows would make a badly under-split extraction look complete.
- Say whether cells pack several questions together, and describe the separator used, so the extraction pass knows to split them.
- Say whether the RFP requires OpenStack to be addressed, and give the evidence.
- Say whether each question expects more than one field to be filled, and name those fields.
- Suggest which of the catalogued knowledge bases and source groups are relevant. Choose only from the supplied catalogue.
- Note anything that will matter when writing the answering prompt: length or page limits, evaluation criteria, the stated incumbent environment, and the document's own terminology.`

// Triage runs the first phase over an already-conveyed document set.
func (c *Client) Triage(ctx context.Context, conv *Conveyance, cat *Catalogue) (*Triage, error) {
	system := Grounding() + "\n\n" + cat.Prompt()

	parts := append([]part{textPart(triageInstruction)}, documentParts(conv)...)

	data, err := c.complete(ctx, "triage", system, parts, "rfp_triage", triageSchema(), c.settings.ReasoningStrategy, triageMaxOutputTokens)
	if err != nil {
		return nil, err
	}

	var t Triage
	if err := yaml.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("parsing triage response: %w", err)
	}

	// Validated locally rather than trusted to the schema, because structured output
	// is honoured per model. An estimate of zero is the one field whose absence would
	// silently disable the later shortfall check, so it is treated as missing.
	var missing []string
	if t.EstimatedQuestions <= 0 {
		missing = append(missing, "a question-count estimate")
	}
	if len(missing) > 0 {
		return nil, &IncompleteError{Call: "triage", Missing: missing}
	}

	t.Report = conv.Report
	t.Model = c.settings.Model
	t.SuggestedKnowledgeBases = cat.FilterKnowledgeBases(t.SuggestedKnowledgeBases)
	t.SuggestedKapaSourceGroups = cat.FilterKapaGroups(t.SuggestedKapaSourceGroups)
	return &t, nil
}

// Summary renders the triage for a terminal, in the order an operator reads it.
func (t *Triage) Summary() string {
	var b strings.Builder
	if t.Title != "" {
		fmt.Fprintf(&b, "Document:   %s\n", t.Title)
	}
	if t.Issuer != "" {
		fmt.Fprintf(&b, "Issuer:     %s\n", t.Issuer)
	}
	fmt.Fprintf(&b, "Questions:  about %d\n", t.EstimatedQuestions)
	fmt.Fprintf(&b, "OpenStack:  %s\n", yesNo(t.OpenStackInScope))
	if t.OpenStackEvidence != "" {
		fmt.Fprintf(&b, "            %s\n", t.OpenStackEvidence)
	}
	fmt.Fprintf(&b, "Packed cells: %s", yesNo(t.CompoundCells))
	if t.Separator != "" {
		fmt.Fprintf(&b, " (separated by %s)", t.Separator)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "Multi-field: %s\n", yesNo(t.MultiFieldAnswers))
	if t.MultiFieldDetail != "" {
		fmt.Fprintf(&b, "            %s\n", t.MultiFieldDetail)
	}
	if len(t.Sections) > 0 {
		fmt.Fprintf(&b, "Sections:   %d\n", len(t.Sections))
		for _, s := range t.Sections {
			prefix := ""
			if s.IDPrefix != "" {
				prefix = fmt.Sprintf(" [%s*]", s.IDPrefix)
			}
			count := ""
			if s.EstimatedQuestions > 0 {
				count = fmt.Sprintf(" (~%d)", s.EstimatedQuestions)
			}
			fmt.Fprintf(&b, "  - %s%s%s\n", s.Name, prefix, count)
		}
	}
	if len(t.SuggestedKnowledgeBases) > 0 {
		fmt.Fprintf(&b, "Suggested KBs: %s\n", strings.Join(t.SuggestedKnowledgeBases, ", "))
	}
	if t.Notes != "" {
		fmt.Fprintf(&b, "Notes:      %s\n", t.Notes)
	}
	return b.String()
}

// ReportSummary renders the per-document conveyance report, so an operator can see
// which documents were read natively and which were converted before deciding to spend
// a generation call.
func (t *Triage) ReportSummary() string {
	var b strings.Builder
	for _, r := range t.Report {
		fmt.Fprintf(&b, "  %-28s %-6s %-10s %s\n", r.Filename, r.Format, r.Path, r.Detail)
	}
	return b.String()
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
