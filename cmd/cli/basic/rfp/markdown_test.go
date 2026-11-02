package rfp

import (
	"strings"
	"testing"
)

// complianceMatrix mirrors the shape of a real RFP compliance matrix: an id, the
// requirement itself, and the three columns beside it that carry the context an
// RFP-level prompt and a domain context are built from.
func complianceMatrix() []SheetTable {
	return []SheetTable{{
		Name: "C. Enhanced Platform Awareness",
		Rows: [][]string{
			{"Req ID", "Requirement", "Description", "Current Environment", "Evaluator Guidance"},
			{"C1", "SR-IOV", "Single root I/O virtualization for NFV data plane", "Enabled on compute nodes", "State supported/not supported and the mechanism"},
			{"C2", "CPU Pinning", "Dedicated cores for latency-sensitive VNFs", "In production", "Name the configuration surface"},
		},
	}}
}

func TestRenderSheetsMarkdownKeepsEveryColumnAndTheSheetName(t *testing.T) {
	out := RenderSheetsMarkdown(complianceMatrix())

	// The sheet name is routing information, not decoration: which sheet a
	// requirement came from is what a domain pattern matches on.
	if !strings.Contains(out, "C. Enhanced Platform Awareness") {
		t.Errorf("sheet name missing from output:\n%s", out)
	}

	// Every column heading must survive. These are precisely the columns the
	// single-column extraction path discards.
	for _, heading := range []string{"Req ID", "Requirement", "Description", "Current Environment", "Evaluator Guidance"} {
		if !strings.Contains(out, heading) {
			t.Errorf("column heading %q missing from output:\n%s", heading, out)
		}
	}

	// Every cell of every data row must survive too, not just the question column.
	for _, cell := range []string{
		"SR-IOV",
		"Single root I/O virtualization for NFV data plane",
		"Enabled on compute nodes",
		"State supported/not supported and the mechanism",
		"CPU Pinning",
		"Dedicated cores for latency-sensitive VNFs",
		"In production",
		"Name the configuration surface",
	} {
		if !strings.Contains(out, cell) {
			t.Errorf("cell %q missing from output:\n%s", cell, out)
		}
	}
}

func TestRenderSheetsMarkdownRendersEveryDataRow(t *testing.T) {
	sheets := []SheetTable{{Name: "S", Rows: [][]string{{"A", "B"}}}}
	for i := 0; i < 200; i++ {
		sheets[0].Rows = append(sheets[0].Rows, []string{"id", "requirement"})
	}

	out := RenderSheetsMarkdown(sheets)
	// Header + separator + 200 data rows. Truncating long sheets would silently
	// lose half an RFP, which is the failure this whole path exists to avoid.
	if got := strings.Count(out, "| id | requirement |"); got != 200 {
		t.Errorf("rendered %d data rows, want 200", got)
	}
}

func TestRenderSheetsMarkdownWidensHeaderRatherThanTruncatingRows(t *testing.T) {
	// A row with more cells than the header row happens in exported spreadsheets.
	// The extra cell must appear rather than being cut to the header's width.
	sheets := []SheetTable{{
		Name: "Ragged",
		Rows: [][]string{
			{"A", "B"},
			{"one", "two", "three"},
		},
	}}

	out := RenderSheetsMarkdown(sheets)
	if !strings.Contains(out, "three") {
		t.Errorf("cell beyond the header width was dropped:\n%s", out)
	}
	// The widened column still needs a name so its cells can be referred to.
	if !strings.Contains(out, "Column 3") {
		t.Errorf("widened column was not given a stable name:\n%s", out)
	}
}

func TestRenderSheetsMarkdownPadsShortRows(t *testing.T) {
	sheets := []SheetTable{{
		Name: "Short",
		Rows: [][]string{
			{"A", "B", "C"},
			{"only"},
		},
	}}

	out := RenderSheetsMarkdown(sheets)
	// A short row keeps its column alignment, so "only" is not read as belonging
	// to a different column than A.
	if !strings.Contains(out, "| only |  |  |") {
		t.Errorf("short row was not padded to the table width:\n%s", out)
	}
}

func TestRenderSheetsMarkdownEscapesPipesAndCollapsesNewlines(t *testing.T) {
	sheets := []SheetTable{{
		Name: "Awkward",
		Rows: [][]string{
			{"Requirement", "Notes"},
			{"A | B", "line one\nline two"},
		},
	}}

	out := RenderSheetsMarkdown(sheets)
	// An unescaped pipe would invent a column; a raw newline would invent a row.
	// Either corrupts the surrounding table, not merely the cell.
	if !strings.Contains(out, `A \| B`) {
		t.Errorf("pipe was not escaped:\n%s", out)
	}
	if strings.Contains(out, "line one\nline two") {
		t.Errorf("newline inside a cell was not collapsed:\n%s", out)
	}
	if !strings.Contains(out, "line one line two") {
		t.Errorf("cell content lost when collapsing the newline:\n%s", out)
	}
	// The row count is what proves the table survived: header, separator, one row.
	if got := strings.Count(out, "\n|"); got < 2 {
		t.Errorf("table structure looks broken:\n%s", out)
	}
}

func TestRenderSheetsMarkdownNamesUnlabelledColumns(t *testing.T) {
	sheets := []SheetTable{{
		Name: "Blanks",
		Rows: [][]string{
			{"Requirement", "", "Guidance"},
			{"SR-IOV", "x", "y"},
		},
	}}

	out := RenderSheetsMarkdown(sheets)
	if !strings.Contains(out, "Column 2") {
		t.Errorf("unlabelled column was not given a stable name:\n%s", out)
	}
}

func TestRenderSheetsMarkdownSkipsEmptyTablesAndLabelsUnnamedOnes(t *testing.T) {
	sheets := []SheetTable{
		{Name: "Empty", Rows: nil},
		{Name: "", Rows: [][]string{{"A"}, {"1"}}},
	}

	out := RenderSheetsMarkdown(sheets)
	if strings.Contains(out, "Empty") {
		t.Errorf("a table with no rows should not be rendered at all:\n%s", out)
	}
	if !strings.Contains(out, "(unnamed)") {
		t.Errorf("a table with no sheet name should still be labelled:\n%s", out)
	}
}

func TestRenderSheetsMarkdownSeparatesMultipleSheets(t *testing.T) {
	sheets := []SheetTable{
		{Name: "One", Rows: [][]string{{"A"}, {"1"}}},
		{Name: "Two", Rows: [][]string{{"B"}, {"2"}}},
	}

	out := RenderSheetsMarkdown(sheets)
	iOne := strings.Index(out, "## Sheet: One")
	iTwo := strings.Index(out, "## Sheet: Two")
	if iOne < 0 || iTwo < 0 {
		t.Fatalf("both sheets should be labelled:\n%s", out)
	}
	if iOne > iTwo {
		t.Error("sheets should be rendered in the order given")
	}
}
