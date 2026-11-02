package api

import (
	"net/http"
	"testing"
)

// TestServerInfoReportsVersion verifies GET /1.0 carries the snap version,
// which the UI shows in the sidebar footer, and that the ragd status card
// reports the same version.
func TestServerInfoReportsVersion(t *testing.T) {
	t.Setenv("SNAP_VERSION", "0.0.9")
	sock, _ := startTestServer(t, map[string]string{})

	code, env := promptRequest(t, sock, http.MethodGet, "/1.0", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /1.0 status = %d, want 200", code)
	}
	meta, _ := env["metadata"].(map[string]any)
	if meta["version"] != "0.0.9" {
		t.Errorf("GET /1.0 version = %v, want 0.0.9", meta["version"])
	}
	// Test binaries carry no injected commit; when Go recorded none either,
	// the field is omitted rather than sent empty.
	if c, present := meta["commit"]; present && c == "" {
		t.Error("GET /1.0 sent an empty commit; it must be omitted when unknown")
	}

	code, env = promptRequest(t, sock, http.MethodGet, "/1.0/status", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /1.0/status status = %d, want 200", code)
	}
	if ragd := statusPayload(t, env)[serviceRagd]; ragd.Version != "0.0.9" {
		t.Errorf("ragd status version = %q, want 0.0.9", ragd.Version)
	}
}
