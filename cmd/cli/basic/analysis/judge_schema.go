package analysis

import "github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"

// verdictSchema is the per-answer verdict.
//
// It uses only constructs Gemini's structured-output subset has accepted live: no
// maxItems, no maxLength. The schema is marshalled from a map, so its properties reach
// Gemini in alphabetical order — comment, confidence, issues — and the model writes
// what is wrong before it commits to a number. Rename a property only with that in
// mind. Everything is validated again locally in validateVerdict.
func verdictSchema() map[string]any {
	issues := make([]any, len(chat.JudgeIssues))
	for i, is := range chat.JudgeIssues {
		issues[i] = is
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"comment": map[string]any{
				"type": "string",
				"description": "What the reviewer must check or change: quote the offending phrase and say what to do. " +
					"Empty only when confidence is 5.",
			},
			"confidence": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     5,
				"description": "The anchored 1–5 score from the rubric.",
			},
			"issues": map[string]any{
				"type":        "array",
				"description": "Every category that applies. Empty when confidence is 5.",
				"items":       map[string]any{"type": "string", "enum": issues},
			},
		},
		"required": []any{"comment", "confidence", "issues"},
	}
}

// consistencySchema is the cross-answer findings list. Each row is quoted, not just
// numbered: a quote can be checked against the row's answer, and a model asked to quote
// every row cannot honestly name hundreds of them for one remark.
func consistencySchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"findings": map[string]any{
				"type":        "array",
				"description": "Every cross-answer problem found. Empty when the answers are consistent.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"comment": map[string]any{
							"type":        "string",
							"description": "What is inconsistent, quoting each row's conflicting phrase, and what the reviewer should check.",
						},
						"rows": map[string]any{
							"type":        "array",
							"description": "Each row involved, with the phrase from that row's answer that conflicts.",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"phrase": map[string]any{
										"type":        "string",
										"description": "The conflicting phrase, copied exactly from this row's answer.",
									},
									"row": map[string]any{
										"type":        "integer",
										"description": "The row number, exactly as given in the input.",
									},
								},
								"required": []any{"phrase", "row"},
							},
						},
					},
					"required": []any{"comment", "rows"},
				},
			},
		},
		"required": []any{"findings"},
	}
}
