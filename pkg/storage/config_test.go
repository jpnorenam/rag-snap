package storage

import (
	"testing"
)

// fakeStorage is an in-memory storage backend holding the same nested shape the
// snapctl backend returns for a `config` read: {"package": {...}, "user": {...}}.
type fakeStorage struct {
	values map[string]any
}

func (s *fakeStorage) Set(_, _ string) error             { return nil }
func (s *fakeStorage) SetDocument(_ string, _ any) error { return nil }
func (s *fakeStorage) Unset(_ string) error              { return nil }

func (s *fakeStorage) Get(_ string) (map[string]any, error) {
	if s.values == nil {
		return nil, ErrorNotFound
	}
	return s.values, nil
}

// newTestConfig builds a Config over the given package and user layers, nested the
// way snapctl reports them.
func newTestConfig(pkg, user map[string]any) Config {
	values := make(map[string]any)
	if pkg != nil {
		values[string(PackageConfig)] = pkg
	}
	if user != nil {
		values[string(UserConfig)] = user
	}
	return &config{storage: &fakeStorage{values: values}}
}

func TestGetAllFromLayerSeparatesLayers(t *testing.T) {
	c := newTestConfig(
		map[string]any{
			"chat": map[string]any{
				"http": map[string]any{"host": "127.0.0.1", "port": "8324"},
			},
		},
		map[string]any{
			"chat": map[string]any{
				"http": map[string]any{"port": "9000"},
			},
		},
	)

	pkg, err := c.GetAllFromLayer(PackageConfig)
	if err != nil {
		t.Fatal(err)
	}
	if pkg["chat.http.host"] != "127.0.0.1" || pkg["chat.http.port"] != "8324" {
		t.Fatalf("unexpected package layer: %v", pkg)
	}

	user, err := c.GetAllFromLayer(UserConfig)
	if err != nil {
		t.Fatal(err)
	}
	if len(user) != 1 || user["chat.http.port"] != "9000" {
		t.Fatalf("user layer should hold only the override, got: %v", user)
	}

	// The merged view still applies precedence.
	all, err := c.GetAll()
	if err != nil {
		t.Fatal(err)
	}
	if all["chat.http.port"] != "9000" || all["chat.http.host"] != "127.0.0.1" {
		t.Fatalf("unexpected merged config: %v", all)
	}
}

// A user override set to the same value as the package value is still an override.
// Provenance cannot be inferred by comparing merged values, which is why the
// per-layer accessor exists.
func TestGetAllFromLayerOverrideEqualToPackageValue(t *testing.T) {
	c := newTestConfig(
		map[string]any{"tika": map[string]any{"http": map[string]any{"port": "9998"}}},
		map[string]any{"tika": map[string]any{"http": map[string]any{"port": "9998"}}},
	)

	user, err := c.GetAllFromLayer(UserConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := user["tika.http.port"]; !found {
		t.Fatal("override with a package-equal value must still appear in the user layer")
	}
}

func TestGetAllFromLayerEmptyLayer(t *testing.T) {
	c := newTestConfig(map[string]any{"verbose": "false"}, nil)

	user, err := c.GetAllFromLayer(UserConfig)
	if err != nil {
		t.Fatal(err)
	}
	if len(user) != 0 {
		t.Fatalf("a never-written layer should be empty, got: %v", user)
	}
}

func TestGetAllFromLayerRejectsUnexpectedShape(t *testing.T) {
	c := &config{storage: &fakeStorage{values: map[string]any{
		string(UserConfig): "not-a-map",
	}}}

	if _, err := c.GetAllFromLayer(UserConfig); err == nil {
		t.Fatal("expected an error for a non-object config layer")
	}
}
