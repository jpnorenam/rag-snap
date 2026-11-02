package knowledge

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/processing"
)

// ErrSourceAlreadyIngested signals that a source with the same identifier is
// already present and completed, and the caller did not request a forced
// re-ingest. Callers decide whether to skip (batch) or surface a conflict
// (single ingest over the API).
var ErrSourceAlreadyIngested = errors.New("source already ingested")

// IngestOptions carries the resolved inputs for ingesting a single source. It
// is the one place the ingest mechanics live, shared by the CLI and the daemon
// so their re-ingest semantics cannot diverge.
type IngestOptions struct {
	// FilePath is the local path to the file fed to Tika (a downloaded/crawled
	// temp file for URLs and repos, or a staged upload).
	FilePath string
	// SourceID is the stable identifier used by forget and metadata.
	SourceID string
	// MetadataPath is stored as SourceMetadata.FilePath — the original URL for
	// crawled sources, the repo-relative path for repo files, otherwise the
	// file path. It is never a staging temp path.
	MetadataPath string
	// SourceURI is the canonical http(s) link a citation points at, stored as
	// SourceMetadata.SourceURI. Empty when no URL is known.
	SourceURI string
	// TargetIndex is the full index name of the destination knowledge base.
	TargetIndex string
	// Label is the explicit label for this source. When empty, the base's
	// default label applies (stored _meta default, or the naming convention).
	Label string
	// Force replaces an existing source: its chunks are removed before
	// re-indexing so a re-ingest does not append duplicate chunks.
	Force bool
}

// SourceCompleted reports whether a source with the given id already exists and
// is in the completed state. Metadata is keyed globally by source id.
func (c *OpenSearchClient) SourceCompleted(ctx context.Context, sourceID string) bool {
	existing, err := c.GetSourceMetadata(ctx, sourceID)
	return err == nil && existing.Status == StatusCompleted
}

// IngestSource runs the Tika extraction + chunking pipeline for one source and
// bulk-indexes the result. When Force is set and the source already exists, its
// prior chunks are deleted first so the re-ingest replaces rather than appends.
// It does NOT itself skip already-completed sources — that policy belongs to the
// caller (see ErrSourceAlreadyIngested).
func (c *OpenSearchClient) IngestSource(ctx context.Context, tikaURL string, opts IngestOptions) error {
	if opts.FilePath == "" {
		return fmt.Errorf("no file to ingest for source %q", opts.SourceID)
	}
	metadataPath := opts.MetadataPath
	if metadataPath == "" {
		metadataPath = opts.FilePath
	}
	if opts.SourceURI != "" && !IsHTTPURL(opts.SourceURI) {
		return fmt.Errorf("source URI %q must be an absolute http(s) URL", opts.SourceURI)
	}
	// Metadata indexes created before citations existed lack the source_uri
	// keyword mapping; add it before the first write.
	if err := c.CreateSourcesIndex(ctx); err != nil {
		return fmt.Errorf("ensuring sources metadata index: %w", err)
	}

	// Resolve the source's label: explicit > base default > naming convention.
	label := opts.Label
	if label == "" {
		var err error
		if label, _, err = c.GetDefaultLabel(ctx, opts.TargetIndex); err != nil {
			return fmt.Errorf("resolving base default label: %w", err)
		}
	}
	if err := ValidateLabel(label); err != nil {
		return err
	}
	// Indexes created before labels existed lack the keyword mapping; without
	// it, dynamic mapping would type the field wrong on first write.
	if err := c.EnsureLabelMapping(ctx, opts.TargetIndex); err != nil {
		return fmt.Errorf("ensuring label mapping: %w", err)
	}

	// Forced re-ingest of an existing source: remove its old chunks first so the
	// base ends up with only the new batch (fixes append-not-replace).
	if opts.Force {
		if _, err := c.GetSourceMetadata(ctx, opts.SourceID); err == nil {
			if _, err := c.DeleteChunksBySourceID(ctx, opts.TargetIndex, opts.SourceID); err != nil {
				return fmt.Errorf("removing existing chunks: %w", err)
			}
		}
	}

	result, err := processing.Ingest(tikaURL, opts.FilePath, opts.SourceID)
	if err != nil {
		return fmt.Errorf("ingest pipeline failed: %w", err)
	}

	now := time.Now().UTC().Format(DateFormat)
	meta := SourceMetadata{
		SourceID:      opts.SourceID,
		FileName:      OriginalFileName(metadataPath),
		FilePath:      metadataPath,
		SourceURI:     opts.SourceURI,
		Checksum:      result.Checksum,
		IndexName:     opts.TargetIndex,
		ChunkCount:    len(result.Chunks),
		ChunkSize:     processing.DefaultChunkSize,
		ChunkOverlap:  processing.DefaultChunkOverlap,
		ContentLength: result.ContentLength,
		Label:         label,
		Status:        StatusProcessing,
		IngestedAt:    now,
		UpdatedAt:     now,
	}
	if result.TikaMetadata != nil {
		meta.ContentType = result.TikaMetadata.ContentType
		meta.Title = result.TikaMetadata.Title
		meta.Author = result.TikaMetadata.Author
		meta.Language = result.TikaMetadata.Language
	}
	if err := c.IndexSourceMetadata(ctx, meta); err != nil {
		return fmt.Errorf("writing source metadata: %w", err)
	}

	docs := make([]Document, len(result.Chunks))
	for i, chunk := range result.Chunks {
		docs[i] = Document{Content: chunk.Content, SourceID: chunk.SourceID, Label: label, CreatedAt: chunk.CreatedAt}
	}

	indexResult, err := c.BulkIndex(ctx, opts.TargetIndex, docs)
	if err != nil {
		_ = c.UpdateSourceStatus(ctx, opts.SourceID, StatusFailed)
		return fmt.Errorf("indexing failed: %w", err)
	}
	if indexResult.Errors > 0 {
		_ = c.UpdateSourceStatus(ctx, opts.SourceID, StatusFailed)
		return fmt.Errorf("partial indexing failure: %d/%d documents failed: %s", indexResult.Errors, indexResult.Total, indexResult.FirstError)
	}
	if err := c.UpdateSourceStatus(ctx, opts.SourceID, StatusCompleted); err != nil {
		return fmt.Errorf("updating source status: %w", err)
	}
	return nil
}

// OriginalFileName derives the recorded file name from the source's original
// input rather than the staged file Tika read: the base name of a path, the
// last path segment of a URL, or empty when neither yields a real name.
func OriginalFileName(metadataPath string) string {
	var name string
	if IsHTTPURL(metadataPath) {
		u, err := url.Parse(metadataPath)
		if err != nil {
			return ""
		}
		name = path.Base(strings.TrimSuffix(u.Path, "/"))
		if unescaped, err := url.PathUnescape(name); err == nil {
			name = unescaped
		}
	} else {
		name = filepath.Base(metadataPath)
	}
	if name == "." || name == "/" || name == "" || processing.IsTempFileName(name) {
		return ""
	}
	return name
}
