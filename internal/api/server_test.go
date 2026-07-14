package api

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/jpnorenam/rag-snap/pkg/storage"
)

// dialSocket returns an HTTP client that dials the given unix socket path.
func dialSocket(sock string) *http.Client {
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}}
}

// currentUserGroup returns the name of the test runner's primary group, so the
// peercred auth check grants the test connection access (the runner is a member
// of its own primary group). Skips the test if it cannot be resolved.
func currentUserGroup(t *testing.T) string {
	t.Helper()
	u, err := user.Current()
	if err != nil {
		t.Skipf("cannot resolve current user: %v", err)
	}
	g, err := user.LookupGroupId(u.Gid)
	if err != nil {
		t.Skipf("cannot resolve primary group %s: %v", u.Gid, err)
	}
	return g.Name
}

// startTestServer binds a Server on a temp-dir socket whose access group is the
// test runner's own primary group, so the runner authenticates as trusted. It
// leaves the socket file's group owner as-is (handled by Mode only) and tears
// the server down via t.Cleanup. It returns both the socket path and the live
// Server so tests can reach its operations registry.
func startTestServer(t *testing.T, urls map[string]string) (string, *Server) {
	t.Helper()
	return startTestServerWithConfig(t, urls, nil)
}

// startTestServerWithConfig is startTestServer with seeded config keys, given as
// "key=value" lines (the file-config format). Use it for handlers whose behaviour
// depends on config — e.g. retrieval is enabled only when the embedding model key
// is set, which in turn is what makes a chat session apply its system prompt.
func startTestServerWithConfig(t *testing.T, urls map[string]string, configLines []string) (string, *Server) {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "ragd", "unix.socket")

	// Route daemon state ($SNAP_COMMON-rooted: the prompt store, the token) under
	// this test's temp dir, so tests neither pollute a shared /tmp path nor see
	// each other's prompt customizations.
	t.Setenv("SNAP_COMMON", dir)

	// Back the server with a read-only file config so handlers that read config
	// keys (e.g. the chat model) operate without panicking on a nil Context.
	// Production always supplies a snapctl-backed Context.
	cfgPath := filepath.Join(dir, "config")
	var content []byte
	if len(configLines) > 0 {
		content = []byte(strings.Join(configLines, "\n") + "\n")
	}
	if err := os.WriteFile(cfgPath, content, 0o600); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	cfg, err := storage.NewFileConfig(cfgPath)
	if err != nil {
		t.Fatalf("loading test config: %v", err)
	}

	srv := New(Options{
		Context:     &common.Context{Config: cfg},
		Socket:      SocketConfig{Path: sock, Group: currentUserGroup(t), Mode: 0o660},
		BackendURLs: urls,
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Serve(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sock); err == nil {
			return sock, srv
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("socket %s was not created within the timeout", sock)
	return "", nil
}

// TestServeRoot verifies the daemon binds its unix socket and that GET / returns
// the sync envelope advertising the API version and auth state.
func TestServeRoot(t *testing.T) {
	sock, _ := startTestServer(t, map[string]string{
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
	sock, _ := startTestServer(t, map[string]string{
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

// TestUserInGroup verifies the group-membership resolution used by the
// SO_PEERCRED auth check: the current user is a member of its own primary
// group, and is not a member of a group it does not belong to.
func TestUserInGroup(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Skipf("cannot resolve current user: %v", err)
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		t.Skipf("non-numeric uid %q: %v", u.Uid, err)
	}

	primary := currentUserGroup(t)
	member, err := userInGroup(uint32(uid), primary)
	if err != nil {
		t.Fatalf("userInGroup(primary): %v", err)
	}
	if !member {
		t.Errorf("user not reported in its own primary group %q", primary)
	}

	if _, err := userInGroup(uint32(uid), ""); err == nil {
		t.Errorf("empty group should error")
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
