package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/jpnorenam/rag-snap/pkg/storage"
)

// memConfig is a writable two-layer config store. The file config the other handler
// tests use is read-only, but the config endpoints exist to write, so they need a
// store that applies the real precedence rules: user overrides package, and a user
// write to a key that exists in neither layer is rejected as unknown — the same guard
// snapctl-backed config enforces.
type memConfig struct {
	pkg  map[string]any
	user map[string]any
}

func newMemConfig(pkg, user map[string]any) *memConfig {
	if pkg == nil {
		pkg = map[string]any{}
	}
	if user == nil {
		user = map[string]any{}
	}
	return &memConfig{pkg: pkg, user: user}
}

func (c *memConfig) layer(confType storage.ConfigType) map[string]any {
	if confType == storage.UserConfig {
		return c.user
	}
	return c.pkg
}

func (c *memConfig) Set(key, value string, confType storage.ConfigType) error {
	if confType == storage.UserConfig {
		if _, found := c.pkg[key]; !found {
			if _, found := c.user[key]; !found {
				return errUnknownConfigKey
			}
		}
	}
	c.layer(confType)[key] = value
	return nil
}

func (c *memConfig) SetDocument(key string, value any, confType storage.ConfigType) error {
	c.layer(confType)[key] = value
	return nil
}

func (c *memConfig) Get(key string) (map[string]any, error) {
	all, _ := c.GetAll()
	out := map[string]any{}
	if v, found := all[key]; found {
		out[key] = v
	}
	return out, nil
}

func (c *memConfig) GetAll() (map[string]any, error) {
	out := make(map[string]any, len(c.pkg)+len(c.user))
	for k, v := range c.pkg {
		out[k] = v
	}
	for k, v := range c.user {
		out[k] = v
	}
	return out, nil
}

func (c *memConfig) GetAllFromLayer(confType storage.ConfigType) (map[string]any, error) {
	src := c.layer(confType)
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out, nil
}

func (c *memConfig) Unset(key string, confType storage.ConfigType) error {
	delete(c.layer(confType), key)
	return nil
}

// errUnknownConfigKey mirrors the "unknown key" error the real store returns, which
// the handler maps to a 400.
var errUnknownConfigKey = errUnknownKey{}

type errUnknownKey struct{}

func (errUnknownKey) Error() string { return "unknown key" }

// startConfigServer starts a daemon backed by the given config layers.
func startConfigServer(t *testing.T, pkg, user map[string]any) (string, *memConfig) {
	t.Helper()
	cfg := newMemConfig(pkg, user)
	sock, _ := startTestServerWithStore(t, t.TempDir(), map[string]string{}, cfg)
	return sock, cfg
}

// configList decodes the metadata of GET /1.0/config.
func configList(t *testing.T, env map[string]any) configListView {
	t.Helper()
	raw, err := json.Marshal(env["metadata"])
	if err != nil {
		t.Fatalf("re-encoding metadata: %v", err)
	}
	var view configListView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("decoding config list: %v", err)
	}
	return view
}

// entryFor finds a key in a config listing.
func entryFor(t *testing.T, view configListView, key string) configEntry {
	t.Helper()
	for _, e := range view.Keys {
		if e.Key == key {
			return e
		}
	}
	t.Fatalf("key %q not found in config listing", key)
	return configEntry{}
}

// hasKey reports whether a key appears in a config listing.
func hasKey(view configListView, key string) bool {
	for _, e := range view.Keys {
		if e.Key == key {
			return true
		}
	}
	return false
}

func TestConfigListReportsLayerProvenance(t *testing.T) {
	sock, _ := startConfigServer(t,
		map[string]any{"chat.http.host": "127.0.0.1", "chat.http.port": "8324"},
		map[string]any{"chat.http.port": "9000"},
	)

	code, env := promptRequest(t, sock, http.MethodGet, "/1.0/config", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /1.0/config status = %d, want 200", code)
	}
	view := configList(t, env)

	if host := entryFor(t, view, "chat.http.host"); host.Layer != "package" || host.Value != "127.0.0.1" {
		t.Fatalf("package key = %+v, want value 127.0.0.1 from the package layer", host)
	}
	if port := entryFor(t, view, "chat.http.port"); port.Layer != "user" || port.Value != "9000" {
		t.Fatalf("overridden key = %+v, want value 9000 from the user layer", port)
	}
	if !view.Writable {
		t.Fatal("an authenticated caller should be reported as able to write")
	}

	// The listing is sorted by key, so a client renders a stable table.
	for i := 1; i < len(view.Keys); i++ {
		if view.Keys[i-1].Key > view.Keys[i].Key {
			t.Fatalf("config listing is not sorted by key: %q before %q", view.Keys[i-1].Key, view.Keys[i].Key)
		}
	}
}

// A user override whose value equals the package value is still an override — the
// client must be able to revert it, so provenance cannot be inferred by comparison.
func TestConfigListOverrideEqualToPackageValue(t *testing.T) {
	sock, _ := startConfigServer(t,
		map[string]any{"tika.http.port": "9998"},
		map[string]any{"tika.http.port": "9998"},
	)

	_, env := promptRequest(t, sock, http.MethodGet, "/1.0/config", nil)
	if entry := entryFor(t, configList(t, env), "tika.http.port"); entry.Layer != "user" {
		t.Fatalf("layer = %q, want user for an override equal to the package value", entry.Layer)
	}
}

func TestConfigListHidesDeprecatedKeys(t *testing.T) {
	sock, _ := startConfigServer(t,
		map[string]any{"model": "legacy", "chat.http.host": "127.0.0.1"},
		nil,
	)

	_, env := promptRequest(t, sock, http.MethodGet, "/1.0/config", nil)
	view := configList(t, env)

	if hasKey(view, "model") {
		t.Fatal("deprecated key \"model\" must not appear in the config listing")
	}
	if !hasKey(view, "chat.http.host") {
		t.Fatal("non-deprecated keys must still be listed")
	}
}

func TestConfigListRedactsSecrets(t *testing.T) {
	sock, _ := startConfigServer(t,
		map[string]any{
			"gdrive.client.secret": "super-secret-value",
			"gdrive.client.id":     "client-id",
		},
		nil,
	)

	_, env := promptRequest(t, sock, http.MethodGet, "/1.0/config", nil)
	view := configList(t, env)

	secret := entryFor(t, view, "gdrive.client.secret")
	if secret.Value != redactedValue {
		t.Fatalf("secret value = %q, want it redacted", secret.Value)
	}
	// The non-secret sibling key must not be caught by the redaction rule.
	if id := entryFor(t, view, "gdrive.client.id"); id.Value != "client-id" {
		t.Fatalf("gdrive.client.id = %q, want the real value", id.Value)
	}

	// The secret must not survive anywhere in the response body.
	if leaks(t, env, "super-secret-value") {
		t.Fatal("the secret value leaked into the response body")
	}
}

// An unset secret reads as empty, not as "<redacted>": the install hook seeds
// gdrive.client.secret empty, and claiming a redaction there would be a lie.
func TestConfigListEmptySecretIsNotRedacted(t *testing.T) {
	sock, _ := startConfigServer(t, map[string]any{"gdrive.client.secret": ""}, nil)

	_, env := promptRequest(t, sock, http.MethodGet, "/1.0/config", nil)
	if entry := entryFor(t, configList(t, env), "gdrive.client.secret"); entry.Value != "" {
		t.Fatalf("unset secret = %q, want an empty value", entry.Value)
	}
}

func TestConfigSetWritesUserLayer(t *testing.T) {
	sock, cfg := startConfigServer(t, map[string]any{"chat.http.port": "8324"}, nil)

	code, _ := promptRequest(t, sock, http.MethodPut, "/1.0/config/chat.http.port",
		configSetRequest{Value: "9000"})
	if code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", code)
	}

	// The write landed in the user layer, not the package layer.
	if got := cfg.user["chat.http.port"]; got != "9000" {
		t.Fatalf("user layer = %v, want 9000", got)
	}
	if got := cfg.pkg["chat.http.port"]; got != "8324" {
		t.Fatalf("package layer = %v, want it untouched", got)
	}

	_, env := promptRequest(t, sock, http.MethodGet, "/1.0/config", nil)
	entry := entryFor(t, configList(t, env), "chat.http.port")
	if entry.Value != "9000" || entry.Layer != "user" {
		t.Fatalf("after write, entry = %+v, want 9000 from the user layer", entry)
	}
}

func TestConfigSetRejectsUnknownKey(t *testing.T) {
	sock, cfg := startConfigServer(t, map[string]any{"chat.http.port": "8324"}, nil)

	code, env := promptRequest(t, sock, http.MethodPut, "/1.0/config/not.a.key",
		configSetRequest{Value: "x"})
	if code != http.StatusBadRequest {
		t.Fatalf("PUT unknown key status = %d, want 400", code)
	}
	if msg, _ := env["error"].(string); msg == "" {
		t.Fatal("a rejected write should carry an error message the client can render")
	}
	if len(cfg.user) != 0 {
		t.Fatalf("nothing should have been written, got %v", cfg.user)
	}
}

func TestConfigSetRejectsDeprecatedKey(t *testing.T) {
	sock, cfg := startConfigServer(t, map[string]any{"model": "legacy"}, nil)

	code, _ := promptRequest(t, sock, http.MethodPut, "/1.0/config/model",
		configSetRequest{Value: "new"})
	if code != http.StatusBadRequest {
		t.Fatalf("PUT deprecated key status = %d, want 400", code)
	}
	if len(cfg.user) != 0 {
		t.Fatalf("a deprecated key must not be written, got %v", cfg.user)
	}
}

// A redacted key stays writable: it is write-only, not read-only.
func TestConfigSetRedactedKeyStaysWritable(t *testing.T) {
	sock, cfg := startConfigServer(t, map[string]any{"gdrive.client.secret": ""}, nil)

	code, env := promptRequest(t, sock, http.MethodPut, "/1.0/config/gdrive.client.secret",
		configSetRequest{Value: "new-secret"})
	if code != http.StatusOK {
		t.Fatalf("PUT secret status = %d, want 200", code)
	}
	if cfg.user["gdrive.client.secret"] != "new-secret" {
		t.Fatal("the secret should have been written to the user layer")
	}

	// The response echoes the entry, and must not echo the secret back.
	if leaks(t, env, "new-secret") {
		t.Fatal("the write response echoed the secret back")
	}
}

func TestConfigUnsetRevertsToPackageValue(t *testing.T) {
	sock, cfg := startConfigServer(t,
		map[string]any{"chat.http.host": "127.0.0.1"},
		map[string]any{"chat.http.host": "10.0.0.1"},
	)

	code, _ := promptRequest(t, sock, http.MethodDelete, "/1.0/config/chat.http.host", nil)
	if code != http.StatusOK {
		t.Fatalf("DELETE status = %d, want 200", code)
	}
	if _, found := cfg.user["chat.http.host"]; found {
		t.Fatal("the user override should have been removed")
	}

	_, env := promptRequest(t, sock, http.MethodGet, "/1.0/config", nil)
	entry := entryFor(t, configList(t, env), "chat.http.host")
	if entry.Value != "127.0.0.1" || entry.Layer != "package" {
		t.Fatalf("after revert, entry = %+v, want the package value", entry)
	}
}

func TestConfigUnsetWithoutOverrideFails(t *testing.T) {
	sock, _ := startConfigServer(t, map[string]any{"chat.http.host": "127.0.0.1"}, nil)

	code, _ := promptRequest(t, sock, http.MethodDelete, "/1.0/config/chat.http.host", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("DELETE without an override status = %d, want 400", code)
	}
}

// leaks reports whether a secret value survived anywhere in a response body.
func leaks(t *testing.T, env map[string]any, secret string) bool {
	t.Helper()
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("re-encoding response: %v", err)
	}
	return strings.Contains(string(raw), secret)
}
