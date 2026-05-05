package basic

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/rfp"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
)

// rfpPrintRefineSummary prints a human-readable summary of what the LLM changed.
func rfpPrintRefineSummary(origLen int, results []chat.RefinedQuestion) {
	var clarified, exploded, unchanged int
	for _, r := range results {
		switch r.Action {
		case "clarified":
			clarified++
		case "exploded":
			exploded++
		default:
			unchanged++
		}
	}
	fmt.Printf("Refinement complete (%d → %d questions): %d clarified, %d exploded, %d unchanged.\n",
		origLen, len(results), clarified, exploded, unchanged)
}

// rfpMaybeRefineQuestions is the safe entry point for the LLM refinement step.
// It connects to the configured LLM, sends the reviewed questions for semantic
// improvement, shows a summary, and asks the user to accept or reject.
// On any error it prints a warning and returns the original questions unchanged.
func rfpMaybeRefineQuestions(ctx *common.Context, questions []rfp.Question) []rfp.Question {
	apiUrls, err := serverApiUrls(ctx)
	if err != nil {
		fmt.Printf("LLM not configured — skipping question refinement (%v).\n", err)
		return questions
	}

	model, _ := getConfigString(ctx, confChatModel)

	fmt.Printf("\nRefining %d question(s) with LLM...\n", len(questions))
	stopProgress := common.StartProgressSpinner("Calling LLM")

	refined, raw, refineErr := chat.RefineQuestions(apiUrls[openAi], model, questions)
	stopProgress()

	if refineErr != nil {
		fmt.Printf("Refinement failed: %v — keeping original questions.\n", refineErr)
		return questions
	}

	rfpPrintRefineSummary(len(questions), raw)

	var accept bool
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Accept refined questions?").
			Affirmative("Yes, use refined").
			Negative("No, keep originals").
			Value(&accept),
	)).Run(); err != nil {
		return questions
	}

	if accept {
		return refined
	}
	return questions
}
