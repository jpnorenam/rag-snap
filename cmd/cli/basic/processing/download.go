package processing

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	trafilatura "github.com/markusmobius/go-trafilatura"
	"golang.org/x/net/html"
)

// WebMetadata holds page-level metadata extracted from a web page.
type WebMetadata struct {
	Title       string
	Author      string
	Description string
	PublishDate string
}

// CrawlURL fetches url, extracts its main content via go-trafilatura, writes the
// resulting HTML to a temp file, and returns the path, extracted metadata, a
// cleanup function, and any error. Size limits from MaxIngestFileSize still apply.
func CrawlURL(url string) (filePath string, meta *WebMetadata, cleanup func(), err error) {
	stopProgress := common.StartProgressSpinner("Fetching page")

	resp, httpErr := http.Get(url) //nolint:gosec // URL comes from authenticated CLI input
	if httpErr != nil {
		stopProgress()
		return "", nil, nil, fmt.Errorf("fetching %s: %w", url, httpErr)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		stopProgress()
		return "", nil, nil, fmt.Errorf("fetching %s: HTTP %d %s", url, resp.StatusCode, resp.Status)
	}

	// Pre-check Content-Length when available.
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		if size, parseErr := strconv.ParseInt(cl, 10, 64); parseErr == nil {
			if sizeErr := ValidateFileSize(size); sizeErr != nil {
				stopProgress()
				return "", nil, nil, fmt.Errorf("remote page too large: %w", sizeErr)
			}
		}
	}

	// Read body with size guard (MaxIngestFileSize+1 so we can detect oversize).
	bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, MaxIngestFileSize+1))
	stopProgress()
	if readErr != nil {
		return "", nil, nil, fmt.Errorf("reading response body: %w", readErr)
	}
	if sizeErr := ValidateFileSize(int64(len(bodyBytes))); sizeErr != nil {
		return "", nil, nil, fmt.Errorf("remote page too large: %w", sizeErr)
	}

	const minExtractedChars = 100

	stopProgress = common.StartProgressSpinner("Extracting content")
	result, extractErr := trafilatura.Extract(bytes.NewReader(bodyBytes), trafilatura.Options{
		Focus:           trafilatura.FavorRecall,
		EnableFallback:  true,
		ExcludeComments: true,
	})
	stopProgress()

	if extractErr != nil {
		return "", nil, nil, fmt.Errorf("extracting content from %s: %w", url, extractErr)
	}
	if result == nil || result.ContentNode == nil {
		return "", nil, nil, fmt.Errorf("no readable content found at %s", url)
	}

	contentText := strings.TrimSpace(result.ContentText)
	if len(contentText) < minExtractedChars {
		return "", nil, nil, fmt.Errorf(
			"extracted only %d characters of text from %s (raw HTML was %d bytes)\n"+
				"The page likely requires JavaScript to render its content.\n"+
				"Try rendering it in a browser, saving the page as HTML, then using --file instead.",
			len(contentText), url, len(bodyBytes),
		)
	}

	// Render the extracted content node back to HTML for the downstream Tika pipeline.
	var buf strings.Builder
	if renderErr := html.Render(&buf, result.ContentNode); renderErr != nil {
		return "", nil, nil, fmt.Errorf("rendering extracted HTML: %w", renderErr)
	}
	if strings.TrimSpace(buf.String()) == "" {
		return "", nil, nil, fmt.Errorf("no readable content found at %s", url)
	}

	tmpFile, tmpErr := os.CreateTemp("", "rag-crawl-*.html")
	if tmpErr != nil {
		return "", nil, nil, fmt.Errorf("creating temp file: %w", tmpErr)
	}
	cleanupFn := func() { os.Remove(tmpFile.Name()) }

	if _, writeErr := io.WriteString(tmpFile, buf.String()); writeErr != nil {
		tmpFile.Close()
		cleanupFn()
		return "", nil, nil, fmt.Errorf("writing extracted content: %w", writeErr)
	}
	if closeErr := tmpFile.Close(); closeErr != nil {
		cleanupFn()
		return "", nil, nil, fmt.Errorf("closing temp file: %w", closeErr)
	}

	webMeta := &WebMetadata{
		Title:       result.Metadata.Title,
		Author:      result.Metadata.Author,
		Description: result.Metadata.Description,
	}
	if !result.Metadata.Date.IsZero() {
		webMeta.PublishDate = result.Metadata.Date.Format("2006-01-02")
	}

	return tmpFile.Name(), webMeta, cleanupFn, nil
}
