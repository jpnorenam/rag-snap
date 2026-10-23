package processing

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jpnorenam/rag-snap/cmd/cli/common"
)

// MaxIngestFileSize is the maximum allowed file size for ingestion (50 MB).
const MaxIngestFileSize = 50 * 1024 * 1024

// ValidateFileSize returns an error if the given size exceeds MaxIngestFileSize.
func ValidateFileSize(size int64) error {
	if size > MaxIngestFileSize {
		return fmt.Errorf("file size %d bytes exceeds maximum allowed %d bytes (50 MB)", size, int64(MaxIngestFileSize))
	}
	return nil
}

// IngestResult holds the output of the Ingest pipeline.
type IngestResult struct {
	Chunks        []Chunk
	Checksum      string        // SHA-256 hex digest of the original file
	ContentLength int64         // file size in bytes
	TikaMetadata  *TikaMetadata // may be nil if metadata extraction fails
}

// Ingest extracts content from a file and splits it into chunks ready for
// indexing. Markdown and plain-text files are read directly; everything else
// goes through Tika.
func Ingest(tikaURL, filePath, sourceID string) (*IngestResult, error) {
	// 1. Compute file checksum and size
	checksum, fileSize, err := checksumAndSize(filePath)
	if err != nil {
		return nil, fmt.Errorf("computing file checksum: %w", err)
	}

	if err := ValidateFileSize(fileSize); err != nil {
		return nil, err
	}

	tika, err := NewTikaClient(tikaURL)
	if err != nil {
		return nil, err
	}

	// 2-3. Extract content as Markdown
	content, err := extractMarkdown(tika, filePath)
	if err != nil {
		return nil, err
	}

	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("no content extracted from %s", filepath.Base(filePath))
	}

	// 4. Extract metadata (non-fatal on error)
	var tikaMeta *TikaMetadata
	tikaMeta, _ = tika.ExtractMetadata(filePath)

	// 5. Chunk the Markdown content (structure-aware)
	stopProgress := common.StartProgressSpinner("Chunking content")
	chunks := ChunkMarkdown(content, sourceID, ChunkOptions{
		Size:    DefaultChunkSize,
		Overlap: DefaultChunkOverlap,
	})
	stopProgress()

	if len(chunks) == 0 {
		return nil, fmt.Errorf("no chunks generated from content")
	}

	return &IngestResult{
		Chunks:        chunks,
		Checksum:      checksum,
		ContentLength: fileSize,
		TikaMetadata:  tikaMeta,
	}, nil
}

// extractMarkdown returns the file's content as Markdown. Markdown and
// plain-text files are read as-is so their line structure survives (see
// textSourceExtensions); other formats are extracted by Tika and converted
// from its XHTML.
func extractMarkdown(tika *TikaClient, filePath string) (string, error) {
	if content, ok, err := readTextSource(filePath); err != nil {
		return "", err
	} else if ok {
		return content, nil
	}

	stopProgress := common.StartProgressSpinner("Extracting content")
	rawHTML, err := tika.ExtractHTML(filePath)
	stopProgress()
	if err != nil {
		return "", fmt.Errorf("content extraction failed: %w", err)
	}

	rawHTML = strings.TrimSpace(rawHTML)
	if rawHTML == "" {
		return "", fmt.Errorf("no content extracted from %s", filepath.Base(filePath))
	}

	// Convert HTML to Markdown (preserves table structure)
	stopProgress = common.StartProgressSpinner("Converting to Markdown")
	content, err := HTMLToMarkdown(rawHTML)
	stopProgress()
	if err != nil {
		return "", fmt.Errorf("HTML to Markdown conversion failed: %w", err)
	}
	return content, nil
}

// checksumAndSize computes the SHA-256 hex digest and file size.
func checksumAndSize(filePath string) (string, int64, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", 0, err
	}

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", 0, err
	}

	return hex.EncodeToString(h.Sum(nil)), info.Size(), nil
}
