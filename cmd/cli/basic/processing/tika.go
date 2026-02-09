package processing

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
)

// TikaMetadata holds metadata fields extracted by the Tika /meta endpoint.
type TikaMetadata struct {
	ContentType string
	Title       string
	Author      string
	Language    string
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
	}

	return meta, nil
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
