package processing

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// TikaMetadata holds metadata fields extracted by the Tika /meta endpoint.
type TikaMetadata struct {
	ContentType string
	Title       string
	Author      string
	Language    string
	// Pages is the document's page count, or 0 when the format carries no page
	// count or Tika did not report one. Tika spells it differently depending on the
	// parser that handled the file, so several keys are consulted.
	Pages int
}

// TikaClient sends files to a Tika server for content extraction.
type TikaClient struct {
	baseURL string
	client  *http.Client
}

// NewTikaClient creates a TikaClient from a Tika URL.
// It strips any path component, keeping only scheme://host:port.
func NewTikaClient(tikaURL string) (*TikaClient, error) {
	u, err := url.Parse(tikaURL)
	if err != nil {
		return nil, fmt.Errorf("invalid tika URL: %w", err)
	}
	base := fmt.Sprintf("%s://%s", u.Scheme, u.Host)
	return &TikaClient{
		baseURL: base,
		client:  &http.Client{},
	}, nil
}

// Extract sends a file to the Tika server and returns the extracted plain text.
func (t *TikaClient) Extract(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("opening file: %w", err)
	}
	defer file.Close()

	req, err := http.NewRequest(http.MethodPut, t.baseURL+"/tika", file)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "text/plain")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("tika request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("tika returned status %d: %s", resp.StatusCode, string(body))
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	return string(content), nil
}

// ExtractHTML sends a file to the Tika server and returns the extracted content as HTML.
// Tika returns XHTML with <table>, <h1>–<h6>, <p> tags that preserve document structure.
func (t *TikaClient) ExtractHTML(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("opening file: %w", err)
	}
	defer file.Close()

	req, err := http.NewRequest(http.MethodPut, t.baseURL+"/tika", file)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "text/html")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("tika request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("tika returned status %d: %s", resp.StatusCode, string(body))
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	return string(content), nil
}

// ExtractMetadata sends a file to the Tika /meta endpoint and returns parsed metadata.
func (t *TikaClient) ExtractMetadata(filePath string) (*TikaMetadata, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("opening file: %w", err)
	}
	defer file.Close()

	req, err := http.NewRequest(http.MethodPut, t.baseURL+"/meta", file)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tika metadata request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("tika /meta returned status %d: %s", resp.StatusCode, string(body))
	}

	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decoding metadata response: %w", err)
	}

	meta := &TikaMetadata{
		ContentType: metaString(raw, "Content-Type"),
		Title:       metaString(raw, "dc:title"),
		Author:      metaString(raw, "dc:creator"),
		Language:    metaString(raw, "language"),
		Pages:       metaPages(raw),
	}

	return meta, nil
}

// pageCountKeys are the metadata keys Tika uses for a page count, most specific
// first. Which one appears depends on the parser that handled the file, so all are
// tried rather than assuming one parser's spelling. Verified against Tika 3.1.0:
// a PDF reports "xmpTPg:NPages" (one P, not two — the XMP paged-text namespace).
var pageCountKeys = []string{"xmpTPg:NPages", "Page-Count", "meta:page-count", "Number of Pages"}

// perPageArrayKeys hold one entry per page, so their length is a page count. They
// are the fallback for a document whose parser reported no explicit count.
var perPageArrayKeys = []string{"pdf:charsPerPage", "pdf:unmappedUnicodeCharsPerPage"}

// metaPages extracts a page count from a metadata map, returning 0 when nothing in
// it carries one. Values arrive as a JSON number or a string depending on the key,
// so both are accepted.
func metaPages(m map[string]any) int {
	for _, key := range pageCountKeys {
		if n := metaInt(m[key]); n > 0 {
			return n
		}
	}
	for _, key := range perPageArrayKeys {
		if arr, ok := m[key].([]any); ok && len(arr) > 0 {
			return len(arr)
		}
	}
	return 0
}

// metaInt coerces a metadata value to a positive int, or 0.
func metaInt(v any) int {
	switch t := v.(type) {
	case float64:
		if t > 0 {
			return int(t)
		}
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(t)); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// metaString defensively extracts a string value from a metadata map.
func metaString(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}
