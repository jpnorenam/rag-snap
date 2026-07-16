package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/processing"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/rfp"
)

// buildQuestionJSON is a single extracted candidate question published on the
// build operation's metadata. It mirrors rfp.Question (id/question/source),
// keeping the same field names the rfp package and the batch manifest use.
type buildQuestionJSON struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Source   string `json:"source,omitempty"`
}

// defaultMinQuestionCellLen matches the CLI's default minimum characters per
// table cell to count as a question. It is the default for the extract pass'
// min_length; the client may override it.
const defaultMinQuestionCellLen = 20

// maxColumnSamples is how many sample cell values per column the inspect pass
// returns so the client can preview and choose a column.
const maxColumnSamples = 3

// buildColumnJSON describes one column of a parsed table in the inspect
// response: its index, a few sample cell values, the average cell length, and
// whether the heuristic suggests it as the question column.
type buildColumnJSON struct {
	Index     int      `json:"index"`
	Sample    []string `json:"sample"`
	AvgLen    int      `json:"avg_len"`
	Suggested bool     `json:"suggested"`
}

// buildTableJSON describes one parsed table (a sheet, or the single CSV table).
type buildTableJSON struct {
	Name      string            `json:"name"`
	PageIndex int               `json:"page_index"`
	Header    []string          `json:"header"`
	RowCount  int               `json:"row_count"`
	Columns   []buildColumnJSON `json:"columns"`
}

// buildSuggestionJSON is the heuristic's default table/column selection.
type buildSuggestionJSON struct {
	TableIndex  int `json:"table_index"`
	ColumnIndex int `json:"column_index"`
}

// swagger:route POST /1.0/answer/build answer answerBuild
//
// Extract candidate questions from a document.
//
// Accepts an uploaded RFP/RFI document (PDF, DOCX, XLSX, CSV) as
// multipart/form-data. Free-text formats (PDF, DOCX) extract candidate
// questions in one pass and publish them on the operation metadata under
// "questions". Tabular formats (XLSX, CSV) do not extract on this pass — the
// question column cannot be reliably guessed — so the operation instead parses
// the document into tables and completes with a discriminated response:
// "needs_column": true, an opaque "build_token", the parsed "tables" (headers
// and per-column samples), and a heuristic "suggested" table/column. The client
// then calls POST /1.0/answer/build/extract for the chosen column. When the
// "refine" field is not "false", an LLM refinement pass is applied to the
// extracted questions. This endpoint never persists a manifest or runs a batch.
//
//	Consumes:
//	- multipart/form-data
//
//	Responses:
//	  202: asyncResponse
//	  400: errorResponse
//	  403: errorResponse
//	  500: errorResponse
func (s *Server) handleAnswerBuild(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(processing.MaxIngestFileSize); err != nil {
		respondError(w, http.StatusBadRequest, "parsing upload: "+err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("missing file upload field %q: %v", "file", err))
		return
	}
	defer file.Close()

	// refine defaults to true; only an explicit "false" disables it.
	refine := !strings.EqualFold(strings.TrimSpace(r.FormValue("refine")), "false")

	format := rfp.DetectFormat(header.Filename, header.Header.Get("Content-Type"))
	if format == "unknown" {
		respondError(w, http.StatusBadRequest, "unsupported file type — supported formats: CSV, XLSX, PDF, DOCX")
		return
	}

	tmp, err := os.CreateTemp("", "ragd-build-*"+filepath.Ext(header.Filename))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "staging upload: "+err.Error())
		return
	}
	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		_ = os.Remove(tmp.Name())
		respondError(w, http.StatusInternalServerError, "staging upload: "+err.Error())
		return
	}
	tmp.Close()
	stagedPath := tmp.Name()

	tikaURL := s.clients.tikaURL()
	inferenceURL := s.clients.openAIURL()
	model := s.clients.chatModelID()
	tabular := format == "xlsx" || format == "csv"

	op, err := s.ops.runTask(
		fmt.Sprintf("Extracting questions from %q", header.Filename),
		nil, true,
		func(ctx context.Context, op *Operation) error {
			defer func() { _ = os.Remove(stagedPath) }()

			if tabular {
				// Inspect pass: parse into tables and publish them for the
				// client to choose a column; extract nothing here.
				tables, err := parseTables(format, stagedPath, tikaURL)
				if err != nil {
					return err
				}
				if len(tables) == 0 {
					return fmt.Errorf("no tables found in document")
				}
				token, err := s.builds.stage(format, tables)
				if err != nil {
					return fmt.Errorf("staging parsed tables: %w", err)
				}
				tablesJSON, suggestion := inspectTables(tables)
				op.UpdateMetadata(map[string]any{
					"needs_column": true,
					"build_token":  token,
					"format":       format,
					"tables":       tablesJSON,
					"suggested":    suggestion,
				})
				return nil
			}

			// Free-text pass: extract in one shot (unchanged behavior).
			questions, err := extractTextQuestions(stagedPath, tikaURL)
			if err != nil {
				return err
			}
			if len(questions) == 0 {
				return fmt.Errorf("no questions could be extracted from the document")
			}
			refined := maybeRefine(ctx, &questions, refine, inferenceURL, model)
			op.UpdateMetadata(map[string]any{
				"questions": toBuildQuestionsJSON(questions),
				"count":     len(questions),
				"refined":   refined,
			})
			return nil
		},
	)
	if err != nil {
		_ = os.Remove(stagedPath)
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondAsync(w, op.url(), op.view())
}

// buildExtractRequest is the JSON body of POST /1.0/answer/build/extract.
type buildExtractRequest struct {
	BuildToken    string `json:"build_token"`
	TableIndex    int    `json:"table_index"`
	ColumnIndex   int    `json:"column_index"`
	IDColumnIndex *int   `json:"id_column_index,omitempty"`
	MinLength     *int   `json:"min_length,omitempty"`
	Refine        *bool  `json:"refine,omitempty"`
}

// swagger:route POST /1.0/answer/build/extract answer answerBuildExtract
//
// Extract questions from a chosen spreadsheet column.
//
// Extracts questions from one column of a table parsed and staged by a prior
// POST /1.0/answer/build call, identified by its build token. Does not
// re-upload or re-parse the document. Publishes the questions on the operation
// metadata in the same shape a free-text build does. An unknown/expired token,
// or an out-of-range table/column, is a 400; a column that yields no questions
// after the min-length filter fails the operation with a column-naming message.
//
//	Responses:
//	  202: asyncResponse
//	  400: errorResponse
//	  403: errorResponse
//	  500: errorResponse
func (s *Server) handleAnswerBuildExtract(w http.ResponseWriter, r *http.Request) {
	var req buildExtractRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	staged, ok := s.builds.get(req.BuildToken)
	if !ok {
		respondError(w, http.StatusBadRequest, "unknown or expired build token — re-upload the document")
		return
	}
	if req.TableIndex < 0 || req.TableIndex >= len(staged.tables) {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("table_index %d out of range", req.TableIndex))
		return
	}
	table := staged.tables[req.TableIndex]
	colCount := tableColumnCount(table)
	if req.ColumnIndex < 0 || req.ColumnIndex >= colCount {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("column_index %d out of range", req.ColumnIndex))
		return
	}

	idCol := -1
	if req.IDColumnIndex != nil {
		idCol = *req.IDColumnIndex
	}
	minLen := defaultMinQuestionCellLen
	if req.MinLength != nil {
		minLen = *req.MinLength
	}
	refine := true
	if req.Refine != nil {
		refine = *req.Refine
	}

	inferenceURL := s.clients.openAIURL()
	model := s.clients.chatModelID()
	token := req.BuildToken
	colIdx := req.ColumnIndex
	colLabel := columnLabel(table, colIdx)

	op, err := s.ops.runTask(
		fmt.Sprintf("Extracting questions from column %q", colLabel),
		nil, true,
		func(ctx context.Context, op *Operation) error {
			qs, extractErr := rfp.ExtractFromTable(table.Rows, colIdx, idCol, minLen)
			if extractErr != nil || len(qs) == 0 {
				return fmt.Errorf("no questions found in column %q — try a different column or a lower minimum length", colLabel)
			}
			// Assign sequential IDs and the table name as source.
			seq := 0
			for i := range qs {
				if qs[i].ID == "" {
					seq++
					qs[i].ID = fmt.Sprintf("%d", seq)
				}
				qs[i].Source = table.Name
			}
			refined := maybeRefine(ctx, &qs, refine, inferenceURL, model)
			// The token's purpose is served once a column has been extracted.
			s.builds.consume(token)
			op.UpdateMetadata(map[string]any{
				"questions": toBuildQuestionsJSON(qs),
				"count":     len(qs),
				"refined":   refined,
			})
			return nil
		},
	)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondAsync(w, op.url(), op.view())
}

// maybeRefine applies the best-effort LLM refinement pass in place when enabled
// and inference is configured; on any failure it keeps the raw questions
// (matching the CLI's rfpMaybeRefineQuestions). Returns whether refinement was
// attempted (for the "refined" metadata flag).
func maybeRefine(ctx context.Context, questions *[]rfp.Question, refine bool, inferenceURL, model string) bool {
	attempted := refine && inferenceURL != "" && model != ""
	if !attempted {
		return false
	}
	if err := ctx.Err(); err != nil {
		return attempted
	}
	if refined, _, err := chat.RefineQuestions(inferenceURL, model, *questions); err == nil && len(refined) > 0 {
		*questions = refined
	}
	return attempted
}

// toBuildQuestionsJSON converts rfp questions to the JSON shape published on
// the operation metadata.
func toBuildQuestionsJSON(questions []rfp.Question) []buildQuestionJSON {
	out := make([]buildQuestionJSON, len(questions))
	for i, q := range questions {
		out[i] = buildQuestionJSON{ID: q.ID, Question: q.Question, Source: q.Source}
	}
	return out
}

// parseTables parses a tabular document into rfp.SheetTable(s): Tika HTML for
// XLSX (one table per sheet, names recovered from the workbook), a single
// synthesized table for CSV.
func parseTables(format, path, tikaURL string) ([]rfp.SheetTable, error) {
	switch format {
	case "xlsx":
		if tikaURL == "" {
			return nil, fmt.Errorf("tika service is not configured — cannot read this document type")
		}
		tikaClient, err := processing.NewTikaClient(tikaURL)
		if err != nil {
			return nil, fmt.Errorf("creating Tika client: %w", err)
		}
		htmlContent, err := tikaClient.ExtractHTML(path)
		if err != nil {
			return nil, fmt.Errorf("tika extraction failed: %w", err)
		}
		sheets := rfp.ParseTikaHTMLSheets(htmlContent)
		if names, err := rfp.XLSXSheetNames(path); err == nil {
			usePage := false
			for _, sh := range sheets {
				if sh.PageIndex > 0 {
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
		return sheets, nil
	case "csv":
		return parseCSVTable(path)
	default:
		return nil, fmt.Errorf("not a tabular format: %s", format)
	}
}

// parseCSVTable reads a CSV file into a single-table shape so it flows through
// the same column-selection path as a spreadsheet sheet.
func parseCSVTable(path string) ([]rfp.SheetTable, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	rd := csv.NewReader(f)
	rd.LazyQuotes = true
	rd.FieldsPerRecord = -1
	rows, err := rd.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("reading CSV: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("CSV file is empty")
	}
	return []rfp.SheetTable{{Name: "CSV", Rows: rows, PageIndex: 0}}, nil
}

// inspectTables builds the client-facing table/column descriptors and the
// heuristic's suggested table+column. The header row is row 0; columns are
// scored to preselect the most likely question column.
func inspectTables(tables []rfp.SheetTable) ([]buildTableJSON, buildSuggestionJSON) {
	out := make([]buildTableJSON, len(tables))
	best := buildSuggestionJSON{TableIndex: 0, ColumnIndex: 0}
	bestScore := -1.0

	for ti, t := range tables {
		colCount := tableColumnCount(t)
		header := make([]string, colCount)
		for c := 0; c < colCount; c++ {
			header[c] = columnHeader(t, c)
		}
		suggestedCol := suggestQuestionColumn(t, colCount)
		cols := make([]buildColumnJSON, colCount)
		for c := 0; c < colCount; c++ {
			cols[c] = buildColumnJSON{
				Index:     c,
				Sample:    columnSamples(t, c, maxColumnSamples),
				AvgLen:    columnAvgLen(t, c),
				Suggested: c == suggestedCol,
			}
		}
		rowCount := len(t.Rows)
		if rowCount > 0 {
			rowCount-- // exclude the header row
		}
		out[ti] = buildTableJSON{
			Name:      t.Name,
			PageIndex: t.PageIndex,
			Header:    header,
			RowCount:  rowCount,
			Columns:   cols,
		}
		// Table-level score = the score of its best column; pick the best table.
		if s := columnScore(t, suggestedCol); s > bestScore {
			bestScore = s
			best = buildSuggestionJSON{TableIndex: ti, ColumnIndex: suggestedCol}
		}
	}
	return out, best
}

// tableColumnCount is the widest row's column count.
func tableColumnCount(t rfp.SheetTable) int {
	n := 0
	for _, row := range t.Rows {
		if len(row) > n {
			n = len(row)
		}
	}
	return n
}

// columnHeader returns the row-0 header for a column, or a synthesized "Column N"
// when row 0 has no value there (headerless sheets).
func columnHeader(t rfp.SheetTable, col int) string {
	if len(t.Rows) > 0 && col < len(t.Rows[0]) {
		if h := strings.TrimSpace(t.Rows[0][col]); h != "" {
			return h
		}
	}
	return fmt.Sprintf("Column %d", col+1)
}

// columnLabel is the header when present, else the synthesized column name.
func columnLabel(t rfp.SheetTable, col int) string {
	return columnHeader(t, col)
}

// columnSamples returns up to n non-empty data cells (skipping the header row),
// each truncated to a safe preview length. Returns a non-nil empty slice (never
// nil) so it marshals to a JSON array, not null — clients index it directly.
func columnSamples(t rfp.SheetTable, col, n int) []string {
	out := []string{}
	for _, row := range dataRows(t) {
		if col >= len(row) {
			continue
		}
		cell := strings.TrimSpace(row[col])
		if cell == "" {
			continue
		}
		if len(cell) > 80 {
			cell = cell[:77] + "..."
		}
		out = append(out, cell)
		if len(out) >= n {
			break
		}
	}
	return out
}

// dataRows returns the rows after the header row.
func dataRows(t rfp.SheetTable) [][]string {
	if len(t.Rows) <= 1 {
		return nil
	}
	return t.Rows[1:]
}

// columnAvgLen is the average trimmed length of a column's non-empty data cells.
func columnAvgLen(t rfp.SheetTable, col int) int {
	total, n := 0, 0
	for _, row := range dataRows(t) {
		if col >= len(row) {
			continue
		}
		cell := strings.TrimSpace(row[col])
		if cell == "" {
			continue
		}
		total += len([]rune(cell))
		n++
	}
	if n == 0 {
		return 0
	}
	return total / n
}

// extractTextQuestions extracts questions from a PDF/DOCX via Tika HTML in
// list/paragraph mode across all pages, then applies the TOC filter. Unchanged
// free-text path.
func extractTextQuestions(path, tikaURL string) ([]rfp.Question, error) {
	if tikaURL == "" {
		return nil, fmt.Errorf("tika service is not configured — cannot extract from this document type")
	}
	tikaClient, err := processing.NewTikaClient(tikaURL)
	if err != nil {
		return nil, fmt.Errorf("creating Tika client: %w", err)
	}
	htmlRaw, err := tikaClient.ExtractHTML(path)
	if err != nil {
		return nil, fmt.Errorf("tika extraction failed: %w", err)
	}
	pages := rfp.SplitHTMLPages(htmlRaw)
	text := strings.Join(pages, "\n")
	questions := rfp.ExtractQuestionsFromText(text)
	questions = rfp.FilterTOCEntries(questions)
	return questions, nil
}

// questionHeaderSynonyms are header tokens that suggest a question column.
var questionHeaderSynonyms = []string{
	"question", "requirement", "ask", "query", "description", "item", "control", "criteria",
}

// suggestQuestionColumn picks the most likely question column via columnScore,
// used only to preselect a default in the inspect response. Never runs
// extraction on its own.
func suggestQuestionColumn(t rfp.SheetTable, colCount int) int {
	best, bestScore := 0, -1.0
	for c := 0; c < colCount; c++ {
		if s := columnScore(t, c); s > bestScore {
			bestScore = s
			best = c
		}
	}
	return best
}

// columnScore rates how likely a column holds questions: header-synonym match,
// average cell length, and question-mark fraction add; an ID/numeric-looking
// column is penalized. Higher is better.
func columnScore(t rfp.SheetTable, col int) float64 {
	score := 0.0

	// Header synonym match.
	header := strings.ToLower(columnHeader(t, col))
	for _, syn := range questionHeaderSynonyms {
		if strings.Contains(header, syn) {
			score += 5
			break
		}
	}

	rows := dataRows(t)
	var nonEmpty, questionMarks, idLike, lenSum int
	for _, row := range rows {
		if col >= len(row) {
			continue
		}
		cell := strings.TrimSpace(row[col])
		if cell == "" {
			continue
		}
		nonEmpty++
		lenSum += len([]rune(cell))
		if strings.HasSuffix(cell, "?") {
			questionMarks++
		}
		if looksLikeID(cell) {
			idLike++
		}
	}
	if nonEmpty == 0 {
		return score // header-only signal
	}

	avgLen := float64(lenSum) / float64(nonEmpty)
	// Average length: prose columns score higher. Scale so a ~60-char column
	// contributes ~6; cap to avoid one giant cell dominating.
	score += minFloat(avgLen/10.0, 8)
	// Question-mark fraction.
	score += 4 * (float64(questionMarks) / float64(nonEmpty))
	// Penalize ID/number-like columns.
	score -= 6 * (float64(idLike) / float64(nonEmpty))

	return score
}

// looksLikeID reports whether a cell reads like an identifier rather than prose:
// short and numeric, or a section-number pattern like "Q1", "1.2", "3.4.1".
func looksLikeID(cell string) bool {
	if len(cell) > 12 {
		return false
	}
	allNumOrDot := true
	for _, r := range cell {
		if (r < '0' || r > '9') && r != '.' {
			allNumOrDot = false
			break
		}
	}
	if allNumOrDot {
		return true
	}
	// "Q" + digits (e.g. Q1, Q1.2).
	if (cell[0] == 'Q' || cell[0] == 'q') && len(cell) > 1 {
		rest := cell[1:]
		for _, r := range rest {
			if (r < '0' || r > '9') && r != '.' {
				return false
			}
		}
		return true
	}
	return false
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
