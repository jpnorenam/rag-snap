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
	Name       string   `yaml:"name,omitempty"`
	Type       string   `yaml:"type"`
	Source     string   `yaml:"source"`
	TargetKB   string   `yaml:"target_kb,omitempty"`
	Branch     string   `yaml:"branch,omitempty"`
	Extensions []string `yaml:"extensions,omitempty"`
	Path       string   `yaml:"path,omitempty"`
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
	if len(batchCfg.Jobs) == 0 {
		return fmt.Errorf("batch file contains no jobs")
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
	targetIndex := FullIndexName(job.TargetKB)
	if job.TargetKB == "" {
		targetIndex = DefaultIndexName()
	}

	switch job.Type {
	case "file":
		path, err := filepath.Abs(job.Source)
		if err != nil {
			return fmt.Errorf("resolving path: %w", err)
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return fmt.Errorf("file not found: %s", path)
		}
		sourceID := job.Name
		if sourceID == "" {
			sourceID = filepath.Base(path)
		}
		return ingestAndIndex(ctx, client, tikaURL, path, sourceID, targetIndex)

	case "url":
		crawled, _, cleanup, err := processing.CrawlURL(job.Source)
		if err != nil {
			return fmt.Errorf("crawling URL: %w", err)
		}
		defer cleanup()
		sourceID := job.Name
		if sourceID == "" {
			sourceID = job.Source
		}
		return ingestAndIndex(ctx, client, tikaURL, crawled, sourceID, targetIndex)

	case "github-repo":
		return processGitHubRepoJob(ctx, client, tikaURL, job, targetIndex)

	case "gitea-repo":
		return processGiteaRepoJob(ctx, client, tikaURL, job, targetIndex)

	default:
		return fmt.Errorf("unsupported job type %q (supported: file, url, github-repo, gitea-repo)", job.Type)
	}
}

// processGitHubRepoJob fetches all matching files from a GitHub repository and indexes them.
func processGitHubRepoJob(ctx context.Context, client *OpenSearchClient, tikaURL string, job BatchJob, targetIndex string) error {
	owner, repo, err := processing.ParseGitHubSource(job.Source)
	if err != nil {
		return fmt.Errorf("parsing GitHub source: %w", err)
	}

	token := os.Getenv("GITHUB_TOKEN")
	entries, err := processing.ListGitHubRepoFiles(owner, repo, job.Branch, job.Path, job.Extensions, token)
	if err != nil {
		return fmt.Errorf("listing repository files: %w", err)
	}

	fmt.Printf("Found %d files in %s/%s\n", len(entries), owner, repo)

	for i, entry := range entries {
		fmt.Printf("  [%d/%d] %s\n", i+1, len(entries), entry.Path)
		tempPath, cleanup, err := processing.FetchRepoFile(entry.RawURL, entry.Path, token)
		if err != nil {
			fmt.Printf("  skip %s: %v\n", entry.Path, err)
			continue
		}
		if ingestErr := ingestAndIndex(ctx, client, tikaURL, tempPath, entry.Path, targetIndex); ingestErr != nil {
			fmt.Printf("  skip %s: %v\n", entry.Path, ingestErr)
		}
		cleanup()
	}
	return nil
}

// processGiteaRepoJob fetches all matching files from a Gitea repository and indexes them.
func processGiteaRepoJob(ctx context.Context, client *OpenSearchClient, tikaURL string, job BatchJob, targetIndex string) error {
	baseURL, owner, repo, err := processing.ParseGiteaSource(job.Source)
	if err != nil {
		return fmt.Errorf("parsing Gitea source: %w", err)
	}

	token := os.Getenv("GITEA_TOKEN")
	entries, err := processing.ListGiteaRepoFiles(baseURL, owner, repo, job.Branch, job.Path, job.Extensions, token)
	if err != nil {
		return fmt.Errorf("listing repository files: %w", err)
	}

	fmt.Printf("Found %d files in %s/%s\n", len(entries), owner, repo)

	for i, entry := range entries {
		fmt.Printf("  [%d/%d] %s\n", i+1, len(entries), entry.Path)
		tempPath, cleanup, err := processing.FetchRepoFile(entry.RawURL, entry.Path, token)
		if err != nil {
			fmt.Printf("  skip %s: %v\n", entry.Path, err)
			continue
		}
		if ingestErr := ingestAndIndex(ctx, client, tikaURL, tempPath, entry.Path, targetIndex); ingestErr != nil {
			fmt.Printf("  skip %s: %v\n", entry.Path, ingestErr)
		}
		cleanup()
	}
	return nil
}

// ingestAndIndex runs the Tika extraction + chunking pipeline and bulk-indexes the result.
func ingestAndIndex(ctx context.Context, client *OpenSearchClient, tikaURL, filePath, sourceID, targetIndex string) error {
	ingestResult, err := processing.Ingest(tikaURL, filePath, sourceID)
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
