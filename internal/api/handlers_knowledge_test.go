package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
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

// TestFallbackLabel verifies unlabeled hits resolve their label from the index
// name via the shared knowledge resolver (the old provenance behavior).
func TestFallbackLabel(t *testing.T) {
	if got := knowledge.ResolveLabel("rag-snap-context-upstream-docs", ""); got != "upstream" {
		t.Errorf("ResolveLabel(upstream, \"\") = %q, want upstream", got)
	}
	if got := knowledge.ResolveLabel("rag-snap-context-default", ""); got != "canonical" {
		t.Errorf("ResolveLabel(default, \"\") = %q, want canonical", got)
	}
	if got := knowledge.ResolveLabel("rag-snap-context-upstream-docs", "internal"); got != "internal" {
		t.Errorf("ResolveLabel(upstream, internal) = %q, want internal", got)
	}
}

// fakeSearchOpenSearch serves the endpoints a search needs: the cluster health
// the client checks on connect, and each index's _search, answered from
// perIndex as OpenSearch would after reranking (score descending).
func fakeSearchOpenSearch(t *testing.T, perIndex map[string][]map[string]any) *httptest.Server {
	t.Helper()
	t.Setenv("OPENSEARCH_USERNAME", "admin")
	t.Setenv("OPENSEARCH_PASSWORD", "admin")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /_cluster/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "green"})
	})
	mux.HandleFunc("/{index}/_search", func(w http.ResponseWriter, r *http.Request) {
		index := r.PathValue("index")
		hits := make([]map[string]any, 0, len(perIndex[index]))
		for _, h := range perIndex[index] {
			hits = append(hits, map[string]any{
				"_index":  index,
				"_id":     h["source_id"],
				"_score":  h["score"],
				"_source": map[string]any{"content": "chunk", "source_id": h["source_id"], "created_at": "2026-11-01"},
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"hits": map[string]any{"total": map[string]any{"value": len(hits)}, "hits": hits},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestSearchMatchedAndCount verifies POST /1.0/search marks each hit with
// "matched" (score > 0), keeps score order, and caps count across bases.
func TestSearchMatchedAndCount(t *testing.T) {
	osSrv := fakeSearchOpenSearch(t, map[string][]map[string]any{
		knowledge.FullIndexName("a"): {
			{"source_id": "a1", "score": 7.5},
			{"source_id": "a2", "score": 0.0},
			{"source_id": "a3", "score": -3.0},
		},
		knowledge.FullIndexName("b"): {
			{"source_id": "b1", "score": 9.1},
			{"source_id": "b2", "score": -1.0},
			{"source_id": "b3", "score": -6.0},
		},
	})
	sock, _ := startTestServerWithConfig(t,
		map[string]string{backendOpenSearch: osSrv.URL},
		[]string{knowledge.ConfEmbeddingModelID + "=embed-id"},
	)
	client := dialSocket(sock)

	buf, _ := json.Marshal(map[string]any{"query": "kafka", "bases": []string{"a", "b"}, "count": 4})
	resp, err := client.Post("http://unix/1.0/search", "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("POST /1.0/search: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
	}

	var env struct {
		Metadata []map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decoding response: %v; body=%s", err, body)
	}

	// count=4 over two bases: the global top 4, not 4 per base.
	want := []struct {
		id      string
		matched bool
	}{{"b1", true}, {"a1", true}, {"a2", false}, {"b2", false}}
	if len(env.Metadata) != len(want) {
		t.Fatalf("got %d hits, want %d: %s", len(env.Metadata), len(want), body)
	}
	for i, w := range want {
		h := env.Metadata[i]
		matched, present := h["matched"].(bool)
		if !present {
			t.Errorf("hit %d has no boolean matched field: %v", i, h)
		}
		if h["source_id"] != w.id || matched != w.matched {
			t.Errorf("hit %d = %v/%v, want %s/%v", i, h["source_id"], h["matched"], w.id, w.matched)
		}
		for _, field := range []string{"score", "base", "source_id", "created_at", "label", "content"} {
			if _, ok := h[field]; !ok {
				t.Errorf("hit %d lacks pre-existing field %q", i, field)
			}
		}
	}
}
