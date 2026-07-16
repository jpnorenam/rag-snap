package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// Default label set. Chunks and sources ingested before labels existed resolve
// to one of these via the legacy index-name convention; the compiled-in default
// prompts reference their tags.
const (
	LabelCanonical = "canonical"
	LabelUpstream  = "upstream"
	LabelKapa      = "kapa-canonical"
)

// labelPattern constrains labels to index-name-like tokens so the bracketed
// tags injected into RAG context stay predictable and cannot smuggle prompt
// text (no spaces, brackets, or newlines).
var labelPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// ValidateLabel rejects labels that do not match the allowed format.
func ValidateLabel(label string) error {
	if !labelPattern.MatchString(label) {
		return fmt.Errorf("invalid label %q: labels must be lowercase letters, digits, and hyphens, start with a letter or digit, and be at most 32 characters", label)
	}
	return nil
}

// InferLabelFromIndex returns the convention-derived label for an index name:
// the kapa pseudo-index maps to LabelKapa, a name containing "upstream" to
// LabelUpstream, anything else to LabelCanonical. This is the read-time
// fallback for data ingested before labels were stored.
func InferLabelFromIndex(indexName string) string {
	lower := strings.ToLower(indexName)
	if lower == KapaIndexName {
		return LabelKapa
	}
	if strings.Contains(lower, "upstream") {
		return LabelUpstream
	}
	return LabelCanonical
}

// ResolveLabel returns the effective label for a chunk: the stored label when
// present, otherwise the legacy index-name inference. Every consumer (REPL,
// daemon, remote clients) resolves labels through this single function.
func ResolveLabel(indexName, storedLabel string) string {
	if storedLabel != "" {
		return storedLabel
	}
	return InferLabelFromIndex(indexName)
}

// LabelTag renders a label as the uppercase bracketed tag shown in search
// output and injected into RAG context, e.g. "internal" -> "[INTERNAL]".
func LabelTag(label string) string {
	return "[" + strings.ToUpper(label) + "]"
}

// EnsureLabelMapping adds the label keyword field to an existing index's
// mapping. Indexes created before the template gained the field need this
// before chunks carrying labels are written or backfilled; adding a field is
// always mapping-compatible, and re-putting an existing one is a no-op.
func (c *OpenSearchClient) EnsureLabelMapping(ctx context.Context, indexName string) error {
	body := map[string]any{
		"properties": map[string]any{
			"label": map[string]any{"type": "keyword"},
		},
	}
	return c.putMapping(ctx, indexName, body)
}

// SetDefaultLabel stores label as the base's default in the index mapping
// _meta, so it travels with the mapping on export/import.
func (c *OpenSearchClient) SetDefaultLabel(ctx context.Context, indexName, label string) error {
	if err := ValidateLabel(label); err != nil {
		return err
	}
	body := map[string]any{
		"_meta": map[string]any{"default_label": label},
	}
	return c.putMapping(ctx, indexName, body)
}

// GetDefaultLabel returns the base's effective default label and whether it is
// stored in the index _meta (true) or derived from the naming convention (false).
func (c *OpenSearchClient) GetDefaultLabel(ctx context.Context, indexName string) (string, bool, error) {
	path := fmt.Sprintf("/%s/_mapping", indexName)
	req, err := c.newAuthenticatedRequest(http.MethodGet, path, nil)
	if err != nil {
		return "", false, fmt.Errorf("creating mapping request: %w", err)
	}

	resp, err := c.client.Client.Perform(req.WithContext(ctx))
	if err != nil {
		return "", false, fmt.Errorf("getting index mapping: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", false, fmt.Errorf("get mapping failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Response shape: {"<index>": {"mappings": {"_meta": {"default_label": ...}, ...}}}
	var mappingResp map[string]struct {
		Mappings struct {
			Meta struct {
				DefaultLabel string `json:"default_label"`
			} `json:"_meta"`
		} `json:"mappings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&mappingResp); err != nil {
		return "", false, fmt.Errorf("decoding mapping response: %w", err)
	}

	for _, m := range mappingResp {
		if m.Mappings.Meta.DefaultLabel != "" {
			return m.Mappings.Meta.DefaultLabel, true, nil
		}
	}
	return InferLabelFromIndex(indexName), false, nil
}

// BackfillLabel sets label on every chunk in the index that lacks one, and on
// the base's source metadata records that lack one. Chunks and sources that
// already carry a label are never overwritten, so explicit per-source labels
// survive a base-level backfill. Returns the number of updated chunks.
func (c *OpenSearchClient) BackfillLabel(ctx context.Context, indexName, label string) (int, error) {
	if err := ValidateLabel(label); err != nil {
		return 0, err
	}
	if err := c.EnsureLabelMapping(ctx, indexName); err != nil {
		return 0, fmt.Errorf("ensuring label mapping: %w", err)
	}

	updated, err := c.updateLabelByQuery(ctx, indexName, label, nil)
	if err != nil {
		return 0, err
	}

	// Source metadata records are keyed globally; scope the backfill to this base.
	indexFilter := map[string]any{"term": map[string]any{"index_name": indexName}}
	if _, err := c.updateLabelByQuery(ctx, sourcesIndexName, label, indexFilter); err != nil {
		return updated, fmt.Errorf("backfilling source metadata: %w", err)
	}

	return updated, nil
}

// updateLabelByQuery runs _update_by_query setting label on documents that
// have no label field, optionally restricted by an extra filter clause.
func (c *OpenSearchClient) updateLabelByQuery(ctx context.Context, indexName, label string, filter map[string]any) (int, error) {
	must := []map[string]any{}
	if filter != nil {
		must = append(must, filter)
	}
	query := map[string]any{
		"script": map[string]any{
			"source": "ctx._source.label = params.label",
			"lang":   "painless",
			"params": map[string]any{"label": label},
		},
		"query": map[string]any{
			"bool": map[string]any{
				"must": must,
				"must_not": []map[string]any{
					{"exists": map[string]any{"field": "label"}},
				},
			},
		},
	}

	bodyBytes, err := json.Marshal(query)
	if err != nil {
		return 0, fmt.Errorf("marshaling update query: %w", err)
	}

	path := fmt.Sprintf("/%s/_update_by_query?conflicts=proceed", indexName)
	req, err := c.newAuthenticatedRequest(http.MethodPost, path, bytes.NewReader(bodyBytes))
	if err != nil {
		return 0, fmt.Errorf("creating update request: %w", err)
	}

	resp, err := c.client.Client.Perform(req.WithContext(ctx))
	if err != nil {
		return 0, fmt.Errorf("updating labels: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("update by query failed with status %d: %s", resp.StatusCode, string(body))
	}

	var updateResp struct {
		Updated int `json:"updated"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&updateResp); err != nil {
		return 0, fmt.Errorf("decoding update response: %w", err)
	}
	return updateResp.Updated, nil
}

// putMapping issues PUT /<index>/_mapping with the given body.
func (c *OpenSearchClient) putMapping(ctx context.Context, indexName string, body map[string]any) error {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshaling mapping body: %w", err)
	}

	path := fmt.Sprintf("/%s/_mapping", indexName)
	req, err := c.newAuthenticatedRequest(http.MethodPut, path, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("creating mapping request: %w", err)
	}

	resp, err := c.client.Client.Perform(req.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("updating index mapping: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("put mapping failed with status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
