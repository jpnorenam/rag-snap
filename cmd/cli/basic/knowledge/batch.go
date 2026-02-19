// CC: removed unused "time" import; date format is taken from chunk.CreatedAt (set by chunker with the correct OpenSearch format)
package knowledge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/processing"
	"gopkg.in/yaml.v3"
)

// YAML Structure

type BatchJob struct {
	Name     string `yaml:"name,omitempty"`
	Type     string `yaml:"type"`
	Source   string `yaml:"source"`
	TargetKB string `yaml:"target_kb,omitempty"`
}

type BatchConfig struct {
	Version string     `yaml:"version"`
	Jobs    []BatchJob `yaml:"jobs"`
}

// ProcessBatch receives the tools (client, urls) and processes the batch configuration file.
func ProcessBatch(ctx context.Context, client *OpenSearchClient, tikaURL string, yamlPath string) error {
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return fmt.Errorf("reading batch file: %w", err)
	}

	var batchCfg BatchConfig
	if err := yaml.Unmarshal(data, &batchCfg); err != nil {
		return fmt.Errorf("parsing batch yaml: %w", err)
	}

	fmt.Printf("Found %d jobs in batch file version %s\n", len(batchCfg.Jobs), batchCfg.Version)

	for i, job := range batchCfg.Jobs {
		fmt.Printf("[%d/%d] Processing: %s\n", i+1, len(batchCfg.Jobs), job.Source)

		if err := processSingleJob(ctx, client, tikaURL, job); err != nil {
			fmt.Printf("❌ Error processing %s: %v\n", job.Source, err)
			continue
		}
		fmt.Printf("✅ Success: %s\n", job.Source)
	}

	return nil
}

func processSingleJob(ctx context.Context, client *OpenSearchClient, tikaURL string, job BatchJob) error {
	// CC: resolve the source to a local file path depending on job type
	var absPath string
	switch job.Type {
	case "file":
		path, err := filepath.Abs(job.Source)
		if err != nil {
			return fmt.Errorf("resolving path: %w", err)
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return fmt.Errorf("file not found: %s", path)
		}
		absPath = path
	case "url":
		crawled, _, cleanup, err := processing.CrawlURL(job.Source)
		if err != nil {
			return fmt.Errorf("crawling URL: %w", err)
		}
		defer cleanup()
		absPath = crawled
	default:
		return fmt.Errorf("unsupported job type %q (supported: file, url)", job.Type)
	}

	// CC: use FullIndexName so bulk goes to "rag-snap-context-<kb>" (with KNN mappings), not the raw KB name
	targetIndex := FullIndexName(job.TargetKB)
	if job.TargetKB == "" {
		targetIndex = DefaultIndexName()
	}

	sourceID := job.Name
	if sourceID == "" {
		sourceID = filepath.Base(absPath)
	}

	// Ingest Pipeline
	ingestResult, err := processing.Ingest(tikaURL, absPath, sourceID)
	if err != nil {
		return fmt.Errorf("ingest pipeline failed: %w", err)
	}

	// CC: use chunk.CreatedAt (set by chunker in "2006-01-02 15:04:05" format) to match the OpenSearch date mapping
	var docs []Document
	for _, chunk := range ingestResult.Chunks {
		doc := Document{
			Content:   chunk.Content,
			SourceID:  sourceID,
			CreatedAt: chunk.CreatedAt,
		}
		docs = append(docs, doc)
	}

	// Indexing
	result, err := client.BulkIndex(ctx, targetIndex, docs)
	if err != nil {
		return fmt.Errorf("indexing failed: %w", err)
	}

	// CC: include OpenSearch error reason to make failures self-diagnosable
	if result.Errors > 0 {
		return fmt.Errorf("partial indexing failure: %d/%d documents failed: %s", result.Errors, result.Total, result.FirstError)
	}

	return nil
}
