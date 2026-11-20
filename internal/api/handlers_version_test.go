package api

import (
	"net/http"
	"testing"

	"github.com/jpnorenam/rag-snap/internal/buildinfo"
)

// TestServerInfoReportsVersion verifies GET /1.0 carries the snap version and
// commit, which the UI shows in the sidebar footer, and that the ragd status
// card reports the same build.
func TestServerInfoReportsVersion(t *testing.T) {
	t.Setenv("SNAP_VERSION", "0.0.9")
	t.Cleanup(buildinfo.SetCommitForTest("cd3ac8925556"))
	sock, _ := startTestServer(t, map[string]string{})

	code, env := promptRequest(t, sock, http.MethodGet, "/1.0", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /1.0 status = %d, want 200", code)
	}
	meta, _ := env["metadata"].(map[string]any)
	if meta["version"] != "0.0.9" {
		t.Errorf("GET /1.0 version = %v, want 0.0.9", meta["version"])
	}
	if meta["commit"] != "cd3ac8925556" {
		t.Errorf("GET /1.0 commit = %v, want cd3ac8925556", meta["commit"])
	}

	code, env = promptRequest(t, sock, http.MethodGet, "/1.0/status", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /1.0/status status = %d, want 200", code)
	}
	if ragd := statusPayload(t, env)[serviceRagd]; ragd.Version != "0.0.9" || ragd.Commit != "cd3ac8925556" {
		t.Errorf("ragd status = %q / %q, want 0.0.9 / cd3ac8925556", ragd.Version, ragd.Commit)
	}
}

// TestServerInfoOmitsAnUnknownCommit verifies the commit is left out, not sent
// empty, when the build recorded none.
func TestServerInfoOmitsAnUnknownCommit(t *testing.T) {
	t.Cleanup(buildinfo.SetCommitForTest(""))
	if buildinfo.Commit() != "" {
		t.Skip("this test binary carries a VCS stamp, so the commit is known")
	}
	sock, _ := startTestServer(t, map[string]string{})
	_, env := promptRequest(t, sock, http.MethodGet, "/1.0", nil)
	meta, _ := env["metadata"].(map[string]any)
	if _, present := meta["commit"]; present {
		t.Errorf("GET /1.0 sent commit %v; it must be omitted when unknown", meta["commit"])
	}
}
