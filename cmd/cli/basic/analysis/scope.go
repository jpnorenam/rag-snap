package analysis

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/rfp"
)

// SheetScopeValue is what analysis does with one sheet of a spreadsheet RFP.
type SheetScopeValue string

// The three things a sheet can be: questions are extracted from it, it is background
// only, or it is not sent at all.
const (
	ScopeAnswer  SheetScopeValue = "answer"
	ScopeContext SheetScopeValue = "context"
	ScopeExclude SheetScopeValue = "exclude"
)

// SheetScope is an operator's requested scope, keyed by sheet name or 1-based position.
// Empty means no scope: every sheet is answered, which is the behaviour without one.
type SheetScope map[string]SheetScopeValue

// SheetInfo is one sheet of a spreadsheet as the operator sees it: its position in
// workbook order, which is what Excel's tab order shows, its name and its data rows.
// Scope is set once a scope has been resolved against the sheets.
type SheetInfo struct {
	Position int             `json:"position"`
	Name     string          `json:"name"`
	DataRows int             `json:"data_rows"`
	Scope    SheetScopeValue `json:"scope,omitempty"`
}

// contextOnlyLabel marks a context-only sheet's heading in the conveyed text. rag-cli
// writes it, not the document, so it cannot be used to claim instruction authority —
// it only narrows what is extracted.
const contextOnlyLabel = " (context only — background, no questions to extract)"

// contextSheetsNote is added to the triage and extraction instructions when the
// conveyed RFP carries context-only sheets.
const contextSheetsNote = "\n\nSheets whose heading is marked \"context only\" are background the operator chose to include. Do not count or extract any question from them; use them only to understand the sheets that are not so marked."

// contextSheetsStrategyNote is the generation-call counterpart: context-only sheets may
// inform the prompt and the domain contexts, which is why they are conveyed at all.
const contextSheetsStrategyNote = "\n\nSheets whose heading is marked \"context only\" hold no questions, but they are background the operator chose to include: use them to inform the prompt and the domain contexts."

// SheetScopeError reports a scope that cannot be applied. Its message lists the sheets
// when a sheet was not found, so the operator can correct the scope without opening the
// workbook.
type SheetScopeError struct {
	Problem string
	Sheets  []SheetInfo
}

func (e *SheetScopeError) Error() string {
	if len(e.Sheets) == 0 {
		return e.Problem
	}
	var b strings.Builder
	b.WriteString(e.Problem)
	b.WriteString("; the sheets are:")
	for _, s := range e.Sheets {
		fmt.Fprintf(&b, "\n  %d  %s", s.Position, s.Name)
	}
	return b.String()
}

// ErrScopeNotSpreadsheet is the error for a scope given for a document without sheets.
func ErrScopeNotSpreadsheet(name string) error {
	return &SheetScopeError{Problem: fmt.Sprintf(
		"a sheet scope applies only to a spreadsheet RFP, and %s is not one; use the analysis instructions to scope other formats", name)}
}

// Validate checks the scope's own values, before any sheet is known.
func (s SheetScope) Validate() error {
	for key, v := range s {
		if strings.TrimSpace(key) == "" {
			return &SheetScopeError{Problem: "the sheet scope names an empty sheet"}
		}
		switch v {
		case ScopeAnswer, ScopeContext, ScopeExclude:
		default:
			return &SheetScopeError{Problem: fmt.Sprintf(
				"sheet %q has scope %q; it must be %q, %q or %q", key, v, ScopeAnswer, ScopeContext, ScopeExclude)}
		}
	}
	return nil
}

// ResolveScope applies a requested scope to a spreadsheet's sheets, returning every
// sheet with its scope set. With no scope every sheet is answered. Once the scope names
// any sheet, every sheet it does not name is excluded. A sheet is named by its position
// or by its name, matched ignoring case and surrounding whitespace; a key matching no
// sheet, a sheet given two different scopes, or a scope that leaves nothing to answer is
// an error, all before any model call.
func ResolveScope(sheets []SheetInfo, req SheetScope) ([]SheetInfo, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	out := make([]SheetInfo, len(sheets))
	copy(out, sheets)

	if len(req) == 0 {
		for i := range out {
			out[i].Scope = ScopeAnswer
		}
		return out, nil
	}

	set := make(map[int]SheetScopeValue, len(req))
	for key, v := range req {
		i := findSheet(out, key)
		if i < 0 {
			return nil, &SheetScopeError{Problem: fmt.Sprintf("the workbook has no sheet %q", strings.TrimSpace(key)), Sheets: sheets}
		}
		if prev, ok := set[i]; ok && prev != v {
			return nil, &SheetScopeError{Problem: fmt.Sprintf(
				"sheet %d (%s) is given two scopes, %s and %s", out[i].Position, out[i].Name, prev, v)}
		}
		set[i] = v
	}

	answered := 0
	for i := range out {
		out[i].Scope = ScopeExclude
		if v, ok := set[i]; ok {
			out[i].Scope = v
		}
		if out[i].Scope == ScopeAnswer {
			answered++
		}
	}
	if answered == 0 {
		return nil, &SheetScopeError{Problem: "the sheet scope leaves no sheet to answer; at least one sheet must be answered"}
	}
	return out, nil
}

// findSheet returns the index of the sheet a scope key names, or -1. A name match is
// tried first, so a sheet literally named "2" is found by its name.
func findSheet(sheets []SheetInfo, key string) int {
	key = strings.TrimSpace(key)
	for i, s := range sheets {
		if strings.EqualFold(strings.TrimSpace(s.Name), key) {
			return i
		}
	}
	if n, err := strconv.Atoi(key); err == nil {
		for i, s := range sheets {
			if s.Position == n {
				return i
			}
		}
	}
	return -1
}

// Scoped reports whether the sheets carry an operator's scope, as opposed to the
// default of answering every sheet.
func Scoped(sheets []SheetInfo) bool {
	for _, s := range sheets {
		if s.Scope != "" && s.Scope != ScopeAnswer {
			return true
		}
	}
	return false
}

// ParseSheetList parses a CLI sheet list: comma-separated names, positions and
// position ranges such as "2-7". A range is recognised only when both ends are
// numbers, so a sheet named "HW-SW" stays a name.
func ParseSheetList(s string) ([]string, error) {
	var out []string
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if lo, hi, ok := parseRange(item); ok {
			if lo < 1 || hi < lo {
				return nil, &SheetScopeError{Problem: fmt.Sprintf("sheet range %q is not a range of positions from 1", item)}
			}
			for n := lo; n <= hi; n++ {
				out = append(out, strconv.Itoa(n))
			}
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func parseRange(item string) (int, int, bool) {
	a, b, found := strings.Cut(item, "-")
	if !found {
		return 0, 0, false
	}
	lo, errA := strconv.Atoi(strings.TrimSpace(a))
	hi, errB := strconv.Atoi(strings.TrimSpace(b))
	if errA != nil || errB != nil {
		return 0, 0, false
	}
	return lo, hi, true
}

// ScopeFromLists builds a scope from the CLI's --sheets and --context-sheets values.
// The same key in both is an error; a name and a position meaning the same sheet are
// caught when the scope is resolved.
func ScopeFromLists(answer, context string) (SheetScope, error) {
	a, err := ParseSheetList(answer)
	if err != nil {
		return nil, err
	}
	c, err := ParseSheetList(context)
	if err != nil {
		return nil, err
	}
	if len(a) == 0 && len(c) == 0 {
		return nil, nil
	}
	scope := SheetScope{}
	for _, k := range a {
		scope[k] = ScopeAnswer
	}
	for _, k := range c {
		if scope[k] == ScopeAnswer {
			return nil, &SheetScopeError{Problem: fmt.Sprintf("sheet %q is in both --sheets and --context-sheets", k)}
		}
		scope[k] = ScopeContext
	}
	return scope, nil
}

// sheetInventory groups converted tables into the workbook's sheets. names is the
// workbook's sheet list in tab order; when it is available and accounts for every
// table, positions follow it, so a sheet Tika emitted no table for keeps its tab number.
// Otherwise each table is its own sheet, in the order converted. tableSheet maps each
// table to its index in the returned inventory.
func sheetInventory(tables []rfp.SheetTable, names []string) (inv []SheetInfo, tableSheet []int) {
	tableSheet = make([]int, len(tables))
	byName := make(map[string]int, len(names))
	for i, n := range names {
		if _, dup := byName[n]; !dup {
			byName[n] = i
		}
	}
	useNames := len(names) > 0
	for _, t := range tables {
		if _, ok := byName[t.Name]; !ok {
			useNames = false
			break
		}
	}

	if useNames {
		inv = make([]SheetInfo, len(names))
		for i, n := range names {
			inv[i] = SheetInfo{Position: i + 1, Name: n}
		}
		for ti, t := range tables {
			si := byName[t.Name]
			tableSheet[ti] = si
			inv[si].DataRows += dataRows(t)
		}
		return inv, tableSheet
	}

	inv = make([]SheetInfo, len(tables))
	for ti, t := range tables {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			name = fmt.Sprintf("Table %d", ti+1)
		}
		inv[ti] = SheetInfo{Position: ti + 1, Name: name, DataRows: dataRows(t)}
		tableSheet[ti] = ti
	}
	return inv, tableSheet
}

func dataRows(t rfp.SheetTable) int {
	if len(t.Rows) == 0 {
		return 0
	}
	return len(t.Rows) - 1 // exclude the header row
}

// SheetScopeHeaderLines renders a resolved scope as manifest provenance lines, one per
// sheet. Empty when the sheets carry no scope, so an unscoped manifest's header is
// unchanged. Shared by every surface that writes a generated manifest.
func SheetScopeHeaderLines(reports []DocumentReport) []string {
	var out []string
	for _, r := range reports {
		if !Scoped(r.Sheets) {
			continue
		}
		out = append(out, fmt.Sprintf("Sheet scope for %s:", r.Filename))
		for _, s := range r.Sheets {
			out = append(out, fmt.Sprintf("  %d %s: %s", s.Position, s.Name, s.Scope))
		}
	}
	return out
}
