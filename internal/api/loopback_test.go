package api

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/jpnorenam/rag-snap/pkg/storage"
)

// startTestServerWithUI binds a Server with both the unix socket and the
// loopback UI listener enabled. It returns the unix socket path, the resolved
// loopback address, the live Server (carrying the generated token), and a
// teardown via t.Cleanup.
func startTestServerWithUI(t *testing.T, urls map[string]string) (string, string, *Server) {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "ragd", "unix.socket")

	// $SNAP_COMMON is unset in tests, so localhostToken falls back to os.TempDir.
	// Point HOME-independent token storage at the test dir via SNAP_COMMON so the
	// token file is isolated per test.
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
		UI:          UIConfig{Enabled: true, Address: "127.0.0.1:0"},
		BackendURLs: urls,
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Serve(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if srv.uiSrv != nil && srv.uiAddr() != "" {
			if _, statErr := os.Stat(sock); statErr == nil {
				return sock, srv.uiAddr(), srv
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("UI listener was not ready within the timeout")
	return "", "", nil
}

var testBackends = map[string]string{
	backendOpenSearch: "http://127.0.0.1:1",
	backendOpenAI:     "http://127.0.0.1:1",
	backendTika:       "http://127.0.0.1:1",
}

// TestLoopbackValidTokenAdmitted verifies a loopback request to /1.0 with the
// valid bearer token is authenticated and served.
func TestLoopbackValidTokenAdmitted(t *testing.T) {
	_, addr, srv := startTestServerWithUI(t, testBackends)

	req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/1.0", nil)
	req.Header.Set("Authorization", "Bearer "+srv.uiToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /1.0 with token: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, body)
	}
}

// TestLoopbackTokenViaCookieAdmitted verifies the cookie carries the token (the
// path the chat websocket relies on).
func TestLoopbackTokenViaCookieAdmitted(t *testing.T) {
	_, addr, srv := startTestServerWithUI(t, testBackends)

	req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/1.0", nil)
	req.AddCookie(&http.Cookie{Name: uiTokenCookie, Value: srv.uiToken})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /1.0 with cookie: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

// TestLoopbackMissingTokenRejected verifies a loopback /1.0 request without a
// token is rejected with 403.
func TestLoopbackMissingTokenRejected(t *testing.T) {
	_, addr, _ := startTestServerWithUI(t, testBackends)

	resp, err := http.Get("http://" + addr + "/1.0")
	if err != nil {
		t.Fatalf("GET /1.0 without token: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

// TestLoopbackInvalidTokenRejected verifies a wrong token is rejected.
func TestLoopbackInvalidTokenRejected(t *testing.T) {
	_, addr, _ := startTestServerWithUI(t, testBackends)

	req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/1.0", nil)
	req.Header.Set("Authorization", "Bearer not-the-real-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /1.0 with bad token: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

// TestLoopbackUIAssetsUnauthenticated verifies the static UI shell loads under
// /ui/ without any token (only /1.0/... is gated).
func TestLoopbackUIAssetsUnauthenticated(t *testing.T) {
	_, addr, _ := startTestServerWithUI(t, testBackends)

	resp, err := http.Get("http://" + addr + "/ui/")
	if err != nil {
		t.Fatalf("GET /ui/: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /ui/ status = %d, want 200 (assets must load unauthenticated)", resp.StatusCode)
	}
}

// TestRootRedirectsToUI verifies GET / on the loopback listener redirects to /ui/.
func TestRootRedirectsToUI(t *testing.T) {
	_, addr, _ := startTestServerWithUI(t, testBackends)

	// Do not follow redirects so we can assert the 302 + Location.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/ui/" {
		t.Fatalf("Location = %q, want /ui/", loc)
	}
}

// TestUnixPeercredUnaffected verifies the unix socket still authenticates via
// SO_PEERCRED and requires no token: GET /1.0 over the socket succeeds for the
// trusted runner.
func TestUnixPeercredUnaffected(t *testing.T) {
	sock, _, _ := startTestServerWithUI(t, testBackends)

	client := dialSocket(sock)
	resp, err := client.Get("http://unix/1.0")
	if err != nil {
		t.Fatalf("GET /1.0 over unix: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unix GET /1.0 status = %d, want 200 (peercred path must be untouched)", resp.StatusCode)
	}
}

// TestNonLoopbackBindRefused verifies listenLoopback refuses a non-loopback
// address.
func TestNonLoopbackBindRefused(t *testing.T) {
	cases := []string{"0.0.0.0:0", ":8080", "8.8.8.8:0"}
	for _, addr := range cases {
		if _, err := listenLoopback(UIConfig{Enabled: true, Address: addr}); err == nil {
			t.Errorf("listenLoopback(%q) succeeded, want refusal", addr)
		}
	}
}

// TestLoopbackBindAccepted verifies a loopback address binds successfully.
func TestLoopbackBindAccepted(t *testing.T) {
	ln, err := listenLoopback(UIConfig{Enabled: true, Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("listenLoopback(127.0.0.1:0): %v", err)
	}
	defer ln.Close()
	if _, ok := ln.Addr().(*net.TCPAddr); !ok {
		t.Fatalf("expected a TCP listener, got %T", ln.Addr())
	}
}
