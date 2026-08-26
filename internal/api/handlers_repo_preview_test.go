package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stubGitea serves the three Gitea API endpoints the repo listing uses: repo
// metadata (default branch), branch tip, and the recursive tree.
func stubGitea(t *testing.T, treePaths []string, truncated bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/git/trees/abc123"):
			tree := make([]map[string]string, 0, len(treePaths))
			for _, p := range treePaths {
				tree = append(tree, map[string]string{"path": p, "type": "blob"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"tree": tree, "truncated": truncated})
		case strings.Contains(r.URL.Path, "/branches/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"commit": map[string]string{"id": "abc123"}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]string{"default_branch": "main"})
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// postPreview posts a repo-preview request and returns the status code and
// response body.
func postPreview(t *testing.T, client *http.Client, body map[string]any) (int, []byte) {
	t.Helper()
	buf, _ := json.Marshal(body)
	resp, err := client.Post("http://unix/1.0/knowledge/repo-preview", "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("POST /1.0/knowledge/repo-preview: %v", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}
	return resp.StatusCode, data
}

// decodePreview decodes a sync envelope body into the preview metadata.
func decodePreview(t *testing.T, body []byte) (files []string, total int, truncated bool) {
	t.Helper()
	var envelope struct {
		Metadata struct {
			Files     []string `json:"files"`
			Total     int      `json:"total"`
			Truncated bool     `json:"truncated"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	return envelope.Metadata.Files, envelope.Metadata.Total, envelope.Metadata.Truncated
}

// TestRepoPreviewGitea lists a stubbed Gitea repo and verifies the preview
// applies the same path-prefix and extension filters as ingestion.
func TestRepoPreviewGitea(t *testing.T) {
	forge := stubGitea(t, []string{"docs/a.md", "docs/b.txt", "src/c.md", "README.md"}, false)
	t.Setenv("GITEA_TOKEN", "tok")

	sock, _ := startTestServer(t, map[string]string{backendOpenSearch: "http://127.0.0.1:1"})
	client := dialSocket(sock)

	status, body := postPreview(t, client, map[string]any{
		"type":       "gitea",
		"source":     forge.URL + "/owner/repo",
		"path":       "docs/",
		"extensions": []string{".md"},
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", status, body)
	}
	files, total, truncated := decodePreview(t, body)
	if total != 1 || len(files) != 1 || files[0] != "docs/a.md" {
		t.Errorf("files = %v (total %d), want exactly [docs/a.md]", files, total)
	}
	if truncated {
		t.Error("truncated = true, want false")
	}
}

// TestRepoPreviewTruncated propagates the upstream truncation flag.
func TestRepoPreviewTruncated(t *testing.T) {
	forge := stubGitea(t, []string{"a.md"}, true)
	t.Setenv("GITEA_TOKEN", "tok")

	sock, _ := startTestServer(t, map[string]string{backendOpenSearch: "http://127.0.0.1:1"})
	client := dialSocket(sock)

	status, body := postPreview(t, client, map[string]any{
		"type":       "gitea",
		"source":     forge.URL + "/owner/repo",
		"extensions": []string{".md"},
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", status, body)
	}
	if _, _, truncated := decodePreview(t, body); !truncated {
		t.Error("truncated = false, want true")
	}
}

// TestRepoPreviewSampleCap caps the returned paths while total counts all matches.
func TestRepoPreviewSampleCap(t *testing.T) {
	paths := make([]string, maxPreviewFiles+50)
	for i := range paths {
		paths[i] = fmt.Sprintf("docs/file-%03d.md", i)
	}
	forge := stubGitea(t, paths, false)
	t.Setenv("GITEA_TOKEN", "tok")

	sock, _ := startTestServer(t, map[string]string{backendOpenSearch: "http://127.0.0.1:1"})
	client := dialSocket(sock)

	status, body := postPreview(t, client, map[string]any{
		"type":       "gitea",
		"source":     forge.URL + "/owner/repo",
		"extensions": []string{".md"},
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", status, body)
	}
	files, total, _ := decodePreview(t, body)
	if len(files) != maxPreviewFiles {
		t.Errorf("len(files) = %d, want %d", len(files), maxPreviewFiles)
	}
	if total != len(paths) {
		t.Errorf("total = %d, want %d", total, len(paths))
	}
}

// TestRepoPreviewValidation covers missing tokens, bad sources, and bad types —
// all rejected with 400 before any forge call.
func TestRepoPreviewValidation(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GITEA_TOKEN", "")

	sock, _ := startTestServer(t, map[string]string{backendOpenSearch: "http://127.0.0.1:1"})
	client := dialSocket(sock)

	cases := []struct {
		name     string
		body     map[string]any
		wantHint string
	}{
		{"missing github token", map[string]any{"type": "github", "source": "owner/repo"}, "GITHUB_TOKEN"},
		{"missing gitea token", map[string]any{"type": "gitea", "source": "https://gitea.example.com/owner/repo"}, "GITEA_TOKEN"},
		{"invalid github source", map[string]any{"type": "github", "source": "just-one-part"}, "invalid GitHub source"},
		{"invalid gitea source", map[string]any{"type": "gitea", "source": "not-a-url"}, "invalid Gitea source"},
		{"unknown type", map[string]any{"type": "svn", "source": "owner/repo"}, "type must be"},
		{"empty source", map[string]any{"type": "github"}, "source is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := postPreview(t, client, tc.body)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", status, body)
			}
			if !strings.Contains(string(body), tc.wantHint) {
				t.Errorf("body %q does not contain %q", body, tc.wantHint)
			}
		})
	}
}
