package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// TestSearchValidation verifies request validation for POST /1.0/search occurs
// before any backend call, so a missing query/bases yields 400 even with an
// unreachable OpenSearch.
func TestSearchValidation(t *testing.T) {
	sock, _ := startTestServer(t, map[string]string{
		backendOpenSearch: "http://127.0.0.1:1",
	})
	client := dialSocket(sock)

	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing query", map[string]any{"bases": []string{"default"}}},
		{"missing bases", map[string]any{"query": "hello"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf, _ := json.Marshal(tc.body)
			resp, err := client.Post("http://unix/1.0/search", "application/json", bytes.NewReader(buf))
			if err != nil {
				t.Fatalf("POST /1.0/search: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("status = %d, want 400; body=%s", resp.StatusCode, body)
			}
		})
	}
}

// TestCreateKnowledgeValidation verifies an empty name is rejected with 400
// before any backend interaction.
func TestCreateKnowledgeValidation(t *testing.T) {
	sock, _ := startTestServer(t, map[string]string{
		backendOpenSearch: "http://127.0.0.1:1",
	})
	client := dialSocket(sock)

	buf, _ := json.Marshal(map[string]any{"name": "   "})
	resp, err := client.Post("http://unix/1.0/knowledge", "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("POST /1.0/knowledge: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want 400; body=%s", resp.StatusCode, body)
	}
}

// TestImportValidation verifies a JSON import with no input_dir is rejected with
// 400 before any backend interaction.
func TestImportValidation(t *testing.T) {
	sock, _ := startTestServer(t, map[string]string{
		backendOpenSearch: "http://127.0.0.1:1",
	})
	client := dialSocket(sock)

	buf, _ := json.Marshal(map[string]any{"name": "kb"})
	resp, err := client.Post("http://unix/1.0/knowledge/import", "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("POST /1.0/knowledge/import: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want 400; body=%s", resp.StatusCode, body)
	}
}

// TestExportDownloadUnknownOperation verifies the archive download for an unknown
// operation id is a 404, independent of any backend.
func TestExportDownloadUnknownOperation(t *testing.T) {
	sock, _ := startTestServer(t, map[string]string{
		backendOpenSearch: "http://127.0.0.1:1",
	})
	client := dialSocket(sock)

	resp, err := client.Get("http://unix/1.0/knowledge/kb/export/does-not-exist/archive")
	if err != nil {
		t.Fatalf("GET export archive: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want 404; body=%s", resp.StatusCode, body)
	}
}

// TestProvenanceLabel verifies the provenance tag derives from the index name.
func TestProvenanceLabel(t *testing.T) {
	if got := provenanceLabel("rag-snap-context-upstream-docs"); got != "upstream" {
		t.Errorf("provenanceLabel(upstream) = %q, want upstream", got)
	}
	if got := provenanceLabel("rag-snap-context-default"); got != "canonical" {
		t.Errorf("provenanceLabel(default) = %q, want canonical", got)
	}
}
