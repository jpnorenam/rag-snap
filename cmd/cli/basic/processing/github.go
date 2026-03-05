package processing

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const gitHubAPIBase = "https://api.github.com"

// RepoEntry represents a single file from a remote git repository.
type RepoEntry struct {
	Path   string // file path within the repo (e.g. "docs/overview.md")
	RawURL string // URL to fetch the raw file content
}

// ParseGitHubSource parses a GitHub source string into owner and repo.
// Accepts "owner/repo" or "https://github.com/owner/repo[/...]".
func ParseGitHubSource(source string) (owner, repo string, err error) {
	source = strings.TrimSuffix(source, "/")
	if strings.HasPrefix(source, "https://github.com/") {
		source = strings.TrimPrefix(source, "https://github.com/")
	}
	parts := strings.SplitN(source, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid GitHub source %q: expected \"owner/repo\" or \"https://github.com/owner/repo\"", source)
	}
	return parts[0], parts[1], nil
}

// ListGitHubRepoFiles returns all files in a GitHub repository matching the
// given extensions and optional path prefix. It uses the Trees API to fetch
// the entire tree in a single API call.
func ListGitHubRepoFiles(owner, repo, branch, pathFilter string, extensions []string, token string) ([]RepoEntry, error) {
	if branch == "" {
		var err error
		branch, err = fetchDefaultBranch(owner, repo, token)
		if err != nil {
			return nil, fmt.Errorf("fetching default branch: %w", err)
		}
	}

	url := fmt.Sprintf("%s/repos/%s/%s/git/trees/%s?recursive=1", gitHubAPIBase, owner, repo, branch)
	body, err := githubGET(url, token)
	if err != nil {
		return nil, fmt.Errorf("fetching repository tree: %w", err)
	}
	defer body.Close()

	var treeResp struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
		Truncated bool `json:"truncated"`
	}
	if err := json.NewDecoder(body).Decode(&treeResp); err != nil {
		return nil, fmt.Errorf("parsing tree response: %w", err)
	}
	if treeResp.Truncated {
		fmt.Println("Warning: repository tree is truncated (>100k files); some files may be skipped")
	}

	extSet := make(map[string]struct{}, len(extensions))
	for _, ext := range extensions {
		extSet[ext] = struct{}{}
	}

	var entries []RepoEntry
	for _, item := range treeResp.Tree {
		if item.Type != "blob" {
			continue
		}
		if pathFilter != "" && !strings.HasPrefix(item.Path, pathFilter) {
			continue
		}
		if _, ok := extSet[filepath.Ext(item.Path)]; !ok {
			continue
		}
		entries = append(entries, RepoEntry{
			Path:   item.Path,
			RawURL: fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, branch, item.Path),
		})
	}
	return entries, nil
}

// FetchRepoFile downloads a raw file from a remote repository and writes it to a temp file.
// The caller must call the returned cleanup function when done.
func FetchRepoFile(rawURL, filePath, token string) (tempPath string, cleanup func(), err error) {
	body, err := githubGET(rawURL, token)
	if err != nil {
		return "", nil, fmt.Errorf("fetching file: %w", err)
	}
	defer body.Close()

	ext := filepath.Ext(filePath)
	if ext == "" {
		ext = ".txt"
	}
	tmpFile, err := os.CreateTemp("", "rag-github-*"+ext)
	if err != nil {
		return "", nil, fmt.Errorf("creating temp file: %w", err)
	}
	cleanupFn := func() { os.Remove(tmpFile.Name()) }

	n, copyErr := io.Copy(tmpFile, io.LimitReader(body, MaxIngestFileSize+1))
	if closeErr := tmpFile.Close(); closeErr != nil {
		cleanupFn()
		return "", nil, fmt.Errorf("closing temp file: %w", closeErr)
	}
	if copyErr != nil {
		cleanupFn()
		return "", nil, fmt.Errorf("writing temp file: %w", copyErr)
	}
	if err := ValidateFileSize(n); err != nil {
		cleanupFn()
		return "", nil, err
	}

	return tmpFile.Name(), cleanupFn, nil
}

// fetchDefaultBranch returns the default branch name for the given repository.
func fetchDefaultBranch(owner, repo, token string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s", gitHubAPIBase, owner, repo)
	body, err := githubGET(url, token)
	if err != nil {
		return "", err
	}
	defer body.Close()

	var repoResp struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.NewDecoder(body).Decode(&repoResp); err != nil {
		return "", fmt.Errorf("parsing repository response: %w", err)
	}
	if repoResp.DefaultBranch == "" {
		return "", fmt.Errorf("default branch not found in repository response")
	}
	return repoResp.DefaultBranch, nil
}

// githubGET performs an authenticated GET to the GitHub API or raw.githubusercontent.com.
func githubGET(url, token string) (io.ReadCloser, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil) //nolint:gosec // URL is constructed from validated user input
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d %s", resp.StatusCode, resp.Status)
	}
	return resp.Body, nil
}
