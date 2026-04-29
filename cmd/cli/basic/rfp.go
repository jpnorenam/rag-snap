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

			// ── Question review ───────────────────────────────────────────────
			var reviewErr error
			questions, reviewErr = rfpReviewQuestions(questions)
			if reviewErr != nil {
				return reviewErr
			}
			if len(questions) == 0 {
				fmt.Println("No questions selected — extraction cancelled.")
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
// multiple sheets and multiple tables per sheet. Each question is tagged with
// the sheet name as its source.
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

	// Overwrite Tika-derived sheet names with accurate names from the XLSX ZIP.
	// Use PageIndex when Tika emitted <div class="page"> boundaries (detected by any
	// PageIndex > 0); otherwise fall back to table-index mapping for Tika versions
	// that don't emit page divs (in which case all PageIndex values are 0 and using
	// PageIndex would map every table to the first sheet name).
	if names, err := rfp.XLSXSheetNames(filePath); err == nil {
		usePage := false
		for _, s := range sheets {
			if s.PageIndex > 0 {
				usePage = true
				break
			}
		}
		for i := range sheets {
			idx := i
			if usePage {
				idx = sheets[i].PageIndex
			}
			if idx < len(names) {
				sheets[i].Name = names[idx]
			}
		}
	}

	return rfpExtractFromTables(sheets)
}

// rfpExtractFromTables handles the shared UI for table selection, column selection,
// min-length filtering, and question extraction. Used by both XLSX and PDF table mode.
func rfpExtractFromTables(sheets []rfp.SheetTable) ([]rfp.Question, error) {
	// ── Table multi-select ────────────────────────────────────────────────────
	var selectedIndices []int
	if len(sheets) == 1 {
		fmt.Printf("Single table found: %q\n", sheets[0].Name)
		selectedIndices = []int{0}
	} else {
		// Count tables per page so we can add "— Table N" suffixes when needed.
		pageTableCount := make(map[int]int)
		for _, s := range sheets {
			pageTableCount[s.PageIndex]++
		}
		pageTableSeq := make(map[int]int)

		sheetOptions := make([]huh.Option[int], len(sheets))
		for i, s := range sheets {
			pageTableSeq[s.PageIndex]++
			label := s.Name
			if pageTableCount[s.PageIndex] > 1 {
				label = fmt.Sprintf("%s — Table %d", s.Name, pageTableSeq[s.PageIndex])
			}
			if len(s.Rows) > 0 && len(s.Rows[0]) > 0 {
				preview := s.Rows[0]
				if len(preview) > 3 {
					preview = preview[:3]
				}
				label = fmt.Sprintf("%s  (columns: %s)", label, strings.Join(preview, " | "))
			}
			sheetOptions[i] = huh.NewOption(label, i)
		}
		if err := huh.NewForm(huh.NewGroup(
			huh.NewMultiSelect[int]().
				Title("Which tables contain RFP questions? (Space to toggle, Enter to confirm):").
				Options(sheetOptions...).
				Value(&selectedIndices),
		)).Run(); err != nil {
			return nil, fmt.Errorf("table selection cancelled: %w", err)
		}
		if len(selectedIndices) == 0 {
			return nil, fmt.Errorf("no tables selected")
		}
	}

	// ── Column selection (based on first selected table's headers) ────────────
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

	// ── Manual column override ────────────────────────────────────────────────
	// Lets the user force a specific column when headers are missing or mis-detected.
	var colOverride string
	if err := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title(fmt.Sprintf("Force column number? (current: %d — leave blank to keep, or enter e.g. 3 for column C):", colIdx+1)).
			Placeholder("").
			Value(&colOverride),
	)).Run(); err == nil {
		if n, parseErr := strconv.Atoi(strings.TrimSpace(colOverride)); parseErr == nil && n >= 1 {
			colIdx = n - 1
		}
	}

	// ── Min-length filter ─────────────────────────────────────────────────────
	var minLenStr string
	if err := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("Minimum characters per cell to include as a question? (0 = no filter, default 20):").
			Placeholder("20").
			Value(&minLenStr),
	)).Run(); err != nil {
		return nil, fmt.Errorf("min length input: %w", err)
	}
	minLen := 20
	if n, parseErr := strconv.Atoi(strings.TrimSpace(minLenStr)); parseErr == nil {
		minLen = n
	}

	// ── Extract from all selected tables, tagging each question with its source ─
	var allQuestions []rfp.Question
	globalSeq := 0
	for _, idx := range selectedIndices {
		sheet := sheets[idx]
		qs, extractErr := rfp.ExtractFromTable(sheet.Rows, colIdx, minLen)
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

// rfpExtractText guides the user through structure-mode selection and extracts
// questions from a PDF or DOCX file via Tika. Supports list/paragraph mode
// (plain-text extraction with optional TOC filter) and table mode (HTML extraction
// with column picker, reusing the XLSX table flow).
func rfpExtractText(filePath, tikaURL string) ([]rfp.Question, error) {
	tikaClient, err := processing.NewTikaClient(tikaURL)
	if err != nil {
		return nil, fmt.Errorf("creating Tika client: %w", err)
	}

	// ── Structure mode ────────────────────────────────────────────────────────
	var structureMode string
	if err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("How are questions structured in this document?").
			Options(
				huh.NewOption("Numbered / bulleted list or question marks", "list"),
				huh.NewOption("Table — extract from a column", "table"),
			).
			Value(&structureMode),
	)).Run(); err != nil {
		return nil, fmt.Errorf("structure mode selection cancelled: %w", err)
	}

	if structureMode == "table" {
		return rfpExtractTextTable(filePath, tikaClient)
	}

	// ── List / paragraph mode ─────────────────────────────────────────────────
	// Use HTML extraction so page boundaries come from <div class="page"> elements,
	// which Tika emits reliably even for PDFs that produce no \f separators in
	// plain-text mode.
	stop := common.StartProgressSpinner("Extracting text via Tika")
	htmlRaw, err := tikaClient.ExtractHTML(filePath)
	stop()
	if err != nil {
		return nil, fmt.Errorf("Tika extraction failed: %w\n"+
			"Ensure the Tika service is running ('rag status')", err)
	}

	pages := rfp.SplitHTMLPages(htmlRaw)
	fmt.Printf("Document has %d page(s).\n\n", len(pages))

	startPage := 1
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

	relevant := strings.Join(pages[startPage-1:], "\n")
	questions := rfp.ExtractQuestionsFromText(relevant)

	// ── TOC filter ────────────────────────────────────────────────────────────
	var filterTOC bool
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Apply table-of-contents filter? (removes entries like '1 Section Title 4')").
			Affirmative("Yes, filter TOC entries").
			Negative("No, keep all").
			Value(&filterTOC),
	)).Run(); err == nil && filterTOC {
		questions = rfp.FilterTOCEntries(questions)
	}

	return questions, nil
}

// rfpExtractTextTable handles PDF/DOCX table mode: Tika HTML extraction followed
// by the same table+column selection UI used for XLSX files.
func rfpExtractTextTable(filePath string, tikaClient *processing.TikaClient) ([]rfp.Question, error) {
	stop := common.StartProgressSpinner("Extracting HTML via Tika")
	htmlContent, err := tikaClient.ExtractHTML(filePath)
	stop()
	if err != nil {
		return nil, fmt.Errorf("Tika HTML extraction failed: %w\n"+
			"Ensure the Tika service is running ('rag status')", err)
	}

	sheets := rfp.ParseTikaHTMLSheets(htmlContent)
	if len(sheets) == 0 {
		return nil, fmt.Errorf("no HTML tables found in document\n" +
			"Tip: Tika may not recognise PDF tables as HTML <table> elements.\n" +
			"Re-run and choose 'Numbered / bulleted list or question marks' instead.")
	}

	maxPage := sheets[len(sheets)-1].PageIndex + 1
	fmt.Printf("Found %d table(s) across %d page(s).\n\n", len(sheets), maxPage)

	startPage := 0 // 0-based PageIndex threshold
	if maxPage > 1 {
		var startPageStr string
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().
				Title(fmt.Sprintf("Show tables starting from page (1–%d, default 1):", maxPage)).
				Placeholder("1").
				Value(&startPageStr),
		)).Run(); err != nil {
			return nil, fmt.Errorf("page input cancelled: %w", err)
		}
		if n, parseErr := strconv.Atoi(strings.TrimSpace(startPageStr)); parseErr == nil && n >= 1 {
			startPage = n - 1
		}
	}

	var filtered []rfp.SheetTable
	for _, s := range sheets {
		if s.PageIndex >= startPage {
			filtered = append(filtered, s)
		}
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no tables found on or after page %d", startPage+1)
	}

	return rfpExtractFromTables(filtered)
}

// rfpReviewQuestions presents all extracted questions in a scrollable multi-select
// with every item pre-checked. The user unchecks entries to exclude them.
// Returned questions are re-sequenced with consecutive IDs.
func rfpReviewQuestions(questions []rfp.Question) ([]rfp.Question, error) {
	allIndices := make([]int, len(questions))
	opts := make([]huh.Option[int], len(questions))
	for i, q := range questions {
		allIndices[i] = i
		label := fmt.Sprintf("[%s] %s", q.ID, q.Question)
		if q.Source != "" {
			label = fmt.Sprintf("[%s][%s] %s", q.ID, q.Source, q.Question)
		}
		if len(label) > 120 {
			label = label[:117] + "..."
		}
		opts[i] = huh.NewOption(label, i)
	}

	selected := allIndices
	if err := huh.NewForm(huh.NewGroup(
		huh.NewMultiSelect[int]().
			Title(fmt.Sprintf("Review %d question(s) — uncheck to remove (Space to toggle, Enter to confirm):", len(questions))).
			Options(opts...).
			Height(20).
			Value(&selected),
	)).Run(); err != nil {
		return nil, fmt.Errorf("review cancelled: %w", err)
	}

	kept := make([]rfp.Question, 0, len(selected))
	for seq, idx := range selected {
		q := questions[idx]
		q.ID = fmt.Sprintf("%d", seq+1)
		kept = append(kept, q)
	}
	return kept, nil
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
