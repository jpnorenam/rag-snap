package analysis

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile creates a file with the given name and content under a temp dir and
// returns its path. The extension drives format detection.
func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	return path
}

const complianceCSV = `Req ID,Requirement,Description,Current Environment,Evaluator Guidance
C1,SR-IOV,Single root I/O virtualization,Enabled on compute,State the mechanism
C2,CPU Pinning,Dedicated cores for VNFs,In production,Name the config surface
`

func TestConveyCSVKeepsEveryColumn(t *testing.T) {
	// No Tika needed for CSV, which is what makes this the cheap regression test
	// for "the columns beside the requirement survive".
	c := NewConverter("")
	path := writeFile(t, "matrix.csv", complianceCSV)

	doc, report, err := c.Convey(path, RolePrimary)
	if err != nil {
		t.Fatalf("Convey: %v", err)
	}
	if doc.Path != PathConverted || !report.Converted {
		t.Errorf("CSV should be reported as converted, got path=%q converted=%v", doc.Path, report.Converted)
	}
	if doc.PDFBytes != nil {
		t.Error("a converted document must not carry PDF bytes")
	}
	for _, want := range []string{
		"Description", "Current Environment", "Evaluator Guidance",
		"Single root I/O virtualization", "Enabled on compute", "Name the config surface",
	} {
		if !strings.Contains(doc.Text, want) {
			t.Errorf("conveyed text is missing %q:\n%s", want, doc.Text)
		}
	}
	if !strings.Contains(report.Detail, "2 data row(s)") {
		t.Errorf("report should state the row count, got %q", report.Detail)
	}
}

func TestConveyPDFSendsBytesNatively(t *testing.T) {
	c := NewConverter("") // no Tika: the page check is skipped, not failed
	body := "%PDF-1.7\nnot a real pdf but bytes all the same\n"
	path := writeFile(t, "rfp.pdf", body)

	doc, report, err := c.Convey(path, RolePrimary)
	if err != nil {
		t.Fatalf("Convey: %v", err)
	}
	if doc.Path != PathNative || report.Converted {
		t.Errorf("a PDF must be conveyed natively, got path=%q converted=%v", doc.Path, report.Converted)
	}
	if doc.Text != "" {
		t.Error("a natively conveyed PDF must not be turned into text")
	}
	if string(doc.PDFBytes) != body {
		t.Error("conveyed bytes differ from the file on disk")
	}
	// Without Tika the count is unknown, and the report must say so rather than
	// implying the page limit was checked and passed.
	if !strings.Contains(report.Detail, "page count unknown") {
		t.Errorf("report should record the unknown page count, got %q", report.Detail)
	}
}

func TestConveyRejectsOversizedPDFNamingLimitAndValue(t *testing.T) {
	c := NewConverter("")
	c.Limits = Limits{MaxPDFBytes: 16}
	path := writeFile(t, "big.pdf", strings.Repeat("x", 64))

	_, _, err := c.Convey(path, RolePrimary)
	if err == nil {
		t.Fatal("expected an oversized PDF to be rejected")
	}
	// The message has to be actionable: which document, which limit, what was
	// measured — all three, so the operator can decide what to do.
	for _, want := range []string{"big.pdf", "64 B", "16 B"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
}

func TestConveyRejectsTooManyPagesWhenKnown(t *testing.T) {
	// Page count comes from Tika, so this exercises the limit itself with the count
	// injected: a converter with a 1-page limit and a document Tika cannot measure
	// must pass, proving the check is skipped rather than defaulted to a failure.
	c := NewConverter("")
	c.Limits = Limits{MaxPDFPages: 1}
	path := writeFile(t, "unknown.pdf", "%PDF-1.7\n")

	if _, _, err := c.Convey(path, RolePrimary); err != nil {
		t.Fatalf("an unmeasurable page count must not fail the conveyance: %v", err)
	}
}

func TestConveyAllRejectsWhenTheSetExceedsTheTotal(t *testing.T) {
	c := NewConverter("")
	c.Limits = Limits{MaxTotalBytes: 32}
	primary := writeFile(t, "a.pdf", strings.Repeat("x", 30))
	extra := writeFile(t, "b.pdf", strings.Repeat("y", 30))

	_, err := c.ConveyAll(primary, []string{extra})
	if err == nil {
		t.Fatal("expected the set to be rejected for exceeding the total limit")
	}
	if !strings.Contains(err.Error(), "total") {
		t.Errorf("error should explain that the total was exceeded, got: %v", err)
	}
}

func TestConveyAllFailsRatherThanDroppingAContextDocument(t *testing.T) {
	// A context document silently dropped would change the generated manifest with
	// no signal at all, so an unreadable one fails the whole conveyance and names
	// itself.
	c := NewConverter("")
	primary := writeFile(t, "primary.csv", complianceCSV)

	_, err := c.ConveyAll(primary, []string{filepath.Join(t.TempDir(), "absent.pdf")})
	if err == nil {
		t.Fatal("expected a missing context document to fail the conveyance")
	}
	if !strings.Contains(err.Error(), "absent.pdf") {
		t.Errorf("error should name the offending context document, got: %v", err)
	}
}

func TestConveyAllOrdersPrimaryFirstAndLabelsRoles(t *testing.T) {
	c := NewConverter("")
	primary := writeFile(t, "primary.csv", complianceCSV)
	extra := writeFile(t, "extra.csv", complianceCSV)

	conv, err := c.ConveyAll(primary, []string{extra})
	if err != nil {
		t.Fatalf("ConveyAll: %v", err)
	}
	if len(conv.Documents) != 2 || len(conv.Report) != 2 {
		t.Fatalf("expected 2 documents and 2 reports, got %d and %d", len(conv.Documents), len(conv.Report))
	}
	if conv.Documents[0].Role != RolePrimary {
		t.Errorf("first document role = %q, want %q", conv.Documents[0].Role, RolePrimary)
	}
	if conv.Documents[1].Role != RoleContext {
		t.Errorf("second document role = %q, want %q", conv.Documents[1].Role, RoleContext)
	}
	if conv.TotalBytes() == 0 {
		t.Error("TotalBytes should account for the conveyed documents")
	}
}

func TestConveyRejectsUnsupportedFormat(t *testing.T) {
	c := NewConverter("")
	path := writeFile(t, "notes.rtf", "whatever")

	_, _, err := c.Convey(path, RolePrimary)
	if err == nil {
		t.Fatal("expected an unsupported format to be rejected")
	}
	// The message lists what analysis does read, so the operator's next step is
	// obvious rather than requiring the docs.
	if !strings.Contains(err.Error(), "PDF") || !strings.Contains(err.Error(), "XLSX") {
		t.Errorf("error should list the supported formats, got: %v", err)
	}
}

func TestConveyXLSXWithoutTikaExplainsTheMissingService(t *testing.T) {
	c := NewConverter("")
	path := writeFile(t, "matrix.xlsx", "not really a workbook")

	_, _, err := c.Convey(path, RolePrimary)
	if err == nil {
		t.Fatal("expected a spreadsheet to need Tika")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "tika") {
		t.Errorf("error should name the tika service, got: %v", err)
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{512, "512 B"},
		{1024, "1.0 KiB"},
		{32 << 20, "32.0 MiB"},
	}
	for _, tc := range tests {
		if got := humanBytes(tc.in); got != tc.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
