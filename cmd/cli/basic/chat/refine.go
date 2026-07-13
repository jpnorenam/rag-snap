package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/rfp"
	"github.com/openai/openai-go/v3"
)

const refineSystemPrompt = `You are an RFP question refinement assistant. Your task is to improve a batch of extracted RFP questions for downstream LLM-based answer generation.

For each question, you will:
1. Resolve cross-references: If a question references another question (e.g., "Detail what was asked in 3.4.2"), inline or contextualize that reference within the question itself.
2. Explode compound questions: If a question implicitly contains multiple sub-questions (e.g., "Detail the supported architecture for: - storage - networking - databases"), break it into separate, discrete questions.
3. Clarify titles as context: If a title precedes a bulleted list, merge the title into the question stem to make the question self-contained.
4. Improve ordering: Reorder questions logically (e.g., prerequisites first) to aid comprehension.
5. Preserve metadata: Keep the original question ID associated with each refined question.

Input format:
[{"id":"1","text":"...","original_order":0}]

Output format (JSON only, no preamble):
[{"original_id":"1","refined_text":"...","new_order":0,"action":"unchanged"}]

Rules:
- If a question is exploded, derive new IDs by appending '.1', '.2', etc. to the original ID.
- Refined text should be a complete, self-contained question (no dangling pronouns or external references).
- action must be one of: "unchanged", "clarified", "exploded".
- Keep output as valid JSON; do not include markdown fences or explanatory text.`

type refineInput struct {
	ID            string `json:"id"`
	Text          string `json:"text"`
	OriginalOrder int    `json:"original_order"`
}

// RefinedQuestion is the per-item refinement result returned by RefineQuestions.
// Exported so callers can inspect Action for summary reporting.
type RefinedQuestion struct {
	OriginalID  string `json:"original_id"`
	RefinedText string `json:"refined_text"`
	NewOrder    int    `json:"new_order"`
	Action      string `json:"action"` // "unchanged" | "clarified" | "exploded"
}

// RefineQuestions sends extracted RFP questions to the configured LLM for
// semantic refinement (cross-reference resolution, compound splitting, title
// clarification). Returns the refined []rfp.Question and raw []RefinedQuestion
// for summary reporting. Returns an error on LLM or JSON parse failure —
// callers must fall back to the original questions.
func RefineQuestions(baseURL, model string, questions []rfp.Question) ([]rfp.Question, []RefinedQuestion, error) {
	input := make([]refineInput, len(questions))
	for i, q := range questions {
		input[i] = refineInput{ID: q.ID, Text: q.Question, OriginalOrder: i}
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling input: %w", err)
	}

	client := openai.NewClient(clientOptions(baseURL)...)

	resp, err := client.Chat.Completions.New(context.Background(), openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(refineSystemPrompt),
			openai.UserMessage(string(inputJSON)),
		},
		Model: model,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("LLM call failed: %w", err)
	}

	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
		return nil, nil, fmt.Errorf("empty response from LLM")
	}

	raw := strings.TrimSpace(StripThinkTags(resp.Choices[0].Message.Content))
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var refined []RefinedQuestion
	if err := json.Unmarshal([]byte(raw), &refined); err != nil {
		return nil, nil, fmt.Errorf("parsing refinement JSON: %w", err)
	}

	for i, r := range refined {
		if r.OriginalID == "" || r.RefinedText == "" {
			return nil, nil, fmt.Errorf("invalid refinement at index %d: missing original_id or refined_text", i)
		}
	}

	sort.Slice(refined, func(i, j int) bool {
		return refined[i].NewOrder < refined[j].NewOrder
	})

	sourceByID := make(map[string]string, len(questions))
	for _, q := range questions {
		sourceByID[q.ID] = q.Source
	}

	result := make([]rfp.Question, 0, len(refined))
	for _, r := range refined {
		source := sourceByID[r.OriginalID]
		if source == "" {
			// Exploded question: strip last ".N" to find the parent's source.
			if idx := strings.LastIndexByte(r.OriginalID, '.'); idx >= 0 {
				source = sourceByID[r.OriginalID[:idx]]
			}
		}
		result = append(result, rfp.Question{
			ID:       r.OriginalID,
			Question: r.RefinedText,
			Source:   source,
		})
	}

	return result, refined, nil
}
