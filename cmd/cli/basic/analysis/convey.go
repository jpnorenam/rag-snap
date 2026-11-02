package analysis

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/processing"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/rfp"
)

// Role distinguishes the RFP being answered from the documents that only add
// context to it. Both are conveyed; the model is told which is which, because a
// requirement to answer and background material call for different treatment.
type Role string

// The two roles a conveyed document plays: the RFP itself, or supporting background.
const (
	RolePrimary Role = "primary"
	RoleContext Role = "context"
)

// Path names how a document reached the model, which is the part of conveyance an
// operator needs to see. "Native" means the model applied its own document
// understanding to the original bytes; "converted" means rag-cli turned the document
// into text first, and the result is only as good as that conversion.
type Path string

// The two conveyance paths a document can take.
const (
	PathNative    Path = "native"
	PathConverted Path = "converted"
)

// Limits bound what may be conveyed, checked before anything is sent.
//
// Gemini documents 50 MB or 1000 pages per PDF, and 100 MB per inline request.
// DefaultLimits sits inside both, and its request total matches the daemon's 48 MiB
// upload cap, so that the common case fails here — with a message naming the document
// and the limit — rather than as an opaque provider rejection after a long upload.
type Limits struct {
	MaxPDFBytes   int64
	MaxPDFPages   int
	MaxTextBytes  int64
	MaxTotalBytes int64
}

// DefaultLimits keeps conveyance inside Gemini's documented limits and the daemon's upload cap.
func DefaultLimits() Limits {
	return Limits{
		MaxPDFBytes:   32 << 20, // 32 MiB
		MaxPDFPages:   600,
		MaxTextBytes:  8 << 20, // 8 MiB of Markdown is an enormous spreadsheet
		MaxTotalBytes: 48 << 20,
	}
}

// Document is one input document in the form it will be sent.
type Document struct {
	Filename string
	Format   string
	Role     Role
	Path     Path

	// PDFBytes is set for a natively conveyed PDF: the original bytes, held raw
	// because the SDK encodes on send — staging them base64 would cost a third more
	// memory and a decode per call. Text is set for a converted document. Exactly one
	// is populated.
	PDFBytes []byte
	Text     string
}

// Size reports the conveyed byte size, which is what the staging bound measures.
func (d Document) Size() int64 {
	return int64(len(d.PDFBytes) + len(d.Text))
}

// DocumentReport is the per-document conveyance report shown to the operator before
// a manifest is generated. A document that was converted rather than read natively
// changes how much to trust the result, and that is invisible otherwise.
type DocumentReport struct {
	Filename  string `json:"filename"`
	Format    string `json:"format"`
	Path      Path   `json:"path"`
	Converted bool   `json:"converted"`
	// Detail is a short human-readable note — sheet and row counts for a converted
	// spreadsheet, page count for a PDF — so the operator can sanity-check that the
	// whole document arrived.
	Detail string `json:"detail,omitempty"`
	Bytes  int64  `json:"bytes"`
}

// Conveyance is a staged, ready-to-send document set together with its report.
type Conveyance struct {
	Documents []Document       `json:"-"`
	Report    []DocumentReport `json:"report"`
}

// TotalBytes is the conveyed size of the whole set.
func (c Conveyance) TotalBytes() int64 {
	var n int64
	for _, d := range c.Documents {
		n += d.Size()
	}
	return n
}

// Converter turns documents on disk into conveyable form.
//
// TikaURL may be empty. It is required only for the formats that must be converted
// (spreadsheets and word-processor documents); a PDF is conveyed as bytes and needs
// Tika only for its page count, which is skipped when Tika is unavailable rather than
// failing the conveyance.
type Converter struct {
	TikaURL string
	Limits  Limits
}

// NewConverter returns a Converter with the default limits.
func NewConverter(tikaURL string) *Converter {
	return &Converter{TikaURL: tikaURL, Limits: DefaultLimits()}
}

// ConveyAll conveys the primary document followed by each context document, in order.
// It fails on the first document it cannot convey: a context document silently dropped
// would change the generated manifest with no signal to the operator.
func (c *Converter) ConveyAll(primaryPath string, contextPaths []string) (*Conveyance, error) {
	var out Conveyance

	doc, report, err := c.Convey(primaryPath, RolePrimary)
	if err != nil {
		return nil, err
	}
	out.Documents = append(out.Documents, doc)
	out.Report = append(out.Report, report)

	for _, p := range contextPaths {
		doc, report, err := c.Convey(p, RoleContext)
		if err != nil {
			return nil, fmt.Errorf("context document %s: %w", filepath.Base(p), err)
		}
		out.Documents = append(out.Documents, doc)
		out.Report = append(out.Report, report)
	}

	if limit := c.Limits.MaxTotalBytes; limit > 0 {
		if total := out.TotalBytes(); total > limit {
			return nil, fmt.Errorf(
				"conveyed documents total %s, above the %s limit for one analysis request; remove a context document or split the RFP",
				humanBytes(total), humanBytes(limit))
		}
	}

	return &out, nil
}

// Convey turns one document into conveyable form.
//
// The rule is the model's own document understanding where it exists, and
// table-preserving Markdown where it does not. A PDF goes as bytes. Every other
// supported format is converted here, because no mainstream model API accepts a
// spreadsheet or a word-processor document at all, and the file types they do accept
// are read as pure text with markup discarded — so uploading the original, or Tika's
// HTML, would lose the table structure this conversion exists to keep.
func (c *Converter) Convey(path string, role Role) (Document, DocumentReport, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Document{}, DocumentReport{}, fmt.Errorf("cannot access file: %w", err)
	}
	name := filepath.Base(path)
	format := rfp.DetectFormat(path, "")

	switch format {
	case "pdf":
		return c.conveyPDF(path, name, role, info.Size())
	case "xlsx", "csv":
		return c.conveyTabular(path, name, format, role)
	case "docx":
		return c.conveyProse(path, name, format, role)
	default:
		return Document{}, DocumentReport{}, fmt.Errorf(
			"unsupported file type %q — analysis reads PDF, XLSX, CSV, DOCX", filepath.Ext(name))
	}
}

// conveyPDF sends the original bytes so the model applies its own document
// understanding to layout, tables and figures rather than receiving a flattened
// transcription.
func (c *Converter) conveyPDF(path, name string, role Role, size int64) (Document, DocumentReport, error) {
	if limit := c.Limits.MaxPDFBytes; limit > 0 && size > limit {
		return Document{}, DocumentReport{}, fmt.Errorf(
			"%s is %s, above the %s per-document limit for a PDF", name, humanBytes(size), humanBytes(limit))
	}

	// The page count comes from Tika because counting pages otherwise would mean a
	// new PDF dependency. When Tika is unavailable the check is skipped rather than
	// failing: the size limit still applies, and the report says the count is
	// unknown so the operator is not misled into thinking it passed.
	pages := 0
	detail := "conveyed as PDF bytes, page count unknown"
	if c.TikaURL != "" {
		if tikaClient, err := processing.NewTikaClient(c.TikaURL); err == nil {
			if meta, metaErr := tikaClient.ExtractMetadata(path); metaErr == nil && meta.Pages > 0 {
				pages = meta.Pages
				detail = fmt.Sprintf("conveyed as PDF bytes, %d pages", pages)
			}
		}
	}
	if limit := c.Limits.MaxPDFPages; limit > 0 && pages > limit {
		return Document{}, DocumentReport{}, fmt.Errorf(
			"%s has %d pages, above the %d-page limit for a PDF", name, pages, limit)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return Document{}, DocumentReport{}, fmt.Errorf("reading %s: %w", name, err)
	}
	doc := Document{
		Filename: name,
		Format:   "pdf",
		Role:     role,
		Path:     PathNative,
		PDFBytes: raw,
	}
	return doc, DocumentReport{
		Filename: name, Format: "pdf", Path: PathNative, Converted: false,
		Detail: detail, Bytes: doc.Size(),
	}, nil
}

// conveyTabular converts a spreadsheet or CSV to Markdown tables, keeping every row,
// every column and every sheet name.
func (c *Converter) conveyTabular(path, name, format string, role Role) (Document, DocumentReport, error) {
	var sheets []rfp.SheetTable

	switch format {
	case "csv":
		var err error
		if sheets, err = rfp.SheetsFromCSV(path); err != nil {
			return Document{}, DocumentReport{}, err
		}
	case "xlsx":
		if c.TikaURL == "" {
			return Document{}, DocumentReport{}, fmt.Errorf(
				"reading %s needs the tika service, which is not configured", name)
		}
		tikaClient, err := processing.NewTikaClient(c.TikaURL)
		if err != nil {
			return Document{}, DocumentReport{}, fmt.Errorf("creating Tika client: %w", err)
		}
		htmlContent, err := tikaClient.ExtractHTML(path)
		if err != nil {
			return Document{}, DocumentReport{}, fmt.Errorf("tika extraction of %s failed: %w", name, err)
		}
		sheets = rfp.SheetsFromTikaHTML(htmlContent, path)
	}

	if len(sheets) == 0 {
		return Document{}, DocumentReport{}, fmt.Errorf("no tables found in %s", name)
	}

	text := rfp.RenderSheetsMarkdown(sheets)
	if err := c.checkTextSize(name, text); err != nil {
		return Document{}, DocumentReport{}, err
	}

	rows := 0
	for _, s := range sheets {
		if len(s.Rows) > 0 {
			rows += len(s.Rows) - 1 // exclude the header row
		}
	}
	doc := Document{Filename: name, Format: format, Role: role, Path: PathConverted, Text: text}
	return doc, DocumentReport{
		Filename: name, Format: format, Path: PathConverted, Converted: true,
		Detail: fmt.Sprintf("converted to Markdown tables: %d sheet(s), %d data row(s), all columns kept",
			len(sheets), rows),
		Bytes: doc.Size(),
	}, nil
}

// conveyProse converts a word-processor document. Tables inside it are recovered as
// Markdown tables the same way a spreadsheet's are; the surrounding prose is kept as
// text. Uploading the original is not an option — no mainstream API accepts the
// format — and uploading Tika's HTML would have its tags stripped.
func (c *Converter) conveyProse(path, name, format string, role Role) (Document, DocumentReport, error) {
	if c.TikaURL == "" {
		return Document{}, DocumentReport{}, fmt.Errorf(
			"reading %s needs the tika service, which is not configured", name)
	}
	tikaClient, err := processing.NewTikaClient(c.TikaURL)
	if err != nil {
		return Document{}, DocumentReport{}, fmt.Errorf("creating Tika client: %w", err)
	}
	htmlContent, err := tikaClient.ExtractHTML(path)
	if err != nil {
		return Document{}, DocumentReport{}, fmt.Errorf("tika extraction of %s failed: %w", name, err)
	}

	tables := rfp.SheetsFromTikaHTML(htmlContent, "")
	prose := strings.TrimSpace(rfp.HTMLToMarkdown(htmlContent))

	var b strings.Builder
	if prose != "" {
		b.WriteString(prose)
	}
	if len(tables) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("# Tables\n\n")
		b.WriteString(rfp.RenderSheetsMarkdown(tables))
	}
	text := strings.TrimSpace(b.String())
	if text == "" {
		return Document{}, DocumentReport{}, fmt.Errorf("no readable content found in %s", name)
	}
	if err := c.checkTextSize(name, text); err != nil {
		return Document{}, DocumentReport{}, err
	}

	doc := Document{Filename: name, Format: format, Role: role, Path: PathConverted, Text: text}
	detail := "converted to Markdown"
	if len(tables) > 0 {
		detail = fmt.Sprintf("converted to Markdown, including %d table(s)", len(tables))
	}
	return doc, DocumentReport{
		Filename: name, Format: format, Path: PathConverted, Converted: true,
		Detail: detail, Bytes: doc.Size(),
	}, nil
}

func (c *Converter) checkTextSize(name, text string) error {
	limit := c.Limits.MaxTextBytes
	if limit > 0 && int64(len(text)) > limit {
		return fmt.Errorf("%s converts to %s of text, above the %s limit",
			name, humanBytes(int64(len(text))), humanBytes(limit))
	}
	return nil
}

// humanBytes renders a byte count the way an operator reads one, so a limit message
// says "32.0 MiB" rather than a raw count they have to divide themselves.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	units := []string{"KiB", "MiB", "GiB"}
	for _, u := range units {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, u)
		}
	}
	return fmt.Sprintf("%.1f TiB", value/unit)
}
