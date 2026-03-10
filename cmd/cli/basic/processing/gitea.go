package processing

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
)

// ParseGiteaSource parses a full Gitea URL into baseURL, owner, and repo.
// source must be a full URL: "https://{host}/{owner}/{repo}[/...]".
func ParseGiteaSource(source string) (baseURL, owner, repo string, err error) {
	source = strings.TrimSuffix(source, "/")
	u, err := url.Parse(source)
	if err != nil || u.Host == "" {
		return "", "", "", fmt.Errorf("invalid Gitea source %q: must be a full URL (e.g. https://opendev.org/owner/repo)", source)
	}
	parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", fmt.Errorf("invalid Gitea source %q: expected https://{host}/{owner}/{repo}", source)
	}
	return fmt.Sprintf("%s://%s", u.Scheme, u.Host), parts[0], parts[1], nil
}

// ListGiteaRepoFiles returns all files in a Gitea repository matching the given
// extensions and optional path prefix. It resolves the branch to a commit SHA
// then fetches the full recursive tree in a single API call.
func ListGiteaRepoFiles(baseURL, owner, repo, branch, pathFilter string, extensions []string, token string) ([]RepoEntry, error) {
	if branch == "" {
		var err error
		branch, err = giteaDefaultBranch(baseURL, owner, repo, token)
		if err != nil {
			return nil, fmt.Errorf("fetching default branch: %w", err)
		}
	}

	commitSHA, err := giteaBranchCommit(baseURL, owner, repo, branch, token)
	if err != nil {
		return nil, fmt.Errorf("resolving branch to commit: %w", err)
	}

	treeURL := fmt.Sprintf("%s/api/v1/repos/%s/%s/git/trees/%s?recursive=true", baseURL, owner, repo, commitSHA)
	body, err := giteaGET(treeURL, token)
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
		fmt.Println("Warning: repository tree is truncated; some files may be skipped")
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
		rawURL := fmt.Sprintf("%s/api/v1/repos/%s/%s/raw/%s?ref=%s",
			baseURL, owner, repo, item.Path, url.QueryEscape(branch))
		entries = append(entries, RepoEntry{
			Path:   item.Path,
			RawURL: rawURL,
		})
	}
	return entries, nil
}

// giteaDefaultBranch returns the default branch of a Gitea repository.
func giteaDefaultBranch(baseURL, owner, repo, token string) (string, error) {
	apiURL := fmt.Sprintf("%s/api/v1/repos/%s/%s", baseURL, owner, repo)
	body, err := giteaGET(apiURL, token)
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

// giteaBranchCommit returns the commit SHA at the tip of a Gitea branch.
func giteaBranchCommit(baseURL, owner, repo, branch, token string) (string, error) {
	apiURL := fmt.Sprintf("%s/api/v1/repos/%s/%s/branches/%s", baseURL, owner, repo, url.PathEscape(branch))
	body, err := giteaGET(apiURL, token)
	if err != nil {
		return "", err
	}
	defer body.Close()

	var branchResp struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if err := json.NewDecoder(body).Decode(&branchResp); err != nil {
		return "", fmt.Errorf("parsing branch response: %w", err)
	}
	if branchResp.Commit.ID == "" {
		return "", fmt.Errorf("commit SHA not found in branch response")
	}
	return branchResp.Commit.ID, nil
}

// giteaGET performs an authenticated GET request to a Gitea instance.
func giteaGET(rawURL, token string) (io.ReadCloser, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil) //nolint:gosec // URL is constructed from validated user input
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	req.Header.Set("Accept", "application/json")

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
