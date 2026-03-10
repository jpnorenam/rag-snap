package knowledge

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ImportOptions configures a knowledge base import.
type ImportOptions struct {
	InputDir string
	Force    bool
}

// resolveInputDir returns the directory to import from. If input is a .tar.gz
// file it is extracted into a temporary directory; the caller must remove it
// via the returned cleanup function.
func resolveInputDir(input string) (dir string, cleanup func(), err error) {
	info, err := os.Stat(input)
	if err != nil {
		return "", nil, fmt.Errorf("accessing input path: %w", err)
	}

	if info.IsDir() {
		return input, func() {}, nil
	}

	if !strings.HasSuffix(input, ".tar.gz") {
		return "", nil, fmt.Errorf("input %q is not a directory or a .tar.gz archive", input)
	}

	tmp, err := os.MkdirTemp("", "rag-import-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating temporary directory: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(tmp) }

	fmt.Printf("Extracting %s...\n", input)
	if err := extractTarGz(input, tmp); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("extracting archive: %w", err)
	}

	// The archive was created with the export directory as the top-level entry
	// (e.g. mybase-export/data.json). Find the single subdirectory inside tmp.
	entries, err := os.ReadDir(tmp)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("reading extracted archive: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			return filepath.Join(tmp, e.Name()), cleanup, nil
		}
	}

	// Files were extracted flat (no subdirectory).
	return tmp, cleanup, nil
}

// extractTarGz extracts a .tar.gz archive into destDir.
func extractTarGz(src, destDir string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("reading gzip stream: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar entry: %w", err)
		}

		// Guard against path traversal.
		target := filepath.Join(destDir, filepath.Clean("/"+hdr.Name))
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("archive entry %q would escape destination directory", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			out, err := os.Create(target)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}
	return nil
}

// importSources reads NDJSON source records produced by elasticdump, rewrites
// index_name to targetIndex, and upserts each record via IndexSourceMetadata.
// This allows transparent restore under a different knowledge base name.
func importSources(ctx context.Context, client *OpenSearchClient, sourcesPath, targetIndex string) (int, error) {
	f, err := os.Open(sourcesPath)
	if err != nil {
		return 0, fmt.Errorf("opening sources file: %w", err)
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	// elasticdump NDJSON lines can be large due to nested JSON; expand buffer.
	scanner.Buffer(make([]byte, 10*1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// elasticdump wraps each document as {"_index":...,"_source":{...}}.
		var raw struct {
			Source json.RawMessage `json:"_source"`
		}
		if err := json.Unmarshal(line, &raw); err != nil {
			return count, fmt.Errorf("parsing source line: %w", err)
		}

		var meta SourceMetadata
		if err := json.Unmarshal(raw.Source, &meta); err != nil {
			return count, fmt.Errorf("decoding source metadata: %w", err)
		}

		meta.IndexName = targetIndex
		if err := client.IndexSourceMetadata(ctx, meta); err != nil {
			return count, fmt.Errorf("indexing source %q: %w", meta.SourceID, err)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		return count, fmt.Errorf("reading sources file: %w", err)
	}

	return count, nil
}

// ImportKnowledgeBase restores a knowledge base from an export directory or
// a .tar.gz archive produced by ExportKnowledgeBase.
func ImportKnowledgeBase(ctx context.Context, client *OpenSearchClient, kbName string, opts ImportOptions) error {
	inputDir, cleanup, err := resolveInputDir(opts.InputDir)
	if err != nil {
		return err
	}
	defer cleanup()

	// Read and validate the manifest.
	manifestPath := filepath.Join(inputDir, "manifest.json")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("reading manifest: %w", err)
	}
	var manifest ExportManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return fmt.Errorf("parsing manifest: %w", err)
	}
	if manifest.Version != "1" {
		return fmt.Errorf("unsupported manifest version %q", manifest.Version)
	}

	// If no kb-name was provided, use the one recorded in the manifest.
	if kbName == "" {
		if manifest.KBName == "" {
			return fmt.Errorf("kb-name not provided and manifest does not contain a knowledge base name")
		}
		kbName = manifest.KBName
		fmt.Printf("Using knowledge base name from manifest: %q\n", kbName)
	}

	// Verify all required files are present.
	for _, name := range []string{"data.json", "mapping.json", "sources.json"} {
		path := filepath.Join(inputDir, name)
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("required file %q not found in %s", name, opts.InputDir)
		}
	}

	targetIndex := FullIndexName(kbName)

	// Block import into a non-empty index unless --force is set.
	count, err := client.CountDocuments(ctx, targetIndex)
	if err == nil && count > 0 && !opts.Force {
		return fmt.Errorf("index %q already contains %d documents; use --force to overwrite", targetIndex, count)
	}

	// Ensure required infrastructure exists.
	if err := client.getOrCreateIndexTemplate(ctx); err != nil {
		return fmt.Errorf("setting up index template: %w", err)
	}
	if err := client.CreateSourcesIndex(ctx); err != nil {
		return fmt.Errorf("setting up sources index: %w", err)
	}
	if err := client.getOrCreateIndex(ctx, targetIndex); err != nil {
		return fmt.Errorf("setting up target index: %w", err)
	}

	bin, nodeDir, err := findElasticdump()
	if err != nil {
		return fmt.Errorf("elasticdump not found: %w", err)
	}

	outputURL := client.AuthenticatedURL("/" + targetIndex)

	// Import mapping (best-effort; template already provides it).
	mappingPath := filepath.Join(inputDir, "mapping.json")
	fmt.Println("Importing mapping...")
	_ = runElasticdump(ctx, bin, nodeDir, []string{
		"--input=" + mappingPath,
		"--output=" + outputURL,
		"--type=mapping",
		"--tlsVerification=false",
	}, os.Stdout, os.Stderr)

	// Import data. --noRefresh speeds up bulk import and pre-computed embeddings
	// are preserved as-is, so the ingest pipeline must not be applied.
	dataPath := filepath.Join(inputDir, "data.json")
	fmt.Println("Importing document data...")
	if err := runElasticdump(ctx, bin, nodeDir, []string{
		"--input=" + dataPath,
		"--output=" + outputURL,
		"--type=data",
		"--limit=100",
		"--tlsVerification=false",
		"--noRefresh",
	}, os.Stdout, os.Stderr); err != nil {
		return fmt.Errorf("importing data: %w", err)
	}

	// Import sources via Go (handles index_name rewrite for rename).
	sourcesPath := filepath.Join(inputDir, "sources.json")
	fmt.Println("Importing source metadata...")
	sourcesImported, err := importSources(ctx, client, sourcesPath, targetIndex)
	if err != nil {
		return fmt.Errorf("importing sources: %w", err)
	}

	fmt.Printf("\nImport complete.\n")
	fmt.Printf("  Sources imported: %d\n", sourcesImported)
	fmt.Printf("  Chunks expected:  %d (from manifest)\n", manifest.ChunkCount)
	return nil
}
