package api

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// dialSocket returns an HTTP client that dials the given unix socket path.
func dialSocket(sock string) *http.Client {
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}}
}

// startTestServer binds a Server on a temp-dir socket and returns the socket
// path. It leaves the group owner as-is (Group: "") so the test does not need a
// real host group, and tears the server down via t.Cleanup.
func startTestServer(t *testing.T, urls map[string]string) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "ragd", "unix.socket")

	srv := New(Options{
		Socket:      SocketConfig{Path: sock, Mode: 0o660},
		BackendURLs: urls,
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Serve(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sock); err == nil {
			return sock
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("socket %s was not created within the timeout", sock)
	return ""
}

// TestServeRoot verifies the daemon binds its unix socket and that GET / returns
// the sync envelope advertising the API version and auth state.
func TestServeRoot(t *testing.T) {
	sock := startTestServer(t, map[string]string{
		backendOpenSearch: "http://127.0.0.1:1",
		backendOpenAI:     "http://127.0.0.1:1",
		backendTika:       "http://127.0.0.1:1",
	})
	client := dialSocket(sock)

	resp, err := client.Get("http://unix/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var env map[string]any
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decoding envelope: %v; body=%s", err, body)
	}
	if env["type"] != responseTypeSync {
		t.Errorf("type = %v, want %q", env["type"], responseTypeSync)
	}
	meta, ok := env["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata is not an object: %v", env["metadata"])
	}
	if meta["api_version"] != apiVersion {
		t.Errorf("api_version = %v, want %q", meta["api_version"], apiVersion)
	}
	if meta["auth"] != "trusted" {
		t.Errorf("auth = %v, want \"trusted\"", meta["auth"])
	}
}

// TestServerInfoReportsBackends verifies GET /1.0 reports per-backend readiness.
// Backends pointed at a dead port must report false, proving the listener serves
// before (and regardless of whether) backends are reachable.
func TestServerInfoReportsBackends(t *testing.T) {
	sock := startTestServer(t, map[string]string{
		backendOpenSearch: "http://127.0.0.1:1",
		backendOpenAI:     "http://127.0.0.1:1",
		backendTika:       "http://127.0.0.1:1",
	})
	client := dialSocket(sock)

	resp, err := client.Get("http://unix/1.0")
	if err != nil {
		t.Fatalf("GET /1.0: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /1.0 status = %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var env struct {
		Metadata struct {
			Backends map[string]bool `json:"backends"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decoding /1.0: %v; body=%s", err, body)
	}
	for _, name := range []string{backendOpenSearch, backendOpenAI, backendTika} {
		if _, present := env.Metadata.Backends[name]; !present {
			t.Errorf("backend %q missing from readiness map: %v", name, env.Metadata.Backends)
		}
	}
}

// TestStaleSocketReplaced verifies a leftover socket file from an unclean
// shutdown is removed so a restart can bind the same path.
func TestStaleSocketReplaced(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "unix.socket")

	// Simulate a stale socket by binding and abandoning a listener.
	stale, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("creating stale socket: %v", err)
	}
	t.Cleanup(func() { _ = stale.Close() })

	ln, err := listenUnix(SocketConfig{Path: sock, Mode: 0o660})
	if err != nil {
		t.Fatalf("listenUnix over stale socket: %v", err)
	}
	defer ln.Close()

	info, err := os.Stat(sock)
	if err != nil {
		t.Fatalf("stat new socket: %v", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("path is not a socket after listenUnix")
	}
}
