package knowledge

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
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
	// crawled sources, otherwise the file path.
	MetadataPath string
	// TargetIndex is the full index name of the destination knowledge base.
	TargetIndex string
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
		FileName:      filepath.Base(opts.FilePath),
		FilePath:      metadataPath,
		Checksum:      result.Checksum,
		IndexName:     opts.TargetIndex,
		ChunkCount:    len(result.Chunks),
		ChunkSize:     processing.DefaultChunkSize,
		ChunkOverlap:  processing.DefaultChunkOverlap,
		ContentLength: result.ContentLength,
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
		docs[i] = Document{Content: chunk.Content, SourceID: chunk.SourceID, CreatedAt: chunk.CreatedAt}
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
