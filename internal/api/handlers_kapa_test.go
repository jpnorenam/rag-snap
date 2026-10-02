package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/pkg/storage"
)

// stubKapaSources serves the kapa.ai sources listing across two pages. Group
// "g-maas" is attached to sources on both pages, so a correct listing reports it
// once; "g-lxd" only appears on the second page, so a listing that stops after
// the first page misses it.
func stubKapaSources(t *testing.T) string {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-KEY") != "key" {
			http.Error(w, `{"detail":"invalid api key"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`{"next": null, "results": [
				{"source_groups": [{"id": "g-maas", "name": "MAAS"}, {"id": "g-lxd", "name": "LXD"}]}
			]}`))
			return
		}
		_, _ = w.Write([]byte(`{"next": "` + srv.URL + r.URL.Path + `?page=2", "results": [
			{"source_groups": [{"id": "g-maas", "name": "MAAS"}]},
			{"source_groups": [{"id": "g-juju", "name": "Juju"}, {"id": "g-maas", "name": "MAAS"}]}
		]}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// startTestServerWithKapa starts a test server whose daemon resolved the given
// kapa client at startup (nil for an unconfigured integration).
func startTestServerWithKapa(t *testing.T, kapa *knowledge.KapaClient) *http.Client {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config")
	if err := os.WriteFile(cfgPath, nil, 0o600); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	cfg, err := storage.NewFileConfig(cfgPath)
	if err != nil {
		t.Fatalf("loading test config: %v", err)
	}
	sock, _ := startTestServerWithStore(t, dir, testBackends(), cfg, func(o *Options) { o.Kapa = kapa })
	return dialSocket(sock)
}

// getKapaGroups requests the listing and decodes a successful response.
func getKapaGroups(t *testing.T, client *http.Client) (int, kapaSourceGroupsView, string) {
	t.Helper()
	resp, err := client.Get("http://unix/1.0/kapa/source-groups")
	if err != nil {
		t.Fatalf("GET /1.0/kapa/source-groups: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var env struct {
		Metadata kapaSourceGroupsView `json:"metadata"`
		Error    string               `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decoding response: %v; body=%s", err, body)
	}
	return resp.StatusCode, env.Metadata, env.Error
}

func TestKapaSourceGroupsListsEveryPageOnce(t *testing.T) {
	client := startTestServerWithKapa(t, knowledge.NewKapaClientAt(stubKapaSources(t), "proj", "key"))

	status, view, errMsg := getKapaGroups(t, client)
	if status != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", status, errMsg)
	}
	if !view.Configured {
		t.Error("configured = false, want true")
	}
	want := []kapaSourceGroup{{"g-juju", "Juju"}, {"g-lxd", "LXD"}, {"g-maas", "MAAS"}}
	if len(view.Groups) != len(want) {
		t.Fatalf("groups = %+v, want %+v (each once, from both pages)", view.Groups, want)
	}
	for i := range want {
		if view.Groups[i] != want[i] {
			t.Errorf("groups[%d] = %+v, want %+v", i, view.Groups[i], want[i])
		}
	}
}

func TestKapaSourceGroupsUnconfigured(t *testing.T) {
	client := startTestServerWithKapa(t, nil)

	status, view, errMsg := getKapaGroups(t, client)
	if status != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", status, errMsg)
	}
	if view.Configured {
		t.Error("configured = true, want false for an unconfigured integration")
	}
	if view.Groups == nil || len(view.Groups) != 0 {
		t.Errorf("groups = %#v, want an empty (non-null) list", view.Groups)
	}
}

func TestKapaSourceGroupsConfiguredButEmpty(t *testing.T) {
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"next": null, "results": []}`))
	}))
	t.Cleanup(empty.Close)
	client := startTestServerWithKapa(t, knowledge.NewKapaClientAt(empty.URL, "proj", "key"))

	status, view, errMsg := getKapaGroups(t, client)
	if status != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", status, errMsg)
	}
	if !view.Configured || len(view.Groups) != 0 {
		t.Errorf("view = %+v, want configured with no groups", view)
	}
}

func TestKapaSourceGroupsUpstreamRejectsKey(t *testing.T) {
	client := startTestServerWithKapa(t, knowledge.NewKapaClientAt(stubKapaSources(t), "proj", "wrong-key"))

	status, _, errMsg := getKapaGroups(t, client)
	if status != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 for rejected credentials", status)
	}
	if errMsg == "" {
		t.Error("error message is empty, want the upstream failure")
	}
}

func TestKapaSourceGroupsUpstreamUnreachable(t *testing.T) {
	client := startTestServerWithKapa(t, knowledge.NewKapaClientAt("http://127.0.0.1:1", "proj", "key"))

	status, _, _ := getKapaGroups(t, client)
	if status != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 for an unreachable upstream", status)
	}
}

func TestKapaSourceGroupsRequiresAuth(t *testing.T) {
	base, _ := startTestServerWithLoopback(t, testBackends())

	resp, err := http.Get(base + "/1.0/kapa/source-groups")
	if err != nil {
		t.Fatalf("GET over loopback: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("unauthenticated status = %d, want 403", resp.StatusCode)
	}
}

// postSearch posts body to POST /1.0/search and decodes the envelope.
func postSearch(t *testing.T, client *http.Client, body string) (int, []searchResult, []string) {
	t.Helper()
	resp, err := client.Post("http://unix/1.0/search", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /1.0/search: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var env struct {
		Metadata []searchResult `json:"metadata"`
		Warnings []string       `json:"warnings"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decoding search response: %v; body=%s", err, raw)
	}
	if resp.StatusCode == http.StatusOK && len(env.Warnings) == 0 && strings.Contains(string(raw), `"warnings"`) {
		t.Errorf("response carries an empty warnings key: %s", raw)
	}
	return resp.StatusCode, env.Metadata, env.Warnings
}

func TestSearchKapaOnlyWithoutEmbeddingModel(t *testing.T) {
	rec := &kapaRecorder{}
	// No embedding model is configured in the test daemon: a kapa.ai-only
	// search must not need one.
	client := startTestServerWithKapa(t, knowledge.NewKapaClientAt(rec.serve(t), "proj", "key"))

	status, hits, warnings := postSearch(t, client, `{"query": "commission", "kapa_groups": ["g-maas"]}`)
	if status != http.StatusOK || len(warnings) != 0 {
		t.Fatalf("status = %d warnings = %q, want 200 without warnings", status, warnings)
	}
	if len(hits) != 1 || hits[0].Base != "kapa.ai" || hits[0].Label != knowledge.LabelKapa {
		t.Errorf("hits = %+v, want one hit with base kapa.ai and label %s", hits, knowledge.LabelKapa)
	}
	if got := rec.calls(); len(got) != 1 || strings.Join(got[0], ",") != "g-maas" {
		t.Errorf("kapa.ai requests = %v, want one for [g-maas]", got)
	}
}

func TestSearchEmptyKapaGroupsIsNotAScope(t *testing.T) {
	rec := &kapaRecorder{}
	client := startTestServerWithKapa(t, knowledge.NewKapaClientAt(rec.serve(t), "proj", "key"))

	status, _, _ := postSearch(t, client, `{"query": "commission", "kapa_groups": []}`)
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: an empty kapa_groups selects nothing", status)
	}
	if got := rec.calls(); len(got) != 0 {
		t.Errorf("kapa.ai queried with an empty selection: %v", got)
	}
}

func TestSearchKapaUnconfiguredWarns(t *testing.T) {
	client := startTestServerWithKapa(t, nil)
	status, hits, warnings := postSearch(t, client, `{"query": "commission", "kapa_groups": ["g-maas"]}`)
	if status != http.StatusOK || len(hits) != 0 {
		t.Fatalf("status = %d hits = %d, want 200 with no hits", status, len(hits))
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "not configured") {
		t.Errorf("warnings = %q, want the kapa.ai not-configured warning", warnings)
	}
}

func TestSearchKapaFailureWarns(t *testing.T) {
	client := startTestServerWithKapa(t, knowledge.NewKapaClientAt("http://127.0.0.1:1", "proj", "key"))
	status, _, warnings := postSearch(t, client, `{"query": "commission", "kapa_groups": ["g-maas"]}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "kapa.ai search failed") {
		t.Errorf("warnings = %q, want the kapa.ai failure warning", warnings)
	}
}
