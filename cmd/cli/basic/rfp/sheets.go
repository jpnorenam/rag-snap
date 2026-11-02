package rfp

import (
	"encoding/csv"
	"fmt"
	"os"
)

// SheetsFromTikaHTML parses Tika's XHTML output into one SheetTable per table and,
// when xlsxPath names a readable workbook, replaces each table's label with the sheet
// name read from the workbook itself.
//
// The recovery matters because Tika truncates and mangles long sheet names, and a
// sheet name is routing information: it is what a domain pattern matches against for
// a question whose id is a bare sequence number. Pass an empty xlsxPath for a format
// that has no workbook to recover names from.
//
// This function is deliberately pure — no HTTP, no Tika client. Callers make the Tika
// call themselves and hand the result here, which keeps this package a parsing library
// usable from the CLI, the daemon and the analysis path without any of them differing
// in how a sheet gets its name.
func SheetsFromTikaHTML(htmlContent, xlsxPath string) []SheetTable {
	sheets := ParseTikaHTMLSheets(htmlContent)
	if xlsxPath == "" {
		return sheets
	}
	names, err := XLSXSheetNames(xlsxPath)
	if err != nil {
		return sheets
	}

	// Tika emits one <div class="page"> per sheet, but a single page can hold more
	// than one table. Index by page when the pages are distinguishable, and fall
	// back to table order when every table landed on page 0.
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
	return sheets
}

// SheetsFromCSV reads a CSV file into a single-table shape, so a CSV flows through
// every path that consumes SheetTable exactly as a spreadsheet sheet does.
func SheetsFromCSV(path string) ([]SheetTable, error) {
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
	return []SheetTable{{Name: "CSV", Rows: rows, PageIndex: 0}}, nil
}
