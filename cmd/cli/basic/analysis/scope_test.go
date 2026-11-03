package analysis

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/rfp"
)

// telcoTabs is the tab layout of the workbook that motivated sheet scope: a read-me,
// six requirement tabs (the last cut at Excel's 31 characters and reusing MIG- ids),
// and a sizing appendix whose "&" arrives decoded.
var telcoTabs = []string{
	"Read Me", "1 Hardware", "2 Platform", "3 Cloud Mgmt Platform", "4 Migration",
	"5 Cloud Services", "6 Delivery capability and re", "App B Sizing & BoQ",
}

func telcoInventory() []SheetInfo {
	out := make([]SheetInfo, len(telcoTabs))
	for i, n := range telcoTabs {
		out[i] = SheetInfo{Position: i + 1, Name: n, DataRows: 10}
	}
	return out
}

func scopesOf(sheets []SheetInfo) []SheetScopeValue {
	out := make([]SheetScopeValue, len(sheets))
	for i, s := range sheets {
		out[i] = s.Scope
	}
	return out
}

func TestResolveScopeWithoutAScopeAnswersEverySheet(t *testing.T) {
	got, err := ResolveScope(telcoInventory(), nil)
	if err != nil {
		t.Fatalf("ResolveScope: %v", err)
	}
	for _, s := range got {
		if s.Scope != ScopeAnswer {
			t.Errorf("sheet %d is %q, want answer", s.Position, s.Scope)
		}
	}
	if Scoped(got) {
		t.Error("an all-answer scope must not count as scoped")
	}
}

func TestResolveScopeExcludesUnnamedSheets(t *testing.T) {
	scope, err := ScopeFromLists("2-7", "read me, 8")
	if err != nil {
		t.Fatalf("ScopeFromLists: %v", err)
	}
	got, err := ResolveScope(telcoInventory(), scope)
	if err != nil {
		t.Fatalf("ResolveScope: %v", err)
	}
	want := []SheetScopeValue{ScopeContext, ScopeAnswer, ScopeAnswer, ScopeAnswer, ScopeAnswer, ScopeAnswer, ScopeAnswer, ScopeContext}
	if fmt.Sprint(scopesOf(got)) != fmt.Sprint(want) {
		t.Errorf("scopes = %v, want %v", scopesOf(got), want)
	}

	got, err = ResolveScope(telcoInventory(), SheetScope{"  6 DELIVERY capability and re ": ScopeAnswer})
	if err != nil {
		t.Fatalf("a truncated tab name must match ignoring case and whitespace: %v", err)
	}
	if got[6].Scope != ScopeAnswer || got[0].Scope != ScopeExclude || got[7].Scope != ScopeExclude {
		t.Errorf("only the named sheet should be answered, got %v", scopesOf(got))
	}
	if _, err := ResolveScope(telcoInventory(), SheetScope{"App B Sizing & BoQ": ScopeContext, "2": ScopeAnswer}); err != nil {
		t.Errorf("a decoded & must match: %v", err)
	}
}

func TestResolveScopeRejectsAnUnknownSheetListingTheSheets(t *testing.T) {
	_, err := ResolveScope(telcoInventory(), SheetScope{"7 Pricing": ScopeAnswer})
	var scopeErr *SheetScopeError
	if !errors.As(err, &scopeErr) {
		t.Fatalf("want a SheetScopeError, got %v", err)
	}
	for _, want := range []string{`no sheet "7 Pricing"`, "1  Read Me", "8  App B Sizing & BoQ"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error is missing %q:\n%s", want, err)
		}
	}
	if _, err := ResolveScope(telcoInventory(), SheetScope{"9": ScopeAnswer}); err == nil {
		t.Error("a position past the last sheet must be rejected")
	}
}

func TestResolveScopeNeedsSomethingToAnswer(t *testing.T) {
	_, err := ResolveScope(telcoInventory(), SheetScope{"1": ScopeContext})
	if err == nil || !strings.Contains(err.Error(), "at least one sheet must be answered") {
		t.Errorf("want the nothing-to-answer error, got %v", err)
	}
}

func TestResolveScopeRejectsTwoScopesForOneSheet(t *testing.T) {
	_, err := ResolveScope(telcoInventory(), SheetScope{"1": ScopeAnswer, "read me": ScopeContext})
	if err == nil || !strings.Contains(err.Error(), "two scopes") {
		t.Errorf("a name and a position naming one sheet differently must be rejected, got %v", err)
	}
	if _, err := ResolveScope(telcoInventory(), SheetScope{"2": "skip"}); err == nil {
		t.Error("an unknown scope value must be rejected")
	}
}

func TestParseSheetList(t *testing.T) {
	got, err := ParseSheetList(" 2-4, HW-SW ,Read Me,,7")
	if err != nil {
		t.Fatalf("ParseSheetList: %v", err)
	}
	if want := "[2 3 4 HW-SW Read Me 7]"; fmt.Sprint(got) != want {
		t.Errorf("got %v, want %s", got, want)
	}
	for _, bad := range []string{"5-2", "0-3"} {
		if _, err := ParseSheetList(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
	if _, err := ScopeFromLists("2,3", "3"); err == nil || !strings.Contains(err.Error(), "both") {
		t.Errorf("a sheet in both lists must be rejected, got %v", err)
	}
	if scope, err := ScopeFromLists("", " "); err != nil || scope != nil {
		t.Errorf("no lists means no scope, got %v, %v", scope, err)
	}
}

func TestSheetInventoryFollowsTheTabList(t *testing.T) {
	tables := []rfp.SheetTable{
		{Name: "Hardware", Rows: [][]string{{"ID"}, {"a"}, {"b"}}},
		{Name: "Migration", Rows: [][]string{{"Item"}, {"x"}}},
		{Name: "Migration", Rows: [][]string{{"ID"}, {"y"}, {"z"}}},
	}
	inv, tableSheet := sheetInventory(tables, []string{"Read Me", "Hardware", "Migration"})
	if len(inv) != 3 || inv[0].Name != "Read Me" || inv[0].DataRows != 0 {
		t.Fatalf("a tab with no table keeps its position, got %+v", inv)
	}
	if inv[2].Position != 3 || inv[2].DataRows != 3 {
		t.Errorf("two tables on one tab are one sheet, got %+v", inv[2])
	}
	if fmt.Sprint(tableSheet) != "[1 2 2]" {
		t.Errorf("tableSheet = %v", tableSheet)
	}

	inv, _ = sheetInventory(tables, nil)
	if len(inv) != 3 || inv[1].Position != 2 {
		t.Errorf("without a tab list each table is a sheet, got %+v", inv)
	}
}

// fakeWorkbook writes a minimal .xlsx carrying only the tab list, which is all
// XLSXSheetNames reads; the cell content comes from the fake Tika.
func fakeWorkbook(t *testing.T, tabs []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rfp.xlsx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("xl/workbook.xml")
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><workbook><sheets>`)
	for i, n := range tabs {
		fmt.Fprintf(&b, `<sheet name="%s" sheetId="%d"/>`, strings.ReplaceAll(n, "&", "&amp;"), i+1)
	}
	b.WriteString(`</sheets></workbook>`)
	_, _ = w.Write([]byte(b.String()))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return path
}

// fakeTika serves Tika's XHTML for a workbook: one page per tab, one table per page.
func fakeTika(t *testing.T, tabs []string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("<html><body>")
	for i, n := range tabs {
		fmt.Fprintf(&b, `<div class="page"><h1>%s</h1><table><tbody><tr><td>ID</td><td>Requirement</td></tr><tr><td>T%d-01</td><td>Requirement on %s</td></tr></tbody></table></div>`,
			strings.ReplaceAll(n, "&", "&amp;"), i+1, strings.ReplaceAll(n, "&", "&amp;"))
	}
	b.WriteString("</body></html>")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(b.String()))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestConveyScopedWorkbook(t *testing.T) {
	tabs := []string{"Read Me", "1 Hardware", "2 Platform", "App B Sizing & BoQ"}
	c := NewConverter(fakeTika(t, tabs))
	path := fakeWorkbook(t, tabs)

	conv, err := c.ConveyAllWith(path, nil, SheetScope{"2": ScopeAnswer, "3": ScopeAnswer, "Read Me": ScopeContext})
	if err != nil {
		t.Fatalf("ConveyAllWith: %v", err)
	}
	text := conv.Documents[0].Text
	for _, want := range []string{"Requirement on 1 Hardware", "Requirement on 2 Platform", "## Sheet: Read Me (context only"} {
		if !strings.Contains(text, want) {
			t.Errorf("conveyed text is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "App B") {
		t.Errorf("an excluded sheet must not be conveyed:\n%s", text)
	}
	report := conv.Report[0]
	if !strings.Contains(report.Detail, "2 of 4 sheet(s) answered, 1 context only, 1 excluded") {
		t.Errorf("detail = %q", report.Detail)
	}
	if got := fmt.Sprint(scopesOf(report.Sheets)); got != "[context answer answer exclude]" {
		t.Errorf("report scopes = %s", got)
	}
	if !conv.HasContextSheets() || fmt.Sprint(conv.contextSheetNames()) != "[Read Me]" {
		t.Error("the conveyance should know it carries a context-only sheet")
	}
}

func TestConveyUnscopedWorkbookIsUnchanged(t *testing.T) {
	tabs := []string{"Read Me", "1 Hardware"}
	c := NewConverter(fakeTika(t, tabs))
	path := fakeWorkbook(t, tabs)

	conv, err := c.ConveyAll(path, nil)
	if err != nil {
		t.Fatalf("ConveyAll: %v", err)
	}
	resp, err := http.Get(c.TikaURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	want := rfp.RenderSheetsMarkdown(rfp.SheetsFromTikaHTML(string(raw), path))
	if conv.Documents[0].Text != want {
		t.Errorf("unscoped text changed:\n%s\nwant:\n%s", conv.Documents[0].Text, want)
	}
	if !strings.HasPrefix(conv.Report[0].Detail, "converted to Markdown tables: 2 sheet(s), 2 data row(s)") {
		t.Errorf("unscoped detail changed: %q", conv.Report[0].Detail)
	}
	if conv.HasContextSheets() || len(SheetScopeHeaderLines(conv.Report)) != 0 {
		t.Error("an unscoped conveyance carries no scope")
	}
}

func TestScopeOnADocumentWithoutSheetsIsRejected(t *testing.T) {
	c := NewConverter("")
	for _, name := range []string{"rfp.pdf", "matrix.csv"} {
		path := writeFile(t, name, "%PDF-1.7\n")
		_, err := c.ConveyAllWith(path, nil, SheetScope{"1": ScopeAnswer})
		if err == nil || !strings.Contains(err.Error(), "applies only to a spreadsheet") {
			t.Errorf("%s: want the not-a-spreadsheet error, got %v", name, err)
		}
	}
}

func TestInventory(t *testing.T) {
	tabs := []string{"Read Me", "1 Hardware"}
	c := NewConverter(fakeTika(t, tabs))
	inv, format, err := c.Inventory(fakeWorkbook(t, tabs))
	if err != nil || format != "xlsx" || len(inv) != 2 || inv[1].Name != "1 Hardware" || inv[1].DataRows != 1 {
		t.Errorf("Inventory = %+v, %q, %v", inv, format, err)
	}
	inv, format, err = c.Inventory(writeFile(t, "rfp.pdf", "%PDF-1.7\n"))
	if err != nil || format != "pdf" || inv == nil || len(inv) != 0 {
		t.Errorf("a PDF has an empty inventory, got %+v, %q, %v", inv, format, err)
	}
	if _, _, err := c.Inventory(writeFile(t, "notes.txt", "x")); err == nil {
		t.Error("an unsupported type must be rejected")
	}
}

func TestContextSheetsAreAnnouncedOnlyWhenPresent(t *testing.T) {
	fs := newFakeServer(t,
		fakeCompletion{content: okTriage},
		fakeCompletion{content: questionsJSON(3)},
		fakeCompletion{content: strategyJSON},
		fakeCompletion{content: okTriage},
	)
	c := testClient(t, fs)

	scoped := testConveyance(t)
	scoped.Report[0].Sheets = []SheetInfo{{Position: 1, Name: "Read Me", Scope: ScopeContext}, {Position: 2, Name: "Matrix", Scope: ScopeAnswer}}
	if _, err := c.TriageWith(context.Background(), scoped, testCatalogue(), ""); err != nil {
		t.Fatalf("TriageWith: %v", err)
	}
	if _, err := c.GenerateWith(context.Background(), scoped, testCatalogue(), testTriage(3),
		Interview{AnswerWords: 100, AnswerLimit: LengthSoft}, GenerateOptions{}); err != nil {
		t.Fatalf("GenerateWith: %v", err)
	}
	for i, want := range []string{"Do not count or extract any question", "Do not count or extract any question", "use them to inform the prompt"} {
		if !strings.Contains(contentsText(fs.requests[i]), want) {
			t.Errorf("request %d does not announce the context-only sheets", i)
		}
	}

	if _, err := c.TriageWith(context.Background(), testConveyance(t), testCatalogue(), ""); err != nil {
		t.Fatalf("TriageWith: %v", err)
	}
	if strings.Contains(contentsText(fs.requests[3]), "context only") {
		t.Error("an unscoped conveyance must not mention context-only sheets")
	}
}

func TestAttributeSectionsCountsContextSheetQuestionsApart(t *testing.T) {
	tr := &Triage{Sections: []TriageSection{{Name: "Hardware", IDPrefix: "HW"}}}
	qs := []chat.BatchQuestion{
		{ID: "HW-01", Question: "a"},
		{ID: "HW-02", Question: "b"},
		{ID: "X", Question: "c", Source: "Read Me (context only — background, no questions to extract)"},
	}
	got := attributeSections(tr, qs, []string{"Read Me"})
	if len(got) != 2 || got[0].Produced != 2 || got[1].Name != "(from a context-only sheet)" || got[1].Produced != 1 {
		t.Errorf("attribution = %+v", got)
	}
}

func TestSheetScopeIsRecordedInTheProvenance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.yaml")
	m := &chat.BatchManifest{Version: "1.0", KnowledgeBases: []string{"vendor-rfp"}, Prompt: "P.",
		Questions: []chat.BatchQuestion{{ID: "C1", Question: "SR-IOV"}}}
	docs := []DocumentReport{{Filename: "rfp.xlsx", Format: "xlsx", Path: PathConverted, Sheets: []SheetInfo{
		{Position: 1, Name: "Read Me", Scope: ScopeContext},
		{Position: 2, Name: "1 Hardware", Scope: ScopeAnswer},
		{Position: 3, Name: "App B", Scope: ScopeExclude},
	}}}
	if err := WriteManifest(path, m, Provenance{Documents: docs}); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	raw, _ := os.ReadFile(path)
	for _, want := range []string{"# Sheet scope for rfp.xlsx:\n", "#   1 Read Me: context\n", "#   2 1 Hardware: answer\n", "#   3 App B: exclude\n"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("header is missing %q:\n%s", want, firstLines(string(raw), 12))
		}
	}
	if _, err := chat.LoadBatchManifest(path); err != nil {
		t.Errorf("a manifest with a scope block must still load: %v", err)
	}
}
