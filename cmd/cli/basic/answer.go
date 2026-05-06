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

	c := &cobra.Command{
		Use:   "batch [manifest.yaml]",
		Short: "Run questions from a YAML manifest and export results to JSON, or build a manifest from a document",
		Long: "Reads a YAML manifest defining a list of questions, runs each through the RAG+LLM pipeline, " +
			"and writes the results to a timestamped JSON file.\n\n" +
			"An optional top-level 'prompt' field in the manifest overrides the default system prompt for the entire batch.\n\n" +
			"Use --build <document> to extract RFP/RFI questions from a PDF, DOCX, XLSX, or CSV file and " +
			"generate a manifest without running the batch.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if buildDoc != "" {
				return cmd.runBuild(buildDoc, outputPath, previewOnly, noRefine)
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
			apiUrls, err := serverApiUrls(cmd.Context)
			if err != nil {
				return fmt.Errorf("getting server API URLs: %w", err)
			}
			knowledgeClient, _ := knowledge.NewClient(apiUrls[opensearch])
			embeddingModelID, _ := getConfigString(cmd.Context, knowledge.ConfEmbeddingModelID)
			return chat.ProcessBatchChat(apiUrls[openAi], knowledgeClient, embeddingModelID, manifest, chat.LoadPrompts(), cmd.Verbose)
		},
	}

	c.Flags().StringVar(&buildDoc, "build", "", "Document path (PDF, DOCX, XLSX, CSV) to extract RFP/RFI questions from and generate a batch manifest")
	c.Flags().StringVarP(&outputPath, "output", "o", "", "Output YAML manifest path (default: <document-name>-rfp.yaml) — used with --build")
	c.Flags().BoolVar(&previewOnly, "preview", false, "Preview extracted questions without saving the manifest — used with --build")
	c.Flags().BoolVar(&noRefine, "no-refine", false, "Skip LLM semantic refinement of extracted questions — used with --build")

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
	fmt.Printf("Run: rag-cli answer batch %s\n", outputPath)
	return nil
}
