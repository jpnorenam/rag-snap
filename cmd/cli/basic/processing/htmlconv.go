package processing

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
)

// HTMLToMarkdown converts Tika's XHTML output to Markdown, preserving
// table structure, headings, paragraphs, and lists.
func HTMLToMarkdown(rawHTML string) (string, error) {
	doc, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return "", fmt.Errorf("parsing HTML: %w", err)
	}

	var buf strings.Builder
	walkNode(&buf, doc)

	return strings.TrimSpace(buf.String()), nil
}

// walkNode recursively traverses the HTML tree and writes Markdown to buf.
func walkNode(buf *strings.Builder, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		text := collapseWhitespace(n.Data)
		if text != "" {
			buf.WriteString(text)
		}
		return

	case html.ElementNode:
		switch n.Data {
		case "h1", "h2", "h3", "h4", "h5", "h6":
			level := int(n.Data[1] - '0')
			buf.WriteString("\n\n")
			buf.WriteString(strings.Repeat("#", level))
			buf.WriteString(" ")
			walkChildren(buf, n)
			buf.WriteString("\n\n")
			return

		case "p":
			buf.WriteString("\n\n")
			walkChildren(buf, n)
			buf.WriteString("\n\n")
			return

		case "table":
			buf.WriteString("\n\n")
			renderTable(buf, n)
			buf.WriteString("\n\n")
			return

		case "ul":
			buf.WriteString("\n")
			renderList(buf, n, false)
			buf.WriteString("\n")
			return

		case "ol":
			buf.WriteString("\n")
			renderList(buf, n, true)
			buf.WriteString("\n")
			return

		case "br":
			buf.WriteString("\n")
			walkChildren(buf, n)
			return

		case "thead", "tbody", "tfoot", "div", "span", "body", "html", "head":
			// Transparent wrappers — just process children
			walkChildren(buf, n)
			return

		default:
			walkChildren(buf, n)
			return
		}

	default:
		walkChildren(buf, n)
	}
}

// walkChildren visits each child node of n.
func walkChildren(buf *strings.Builder, n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkNode(buf, c)
	}
}

// renderTable converts a <table> element into a Markdown pipe table.
func renderTable(buf *strings.Builder, tableNode *html.Node) {
	rows := collectRows(tableNode)
	if len(rows) == 0 {
		return
	}

	// Determine max columns across all rows
	maxCols := 0
	for _, row := range rows {
		if len(row) > maxCols {
			maxCols = len(row)
		}
	}

	// Pad rows to uniform column count
	for i := range rows {
		for len(rows[i]) < maxCols {
			rows[i] = append(rows[i], "")
		}
	}

	// First row is the header
	writeTableRow(buf, rows[0])

	// Separator
	sep := make([]string, maxCols)
	for i := range sep {
		sep[i] = "---"
	}
	writeTableRow(buf, sep)

	// Data rows
	for _, row := range rows[1:] {
		writeTableRow(buf, row)
	}
}

// writeTableRow writes a single Markdown table row: | col1 | col2 | ...
func writeTableRow(buf *strings.Builder, cells []string) {
	buf.WriteString("|")
	for _, cell := range cells {
		buf.WriteString(" ")
		buf.WriteString(cell)
		buf.WriteString(" |")
	}
	buf.WriteString("\n")
}

// collectRows walks a <table> node and returns a 2D slice of cell text.
// It handles <thead>, <tbody>, <tfoot> wrappers and <th>/<td> cells.
func collectRows(tableNode *html.Node) [][]string {
	var rows [][]string
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "tr" {
			row := collectCells(n)
			rows = append(rows, row)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(tableNode)
	return rows
}

// collectCells extracts text from <td> and <th> elements within a <tr>.
func collectCells(tr *html.Node) []string {
	var cells []string
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
			text := nodeText(c)
			// Replace newlines within cells with spaces
			text = strings.ReplaceAll(text, "\n", " ")
			text = strings.TrimSpace(text)
			cells = append(cells, text)
		}
	}
	return cells
}

// nodeText recursively extracts all text content from a node.
func nodeText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var buf strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		buf.WriteString(nodeText(c))
	}
	return buf.String()
}

// renderList converts <ul> or <ol> to Markdown list items.
func renderList(buf *strings.Builder, listNode *html.Node, ordered bool) {
	idx := 1
	for c := listNode.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "li" {
			text := strings.TrimSpace(nodeText(c))
			text = strings.ReplaceAll(text, "\n", " ")
			if ordered {
				buf.WriteString(fmt.Sprintf("%d. %s\n", idx, text))
				idx++
			} else {
				buf.WriteString("- " + text + "\n")
			}
		}
	}
}

// collapseWhitespace replaces runs of whitespace with a single space.
func collapseWhitespace(s string) string {
	var buf strings.Builder
	inSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !inSpace {
				buf.WriteRune(' ')
				inSpace = true
			}
		} else {
			buf.WriteRune(r)
			inSpace = false
		}
	}
	return buf.String()
}
