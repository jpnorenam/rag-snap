package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeOpenSearch serves the two endpoints the status probe uses: the root (its health
// check) and the ML model search (its deployed-model list). The search response is the
// shape OpenSearch really returns, so the decoding is exercised, not assumed.
func fakeOpenSearch(t *testing.T, deployed []map[string]any) *httptest.Server {
	t.Helper()

	// The client reads its credentials from the environment, as it does in production.
	t.Setenv("OPENSEARCH_USERNAME", "admin")
	t.Setenv("OPENSEARCH_PASSWORD", "admin")

	hits := make([]map[string]any, 0, len(deployed))
	for _, d := range deployed {
		hits = append(hits, map[string]any{
			"_id":     d["id"],
			"_source": d,
		})
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"version": map[string]any{"number": "2.11.0"}})
	})
	mux.HandleFunc("POST /_plugins/_ml/models/_search", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"hits": map[string]any{
				"total": map[string]any{"value": len(hits)},
				"hits":  hits,
			},
		})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// fakeTika serves Tika's /version endpoint, which returns a bare version string.
func fakeTika(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte("Apache Tika 3.1.0"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// statusPayload decodes the metadata of GET /1.0/status.
func statusPayload(t *testing.T, env map[string]any) map[string]serviceStatus {
	t.Helper()
	raw, err := json.Marshal(env["metadata"])
	if err != nil {
		t.Fatalf("re-encoding metadata: %v", err)
	}
	var payload map[string]serviceStatus
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decoding status payload: %v", err)
	}
	return payload
}

// embeddingModel is the deployed embedding model the fake OpenSearch reports.
var embeddingModel = map[string]any{
	"id":             "embed-id",
	"name":           "huggingface/sentence-transformers/msmarco-distilbert-base-tas-b",
	"algorithm":      "TEXT_EMBEDDING",
	"model_version":  "1",
	"model_group_id": "group-1",
}

func TestStatusReportsDeployedModels(t *testing.T) {
	osSrv := fakeOpenSearch(t, []map[string]any{embeddingModel})
	tika := fakeTika(t)

	dir := t.TempDir()
	cfg := newMemConfig(map[string]any{
		"knowledge.model.embedding": "embed-id",
	}, nil)
	sock, _ := startTestServerWithStore(t, dir, map[string]string{
		backendOpenSearch: osSrv.URL,
		backendTika:       tika.URL + "/tika", // the configured URL carries Tika's path
		backendOpenAI:     "http://127.0.0.1:1",
	}, cfg)

	code, env := promptRequest(t, sock, http.MethodGet, "/1.0/status", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /1.0/status status = %d, want 200", code)
	}
	payload := statusPayload(t, env)

	opensearch := payload[serviceOpenSearch]
	if opensearch.State != serviceRunning {
		t.Fatalf("opensearch state = %q (%s), want running", opensearch.State, opensearch.Error)
	}
	if len(opensearch.DeployedModels) != 1 {
		t.Fatalf("deployed models = %+v, want exactly one", opensearch.DeployedModels)
	}
	got := opensearch.DeployedModels[0]
	if got.ID != "embed-id" || got.Algorithm != "TEXT_EMBEDDING" || got.ModelVersion != "1" ||
		got.ModelGroupID != "group-1" || got.Name == "" {
		t.Fatalf("deployed model = %+v, want every field populated from _source", got)
	}

	// The configured model is deployed, so it is flagged as such.
	if len(opensearch.Models) != 1 || !opensearch.Models[0].Deployed {
		t.Fatalf("configured models = %+v, want the embedding model flagged deployed", opensearch.Models)
	}

	// Tika answers /version at its root, even though the configured URL has a path.
	if tikaSvc := payload[serviceTika]; tikaSvc.State != serviceRunning || tikaSvc.Version != "Apache Tika 3.1.0" {
		t.Fatalf("tika = %+v, want running with its version", tikaSvc)
	}
}

// The failure this endpoint exists to surface: a model ID is configured, but
// OpenSearch does not have it deployed. Retrieval is broken and nothing else says so.
func TestStatusFlagsConfiguredModelThatIsNotDeployed(t *testing.T) {
	osSrv := fakeOpenSearch(t, []map[string]any{embeddingModel})

	cfg := newMemConfig(map[string]any{
		"knowledge.model.embedding": "stale-id", // not the deployed embed-id
	}, nil)
	sock, _ := startTestServerWithStore(t, t.TempDir(), map[string]string{
		backendOpenSearch: osSrv.URL,
	}, cfg)

	_, env := promptRequest(t, sock, http.MethodGet, "/1.0/status", nil)
	opensearch := statusPayload(t, env)[serviceOpenSearch]

	if len(opensearch.Models) != 1 {
		t.Fatalf("configured models = %+v, want the configured embedding model", opensearch.Models)
	}
	if opensearch.Models[0].Deployed {
		t.Fatal("a configured model absent from the deployed list must not be flagged deployed")
	}
}

// One service down must not take the endpoint, or the other services, with it.
func TestStatusDegradesPerService(t *testing.T) {
	tika := fakeTika(t)

	// Credentials are present, so an unreachable OpenSearch is unreachable because
	// nothing is listening — not because the client could not be built.
	t.Setenv("OPENSEARCH_USERNAME", "admin")
	t.Setenv("OPENSEARCH_PASSWORD", "admin")

	sock, _ := startTestServerWithStore(t, t.TempDir(), map[string]string{
		backendOpenSearch: "http://127.0.0.1:1", // nothing listening
		backendTika:       tika.URL + "/tika",
		backendOpenAI:     "http://127.0.0.1:1", // nothing listening
	}, newMemConfig(nil, nil))

	code, env := promptRequest(t, sock, http.MethodGet, "/1.0/status", nil)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 even with backends down", code)
	}
	payload := statusPayload(t, env)

	if opensearch := payload[serviceOpenSearch]; opensearch.State != serviceUnreachable {
		t.Fatalf("opensearch state = %q, want unreachable", opensearch.State)
	}
	if inference := payload[serviceInference]; inference.State != serviceUnreachable {
		t.Fatalf("inference state = %q, want unreachable", inference.State)
	}
	// The reachable service still reports its detail.
	if tikaSvc := payload[serviceTika]; tikaSvc.State != serviceRunning || tikaSvc.Version == "" {
		t.Fatalf("tika = %+v, want it running and unaffected", tikaSvc)
	}
}

// An unresolvable endpoint is "not configured", not a failure to be debugged.
func TestStatusReportsUnconfiguredService(t *testing.T) {
	sock, _ := startTestServerWithStore(t, t.TempDir(), map[string]string{
		backendOpenSearch: "",
	}, newMemConfig(nil, nil))

	_, env := promptRequest(t, sock, http.MethodGet, "/1.0/status", nil)
	payload := statusPayload(t, env)

	if opensearch := payload[serviceOpenSearch]; opensearch.State != serviceNotConfigured {
		t.Fatalf("opensearch state = %q, want %q", opensearch.State, serviceNotConfigured)
	}
}

// The daemon reports itself and its listeners, and never its localhost token.
func TestStatusReportsDaemonWithoutLeakingToken(t *testing.T) {
	sock, srv := startTestServerWithStore(t, t.TempDir(), map[string]string{},
		newMemConfig(nil, nil))
	srv.token = "super-secret-token"

	_, env := promptRequest(t, sock, http.MethodGet, "/1.0/status", nil)
	payload := statusPayload(t, env)

	ragd := payload[serviceRagd]
	if ragd.State != serviceRunning || ragd.APIVersion != apiVersion {
		t.Fatalf("ragd = %+v, want it running with the API version", ragd)
	}
	if ragd.Listeners == nil || ragd.Listeners.Socket == "" {
		t.Fatalf("ragd listeners = %+v, want the socket path", ragd.Listeners)
	}
	if leaks(t, env, "super-secret-token") {
		t.Fatal("the localhost token leaked into the status payload")
	}
}
