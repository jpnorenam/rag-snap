package rfp

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
)

// HTMLToMarkdown converts Tika's XHTML prose to Markdown, preserving the heading
// structure and lists that carry a document's section layout.
//
// Tables are deliberately skipped: they are rendered separately by
// RenderSheetsMarkdown, which keeps their columns aligned. Emitting them here as
// flattened paragraphs would produce the very column loss that conversion exists to
// avoid, and would duplicate content besides.
//
// Headings are what make this worth doing rather than sending plain text. A section
// number and title is how an RFP names the part of itself a requirement belongs to,
// and that is exactly what a generated domain pattern has to key on.
func HTMLToMarkdown(htmlContent string) string {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return ""
	}

	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "table":
				return // rendered separately, with its columns intact
			case "head", "script", "style":
				return
			case "h1", "h2", "h3", "h4", "h5", "h6":
				level := int(n.Data[1] - '0')
				if text := cleanInline(tikaNodeText(n)); text != "" {
					fmt.Fprintf(&b, "\n%s %s\n\n", strings.Repeat("#", level), text)
				}
				return
			case "li":
				if text := cleanInline(tikaNodeText(n)); text != "" {
					fmt.Fprintf(&b, "- %s\n", text)
				}
				return
			case "p", "div":
				// Only emit a paragraph for a node that has no block-level
				// descendants of its own, so a wrapping div does not duplicate
				// the content of the blocks inside it.
				if !hasBlockChild(n) {
					if text := cleanInline(tikaNodeText(n)); text != "" {
						fmt.Fprintf(&b, "%s\n\n", text)
					}
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	return strings.TrimSpace(collapseBlankLines(b.String()))
}

// blockElements are the elements HTMLToMarkdown emits in their own right. A container
// holding any of them is traversed rather than flattened.
var blockElements = map[string]bool{
	"p": true, "div": true, "table": true, "li": true, "ul": true, "ol": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
}

func hasBlockChild(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && blockElements[c.Data] {
			return true
		}
		if c.Type == html.ElementNode && hasBlockChild(c) {
			return true
		}
	}
	return false
}

// cleanInline normalises whitespace within a run of text without escaping table
// separators, which only matter inside a table cell.
func cleanInline(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	return strings.TrimSpace(collapseSpaces(s))
}

// collapseBlankLines reduces runs of blank lines to one, so the heading and paragraph
// spacing above does not accumulate into pages of whitespace.
func collapseBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if blank {
				continue
			}
			blank = true
			out = append(out, "")
			continue
		}
		blank = false
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// RenderSheetsMarkdown renders parsed tables as Markdown, one labelled section per
// table, for conveyance to a document-capable model.
//
// This is the deliberate opposite of ExtractFromTable, which takes a single column by
// index. No model API accepts a spreadsheet or a word-processor document, and the
// formats that are accepted as files are read as pure text with markup discarded — so
// a table has to arrive as text whose structure survives being read as text, which is
// what a Markdown table is.
//
// Every row and every column is rendered. The columns beside a requirement — its
// description, the current-environment note, the evaluator guidance — are the material
// an RFP-level prompt and a domain context are built from, so dropping a column judged
// uninteresting would discard exactly what makes the output worth having. Each table
// keeps its sheet name, because which sheet a requirement came from is itself routing
// information.
func RenderSheetsMarkdown(sheets []SheetTable) string {
	var b strings.Builder
	for _, sheet := range sheets {
		if len(sheet.Rows) == 0 {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		name := strings.TrimSpace(sheet.Name)
		if name == "" {
			name = "(unnamed)"
		}
		fmt.Fprintf(&b, "## Sheet: %s\n\n", name)
		b.WriteString(renderTableMarkdown(sheet.Rows))
	}
	return b.String()
}

// renderTableMarkdown renders one table. Row 0 is treated as the header, matching
// ExtractFromTable and the rest of this package.
func renderTableMarkdown(rows [][]string) string {
	width := 0
	for _, row := range rows {
		if len(row) > width {
			width = len(row)
		}
	}
	if width == 0 {
		return ""
	}

	// The header is widened to the widest row rather than the reverse. A row with
	// more cells than the header row is a real occurrence in exported spreadsheets,
	// and truncating it to the header width would silently drop data.
	header := make([]string, width)
	for i := range header {
		cell := ""
		if i < len(rows[0]) {
			cell = cleanCell(rows[0][i])
		}
		if cell == "" {
			// An unlabelled column still needs a stable name, so a cell under it
			// can be referred to at all.
			cell = fmt.Sprintf("Column %d", i+1)
		}
		header[i] = cell
	}

	var b strings.Builder
	writeRow(&b, header)
	b.WriteString("|" + strings.Repeat(" --- |", width) + "\n")
	for _, row := range rows[1:] {
		cells := make([]string, width)
		for i := range cells {
			if i < len(row) {
				cells[i] = cleanCell(row[i])
			}
		}
		writeRow(&b, cells)
	}
	return b.String()
}

func writeRow(b *strings.Builder, cells []string) {
	b.WriteString("|")
	for _, c := range cells {
		b.WriteString(" ")
		b.WriteString(c)
		b.WriteString(" |")
	}
	b.WriteString("\n")
}

// cleanCell makes a cell safe to place inside a Markdown table cell without losing
// its content: pipes are escaped so they cannot be read as column separators, and
// line breaks collapse to spaces so one cell cannot become several rows. Both would
// otherwise corrupt the surrounding table rather than merely the cell.
func cleanCell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.TrimSpace(collapseSpaces(s))
}

// collapseSpaces reduces runs of whitespace to a single space, so a cell padded for
// display in a spreadsheet does not arrive as a wall of blanks.
func collapseSpaces(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	lastSpace := false
	for _, r := range s {
		if r == ' ' {
			if !lastSpace {
				b.WriteRune(r)
			}
			lastSpace = true
			continue
		}
		lastSpace = false
		b.WriteRune(r)
	}
	return b.String()
}
