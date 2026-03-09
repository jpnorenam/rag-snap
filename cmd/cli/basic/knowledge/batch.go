package knowledge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/processing"
	"gopkg.in/yaml.v3"
)

// BatchJob describes a single document ingestion task within a batch config.
type BatchJob struct {
	Name     string `yaml:"name,omitempty"`
	Type     string `yaml:"type"`
	Source   string `yaml:"source"`
	TargetKB string `yaml:"target_kb,omitempty"`
}

// BatchConfig is the top-level structure of a batch YAML file.
type BatchConfig struct {
	Version string     `yaml:"version"`
	Jobs    []BatchJob `yaml:"jobs"`
}

// ProcessBatch reads a YAML batch file and ingests each job into OpenSearch.
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

// processSingleJob ingests one job from a batch config into OpenSearch.
func processSingleJob(ctx context.Context, client *OpenSearchClient, tikaURL string, job BatchJob) error {
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

	targetIndex := FullIndexName(job.TargetKB)
	if job.TargetKB == "" {
		targetIndex = DefaultIndexName()
	}

	sourceID := job.Name
	if sourceID == "" {
		sourceID = filepath.Base(absPath)
	}

	ingestResult, err := processing.Ingest(tikaURL, absPath, sourceID)
	if err != nil {
		return fmt.Errorf("ingest pipeline failed: %w", err)
	}

	var docs []Document
	for _, chunk := range ingestResult.Chunks {
		docs = append(docs, Document{
			Content:   chunk.Content,
			SourceID:  sourceID,
			CreatedAt: chunk.CreatedAt,
		})
	}

	result, err := client.BulkIndex(ctx, targetIndex, docs)
	if err != nil {
		return fmt.Errorf("indexing failed: %w", err)
	}

	if result.Errors > 0 {
		return fmt.Errorf("partial indexing failure: %d/%d documents failed: %s", result.Errors, result.Total, result.FirstError)
	}

	return nil
}
