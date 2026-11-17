package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/pkg/storage"
)

// stubOpenSearchWithKB is stubOpenSearch plus an index listing holding one knowledge
// base, "vendor-rfp", so the analysis catalogue loads (it refuses an empty store).
func stubOpenSearchWithKB(t *testing.T) string {
	t.Helper()
	t.Setenv("OPENSEARCH_USERNAME", "stub")
	t.Setenv("OPENSEARCH_PASSWORD", "stub")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/_cluster/health"):
			_ = json.NewEncoder(w).Encode(map[string]any{"cluster_name": "stub", "status": "green", "number_of_nodes": 1})
		case strings.HasPrefix(r.URL.Path, "/_cat/indices"):
			_ = json.NewEncoder(w).Encode([]map[string]string{
				{"index": "rag-snap-context-vendor-rfp", "health": "green", "status": "open", "docs.count": "3"},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// startAnalysisServerWithKapa starts a daemon with analysis configured, a reachable
// knowledge store holding one knowledge base, and the given kapa client.
func startAnalysisServerWithKapa(t *testing.T, kapa *knowledge.KapaClient) (string, *Server) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config")
	if err := os.WriteFile(cfgPath, []byte("analysis.enabled=true\nanalysis.model=gemini-3.1-pro-preview\n"), 0o600); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	cfg, err := storage.NewFileConfig(cfgPath)
	if err != nil {
		t.Fatalf("loading test config: %v", err)
	}
	t.Setenv("GEMINI_API_KEY", "test-key")
	urls := map[string]string{
		backendOpenSearch: stubOpenSearchWithKB(t),
		backendOpenAI:     "http://127.0.0.1:1",
		backendTika:       "http://127.0.0.1:1",
	}
	return startTestServerWithStore(t, dir, urls, cfg, func(o *Options) { o.Kapa = kapa })
}

func TestAnalysisCatalogueOffersKapaSourceGroups(t *testing.T) {
	_, srv := startAnalysisServerWithKapa(t, knowledge.NewKapaClientAt(stubKapaSources(t), "proj", "key"))

	_, cat, err := srv.analysisClient()
	if err != nil {
		t.Fatalf("analysisClient: %v", err)
	}
	ids := make([]string, 0, len(cat.KapaGroups))
	for _, g := range cat.KapaGroups {
		ids = append(ids, g.ID)
	}
	// Sorted by display name: Juju, LXD, MAAS.
	if strings.Join(ids, ",") != "g-juju,g-lxd,g-maas" {
		t.Errorf("catalogue kapa groups = %v, want the three listed source groups", ids)
	}
	prompt := cat.Prompt()
	if strings.Contains(prompt, "none available") || !strings.Contains(prompt, "identifier: g-maas") {
		t.Errorf("the model must be shown the kapa source groups; prompt:\n%s", prompt)
	}
}

func TestAnalysisCatalogueWithoutKapaOffersNoGroups(t *testing.T) {
	_, srv := startAnalysisServerWithKapa(t, nil)

	_, cat, err := srv.analysisClient()
	if err != nil {
		t.Fatalf("analysisClient: %v", err)
	}
	if len(cat.KapaGroups) != 0 {
		t.Errorf("no kapa client, yet the catalogue offers %v", cat.KapaGroups)
	}
	if !strings.Contains(cat.Prompt(), "none available") {
		t.Error("without kapa the prompt must tell the model to omit kapa_source_groups")
	}
}

// postCheck submits a manifest body to /1.0/answer/check and decodes the result.
func postCheck(t *testing.T, sock, body string) chat.CheckResult {
	t.Helper()
	resp, err := dialSocket(sock).Post("http://unix/1.0/answer/check", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /1.0/answer/check: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var env struct {
		Metadata chat.CheckResult `json:"metadata"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return env.Metadata
}

const kapaCheckManifest = `{
  "version": "1.0",
  "knowledge_bases": ["vendor-rfp"],
  "kapa_source_groups": ["g-maas", "LXD", "g-missing"],
  "prompt": "Be direct, no filler. At most 100 words. Say when no context is available.",
  "questions": [{"id": "C1", "question": "SR-IOV"}]
}`

func TestAnswerCheckVerifiesKapaGroupsWhenKapaIsConfigured(t *testing.T) {
	sock, _ := startAnalysisServerWithKapa(t, knowledge.NewKapaClientAt(stubKapaSources(t), "proj", "key"))

	got := postCheck(t, sock, kapaCheckManifest)
	if !got.ReferencesChecked || !got.KapaChecked {
		t.Fatalf("references_checked=%v kapa_checked=%v, want both true", got.ReferencesChecked, got.KapaChecked)
	}
	var unknown []string
	for _, f := range got.Findings {
		if f.Code == chat.CheckUnknownKapaGroup {
			unknown = append(unknown, f.Message)
		}
	}
	// An identifier and a display name both resolve; only the invented group is flagged.
	if len(unknown) != 1 || !strings.Contains(unknown[0], "g-missing") {
		t.Errorf("unknown kapa group findings = %v, want exactly one naming g-missing", unknown)
	}
}

func TestAnswerCheckSkipsKapaWhenTheListingFails(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream down", http.StatusBadGateway)
	}))
	t.Cleanup(failing.Close)
	sock, _ := startAnalysisServerWithKapa(t, knowledge.NewKapaClientAt(failing.URL, "proj", "key"))

	got := postCheck(t, sock, kapaCheckManifest)
	if !got.ReferencesChecked {
		t.Error("the knowledge store was reachable, so references should be checked")
	}
	if got.KapaChecked {
		t.Error("a failed kapa listing must be reported as not checked, not as an empty list")
	}
	for _, f := range got.Findings {
		if f.Code == chat.CheckUnknownKapaGroup {
			t.Errorf("kapa group flagged although kapa could not be consulted: %s", f.Message)
		}
	}
}
