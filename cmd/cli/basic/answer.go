package basic

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/rfp"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/spf13/cobra"
)

type answerCommand struct {
	*common.Context
}

func AnswerCommand(ctx *common.Context) *cobra.Command {
	var cmd answerCommand
	cmd.Context = ctx

	cobraCmd := &cobra.Command{
		Use:     "answer",
		Aliases: []string{"a"},
		Short:   "Answer questions using the knowledge base",
		Long:    "Run structured question-answering sessions grounded in your knowledge bases.",
		GroupID: groupID,
	}

	cobraCmd.AddCommand(cmd.batchCommand())

	addDebugFlags(cobraCmd, ctx)

	return cobraCmd
}

func (cmd *answerCommand) batchCommand() *cobra.Command {
	var buildDoc string
	var outputPath string
	var previewOnly bool
	var noRefine bool
	var temperature float64
	var analyze bool
	var contextDocs []string
	var checkPath string

	c := &cobra.Command{
		Use:   "batch [manifest.yaml]",
		Short: "Run questions from a YAML manifest and export results to JSON, or build a manifest from a document",
		Long: "Reads a YAML manifest defining a list of questions, runs each through the RAG+LLM pipeline, " +
			"and writes the results to a timestamped JSON file.\n\n" +
			"An optional top-level 'prompt' field in the manifest overrides the default system prompt for the entire batch.\n" +
			"Alternatively, 'prompt_ref: <name>' selects a stored answer_system_prompt variant (requires the ragd daemon; mutually exclusive with 'prompt').\n\n" +
			"Use --build <document> to extract RFP/RFI questions from a PDF, DOCX, XLSX, or CSV file and " +
			"generate a manifest without running the batch.\n\n" +
			"Add --analyze to that to hand the document to Google's Gemini API instead, which produces a " +
			"complete manifest: questions, knowledge bases, an RFP-level prompt, and a populated domains block. " +
			"Pass --context for supporting documents that add background. Analysis is off by default and sends " +
			"document content to Google under GEMINI_API_KEY; see 'rag get analysis.enabled'.\n\n" +
			"Use --check <manifest.yaml> to validate a manifest without running it: routing reachability, " +
			"unknown knowledge bases, duplicate ids, and whether the prompt still carries its rules.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if checkPath != "" {
				return cmd.runCheck(checkPath)
			}
			if buildDoc != "" {
				if analyze {
					return cmd.runAnalyze(buildDoc, contextDocs, outputPath)
				}
				return cmd.runBuild(buildDoc, outputPath, previewOnly, noRefine)
			}
			if analyze {
				return fmt.Errorf("--analyze applies to --build <document>; pass the document to analyse")
			}
			if len(contextDocs) > 0 {
				return fmt.Errorf("--context applies to --build <document> --analyze")
			}
			if len(args) == 0 {
				return fmt.Errorf("requires a manifest file argument, or use --build <document> to generate one")
			}
			manifest, err := chat.LoadBatchManifest(args[0])
			if err != nil {
				return fmt.Errorf("loading batch manifest: %w", err)
			}
			if manifest.Model == "" {
				manifest.Model, _ = getConfigString(cmd.Context, confChatModel)
			}

			// Prefer the daemon: it runs the batch as an async operation and
			// returns the structured results, which we write to the same
			// timestamped JSON file the direct path produces.
			if dc := daemonClient(cmd.Context); dc != nil {
				return cmd.runBatchRemote(dc, manifest, temperature)
			}

			// A prompt_ref names a stored variant, which only the daemon can
			// resolve. Fail clearly in direct mode rather than silently ignoring it.
			if manifest.PromptRef != "" {
				return fmt.Errorf("manifest 'prompt_ref' names a stored prompt variant, which requires the ragd daemon; start it and retry, or use an inline 'prompt' instead")
			}

			apiUrls, err := serverApiUrls(cmd.Context)
			if err != nil {
				return fmt.Errorf("getting server API URLs: %w", err)
			}
			// Every question is grounded on the knowledge bases, so an unusable
			// client must fail the run: otherwise each answer silently degrades to
			// the fixed no-context string without the LLM ever being asked.
			knowledgeClient, err := knowledge.NewClient(apiUrls[opensearch])
			if err != nil {
				return fmt.Errorf("knowledge base not available in direct mode (ragd not reachable): %w; "+
					"run with sudo or join the ragd socket group to use the daemon, or export OPENSEARCH_USERNAME/OPENSEARCH_PASSWORD", err)
			}
			embeddingModelID, err := getConfigString(cmd.Context, knowledge.ConfEmbeddingModelID)
			if err != nil || embeddingModelID == "" {
				return fmt.Errorf("embedding model ID is not configured (%s); run 'knowledge init' and set it", knowledge.ConfEmbeddingModelID)
			}
			kapaClient := buildKapaClient(cmd.Context)
			return chat.ProcessBatchChat(apiUrls[openAi], knowledgeClient, kapaClient, embeddingModelID, manifest, chat.LoadPrompts(), temperature, cmd.Verbose)
		},
	}

	c.Flags().StringVar(&buildDoc, "build", "", "Document path (PDF, DOCX, XLSX, CSV) to extract RFP/RFI questions from and generate a batch manifest")
	c.Flags().StringVarP(&outputPath, "output", "o", "", "Output YAML manifest path (default: <document-name>-rfp.yaml) — used with --build")
	c.Flags().BoolVar(&previewOnly, "preview", false, "Preview extracted questions without saving the manifest — used with --build")
	c.Flags().BoolVar(&noRefine, "no-refine", false, "Skip LLM semantic refinement of extracted questions — used with --build")
	c.Flags().BoolVar(&analyze, "analyze", false, "Generate a complete manifest (questions, prompt, knowledge bases, domains) with Gemini — used with --build")
	c.Flags().StringArrayVar(&contextDocs, "context", nil, "Supporting document that adds context, repeatable — used with --build --analyze")
	c.Flags().StringVar(&checkPath, "check", "", "Validate a manifest without running it, and exit non-zero if it carries defects")
	c.Flags().Float64Var(&temperature, "temperature", 0.1, "Sampling temperature (0.0–1.0); lower = more deterministic")

	return c
}

func (cmd *answerCommand) runBuild(docPath, outputPath string, previewOnly, noRefine bool) error {
	if _, err := os.Stat(docPath); err != nil {
		return fmt.Errorf("cannot access file: %w", err)
	}

	format := rfp.DetectFormat(docPath, "")
	fmt.Printf("Detected format: %s  (%s)\n\n", strings.ToUpper(format), filepath.Base(docPath))

	if format == "unknown" {
		return fmt.Errorf("unsupported file type — supported formats: CSV, XLSX, PDF, DOCX")
	}

	defaultOutput := strings.TrimSuffix(filepath.Base(docPath), filepath.Ext(docPath)) + "-rfp.yaml"
	if outputPath == "" {
		outputPath = defaultOutput
	}

	var (
		questions  []rfp.Question
		extractErr error
	)

	switch format {
	case "csv":
		questions, extractErr = rfpExtractCSV(docPath)

	case "xlsx":
		var tikaURL string
		if tikaURL, extractErr = rfpTikaURL(cmd.Context); extractErr == nil {
			questions, extractErr = rfpExtractXLSX(docPath, tikaURL)
		}

	case "pdf", "docx":
		var tikaURL string
		if tikaURL, extractErr = rfpTikaURL(cmd.Context); extractErr == nil {
			questions, extractErr = rfpExtractText(docPath, tikaURL)
		}
	}

	if extractErr != nil {
		return extractErr
	}
	if len(questions) == 0 {
		return fmt.Errorf("no questions could be extracted from the document")
	}

	rfpPrintPreview(questions)

	if previewOnly {
		fmt.Println("(Preview only — remove --preview to generate the manifest.)")
		return nil
	}

	var reviewErr error
	questions, reviewErr = rfpReviewQuestions(questions)
	if reviewErr != nil {
		return reviewErr
	}
	if len(questions) == 0 {
		fmt.Println("No questions selected — extraction cancelled.")
		return nil
	}

	if !noRefine {
		questions = rfpMaybeRefineQuestions(cmd.Context, questions)
		if len(questions) == 0 {
			fmt.Println("No questions remaining after refinement — extraction cancelled.")
			return nil
		}
	}

	kbs, err := rfpSelectKnowledgeBases(cmd.Context)
	if err != nil {
		return err
	}

	if err := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("Output manifest path:").
			Value(&outputPath),
	)).Run(); err != nil {
		return fmt.Errorf("output path input: %w", err)
	}
	if strings.TrimSpace(outputPath) == "" {
		outputPath = defaultOutput
	}

	manifest := &rfp.Manifest{
		Version:        "1.0",
		KnowledgeBases: kbs,
		Questions:      questions,
	}
	if err := rfp.WriteManifest(outputPath, manifest); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}

	fmt.Printf("\nManifest saved to %s  (%d questions)\n", outputPath, len(questions))
	fmt.Printf("Run: %s answer batch %s\n", cliCommand(), outputPath)
	return nil
}
