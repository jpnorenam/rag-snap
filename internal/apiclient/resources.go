package apiclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
)

// KnowledgeBase is the client view of a knowledge base summary from
// GET/POST /1.0/knowledge.
type KnowledgeBase struct {
	Name         string `json:"name"`
	Index        string `json:"index"`
	Health       string `json:"health"`
	Status       string `json:"status"`
	DocsCount    string `json:"docs_count"`
	StoreSize    string `json:"store_size"`
	DefaultLabel string `json:"default_label,omitempty"`
}

// Source is the client view of source metadata.
type Source struct {
	SourceID      string `json:"source_id"`
	FileName      string `json:"file_name"`
	FilePath      string `json:"file_path"`
	ContentType   string `json:"content_type,omitempty"`
	Checksum      string `json:"checksum"`
	IndexName     string `json:"index_name"`
	ChunkCount    int    `json:"chunk_count"`
	ChunkSize     int    `json:"chunk_size"`
	ChunkOverlap  int    `json:"chunk_overlap"`
	ContentLength int64  `json:"content_length"`
	Label         string `json:"label,omitempty"`
	Status        string `json:"status"`
	IngestedAt    string `json:"ingested_at"`
	UpdatedAt     string `json:"updated_at"`
	Title         string `json:"title,omitempty"`
	Author        string `json:"author,omitempty"`
	Language      string `json:"language,omitempty"`
}

// LoopbackInfo is the client view of the loopback listener's state from the
// GET /1.0 config summary. When Enabled, Address/URL give the resolved listen
// address and Token is the localhost bearer token — obtained here over the
// trusted unix socket so a client never reads the owner-only token file.
type LoopbackInfo struct {
	Enabled   bool   `json:"enabled"`
	Address   string `json:"address,omitempty"`
	URL       string `json:"url,omitempty"`
	Token     string `json:"token,omitempty"`
	TokenPath string `json:"token_path,omitempty"`
}

// ServerInfo fetches GET /1.0 and returns the loopback section of its config
// summary, so a trusted unix-socket client can discover the loopback listener's
// (OS-assigned) port and its bearer token.
func (c *Client) ServerInfo(ctx context.Context) (*LoopbackInfo, error) {
	var info struct {
		Config struct {
			Loopback LoopbackInfo `json:"loopback"`
		} `json:"config"`
	}
	if err := c.Sync(ctx, "GET", "/1.0", nil, &info); err != nil {
		return nil, err
	}
	return &info.Config.Loopback, nil
}

// SearchHit is the client view of a single search result. Label is the hit's
// resolved knowledge label (stored chunk label, or the daemon's index-name
// fallback for unlabeled chunks).
type SearchHit struct {
	Score     float64 `json:"score"`
	Base      string  `json:"base"`
	SourceID  string  `json:"source_id"`
	CreatedAt string  `json:"created_at"`
	Label     string  `json:"label"`
	Content   string  `json:"content"`
}

// ListKnowledge returns all knowledge bases.
func (c *Client) ListKnowledge(ctx context.Context) ([]KnowledgeBase, error) {
	var bases []KnowledgeBase
	if err := c.Sync(ctx, "GET", "/1.0/knowledge", nil, &bases); err != nil {
		return nil, err
	}
	return bases, nil
}

// CreateKnowledge creates a knowledge base by name. defaultLabel optionally
// sets the base's default knowledge label; empty leaves the convention-derived
// default in place.
func (c *Client) CreateKnowledge(ctx context.Context, name, defaultLabel string) (*KnowledgeBase, error) {
	var kb KnowledgeBase
	body := map[string]string{"name": name}
	if defaultLabel != "" {
		body["default_label"] = defaultLabel
	}
	if err := c.Sync(ctx, "POST", "/1.0/knowledge", body, &kb); err != nil {
		return nil, err
	}
	return &kb, nil
}

// GetKnowledge returns a single knowledge base's detail.
func (c *Client) GetKnowledge(ctx context.Context, name string) (*KnowledgeBase, error) {
	var kb KnowledgeBase
	if err := c.Sync(ctx, "GET", "/1.0/knowledge/"+name, nil, &kb); err != nil {
		return nil, err
	}
	return &kb, nil
}

// SetKnowledgeLabel sets a base's default knowledge label. When
// applyToExisting is set the daemon backfills unlabeled chunks and source
// records as an async operation and the operation URL is returned; otherwise
// the update is synchronous and the returned URL is empty.
func (c *Client) SetKnowledgeLabel(ctx context.Context, name, label string, applyToExisting bool) (string, error) {
	body := map[string]any{"default_label": label, "apply_to_existing": applyToExisting}
	if applyToExisting {
		return c.Async(ctx, "PATCH", "/1.0/knowledge/"+name, body)
	}
	return "", c.Sync(ctx, "PATCH", "/1.0/knowledge/"+name, body, nil)
}

// DeleteKnowledge deletes a knowledge base and its sources.
func (c *Client) DeleteKnowledge(ctx context.Context, name string) error {
	return c.Sync(ctx, "DELETE", "/1.0/knowledge/"+name, nil, nil)
}

// ListSources lists the sources in a knowledge base.
func (c *Client) ListSources(ctx context.Context, name string) ([]Source, error) {
	var sources []Source
	if err := c.Sync(ctx, "GET", "/1.0/knowledge/"+name+"/sources", nil, &sources); err != nil {
		return nil, err
	}
	return sources, nil
}

// GetSource returns a single source's metadata.
func (c *Client) GetSource(ctx context.Context, name, id string) (*Source, error) {
	var src Source
	if err := c.Sync(ctx, "GET", "/1.0/knowledge/"+name+"/sources/"+id, nil, &src); err != nil {
		return nil, err
	}
	return &src, nil
}

// DeleteSource forgets a source (removes its chunks and metadata).
func (c *Client) DeleteSource(ctx context.Context, name, id string) error {
	return c.Sync(ctx, "DELETE", "/1.0/knowledge/"+name+"/sources/"+id, nil, nil)
}

// Search runs hybrid search over the named bases.
func (c *Client) Search(ctx context.Context, query string, bases []string, count int) ([]SearchHit, error) {
	var hits []SearchHit
	body := map[string]any{"query": query, "bases": bases, "count": count}
	if err := c.Sync(ctx, "POST", "/1.0/search", body, &hits); err != nil {
		return nil, err
	}
	return hits, nil
}

// IngestURL starts an ingest operation for a single URL source and returns the
// operation URL. label optionally overrides the base's default knowledge label.
func (c *Client) IngestURL(ctx context.Context, name, sourceID, url, label string) (string, error) {
	body := map[string]any{"source_id": sourceID, "url": url}
	if label != "" {
		body["label"] = label
	}
	return c.Async(ctx, "POST", "/1.0/knowledge/"+name+"/sources", body)
}

// IngestFile uploads a local file to a knowledge base as a multipart ingest
// operation and returns the operation URL. label optionally overrides the
// base's default knowledge label.
func (c *Client) IngestFile(ctx context.Context, name, sourceID, path, label string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if sourceID != "" {
		if err := mw.WriteField("source_id", sourceID); err != nil {
			return "", err
		}
	}
	if label != "" {
		if err := mw.WriteField("label", label); err != nil {
			return "", err
		}
	}
	part, err := mw.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, f); err != nil {
		return "", err
	}
	if err := mw.Close(); err != nil {
		return "", err
	}

	env, err := c.doRaw(ctx, "POST", "/1.0/knowledge/"+name+"/sources", &buf, mw.FormDataContentType())
	if err != nil {
		return "", err
	}
	if env.Type == responseTypeError {
		return "", apiError(env)
	}
	if env.Operation == "" {
		return "", fmt.Errorf("expected an async operation but got a %q response", env.Type)
	}
	return env.Operation, nil
}

// EngineInit starts the knowledge-engine init operation and returns the
// operation URL.
func (c *Client) EngineInit(ctx context.Context) (string, error) {
	return c.Async(ctx, "POST", "/1.0/knowledge-engine", nil)
}

// EngineModel is the client view of a model registered in the engine's model
// group, from GET /1.0/knowledge-engine/models.
type EngineModel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	State       string `json:"state"`
	SizeBytes   int64  `json:"size_bytes"`
	WorkerNodes int    `json:"worker_nodes"`
	Role        string `json:"role"`
}

// ListEngineModels returns the engine's registered models with their deployment
// state and engine role.
func (c *Client) ListEngineModels(ctx context.Context) ([]EngineModel, error) {
	var models []EngineModel
	if err := c.Sync(ctx, "GET", "/1.0/knowledge-engine/models", nil, &models); err != nil {
		return nil, err
	}
	return models, nil
}

// DeleteEngineModel undeploys and deletes a model. force removes a model the
// engine currently uses.
func (c *Client) DeleteEngineModel(ctx context.Context, id string, force bool) error {
	path := "/1.0/knowledge-engine/models/" + id
	if force {
		path += "?force=true"
	}
	return c.Sync(ctx, "DELETE", path, nil, nil)
}

// Export starts an export operation for a knowledge base and returns the
// operation URL.
func (c *Client) Export(ctx context.Context, name, outputDir string, compress bool) (string, error) {
	body := map[string]any{"output_dir": outputDir, "compress": compress}
	return c.Async(ctx, "POST", "/1.0/knowledge/"+name+"/export", body)
}

// Import starts an import operation from a local export directory and returns
// the operation URL.
func (c *Client) Import(ctx context.Context, name, inputDir string, force bool) (string, error) {
	body := map[string]any{"name": name, "input_dir": inputDir, "force": force}
	return c.Async(ctx, "POST", "/1.0/knowledge/import", body)
}

// AnswerBatch posts a prepared batch manifest and returns the operation URL.
// The manifest is passed through as-is (it must match the API's expected JSON).
func (c *Client) AnswerBatch(ctx context.Context, manifest any) (string, error) {
	return c.Async(ctx, "POST", "/1.0/answer/batch", manifest)
}

// Prompt is the client view of one prompt slot: its effective value, the
// built-in default it falls back to, and whether an override is active. For
// generation slots it also carries the active variant name and the stored
// variant names. The daemon store is what chat sessions and batch runs are
// seeded from.
type Prompt struct {
	Name       string   `json:"name"`
	Value      string   `json:"value"`
	Default    string   `json:"default"`
	Customized bool     `json:"customized"`
	Active     string   `json:"active,omitempty"`
	Variants   []string `json:"variants,omitempty"`
}

// PromptVariant is the client view of one named variant's head value and
// metadata.
type PromptVariant struct {
	Name    string `json:"name"`
	Slot    string `json:"slot"`
	Value   string `json:"value"`
	Version int    `json:"version"`
	Active  bool   `json:"active"`
}

// PromptVariantSummary is the transcript-free view of a variant in a listing.
type PromptVariantSummary struct {
	Name     string `json:"name"`
	Versions int    `json:"versions"`
	Active   bool   `json:"active"`
}

// PromptVersion is one entry in a variant's version history.
type PromptVersion struct {
	Version int    `json:"version"`
	Value   string `json:"value"`
}

// ListPrompts returns the prompt templates in the daemon's canonical order.
func (c *Client) ListPrompts(ctx context.Context) ([]Prompt, error) {
	var prompts []Prompt
	if err := c.Sync(ctx, "GET", "/1.0/prompts", nil, &prompts); err != nil {
		return nil, err
	}
	return prompts, nil
}

// GetPrompt returns a single prompt template.
func (c *Client) GetPrompt(ctx context.Context, name string) (*Prompt, error) {
	var prompt Prompt
	if err := c.Sync(ctx, "GET", "/1.0/prompts/"+name, nil, &prompt); err != nil {
		return nil, err
	}
	return &prompt, nil
}

// SetPrompt stores a customization for a prompt template. An empty value is
// rejected by the daemon: reset a prompt with ResetPrompt instead.
func (c *Client) SetPrompt(ctx context.Context, name, value string) (*Prompt, error) {
	var prompt Prompt
	body := map[string]string{"value": value}
	if err := c.Sync(ctx, "PUT", "/1.0/prompts/"+name, body, &prompt); err != nil {
		return nil, err
	}
	return &prompt, nil
}

// ResetPrompt drops a prompt's customization so it resolves to the built-in
// default again. Resetting an uncustomized prompt succeeds as a no-op.
func (c *Client) ResetPrompt(ctx context.Context, name string) (*Prompt, error) {
	var prompt Prompt
	if err := c.Sync(ctx, "DELETE", "/1.0/prompts/"+name, nil, &prompt); err != nil {
		return nil, err
	}
	return &prompt, nil
}

// ListPromptVariants returns the variants of a generation slot.
func (c *Client) ListPromptVariants(ctx context.Context, slot string) ([]PromptVariantSummary, error) {
	var out []PromptVariantSummary
	if err := c.Sync(ctx, "GET", "/1.0/prompts/"+slot+"/variants", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetPromptVariant returns one variant's head value and metadata.
func (c *Client) GetPromptVariant(ctx context.Context, slot, name string) (*PromptVariant, error) {
	var v PromptVariant
	if err := c.Sync(ctx, "GET", "/1.0/prompts/"+slot+"/variants/"+name, nil, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// SavePromptVariant appends a new version to a variant, creating it if absent.
func (c *Client) SavePromptVariant(ctx context.Context, slot, name, value string) (*PromptVariant, error) {
	var v PromptVariant
	body := map[string]string{"value": value}
	if err := c.Sync(ctx, "PUT", "/1.0/prompts/"+slot+"/variants/"+name, body, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// CreatePromptVariant stores a new variant from an initial value, failing if the
// name is already in use.
func (c *Client) CreatePromptVariant(ctx context.Context, slot, name, value string) (*PromptVariant, error) {
	var v PromptVariant
	body := map[string]string{"name": name, "value": value}
	if err := c.Sync(ctx, "POST", "/1.0/prompts/"+slot+"/variants", body, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// DeletePromptVariant removes a variant. The active variant cannot be deleted.
func (c *Client) DeletePromptVariant(ctx context.Context, slot, name string) error {
	return c.Sync(ctx, "DELETE", "/1.0/prompts/"+slot+"/variants/"+name, nil, nil)
}

// PromptVariantVersions returns a variant's full version history.
func (c *Client) PromptVariantVersions(ctx context.Context, slot, name string) ([]PromptVersion, error) {
	var out []PromptVersion
	if err := c.Sync(ctx, "GET", "/1.0/prompts/"+slot+"/variants/"+name+"/versions", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RestorePromptVariant appends a new head version carrying an earlier version's
// content.
func (c *Client) RestorePromptVariant(ctx context.Context, slot, name string, version int) (*PromptVariant, error) {
	var v PromptVariant
	body := map[string]int{"version": version}
	if err := c.Sync(ctx, "POST", "/1.0/prompts/"+slot+"/variants/"+name+"/restore", body, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// ActivatePrompt points a slot's active pointer at a variant, or at the built-in
// default when name is empty.
func (c *Client) ActivatePrompt(ctx context.Context, slot, name string) (*Prompt, error) {
	var prompt Prompt
	body := map[string]string{"active": name}
	if err := c.Sync(ctx, "PATCH", "/1.0/prompts/"+slot, body, &prompt); err != nil {
		return nil, err
	}
	return &prompt, nil
}
