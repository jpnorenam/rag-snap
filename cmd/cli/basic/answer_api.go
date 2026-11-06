package basic

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/jpnorenam/rag-snap/internal/apiclient"
)

// batchManifestJSON mirrors the daemon's POST /1.0/answer/batch body. The CLI's
// chat.BatchManifest is YAML-tagged, so we re-shape it for the JSON API.
type batchManifestJSON struct {
	Version          string            `json:"version,omitempty"`
	Model            string            `json:"model,omitempty"`
	KnowledgeBases   []string          `json:"knowledge_bases,omitempty"`
	KapaSourceGroups []string          `json:"kapa_source_groups,omitempty"`
	Prompt           string            `json:"prompt,omitempty"`
	PromptRef        string            `json:"prompt_ref,omitempty"`
	Domains          []batchDomainJSON `json:"domains,omitempty"`
	Temperature      *float64          `json:"temperature,omitempty"`
	// Judge is false to skip judging; absent leaves it to the daemon's configuration.
	Judge     *bool               `json:"judge,omitempty"`
	Questions []batchQuestionJSON `json:"questions"`
}

type batchQuestionJSON struct {
	ID       string   `json:"id,omitempty"`
	Question string   `json:"question"`
	Source   string   `json:"source,omitempty"`
	Keywords []string `json:"keywords,omitempty"`
}

type batchDomainJSON struct {
	Match    string   `json:"match"`
	Context  string   `json:"context,omitempty"`
	Keywords []string `json:"keywords,omitempty"`
}

// batchManifestBody re-shapes a loaded manifest into the daemon's JSON request
// body. Every manifest field the run honours has to be carried here, or the
// daemon-backed path silently runs a different manifest than the direct path.
func batchManifestBody(manifest *chat.BatchManifest, temperature float64, noJudge bool) batchManifestJSON {
	questions := make([]batchQuestionJSON, len(manifest.Questions))
	for i, q := range manifest.Questions {
		questions[i] = batchQuestionJSON{
			ID:       q.ID,
			Question: q.Question,
			Source:   q.Source,
			Keywords: []string(q.Keywords),
		}
	}
	var domains []batchDomainJSON
	if len(manifest.Domains) > 0 {
		domains = make([]batchDomainJSON, len(manifest.Domains))
		for i, d := range manifest.Domains {
			domains[i] = batchDomainJSON{
				Match:    d.Match,
				Context:  d.Context,
				Keywords: []string(d.Keywords),
			}
		}
	}
	var judge *bool
	if noJudge {
		judge = new(bool)
	}
	return batchManifestJSON{
		Judge:            judge,
		Version:          manifest.Version,
		Model:            manifest.Model,
		KnowledgeBases:   manifest.KnowledgeBases,
		KapaSourceGroups: manifest.KapaSourceGroups,
		Prompt:           manifest.Prompt,
		PromptRef:        manifest.PromptRef,
		Domains:          domains,
		Temperature:      &temperature,
		Questions:        questions,
	}
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

// batchWarningsFrom decodes the warnings the daemon publishes in the operation
// metadata (for example kapa.ai requested but not configured).
func batchWarningsFrom(op *apiclient.Operation) []string {
	raw, _ := op.Metadata["warnings"].([]any)
	warnings := make([]string, 0, len(raw))
	for _, w := range raw {
		if s, ok := w.(string); ok {
			warnings = append(warnings, s)
		}
	}
	return warnings
}

// runBatchRemote posts a prepared manifest to the daemon, waits for the async
// operation with progress, and writes the structured results to the same
// timestamped JSON file the direct path produces.
func (cmd *answerCommand) runBatchRemote(dc *apiclient.Client, manifest *chat.BatchManifest, temperature float64, noJudge bool) error {
	fmt.Printf("Found %d questions in batch manifest version %s\n", len(manifest.Questions), manifest.Version)

	body := batchManifestBody(manifest, temperature, noJudge)

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
	// shown is the warnings printed live. The daemon groups a repeated problem
	// into one entry that grows as more questions hit it, so only an entry's
	// first sighting is printed here; the final grouped list follows the run.
	var shown []string
	judgeNoticed := false
	printResults := func(op *apiclient.Operation) {
		if warnings := batchWarningsFrom(op); len(shown) < len(warnings) {
			haltSpinner()
			for _, w := range warnings[len(shown):] {
				fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
				shown = append(shown, w)
			}
		}
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
		if reason := op.MetadataString("judge_unavailable"); reason != "" && !judgeNoticed {
			judgeNoticed = true
			haltSpinner()
			fmt.Printf("Answers will not be judged: %s\n(Pass --no-judge, or set judge.enabled=false, to stop this notice.)\n", reason)
		}
		if op.MetadataString("phase") == "judging" {
			label := fmt.Sprintf("Judging answers %d/%d", op.MetadataInt("judged_done"), op.MetadataInt("judged_total"))
			if spinning {
				updateSpinner(label)
			} else {
				updateSpinner, stopSpinner = common.StartUpdatableSpinner(label)
				spinning = true
			}
			return
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
	if final := batchWarningsFrom(op); !slices.Equal(final, shown) {
		fmt.Fprintln(os.Stderr, "Warnings for this run:")
		for _, w := range final {
			fmt.Fprintf(os.Stderr, "  %s\n", w)
		}
	}

	// The operation metadata carries the structured results on success.
	out := chat.BatchOutput{
		GeneratedAt: op.MetadataString("generated_at"),
		Model:       op.MetadataString("model"),
		Prompt:      op.MetadataString("prompt"),
		Judge:       judgeSummaryFrom(op),
		Results:     batchResultsFrom(op),
	}
	if len(out.Results) == 0 {
		return fmt.Errorf("all questions failed; no results to write")
	}

	filename := fmt.Sprintf("batch-results-%s.json", time.Now().Format("20060102-150405"))
	if err := chat.WriteBatchResults(filename, &out); err != nil {
		return fmt.Errorf("writing results file: %w", err)
	}
	if summary := chat.FormatJudgeSummary(&out); summary != "" {
		fmt.Printf("\n%s", summary)
	}
	fmt.Printf("\nResults saved to %s\n", filename)
	return nil
}

// judgeSummaryFrom decodes the judge summary the daemon publishes on a judged run.
// Returns nil when the run was not judged.
func judgeSummaryFrom(op *apiclient.Operation) *chat.JudgeSummary {
	raw, ok := op.Metadata["judge"]
	if !ok || raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var s chat.JudgeSummary
	if err := json.Unmarshal(b, &s); err != nil {
		return nil
	}
	return &s
}
