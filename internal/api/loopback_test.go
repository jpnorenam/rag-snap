package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/jpnorenam/rag-snap/pkg/storage"
)

// startTestServerWithLoopback binds a Server with the loopback listener enabled
// on an OS-assigned 127.0.0.1 port. It points $SNAP_COMMON at a temp dir so the
// token file is written there, waits for the loopback listener to become
// reachable, and returns the resolved base URL and the live Server (so tests can
// read s.token). Teardown is via t.Cleanup.
func startTestServerWithLoopback(t *testing.T, urls map[string]string) (string, *Server) {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "ragd", "unix.socket")

	// Route the token file under a temp $SNAP_COMMON for this test.
	t.Setenv("SNAP_COMMON", dir)

	cfgPath := filepath.Join(dir, "config")
	if err := os.WriteFile(cfgPath, nil, 0o600); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	cfg, err := storage.NewFileConfig(cfgPath)
	if err != nil {
		t.Fatalf("loading test config: %v", err)
	}

	srv := New(Options{
		Context:     &common.Context{Config: cfg},
		Socket:      SocketConfig{Path: sock, Group: currentUserGroup(t), Mode: 0o660},
		Loopback:    LoopbackConfig{Enabled: true, Address: "127.0.0.1:0"},
		BackendURLs: urls,
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Serve(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if srv.loopbackListenAddr != "" {
			// Confirm the port actually accepts a connection before returning.
			resp, err := http.Get("http://" + srv.loopbackListenAddr + "/")
			if err == nil {
				resp.Body.Close()
				return "http://" + srv.loopbackListenAddr, srv
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("loopback listener was not reachable within the timeout")
	return "", nil
}

func testBackends() map[string]string {
	return map[string]string{
		backendOpenSearch: "http://127.0.0.1:1",
		backendOpenAI:     "http://127.0.0.1:1",
		backendTika:       "http://127.0.0.1:1",
	}
}

// getWith1_0 issues GET /1.0 over the loopback listener with the given request
// mutator (to set a header or cookie) and returns the response.
func getLoopback(t *testing.T, base string, mutate func(*http.Request)) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+"/1.0", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	if mutate != nil {
		mutate(req)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /1.0 over loopback: %v", err)
	}
	return resp
}

// TestLoopbackValidBearerAdmitted verifies a request bearing the valid token as
// an Authorization header is authenticated on the loopback listener.
func TestLoopbackValidBearerAdmitted(t *testing.T) {
	base, srv := startTestServerWithLoopback(t, testBackends())
	resp := getLoopback(t, base, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+srv.token)
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("valid bearer: status = %d, want 200", resp.StatusCode)
	}
}

// TestLoopbackValidCookieAdmitted verifies a request bearing the valid token as
// the rag_ui_token cookie is authenticated (the websocket-upgrade path).
func TestLoopbackValidCookieAdmitted(t *testing.T) {
	base, srv := startTestServerWithLoopback(t, testBackends())
	resp := getLoopback(t, base, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: uiTokenCookie, Value: srv.token})
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("valid cookie: status = %d, want 200", resp.StatusCode)
	}
}

// TestLoopbackMissingTokenRejected verifies a request with no token is rejected.
func TestLoopbackMissingTokenRejected(t *testing.T) {
	base, _ := startTestServerWithLoopback(t, testBackends())
	resp := getLoopback(t, base, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("missing token: status = %d, want 403", resp.StatusCode)
	}
}

// TestLoopbackInvalidTokenRejected verifies a request with a wrong token is
// rejected.
func TestLoopbackInvalidTokenRejected(t *testing.T) {
	base, _ := startTestServerWithLoopback(t, testBackends())
	resp := getLoopback(t, base, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer not-the-real-token")
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("invalid token: status = %d, want 403", resp.StatusCode)
	}
}

// TestRequireLoopbackHost verifies the host validation admits loopback targets
// and refuses all-interfaces and non-loopback hosts.
func TestRequireLoopbackHost(t *testing.T) {
	valid := []string{"127.0.0.1", "::1", "localhost"}
	for _, h := range valid {
		if err := requireLoopbackHost(h); err != nil {
			t.Errorf("requireLoopbackHost(%q) = %v, want nil", h, err)
		}
	}
	invalid := []string{"", "0.0.0.0", "192.168.1.10", "example.com"}
	for _, h := range invalid {
		if err := requireLoopbackHost(h); err == nil {
			t.Errorf("requireLoopbackHost(%q) = nil, want error", h)
		}
	}
}

// TestListenLoopbackRefusesNonLoopback verifies listenLoopback refuses to bind an
// all-interfaces or non-loopback address.
func TestListenLoopbackRefusesNonLoopback(t *testing.T) {
	for _, addr := range []string{":0", "0.0.0.0:0"} {
		ln, err := listenLoopback(LoopbackConfig{Enabled: true, Address: addr})
		if err == nil {
			ln.Close()
			t.Errorf("listenLoopback(%q) = nil error, want refusal", addr)
		}
	}

	// A loopback address must succeed.
	ln, err := listenLoopback(LoopbackConfig{Enabled: true, Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("listenLoopback(127.0.0.1:0): %v", err)
	}
	ln.Close()
}
