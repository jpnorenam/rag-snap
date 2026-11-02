package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/processing"
)

// Citation identifies the document a search hit came from, in a form fit for
// showing to a user. It is document-level: chunks of the same source share a
// Key. Locator is reserved for a future section/cell reference and is always
// empty today.
type Citation struct {
	// Key is the cited document's identity; today the hit's source id.
	Key string `json:"key"`
	// Label is the human-readable name of the document. Never empty once
	// resolved.
	Label string `json:"label"`
	// Href is a clickable link to the document, empty when none is recorded.
	Href    string `json:"href,omitempty"`
	Locator string `json:"locator,omitempty"`
	// Base is the knowledge-base name the hit came from ("kapa" for Kapa hits).
	Base string `json:"base"`
	// Tag is the hit's resolved knowledge label (e.g. "canonical").
	Tag string `json:"tag"`
	// Via records which resolvers supplied the fields, for verbose output.
	Via string `json:"-"`
}

// CitationResolver fills the citations of a batch of hits in place. A resolver
// only fills fields that are still empty, so the order of a chain expresses
// priority. Resolvers never fail: a resolver that cannot do its job leaves the
// fields for the next one.
type CitationResolver interface {
	Resolve(ctx context.Context, hits []SearchHit)
}

// ResolveCitations runs chain over hits in order.
func ResolveCitations(ctx context.Context, chain []CitationResolver, hits []SearchHit) {
	for _, r := range chain {
		r.Resolve(ctx, hits)
	}
}

// DefaultCitationChain is the resolver chain retrieval applies: Kapa source
// URLs, then the source-metadata record (when a client is available), then the
// source id as the never-failing fallback. A future chunk-level reference
// resolver belongs at the head of this chain.
func DefaultCitationChain(c *OpenSearchClient) []CitationResolver {
	chain := []CitationResolver{KapaResolver{}}
	if c != nil {
		chain = append(chain, MetadataResolver{Store: c})
	}
	return append(chain, SourceIDResolver{})
}

// KapaResolver resolves Kapa hits, whose source id is the document URL.
type KapaResolver struct{}

// Resolve links and labels Kapa hits by their source URL.
func (KapaResolver) Resolve(_ context.Context, hits []SearchHit) {
	for i := range hits {
		h := &hits[i]
		if h.Index != KapaIndexName {
			continue
		}
		cit := &h.Citation
		if cit.Href == "" && IsHTTPURL(h.SourceID) {
			cit.Href = h.SourceID
			addVia(cit, "href=kapa")
		}
		if cit.Label == "" && h.SourceID != "" {
			cit.Label = h.SourceID
			addVia(cit, "label=kapa")
		}
		if cit.Base == "" {
			cit.Base = "kapa"
		}
	}
}

// SourceMetadataStore looks up source metadata records by source id.
type SourceMetadataStore interface {
	MultiGetSourceMetadata(ctx context.Context, sourceIDs []string) (map[string]SourceMetadata, error)
}

// MetadataResolver resolves local hits from their source-metadata record with
// a single lookup for the distinct source ids in the batch.
type MetadataResolver struct {
	Store SourceMetadataStore
}

// Resolve fills links and labels from the source-metadata records of the
// batch's local hits; a failed lookup leaves them for the next resolver.
func (r MetadataResolver) Resolve(ctx context.Context, hits []SearchHit) {
	var ids []string
	seen := make(map[string]bool)
	for _, h := range hits {
		if h.Index == KapaIndexName || h.SourceID == "" || seen[h.SourceID] {
			continue
		}
		if h.Citation.Label != "" && h.Citation.Href != "" {
			continue
		}
		seen[h.SourceID] = true
		ids = append(ids, h.SourceID)
	}
	if len(ids) == 0 {
		return
	}

	metas, err := r.Store.MultiGetSourceMetadata(ctx, ids)
	if err != nil {
		for i := range hits {
			if seen[hits[i].SourceID] {
				addVia(&hits[i].Citation, "metadata unavailable: "+err.Error())
			}
		}
		return
	}

	for i := range hits {
		h := &hits[i]
		if !seen[h.SourceID] {
			continue
		}
		meta, ok := metas[h.SourceID]
		if !ok {
			continue
		}
		cit := &h.Citation
		if cit.Href == "" {
			switch {
			case IsHTTPURL(meta.SourceURI):
				cit.Href = meta.SourceURI
				addVia(cit, "href=source_uri")
			case IsHTTPURL(meta.FilePath):
				cit.Href = meta.FilePath
				addVia(cit, "href=file_path")
			}
		}
		if cit.Label == "" {
			if title := cleanTitle(meta.Title); usableTitle(title, meta.FileName) {
				cit.Label = title
				addVia(cit, "label=title")
			} else if meta.FileName != "" && !processing.IsTempFileName(meta.FileName) {
				cit.Label = meta.FileName
				addVia(cit, "label=file_name")
			}
		}
	}
}

// SourceIDResolver is the final fallback: it fills every remaining identity
// and display field from the hit itself, so every hit leaves the chain with a
// non-empty Key and Label.
type SourceIDResolver struct{}

// Resolve fills every field still empty from the hit itself.
func (SourceIDResolver) Resolve(_ context.Context, hits []SearchHit) {
	for i := range hits {
		h := &hits[i]
		cit := &h.Citation
		if cit.Key == "" {
			cit.Key = h.SourceID
		}
		if cit.Label == "" {
			cit.Label = h.SourceID
			addVia(cit, "label=source_id")
		}
		if cit.Base == "" {
			if name, err := KnowledgeBaseNameFromIndex(h.Index); err == nil {
				cit.Base = name
			} else {
				cit.Base = h.Index
			}
		}
		if cit.Tag == "" {
			cit.Tag = h.Label
		}
	}
}

func addVia(cit *Citation, step string) {
	if cit.Via == "" {
		cit.Via = step
		return
	}
	cit.Via += ", " + step
}

// IsHTTPURL reports whether s is an absolute http(s) URL with a host — the
// only form a citation link or a user-supplied source URI may take.
func IsHTTPURL(s string) bool {
	if s == "" {
		return false
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

var whitespaceRun = regexp.MustCompile(`\s+`)

// cleanTitle collapses whitespace (including newlines) so an extracted title
// renders on one line.
func cleanTitle(title string) string {
	return strings.TrimSpace(whitespaceRun.ReplaceAllString(title, " "))
}

// junkTitlePattern matches the placeholder titles office suites write into
// document properties instead of a real title.
var junkTitlePattern = regexp.MustCompile(`(?i)^(microsoft (word|excel|powerpoint) - |untitled)`)

// usableTitle reports whether an extracted document title is worth showing in
// preference to the file name.
func usableTitle(title, fileName string) bool {
	n := utf8.RuneCountInString(title)
	if n < 3 || n > 200 {
		return false
	}
	if junkTitlePattern.MatchString(title) {
		return false
	}
	if fileName != "" {
		stem := strings.TrimSuffix(fileName, filepath.Ext(fileName))
		if strings.EqualFold(title, fileName) || strings.EqualFold(title, stem) {
			return false
		}
	}
	return true
}

// MultiGetSourceMetadata fetches the metadata records for sourceIDs in one
// request. Ids with no record are absent from the result.
func (c *OpenSearchClient) MultiGetSourceMetadata(ctx context.Context, sourceIDs []string) (map[string]SourceMetadata, error) {
	bodyBytes, err := json.Marshal(map[string]any{"ids": sourceIDs})
	if err != nil {
		return nil, fmt.Errorf("marshaling mget body: %w", err)
	}

	path := fmt.Sprintf("/%s/_mget", sourcesIndexName)
	req, err := c.newAuthenticatedRequest(http.MethodPost, path, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating mget request: %w", err)
	}
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Client.Perform(req)
	if err != nil {
		return nil, fmt.Errorf("fetching source metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("mget source metadata failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var mgetResp struct {
		Docs []struct {
			ID     string         `json:"_id"`
			Found  bool           `json:"found"`
			Source SourceMetadata `json:"_source"`
		} `json:"docs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&mgetResp); err != nil {
		return nil, fmt.Errorf("decoding mget response: %w", err)
	}

	out := make(map[string]SourceMetadata, len(mgetResp.Docs))
	for _, d := range mgetResp.Docs {
		if d.Found {
			out[d.ID] = d.Source
		}
	}
	return out, nil
}

// SourceLine renders a citation for a one-line "source:" display: the label,
// then the link when there is one, then the source id in parentheses when the
// label is not already the source id.
func SourceLine(label, href, sourceID string) string {
	if label == "" {
		label = sourceID
	}
	line := label
	if href != "" {
		line += " — " + href
	}
	if sourceID != "" && sourceID != label {
		line += " (" + sourceID + ")"
	}
	return line
}
