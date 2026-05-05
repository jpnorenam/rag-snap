// Package rfp extracts RFP/RFI questions from structured documents (PDF, DOCX, XLSX, CSV)
// and writes a YAML manifest compatible with 'answer batch'.
package rfp

import (
	"archive/zip"
	"bufio"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
	"gopkg.in/yaml.v3"
)

// Question is a single extracted RFP question, compatible with the 'answer batch' YAML format.
// Source is written when present (e.g. the sheet name for XLSX) and is ignored by 'answer batch'.
type Question struct {
	ID       string `yaml:"id"`
	Question string `yaml:"question"`
	Source   string `yaml:"source,omitempty"`
}

// Manifest is the output YAML manifest, readable by 'answer batch'.
type Manifest struct {
	Version        string     `yaml:"version"`
	KnowledgeBases []string   `yaml:"knowledge_bases,omitempty"`
	Questions      []Question `yaml:"questions"`
}

// SheetTable holds the name and parsed rows of one table extracted from Tika HTML.
// For XLSX files the Name is derived from the sheet label in the surrounding HTML.
// PageIndex is the 0-based index of the Tika "page" (Excel sheet / PDF page) that
// contains this table; used to correctly map sheet names when one page has multiple tables.
type SheetTable struct {
	Name      string     // sheet/tab name (falls back to "Table N" when undetectable)
	Rows      [][]string // rows × columns of cell text
	PageIndex int        // 0-based page/sheet index from Tika HTML
}

// DetectFormat returns the document format ("csv", "xlsx", "pdf", "docx", or "unknown")
// based on file extension. contentType (from Tika /meta) is used as fallback when
// the extension is not recognised.
func DetectFormat(filePath, contentType string) string {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".csv":
		return "csv"
	case ".xlsx", ".xls":
		return "xlsx"
	case ".pdf":
		return "pdf"
	case ".docx", ".doc":
		return "docx"
	}
	ct := strings.ToLower(contentType)
	switch {
	case strings.Contains(ct, "csv") || strings.Contains(ct, "comma-separated"):
		return "csv"
	case strings.Contains(ct, "spreadsheet") || strings.Contains(ct, "excel"):
		return "xlsx"
	case strings.Contains(ct, "pdf"):
		return "pdf"
	case strings.Contains(ct, "wordprocessing") || strings.Contains(ct, "msword"):
		return "docx"
	}
	return "unknown"
}

// CSVHeaders reads and returns the header row of a CSV file.
func CSVHeaders(filePath string) ([]string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.LazyQuotes = true

	row, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("reading CSV header: %w", err)
	}
	headers := make([]string, len(row))
	for i, h := range row {
		headers[i] = strings.TrimSpace(h)
	}
	return headers, nil
}

// ExtractFromCSV reads the specified column (0-indexed) of a CSV file as questions.
// The first row is treated as a header and skipped.
// idColIdx is the 0-based column index to use as the question ID; pass -1 to
// assign sequential IDs automatically.
func ExtractFromCSV(filePath string, columnIdx, idColIdx int) ([]Question, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.LazyQuotes = true
	r.FieldsPerRecord = -1

	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("reading CSV: %w", err)
	}
	if len(records) <= 1 {
		return nil, fmt.Errorf("CSV file has no data rows")
	}

	var questions []Question
	seq := 0
	for _, row := range records[1:] {
		if columnIdx >= len(row) {
			continue
		}
		text := strings.TrimSpace(row[columnIdx])
		if text == "" {
			continue
		}
		id := ""
		if idColIdx >= 0 && idColIdx < len(row) {
			id = strings.TrimSpace(row[idColIdx])
		}
		if id == "" {
			seq++
			id = fmt.Sprintf("%d", seq)
		}
		questions = append(questions, Question{
			ID:       id,
			Question: text,
		})
	}
	if len(questions) == 0 {
		return nil, fmt.Errorf("no questions found in column %d", columnIdx+1)
	}
	return questions, nil
}

// ParseTikaHTMLSheets parses Tika's XHTML output and returns one SheetTable per
// <table> element found. The Name field is derived from the text node (typically a
// <p> or heading) that immediately precedes the table within the same parent — this
// is how Tika renders Excel sheet names. Falls back to "Table N" when no label is found.
func ParseTikaHTMLSheets(htmlContent string) []SheetTable {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return nil
	}
	var sheets []SheetTable
	tableNum := 0
	pageIdx := -1 // incremented to 0 on first <div class="page">

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "div" {
			for _, attr := range n.Attr {
				if attr.Key == "class" && attr.Val == "page" {
					pageIdx++
					break
				}
			}
		}
		if n.Type == html.ElementNode && n.Data == "table" {
			tableNum++
			if rows := tikaTableRows(n); len(rows) > 0 {
				pi := pageIdx
				if pi < 0 {
					pi = 0
				}
				sheets = append(sheets, SheetTable{
					Name:      sheetNameBefore(n, tableNum),
					Rows:      rows,
					PageIndex: pi,
				})
			}
			return // don't recurse into nested tables
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return sheets
}

// sheetNameBefore returns the trimmed text of the nearest non-empty preceding sibling
// (or the parent's preceding sibling) of tableNode — typically a <p> containing the
// sheet name in Tika's XLSX XHTML output. Falls back to "Table N".
func sheetNameBefore(tableNode *html.Node, tableNum int) string {
	for sib := tableNode.PrevSibling; sib != nil; sib = sib.PrevSibling {
		if text := strings.TrimSpace(tikaNodeText(sib)); text != "" {
			return text
		}
	}
	// Try one level up (table nested inside a div/page)
	if tableNode.Parent != nil {
		for sib := tableNode.Parent.PrevSibling; sib != nil; sib = sib.PrevSibling {
			if text := strings.TrimSpace(tikaNodeText(sib)); text != "" {
				return text
			}
		}
	}
	return fmt.Sprintf("Table %d", tableNum)
}

func tikaTableRows(tableNode *html.Node) [][]string {
	var rows [][]string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "tr" {
			if cells := tikaRowCells(n); len(cells) > 0 {
				rows = append(rows, cells)
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(tableNode)
	return rows
}

func tikaRowCells(tr *html.Node) []string {
	var cells []string
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
			text := tikaNodeText(c)
			text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
			cells = append(cells, text)
		}
	}
	return cells
}

func tikaNodeText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(tikaNodeText(c))
	}
	return sb.String()
}

// XLSXSheetNames reads sheet names directly from the XLSX ZIP archive (xl/workbook.xml),
// returning them in workbook order. This is more reliable than parsing Tika's HTML output,
// which can truncate or mangle long sheet names.
func XLSXSheetNames(path string) ([]string, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("opening xlsx: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name != "xl/workbook.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("opening workbook.xml: %w", err)
		}
		defer rc.Close()

		// Use token-based parsing so namespace handling is irrelevant.
		var names []string
		dec := xml.NewDecoder(rc)
		for {
			tok, err := dec.Token()
			if err != nil {
				break
			}
			se, ok := tok.(xml.StartElement)
			if ok && se.Name.Local == "sheet" {
				for _, attr := range se.Attr {
					if attr.Name.Local == "name" {
						names = append(names, attr.Value)
						break
					}
				}
			}
		}
		return names, nil
	}
	return nil, fmt.Errorf("xl/workbook.xml not found in xlsx")
}

// ExtractFromTable reads the specified column (0-indexed) from a parsed table.
// The first row is treated as a header and skipped. IDs and Source are left empty
// so the caller can assign global sequential IDs across multiple sheets.
// minLen skips cells shorter than that many characters; pass 0 to include all.
// ExtractFromTable extracts questions from the given column of a table.
// idColIdx is the 0-based column index to use as the question ID; pass -1 to
// leave ID empty (the caller assigns sequential IDs).
func ExtractFromTable(table [][]string, columnIdx, idColIdx, minLen int) ([]Question, error) {
	if len(table) <= 1 {
		return nil, fmt.Errorf("table has no data rows")
	}
	var questions []Question
	for _, row := range table[1:] {
		if columnIdx >= len(row) {
			continue
		}
		text := strings.TrimSpace(row[columnIdx])
		if text == "" {
			continue
		}
		if minLen > 0 && len(text) < minLen {
			continue
		}
		id := ""
		if idColIdx >= 0 && idColIdx < len(row) {
			id = strings.TrimSpace(row[idColIdx])
		}
		questions = append(questions, Question{ID: id, Question: text})
	}
	if len(questions) == 0 {
		return nil, fmt.Errorf("no questions found in column %d", columnIdx+1)
	}
	return questions, nil
}

// tocEntryRe matches typical table-of-contents lines: starts with a section number
// (including dotted subsections like "2.1" or "3.2.1"), has some text, and ends with
// a 1–4 digit page number (e.g. "1 Project Drivers 4", "2.1 Background 5").
var tocEntryRe = regexp.MustCompile(`^[\d.]+\s+.+\s+\d{1,4}$`)

// FilterTOCEntries removes questions that look like table-of-contents entries.
func FilterTOCEntries(questions []Question) []Question {
	var out []Question
	for _, q := range questions {
		if !tocEntryRe.MatchString(q.Question) {
			out = append(out, q)
		}
	}
	return out
}

// SplitTextPages splits Tika plain-text output into pages.
// Tika uses form-feed characters (\f, 0x0C) as page separators; empty pages are dropped.
func SplitTextPages(text string) []string {
	parts := strings.Split(text, "\f")
	var pages []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			pages = append(pages, p)
		}
	}
	if len(pages) == 0 {
		return []string{text}
	}
	return pages
}

// SplitHTMLPages extracts per-page plain text from Tika's XHTML output.
// Tika wraps each page in <div class="page"> in HTML mode, even for PDFs where
// plain-text extraction produces no form-feed separators.
// Falls back to a single entry containing the full document text when no page
// divs are found.
func SplitHTMLPages(htmlContent string) []string {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return []string{htmlContent}
	}

	var pages []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "div" {
			for _, attr := range n.Attr {
				if attr.Key == "class" && attr.Val == "page" {
					if text := strings.TrimSpace(tikaNodeText(n)); text != "" {
						pages = append(pages, text)
					}
					return // don't descend into nested page divs
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	if len(pages) == 0 {
		return []string{strings.TrimSpace(tikaNodeText(doc))}
	}
	return pages
}

// numBulletRe matches lines that begin a numbered or bulleted list item.
// Group 1 is the prefix (e.g. "3.4.1 ", "2) ", "• "); group 2 is the item text.
// Numeric prefixes (starting with a digit) are used as the question ID.
// Handles: "3.4.1 ", "1.1. ", "1. ", "2) ", "1 ", "• ", "- ", "* ", "Q. ", "Q: "
var numBulletRe = regexp.MustCompile(
	`^\s*(\d+(?:\.\d+)*\.?\s+|\d+[\.)\s]\s*|[•\-\*]\s+|Q[\.:\)]\s*)(.+)`,
)

// ExtractQuestionsFromText detects RFP questions in a block of plain text.
// It first attempts structured detection (numbered/bulleted lists), then falls
// back to collecting paragraphs that end with "?".
func ExtractQuestionsFromText(text string) []Question {
	if q := extractByPatterns(text); len(q) > 0 {
		return q
	}
	return extractByQuestionMark(text)
}

func extractByPatterns(text string) []Question {
	var questions []Question
	var cur strings.Builder
	seq := 0
	currentID := ""

	flush := func() {
		q := strings.TrimSpace(cur.String())
		cur.Reset()
		id := currentID
		currentID = ""
		if len(q) < 10 {
			return
		}
		if id == "" {
			seq++
			id = fmt.Sprintf("%d", seq)
		}
		questions = append(questions, Question{ID: id, Question: q})
	}

	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := scanner.Text()
		stripped := strings.TrimSpace(line)
		if stripped == "" {
			flush()
			continue
		}
		if m := numBulletRe.FindStringSubmatch(line); m != nil {
			flush()
			prefix := strings.TrimSpace(m[1])
			cur.WriteString(strings.TrimSpace(m[2]))
			// Use numeric prefixes (e.g. "3.4.1", "2)") as the question ID;
			// bullets and Q-prefix items keep sequential auto-numbering.
			if len(prefix) > 0 && prefix[0] >= '0' && prefix[0] <= '9' {
				currentID = strings.TrimRight(prefix, ".)")
			}
		} else if cur.Len() > 0 {
			cur.WriteByte(' ')
			cur.WriteString(stripped)
		}
	}
	flush()
	return questions
}

func extractByQuestionMark(text string) []Question {
	var questions []Question
	var cur strings.Builder
	seq := 0

	flush := func() {
		q := strings.TrimSpace(cur.String())
		cur.Reset()
		if len(q) < 10 {
			return
		}
		seq++
		questions = append(questions, Question{ID: fmt.Sprintf("%d", seq), Question: q})
	}

	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		stripped := strings.TrimSpace(scanner.Text())
		if stripped == "" {
			if strings.HasSuffix(strings.TrimSpace(cur.String()), "?") {
				flush()
			} else {
				cur.Reset()
			}
			continue
		}
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(stripped)
		if strings.HasSuffix(stripped, "?") {
			flush()
		}
	}
	return questions
}

// WriteManifest marshals manifest to YAML and writes it to path with 2-space indentation.
func WriteManifest(path string, m *Manifest) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating file: %w", err)
	}
	defer f.Close()

	header := fmt.Sprintf("# Generated by rag-cli answer batch --build on %s\n",
		time.Now().UTC().Format("2006-01-02 15:04:05 UTC"))
	if _, err := f.WriteString(header); err != nil {
		return fmt.Errorf("writing header: %w", err)
	}

	enc := yaml.NewEncoder(f)
	enc.SetIndent(2)
	if err := enc.Encode(m); err != nil {
		return fmt.Errorf("encoding YAML: %w", err)
	}
	return enc.Close()
}
