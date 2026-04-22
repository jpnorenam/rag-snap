package basic

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/processing"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/rfp"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/spf13/cobra"
)

// ExtractRFPCommand returns the 'extract-rfp' command that guides the user through
// extracting RFP/RFI questions from a document and writing a batch YAML manifest.
func ExtractRFPCommand(ctx *common.Context) *cobra.Command {
	var outputPath string
	var previewOnly bool

	cobraCmd := &cobra.Command{
		Use:   "extract-rfp <document-path>",
		Short: "Extract RFP questions from a document and generate a batch manifest",
		Long: "Parse a PDF, DOCX, XLSX, or CSV document to extract RFP/RFI questions\n" +
			"and produce a YAML manifest compatible with 'answer batch'.\n\n" +
			"The command guides you through format-specific parameters\n" +
			"(start page, sheet, column) and lets you review extracted questions\n" +
			"before saving the manifest.",
		Args:    cobra.ExactArgs(1),
		GroupID: groupID,
		RunE: func(_ *cobra.Command, args []string) error {
			docPath := args[0]

			if _, err := os.Stat(docPath); err != nil {
				return fmt.Errorf("cannot access file: %w", err)
			}

			format := rfp.DetectFormat(docPath, "")
			fmt.Printf("Detected format: %s  (%s)\n\n", strings.ToUpper(format), filepath.Base(docPath))

			if format == "unknown" {
				return fmt.Errorf("unsupported file type — supported formats: CSV, XLSX, PDF, DOCX")
			}

			// Compute the default output path before any prompts so it can be
			// used as the pre-filled value in the output path input.
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
				if tikaURL, extractErr = rfpTikaURL(ctx); extractErr == nil {
					questions, extractErr = rfpExtractXLSX(docPath, tikaURL)
				}

			case "pdf", "docx":
				var tikaURL string
				if tikaURL, extractErr = rfpTikaURL(ctx); extractErr == nil {
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

			// ── Confirm ──────────────────────────────────────────────────────
			var proceed bool
			if err := huh.NewForm(huh.NewGroup(
				huh.NewConfirm().
					Title(fmt.Sprintf("Proceed with %d extracted question(s)?", len(questions))).
					Affirmative("Yes, save manifest").
					Negative("No, cancel").
					Value(&proceed),
			)).Run(); err != nil || !proceed {
				fmt.Println("Extraction cancelled.")
				return nil
			}

			// ── Knowledge bases ───────────────────────────────────────────────
			kbs, err := rfpSelectKnowledgeBases(ctx)
			if err != nil {
				return err
			}

			// ── Output path ───────────────────────────────────────────────────
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
		},
	}

	cobraCmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output YAML manifest path (default: <document-name>-rfp.yaml)")
	cobraCmd.Flags().BoolVar(&previewOnly, "preview", false, "Preview extracted questions without saving the manifest")

	addDebugFlags(cobraCmd, ctx)

	return cobraCmd
}

// rfpTikaURL returns the Tika server URL by reading only the tika.http.* config keys.
// Unlike serverApiUrls it does not require chat or opensearch config to be present.
func rfpTikaURL(ctx *common.Context) (string, error) {
	host, err := getConfigString(ctx, confTikaHttpHost)
	if err != nil {
		return "", fmt.Errorf("Tika host not configured — run: rag set tika.http.host <host>")
	}
	port, err := getConfigString(ctx, confTikaHttpPort)
	if err != nil {
		return "", fmt.Errorf("Tika port not configured — run: rag set tika.http.port <port>")
	}
	tikaPath, _ := getConfigString(ctx, confTikaHttpPath) // optional
	tikaTLS := getConfigBool(ctx, confTikaHttpTLS, false)
	return buildServiceURL(host, port, tikaPath, tikaTLS), nil
}

// rfpOpenSearchURL returns the OpenSearch URL by reading only the knowledge.http.* config keys.
func rfpOpenSearchURL(ctx *common.Context) (string, error) {
	host, err := getConfigString(ctx, confOpenSearchHttpHost)
	if err != nil {
		return "", fmt.Errorf("OpenSearch host not configured")
	}
	port, err := getConfigString(ctx, confOpenSearchHttpPort)
	if err != nil {
		return "", fmt.Errorf("OpenSearch port not configured")
	}
	osTLS := getConfigBool(ctx, confOpenSearchHttpTLS, true)
	return buildServiceURL(host, port, "", osTLS), nil
}

// rfpSelectKnowledgeBases attempts to list knowledge bases from OpenSearch and
// presents a multi-select. Falls back to a plain text input if OpenSearch is
// not reachable or not configured.
func rfpSelectKnowledgeBases(ctx *common.Context) ([]string, error) {
	osURL, err := rfpOpenSearchURL(ctx)
	if err == nil {
		stop := common.StartProgressSpinner("Fetching knowledge bases")
		client, clientErr := knowledge.NewClient(osURL)
		var indexes []knowledge.IndexInfo
		if clientErr == nil {
			indexes, clientErr = client.ListIndexes(context.Background())
		}
		stop()

		if clientErr == nil && len(indexes) > 0 {
			return rfpKBMultiSelect(indexes)
		}
		// OpenSearch reachable but no KBs yet — fall through to text input
		if clientErr == nil {
			fmt.Println("No knowledge bases found. You can add them later by editing the manifest.")
		}
	}

	// Fallback: free-text input
	var kbInput string
	if err := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("Knowledge bases to include (comma-separated, leave blank to skip):").
			Placeholder("base1, base2").
			Value(&kbInput),
	)).Run(); err != nil {
		return nil, fmt.Errorf("knowledge base input: %w", err)
	}

	var kbs []string
	for _, kb := range strings.Split(kbInput, ",") {
		if kb = strings.TrimSpace(kb); kb != "" {
			kbs = append(kbs, kb)
		}
	}
	return kbs, nil
}

// rfpKBMultiSelect presents an interactive multi-select over available knowledge bases.
func rfpKBMultiSelect(indexes []knowledge.IndexInfo) ([]string, error) {
	options := make([]huh.Option[string], len(indexes))
	for i, idx := range indexes {
		name, _ := knowledge.KnowledgeBaseNameFromIndex(idx.Name)
		label := fmt.Sprintf("%s  (%s docs, %s)", name, idx.DocsCount, idx.StoreSize)
		options[i] = huh.NewOption(label, name)
	}

	var selected []string
	if err := huh.NewForm(huh.NewGroup(
		huh.NewMultiSelect[string]().
			Title("Select knowledge bases (Space to toggle, Enter to confirm):").
			Options(options...).
			Value(&selected),
	)).Run(); err != nil {
		return nil, fmt.Errorf("knowledge base selection cancelled: %w", err)
	}
	return selected, nil
}

// rfpExtractCSV guides the user through column selection and extracts questions from a CSV.
func rfpExtractCSV(filePath string) ([]rfp.Question, error) {
	headers, err := rfp.CSVHeaders(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading CSV headers: %w", err)
	}
	if len(headers) == 0 {
		return nil, fmt.Errorf("CSV file has no columns")
	}

	// Single-column CSV: skip the prompt.
	if len(headers) == 1 {
		fmt.Printf("Single column detected: %q\n", headers[0])
		return rfp.ExtractFromCSV(filePath, 0)
	}

	options := make([]huh.Option[int], len(headers))
	for i, h := range headers {
		label := h
		if label == "" {
			label = fmt.Sprintf("Column %d", i+1)
		} else {
			label = fmt.Sprintf("Column %d: %s", i+1, h)
		}
		options[i] = huh.NewOption(label, i)
	}

	var colIdx int
	if err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[int]().
			Title("Which column contains the RFP questions?").
			Options(options...).
			Value(&colIdx),
	)).Run(); err != nil {
		return nil, fmt.Errorf("column selection cancelled: %w", err)
	}

	stop := common.StartProgressSpinner("Extracting questions from CSV")
	questions, err := rfp.ExtractFromCSV(filePath, colIdx)
	stop()
	return questions, err
}

// rfpExtractXLSX guides the user through sheet and column selection, supporting
// multiple sheets. Each question is tagged with the sheet name as its source.
func rfpExtractXLSX(filePath, tikaURL string) ([]rfp.Question, error) {
	stop := common.StartProgressSpinner("Parsing XLSX via Tika")
	tikaClient, err := processing.NewTikaClient(tikaURL)
	if err != nil {
		stop()
		return nil, fmt.Errorf("creating Tika client: %w", err)
	}
	htmlContent, err := tikaClient.ExtractHTML(filePath)
	stop()
	if err != nil {
		return nil, fmt.Errorf("Tika HTML extraction failed: %w\n"+
			"Ensure the Tika service is running ('rag status')", err)
	}

	sheets := rfp.ParseTikaHTMLSheets(htmlContent)
	if len(sheets) == 0 {
		return nil, fmt.Errorf("no tables found in document — ensure the XLSX contains a structured table")
	}

	// Overwrite Tika-derived sheet names with the accurate names from the XLSX ZIP.
	// Tika's HTML output can truncate or mangle long sheet names.
	if names, err := rfp.XLSXSheetNames(filePath); err == nil {
		for i := range sheets {
			if i < len(names) {
				sheets[i].Name = names[i]
			}
		}
	}

	// ── Sheet multi-select ────────────────────────────────────────────────────
	var selectedIndices []int
	if len(sheets) == 1 {
		fmt.Printf("Single sheet found: %q\n", sheets[0].Name)
		selectedIndices = []int{0}
	} else {
		sheetOptions := make([]huh.Option[int], len(sheets))
		for i, s := range sheets {
			label := s.Name
			if len(s.Rows) > 0 && len(s.Rows[0]) > 0 {
				preview := s.Rows[0]
				if len(preview) > 3 {
					preview = preview[:3]
				}
				label = fmt.Sprintf("%s  (columns: %s)", s.Name, strings.Join(preview, " | "))
			}
			sheetOptions[i] = huh.NewOption(label, i)
		}
		if err := huh.NewForm(huh.NewGroup(
			huh.NewMultiSelect[int]().
				Title("Which sheets contain RFP questions? (Space to toggle, Enter to confirm):").
				Options(sheetOptions...).
				Value(&selectedIndices),
		)).Run(); err != nil {
			return nil, fmt.Errorf("sheet selection cancelled: %w", err)
		}
		if len(selectedIndices) == 0 {
			return nil, fmt.Errorf("no sheets selected")
		}
	}

	// ── Column selection (based on first selected sheet's headers) ────────────
	firstSheet := sheets[selectedIndices[0]]
	colIdx := 0
	if len(firstSheet.Rows) > 0 && len(firstSheet.Rows[0]) > 1 {
		headers := firstSheet.Rows[0]
		colOptions := make([]huh.Option[int], len(headers))
		for i, h := range headers {
			label := h
			if label == "" {
				label = fmt.Sprintf("Column %d", i+1)
			} else {
				label = fmt.Sprintf("Column %d: %s", i+1, h)
			}
			colOptions[i] = huh.NewOption(label, i)
		}
		if err := huh.NewForm(huh.NewGroup(
			huh.NewSelect[int]().
				Title("Which column contains the question text?").
				Options(colOptions...).
				Value(&colIdx),
		)).Run(); err != nil {
			return nil, fmt.Errorf("column selection cancelled: %w", err)
		}
	} else if len(firstSheet.Rows) > 0 {
		fmt.Printf("Single column detected: %q\n", firstSheet.Rows[0][0])
	}

	// ── Extract from all selected sheets, tagging each question with its source ─
	var allQuestions []rfp.Question
	globalSeq := 0
	for _, idx := range selectedIndices {
		sheet := sheets[idx]
		qs, extractErr := rfp.ExtractFromTable(sheet.Rows, colIdx)
		if extractErr != nil {
			fmt.Printf("  (skip %q: %v)\n", sheet.Name, extractErr)
			continue
		}
		for _, q := range qs {
			globalSeq++
			q.ID = fmt.Sprintf("%d", globalSeq)
			q.Source = sheet.Name
			allQuestions = append(allQuestions, q)
		}
	}
	return allQuestions, nil
}

// rfpExtractText guides the user through page selection and extracts questions
// from a PDF or DOCX file via Tika plain-text extraction.
func rfpExtractText(filePath, tikaURL string) ([]rfp.Question, error) {
	stop := common.StartProgressSpinner("Extracting text via Tika")
	tikaClient, err := processing.NewTikaClient(tikaURL)
	if err != nil {
		stop()
		return nil, fmt.Errorf("creating Tika client: %w", err)
	}
	rawText, err := tikaClient.Extract(filePath)
	stop()
	if err != nil {
		return nil, fmt.Errorf("Tika text extraction failed: %w\n"+
			"Ensure the Tika service is running ('rag status')", err)
	}

	pages := rfp.SplitTextPages(rawText)
	fmt.Printf("Document has %d page(s).\n\n", len(pages))

	startPage := 1
	if len(pages) > 1 {
		var startPageStr string
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().
				Title(fmt.Sprintf("Which page do the RFP questions start on? (1–%d, default 1):", len(pages))).
				Placeholder("1").
				Value(&startPageStr),
		)).Run(); err != nil {
			return nil, fmt.Errorf("page input cancelled: %w", err)
		}
		if n, parseErr := strconv.Atoi(strings.TrimSpace(startPageStr)); parseErr == nil {
			if n >= 1 && n <= len(pages) {
				startPage = n
			}
		}
	}

	relevant := strings.Join(pages[startPage-1:], "\n")
	questions := rfp.ExtractQuestionsFromText(relevant)
	return questions, nil
}

// rfpPrintPreview prints up to 5 extracted questions to stdout.
func rfpPrintPreview(questions []rfp.Question) {
	preview := questions
	if len(preview) > 5 {
		preview = preview[:5]
	}
	fmt.Printf("Extracted %d question(s). Preview (first %d):\n\n", len(questions), len(preview))
	for _, q := range preview {
		src := ""
		if q.Source != "" {
			src = fmt.Sprintf(" [%s]", q.Source)
		}
		text := q.Question
		if len(text) > 110 {
			text = text[:107] + "..."
		}
		fmt.Printf("  [%s]%s %s\n", q.ID, src, text)
	}
	if len(questions) > 5 {
		fmt.Printf("  ... and %d more.\n", len(questions)-5)
	}
	fmt.Println()
}
