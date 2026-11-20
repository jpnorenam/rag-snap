package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// workbookBytes is a minimal .xlsx carrying only the tab list; the cell content comes
// from tikaWorkbook, which plays Tika's XHTML for the same tabs.
func workbookBytes(t *testing.T, tabs []string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("xl/workbook.xml")
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><workbook><sheets>`)
	for i, n := range tabs {
		fmt.Fprintf(&b, `<sheet name="%s" sheetId="%d"/>`, n, i+1)
	}
	b.WriteString(`</sheets></workbook>`)
	_, _ = w.Write([]byte(b.String()))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func tikaWorkbook(t *testing.T, tabs []string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("<html><body>")
	for _, n := range tabs {
		fmt.Fprintf(&b, `<div class="page"><h1>%s</h1><table><tbody><tr><td>ID</td></tr><tr><td>a</td></tr><tr><td>b</td></tr></tbody></table></div>`, n)
	}
	b.WriteString("</body></html>")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(b.String()))
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/tika"
}

type sheetsResponse struct {
	Format string `json:"format"`
	Sheets []struct {
		Position int    `json:"position"`
		Name     string `json:"name"`
		DataRows int    `json:"data_rows"`
	} `json:"sheets"`
}

func postSheets(t *testing.T, client *http.Client, filename, content string) (int, sheetsResponse, string) {
	t.Helper()
	body, contentType := multipartFile(t, filename, content, nil)
	resp, err := client.Post("http://unix/1.0/answer/analyze/sheets", contentType, body)
	if err != nil {
		t.Fatalf("POST /1.0/answer/analyze/sheets: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var env struct {
		Metadata sheetsResponse `json:"metadata"`
	}
	_ = json.Unmarshal(raw, &env)
	return resp.StatusCode, env.Metadata, string(raw)
}

func TestSheetsInventoryNeedsNoAnalysisConfiguration(t *testing.T) {
	// Analysis unconfigured and no key: listing sheets makes no model call, so it works.
	t.Setenv("GEMINI_API_KEY", "")
	tabs := []string{"Read Me", "1 Hardware", "App B"}
	sock, _ := startTestServer(t, map[string]string{backendTika: tikaWorkbook(t, tabs)})
	client := dialSocket(sock)

	status, got, raw := postSheets(t, client, "rfp.xlsx", workbookBytes(t, tabs))
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, raw)
	}
	if got.Format != "xlsx" || len(got.Sheets) != 3 {
		t.Fatalf("inventory = %+v", got)
	}
	if s := got.Sheets[1]; s.Position != 2 || s.Name != "1 Hardware" || s.DataRows != 2 {
		t.Errorf("sheet 2 = %+v", s)
	}
}

func TestSheetsInventoryForOtherFormats(t *testing.T) {
	sock, _ := startTestServer(t, map[string]string{backendTika: "http://127.0.0.1:1"})
	client := dialSocket(sock)

	if status, got, raw := postSheets(t, client, "rfp.pdf", "%PDF-1.7\n"); status != http.StatusOK || got.Format != "pdf" || len(got.Sheets) != 0 || !strings.Contains(raw, `"sheets":[]`) {
		t.Errorf("a PDF lists no sheets: status %d, %s", status, raw)
	}
	if status, got, raw := postSheets(t, client, "rfp.csv", "Req,Text\nC1,SR-IOV\n"); status != http.StatusOK || len(got.Sheets) != 1 {
		t.Errorf("a CSV is one sheet: status %d, %s", status, raw)
	}
	if status, _, raw := postSheets(t, client, "notes.txt", "x"); status != http.StatusBadRequest {
		t.Errorf("an unsupported type is a 400: status %d, %s", status, raw)
	}
	if status, _, raw := postSheets(t, client, "empty.csv", ""); status != http.StatusBadRequest {
		t.Errorf("a CSV with nothing in it is the document's fault, a 400: status %d, %s", status, raw)
	}
	if status, _, raw := postSheets(t, client, "rfp.xlsx", "not a zip"); status != http.StatusInternalServerError || !strings.Contains(raw, "tika") {
		t.Errorf("a workbook Tika cannot reach is a 500 naming tika: status %d, %s", status, raw)
	}
}

func TestAnalyzeRejectsABadScopeBeforeAnyOperation(t *testing.T) {
	sock, _ := startTestServerWithConfig(t, map[string]string{backendTika: "http://127.0.0.1:1"},
		[]string{"analysis.enabled=true", "analysis.model=gemini-3.1-pro-preview"})
	t.Setenv("GEMINI_API_KEY", "test-key")
	client := dialSocket(sock)

	for _, tc := range []struct {
		name, file, content, sheets, want string
	}{
		{"not JSON", "rfp.xlsx", "x", `2-7`, "JSON object"},
		{"unknown value", "rfp.xlsx", "x", `{"2":"skip"}`, "skip"},
		{"not a spreadsheet", "rfp.csv", "Req,Text\nC1,SR-IOV\n", `{"1":"answer"}`, "applies only to a spreadsheet"},
	} {
		body, contentType := multipartFile(t, tc.file, tc.content, map[string]string{"sheets": tc.sheets})
		resp, err := client.Post("http://unix/1.0/answer/analyze", contentType, body)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(raw), tc.want) {
			t.Errorf("%s: status %d, body %s — want a 400 containing %q", tc.name, resp.StatusCode, raw, tc.want)
		}
		if strings.Contains(string(raw), "operation") {
			t.Errorf("%s: no operation may be started: %s", tc.name, raw)
		}
	}
}
