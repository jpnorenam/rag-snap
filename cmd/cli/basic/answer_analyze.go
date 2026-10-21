package basic

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/analysis"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
)

// extractionFallbackHint is appended to every analysis failure. Analysis being
// unavailable is an expected state, not a fault, and the operator has a working
// alternative — saying so turns a dead end into a next step.
const extractionFallbackHint = "\n\nThe pattern-based extraction path is unaffected: drop --analyze to extract questions " +
	"from this document without the model, then fill in the prompt and domains by hand."

// runAnalyze is the two-phase LLM analysis flow: convey and triage, interview, generate.
//
// Every failure it can return is wrapped with extractionFallbackHint by the caller, so
// an operator whose analysis is unconfigured or whose provider is unreachable is told
// what still works rather than being left at a dead end.
func (cmd *answerCommand) runAnalyze(docPath string, contextPaths []string, outputPath string) error {
	if err := cmd.analyze(docPath, contextPaths, outputPath); err != nil {
		return fmt.Errorf("%w%s", err, extractionFallbackHint)
	}
	return nil
}

func (cmd *answerCommand) analyze(docPath string, contextPaths []string, outputPath string) error {
	if _, err := os.Stat(docPath); err != nil {
		return fmt.Errorf("cannot access file: %w", err)
	}
	for _, p := range contextPaths {
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("cannot access context file %s: %w", filepath.Base(p), err)
		}
	}

	settings, err := analysis.LoadSettings(cmd.Config)
	if err != nil {
		return err
	}
	if err := analysis.Validate(settings); err != nil {
		return err
	}

	client, err := analysis.NewClient(settings, cmd.Verbose)
	if err != nil {
		return err
	}

	knowledgeClient, catalogue, err := cmd.analysisCatalogue()
	if err != nil {
		return err
	}

	// Conveyance: the model's own document understanding for a PDF, table-preserving
	// Markdown for anything it cannot read.
	tikaURL, _ := rfpTikaURL(cmd.Context)
	converter := analysis.NewConverter(tikaURL)

	stop := common.StartProgressSpinner("Reading documents")
	conveyance, err := converter.ConveyAll(docPath, contextPaths)
	stop()
	if err != nil {
		return err
	}

	fmt.Printf("\nConveyed %d document(s) to the Gemini API (model %s):\n",
		len(conveyance.Documents), settings.Model)
	for _, r := range conveyance.Report {
		fmt.Printf("  %-30s %-6s %-10s %s\n", r.Filename, r.Format, r.Path, r.Detail)
	}
	fmt.Println()

	// Phase one.
	stop = common.StartProgressSpinner("Analysing (triage)")
	triage, err := client.Triage(context.Background(), conveyance, catalogue)
	stop()
	if err != nil {
		return err
	}

	fmt.Println(triage.Summary())

	if !common.ConfirmationPrompt("Generate a manifest from this document?") {
		fmt.Println("Cancelled — no manifest generated.")
		return nil
	}

	// The interview, conditioned on what triage found.
	interview, err := runInterview(settings, triage)
	if err != nil {
		return err
	}

	// Phase two, over the documents already conveyed. A shortfall is the operator's
	// decision rather than a dead end, so it loops: accept the produced questions,
	// re-run the extraction one effort level higher, or abandon.
	var (
		manifest *chat.BatchManifest
		opts     analysis.GenerateOptions
		accepted *analysis.ShortfallAcceptance
	)
	for {
		stop = common.StartProgressSpinner("Generating manifest")
		manifest, err = client.GenerateWith(context.Background(), conveyance, catalogue, triage, interview, opts)
		stop()
		sf, isShortfall := analysis.AsShortfall(err)
		if !isShortfall {
			break
		}
		// Acceptance must be an explicit choice, so without a terminal to ask on, the
		// shortfall stays an error.
		if !common.Interactive() {
			return err
		}
		fmt.Printf("\n%s\n\n", sf.Error())
		choice, choiceErr := shortfallDecision(sf)
		if choiceErr != nil {
			return choiceErr
		}
		switch choice {
		case shortfallAccept:
			opts = analysis.GenerateOptions{AcceptedQuestions: sf.Questions}
			accepted = &analysis.ShortfallAcceptance{Expected: sf.Expected, Produced: sf.Produced}
		case shortfallRerun:
			opts = analysis.GenerateOptions{QuestionsEffort: analysis.NextEffort(sf.Effort)}
			accepted = nil
		default:
			return fmt.Errorf("abandoned after a question-count shortfall; no manifest was written")
		}
	}
	if err != nil {
		return err
	}

	// The check closes the loop: a manifest written by a model that has never seen
	// rag-cli is held against what rag-cli will actually do with it.
	result := chat.CheckManifest(manifest, checkRefsFor(knowledgeClient, buildKapaClient(cmd.Context)))
	printManifestSummary(manifest, triage)
	printCheckResult(result)

	if outputPath == "" {
		outputPath = strings.TrimSuffix(filepath.Base(docPath), filepath.Ext(docPath)) + ".yaml"
	}
	if err := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Output manifest path:").Value(&outputPath),
	)).Run(); err != nil {
		return fmt.Errorf("output path input: %w", err)
	}
	if strings.TrimSpace(outputPath) == "" {
		return fmt.Errorf("no output path given")
	}

	// Written only after validation, and never over a previously valid file on failure:
	// Generate already validated, and WriteManifest creates the file only once it is
	// ready to encode.
	provenance := analysis.Provenance{Model: client.Model(), Documents: conveyance.Report, ShortfallAccepted: accepted}
	if err := analysis.WriteManifest(outputPath, manifest, provenance); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}

	fmt.Printf("\nManifest saved to %s\n", outputPath)
	fmt.Printf("Review the prompt and domain contexts, then run: %s answer batch %s\n", cliCommand(), outputPath)
	return nil
}

const (
	shortfallAbandon = "abandon"
	shortfallRerun   = "rerun"
	shortfallAccept  = "accept"
)

// shortfallDecision asks what to do about a shortfall. Abandoning is listed first, so the
// preselected answer costs nothing and accepts nothing: accepting fewer questions than
// estimated must be chosen, never defaulted into.
func shortfallDecision(sf *analysis.ShortfallError) (string, error) {
	options := []huh.Option[string]{
		huh.NewOption("Abandon — write no manifest", shortfallAbandon),
	}
	if next := analysis.NextEffort(sf.Effort); next != analysis.EffortUnset {
		options = append(options, huh.NewOption(
			fmt.Sprintf("Re-run the question extraction at reasoning %q (one more questions call)", next), shortfallRerun))
	}
	options = append(options, huh.NewOption(
		fmt.Sprintf("Continue with the %d questions produced", sf.Produced), shortfallAccept))

	choice := shortfallAbandon
	if err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Fewer questions than triage estimated. What now?").
			Options(options...).
			Value(&choice),
	)).Run(); err != nil {
		return "", fmt.Errorf("shortfall decision: %w", err)
	}
	return choice, nil
}

// runInterview asks the questions whose answers shape the manifest. The OpenStack
// question is asked only when triage found OpenStack in scope, which is the whole
// reason triage runs first.
func runInterview(settings analysis.Settings, triage *analysis.Triage) (analysis.Interview, error) {
	iv := analysis.DefaultInterview(settings)

	words := strconv.Itoa(iv.AnswerWords)
	limit := string(iv.AnswerLimit)

	fields := []huh.Field{
		huh.NewInput().
			Title("Desired answer length in words:").
			Description("Applies to every answer in the batch.").
			Value(&words).
			Validate(func(s string) error {
				n, err := strconv.Atoi(strings.TrimSpace(s))
				if err != nil || n <= 0 {
					return fmt.Errorf("enter a positive whole number of words")
				}
				return nil
			}),
		huh.NewSelect[string]().
			Title("Is that length a hard limit or a soft target?").
			Options(
				huh.NewOption("Soft target — may be slightly exceeded", string(analysis.LengthSoft)),
				huh.NewOption("Hard limit — must never be exceeded", string(analysis.LengthHard)),
			).
			Value(&limit),
	}

	flavour := string(analysis.OpenStackSunbeam)
	if triage != nil && triage.OpenStackInScope {
		fields = append(fields, huh.NewSelect[string]().
			Title("This RFP requires OpenStack. Which offering should answers present?").
			Description("Answers will describe this offering and will not present the other as the proposal.").
			Options(
				huh.NewOption("Canonical OpenStack with Sunbeam", string(analysis.OpenStackSunbeam)),
				huh.NewOption("Charmed OpenStack", string(analysis.OpenStackCharmed)),
			).
			Value(&flavour))
	}

	if err := huh.NewForm(huh.NewGroup(fields...)).Run(); err != nil {
		return analysis.Interview{}, fmt.Errorf("interview cancelled: %w", err)
	}

	n, err := strconv.Atoi(strings.TrimSpace(words))
	if err != nil || n <= 0 {
		return analysis.Interview{}, fmt.Errorf("invalid answer length %q", words)
	}
	iv.AnswerWords = n

	if iv.AnswerLimit, err = analysis.ParseLengthMode(limit); err != nil {
		return analysis.Interview{}, err
	}
	if triage != nil && triage.OpenStackInScope {
		if iv.OpenStack, err = analysis.ParseOpenStackFlavour(flavour); err != nil {
			return analysis.Interview{}, err
		}
	}
	return iv, nil
}

// runCheck validates a manifest without running it and without calling any model.
// It returns a distinct error when defects are found so the command exits non-zero.
func (cmd *answerCommand) runCheck(path string) error {
	manifest, err := chat.LoadBatchManifest(path)
	if err != nil {
		return fmt.Errorf("loading manifest: %w", err)
	}

	// The reference checks need the knowledge store. When it is unreachable they are
	// reported as not performed rather than as passing, so the check never offers false
	// reassurance about something it did not look at.
	var refs *chat.CheckRefs
	if kc, _, catErr := cmd.analysisCatalogue(); catErr == nil {
		refs = checkRefsFor(kc, buildKapaClient(cmd.Context))
	}

	result := chat.CheckManifest(manifest, refs)
	printCheckResult(result)

	if result.HasDefects() {
		return fmt.Errorf("%d defect(s) found in %s", len(result.Defects()), filepath.Base(path))
	}
	return nil
}

// analysisCatalogue reads the live retrieval catalogue, returning the knowledge client
// alongside it so callers can reuse the connection for the check's reference lookups.
func (cmd *answerCommand) analysisCatalogue() (*knowledge.OpenSearchClient, *analysis.Catalogue, error) {
	osURL, err := rfpOpenSearchURL(cmd.Context)
	if err != nil {
		return nil, nil, fmt.Errorf("resolving the knowledge store address: %w", err)
	}
	kc, err := knowledge.NewClient(osURL)
	if err != nil {
		return nil, nil, fmt.Errorf("reaching the knowledge store: %w", err)
	}
	cat, err := analysis.LoadCatalogue(context.Background(), kc, buildKapaClient(cmd.Context))
	if err != nil {
		return nil, nil, err
	}
	return kc, cat, nil
}

// checkRefsFor builds the existing-target lists the check resolves against. It returns
// nil when the knowledge store is unavailable, which the check reports as "not
// performed".
func checkRefsFor(kc *knowledge.OpenSearchClient, kapa *knowledge.KapaClient) *chat.CheckRefs {
	if kc == nil {
		return nil
	}
	cat, err := analysis.LoadCatalogue(context.Background(), kc, kapa)
	if err != nil {
		return nil
	}
	refs := &chat.CheckRefs{}
	for _, kb := range cat.KnowledgeBases {
		refs.KnowledgeBases = append(refs.KnowledgeBases, kb.Name)
	}
	// Kapa is only reported as checked when a client existed to consult. Marking it
	// checked with an empty list would flag every kapa group in a valid manifest.
	if kapa != nil {
		refs.KapaChecked = true
		for _, g := range cat.KapaGroups {
			refs.KapaGroups = append(refs.KapaGroups, g.ID, g.Name)
		}
	}
	return refs
}

// printManifestSummary shows what was generated, before the operator commits to it.
func printManifestSummary(m *chat.BatchManifest, triage *analysis.Triage) {
	fmt.Printf("\nGenerated manifest\n")
	fmt.Printf("  Questions:       %d", len(m.Questions))
	if triage != nil && triage.EstimatedQuestions > 0 {
		fmt.Printf(" (triage estimated about %d)", triage.EstimatedQuestions)
	}
	fmt.Println()
	fmt.Printf("  Knowledge bases: %s\n", strings.Join(m.KnowledgeBases, ", "))
	if len(m.KapaSourceGroups) > 0 {
		fmt.Printf("  Kapa groups:     %d\n", len(m.KapaSourceGroups))
	}
	fmt.Printf("  Domains:         %d\n", len(m.Domains))
	fmt.Printf("  Prompt:          %d words\n", len(strings.Fields(m.Prompt)))
}

// printCheckResult renders the findings, grouped by severity so a manifest carrying
// only warnings does not read as a failure.
func printCheckResult(r chat.CheckResult) {
	fmt.Printf("\nCheck: %d question(s), %d domain(s)\n", r.Questions, r.Domains)

	if len(r.DomainMatches) > 0 {
		fmt.Println("\n  Domain routing:")
		for _, dm := range r.DomainMatches {
			note := ""
			if dm.Count == 0 {
				note = "  <- matches nothing"
			}
			fmt.Printf("    %-24s %4d question(s)%s\n", dm.Match, dm.Count, note)
			// A single-question entry is shown with its question and the start of its
			// context, so a context attached to the wrong question is visible here.
			if q := dm.OnlyQuestion; q != nil {
				fmt.Printf("    %-24s      ↳ %s: %s\n", "", q.ID, q.Text)
				if dm.ContextPreview != "" {
					fmt.Printf("    %-24s        context: %s\n", "", dm.ContextPreview)
				}
			}
		}
	}

	// Each reference check reports separately whether it ran, so nothing implies a
	// check passed when it never happened.
	if !r.ReferencesChecked {
		fmt.Println("\n  Knowledge base references: NOT CHECKED (the knowledge store was unreachable)")
	} else if !r.KapaChecked {
		fmt.Println("\n  Kapa source group references: NOT CHECKED (kapa is not configured)")
	}

	if r.Clean() {
		fmt.Println("\n  No findings.")
		return
	}

	// Defects and warnings are labelled in words, not only by position or colour, so
	// the distinction survives a terminal without colour and a copy-paste into a ticket.
	if defects := r.Defects(); len(defects) > 0 {
		fmt.Printf("\n  %d defect(s) — these would fail a run or contribute nothing:\n", len(defects))
		for _, f := range defects {
			fmt.Printf("    DEFECT   %s\n", f.Message)
		}
	}
	if warnings := r.Warnings(); len(warnings) > 0 {
		fmt.Printf("\n  %d warning(s) — these run, but may not be what was intended:\n", len(warnings))
		for _, f := range warnings {
			fmt.Printf("    warning  %s\n", f.Message)
		}
	}
	fmt.Println()
}
