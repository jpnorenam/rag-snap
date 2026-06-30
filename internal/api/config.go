package api

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"

	"github.com/canonical/go-snapctl/env"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/jpnorenam/rag-snap/cmd/cli/config"
)

// Config keys the daemon reads from the snapctl-backed store. These mirror the
// keys the CLI uses (so a single config seeds both), plus the api.socket.* keys
// introduced for the daemon.
const (
	confOpenAiHTTPHost = "chat.http.host"
	confOpenAiHTTPPort = "chat.http.port"
	confOpenAiHTTPPath = "chat.http.path"
	confOpenAiHTTPTLS  = "chat.http.tls"

	confOpenSearchHTTPHost = "knowledge.http.host"
	confOpenSearchHTTPPort = "knowledge.http.port"
	confOpenSearchHTTPTLS  = "knowledge.http.tls"

	confTikaHTTPHost = "tika.http.host"
	confTikaHTTPPort = "tika.http.port"
	confTikaHTTPPath = "tika.http.path"
	confTikaHTTPTLS  = "tika.http.tls"

	confAPISocketGroup = "api.socket.group"
	confAPISocketMode  = "api.socket.mode"
)

// Backend names used as keys in the BackendURLs map and readiness tracker.
const (
	backendOpenAI     = "openai"
	backendOpenSearch = "opensearch"
	backendTika       = "tika"
)

// Defaults applied when a key is unset.
const (
	defaultSocketGroup = "rag"
	// defaultSocketMode is 0666 (world-connectable) because the socket cannot be
	// chowned to api.socket.group under strict confinement; the SO_PEERCRED check
	// is the access gate, not the file mode. See socket.go / design Decision 1.
	defaultSocketMode = 0o666
)

// ResolveBackendURLs builds the service URL map from config. It is the daemon's
// equivalent of the CLI's serverApiUrls and reads the same keys. Missing keys
// yield an error so the daemon fails loudly on a half-configured install.
func ResolveBackendURLs(ctx *common.Context) (map[string]string, error) {
	openAiHost, err := requireString(ctx, confOpenAiHTTPHost)
	if err != nil {
		return nil, err
	}
	openAiPort, err := requireString(ctx, confOpenAiHTTPPort)
	if err != nil {
		return nil, err
	}
	openAiPath, _ := config.GetString(ctx.Config, confOpenAiHTTPPath)

	osHost, err := requireString(ctx, confOpenSearchHTTPHost)
	if err != nil {
		return nil, err
	}
	osPort, err := requireString(ctx, confOpenSearchHTTPPort)
	if err != nil {
		return nil, err
	}

	tikaHost, err := requireString(ctx, confTikaHTTPHost)
	if err != nil {
		return nil, err
	}
	tikaPort, err := requireString(ctx, confTikaHTTPPort)
	if err != nil {
		return nil, err
	}
	tikaPath, _ := config.GetString(ctx.Config, confTikaHTTPPath)

	return map[string]string{
		backendOpenAI:     buildURL(openAiHost, openAiPort, openAiPath, getBool(ctx, confOpenAiHTTPTLS, false)),
		backendOpenSearch: buildURL(osHost, osPort, "", getBool(ctx, confOpenSearchHTTPTLS, true)),
		backendTika:       buildURL(tikaHost, tikaPort, tikaPath, getBool(ctx, confTikaHTTPTLS, false)),
	}, nil
}

// ResolveSocketConfig builds the socket config from $SNAP_COMMON and the
// api.socket.* keys, applying defaults when unset.
func ResolveSocketConfig(ctx *common.Context) SocketConfig {
	base := env.SnapCommon()
	if base == "" {
		// Outside a snap (e.g. local dev), fall back to a temp-dir socket.
		base = os.TempDir()
	}

	group, _ := config.GetString(ctx.Config, confAPISocketGroup)
	if group == "" {
		group = defaultSocketGroup
	}

	mode := os.FileMode(defaultSocketMode)
	if raw, _ := config.GetString(ctx.Config, confAPISocketMode); raw != "" {
		if parsed, err := strconv.ParseUint(raw, 8, 32); err == nil {
			mode = os.FileMode(parsed)
		}
	}

	return SocketConfig{
		Path:  filepath.Join(base, "ragd", "unix.socket"),
		Group: group,
		Mode:  mode,
	}
}

func requireString(ctx *common.Context, key string) (string, error) {
	val, err := config.GetString(ctx.Config, key)
	if err != nil {
		return "", err
	}
	if val == "" {
		return "", fmt.Errorf("config key %q is not set", key)
	}
	return val, nil
}

func getBool(ctx *common.Context, key string, fallback bool) bool {
	val, err := config.GetString(ctx.Config, key)
	if err != nil || val == "" {
		return fallback
	}
	return val == "true" || val == "1"
}

func buildURL(host, port, path string, secure bool) string {
	u := url.URL{Host: fmt.Sprintf("%s:%s", host, port), Path: path}
	if secure {
		u.Scheme = "https"
	} else {
		u.Scheme = "http"
	}
	return u.String()
}
