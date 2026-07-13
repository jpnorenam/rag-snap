package basic

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/jpnorenam/rag-snap/internal/apiclient"
)

// batchManifestJSON mirrors the daemon's POST /1.0/answer/batch body. The CLI's
// chat.BatchManifest is YAML-tagged, so we re-shape it for the JSON API.
type batchManifestJSON struct {
	Version        string              `json:"version,omitempty"`
	Model          string              `json:"model,omitempty"`
	KnowledgeBases []string            `json:"knowledge_bases,omitempty"`
	Prompt         string              `json:"prompt,omitempty"`
	Temperature    *float64            `json:"temperature,omitempty"`
	Questions      []batchQuestionJSON `json:"questions"`
}

type batchQuestionJSON struct {
	ID       string   `json:"id,omitempty"`
	Question string   `json:"question"`
	Keywords []string `json:"keywords,omitempty"`
}

// batchResultsFrom decodes the per-question results the daemon publishes in the
// operation metadata. Returns nil when no results are present yet.
func batchResultsFrom(op *apiclient.Operation) []chat.BatchResult {
	raw, ok := op.Metadata["results"]
	if !ok {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var results []chat.BatchResult
	if err := json.Unmarshal(b, &results); err != nil {
		return nil
	}
	return results
}

// runBatchRemote posts a prepared manifest to the daemon, waits for the async
// operation with progress, and writes the structured results to the same
// timestamped JSON file the direct path produces.
func (cmd *answerCommand) runBatchRemote(dc *apiclient.Client, manifest *chat.BatchManifest, temperature float64) error {
	fmt.Printf("Found %d questions in batch manifest version %s\n", len(manifest.Questions), manifest.Version)

	questions := make([]batchQuestionJSON, len(manifest.Questions))
	for i, q := range manifest.Questions {
		questions[i] = batchQuestionJSON{ID: q.ID, Question: q.Question, Keywords: []string(q.Keywords)}
	}
	temp := temperature
	body := batchManifestJSON{
		Version:        manifest.Version,
		Model:          manifest.Model,
		KnowledgeBases: manifest.KnowledgeBases,
		Prompt:         manifest.Prompt,
		Temperature:    &temp,
		Questions:      questions,
	}

	opURL, err := dc.AnswerBatch(context.Background(), body)
	if err != nil {
		return err
	}

	// Render each Q&A pair as the daemon publishes it, matching the direct-mode
	// `answer batch` output rather than a bare progress counter. Between answers
	// a spinner reports which question is being worked on, so the wait shows
	// activity like the direct path does instead of sitting blank.
	total := len(manifest.Questions)
	printed := 0
	var updateSpinner func(string)
	var stopSpinner func()
	spinning := false
	haltSpinner := func() {
		if spinning {
			stopSpinner()
			spinning = false
		}
	}
	showSpinner := func(done int) {
		label := fmt.Sprintf("Answering question %d/%d", min(done+1, total), total)
		if spinning {
			updateSpinner(label)
			return
		}
		updateSpinner, stopSpinner = common.StartUpdatableSpinner(label)
		spinning = true
	}
	printResults := func(op *apiclient.Operation) {
		results := batchResultsFrom(op)
		if printed < len(results) {
			// Stop the spinner before writing so its redraw does not garble the
			// Q&A lines, then resume it for the next in-flight question below.
			haltSpinner()
			for ; printed < len(results); printed++ {
				r := results[printed]
				fmt.Printf("[%d/%d] Question: %s\n", printed+1, total, r.Question)
				fmt.Printf("Answer: %s\n---\n", r.Answer)
			}
		}
		if done := op.MetadataInt("questions_done"); done < total {
			showSpinner(done)
		}
	}

	op, err := dc.WaitForOperation(context.Background(), opURL, apiclient.WaitOptions{
		OnProgress: printResults,
	})
	haltSpinner()
	if err != nil {
		return err
	}
	printResults(op) // flush any results only present in the terminal view
	haltSpinner()

	// The operation metadata carries the structured results on success.
	out := chat.BatchOutput{
		GeneratedAt: op.MetadataString("generated_at"),
		Model:       op.MetadataString("model"),
		Results:     batchResultsFrom(op),
	}
	if len(out.Results) == 0 {
		return fmt.Errorf("all questions failed; no results to write")
	}

	filename := fmt.Sprintf("batch-results-%s.json", time.Now().Format("20060102-150405"))
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling results: %w", err)
	}
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("writing results file: %w", err)
	}
	fmt.Printf("\nResults saved to %s\n", filename)
	return nil
}
