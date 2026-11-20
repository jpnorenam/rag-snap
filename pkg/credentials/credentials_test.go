package credentials

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const secret = "s3cr3t-value"

func setup(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	for k := range supported {
		t.Setenv(k, "")
		if err := os.Unsetenv(k); err != nil {
			t.Fatal(err)
		}
	}
	p := filepath.Join(t.TempDir(), FileName)
	if content != "" {
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
	}
	Enable(p)
	t.Cleanup(func() { Enable("") })
	return p
}

func TestFileFallback(t *testing.T) {
	setup(t, `{"OPENSEARCH_USERNAME":"admin","OPENSEARCH_PASSWORD":"`+secret+`"}`, 0o600)
	v, ok, err := Lookup("OPENSEARCH_PASSWORD")
	if err != nil || !ok || v != secret {
		t.Fatalf("got %q %v %v", v, ok, err)
	}
	if _, ok, err := Lookup("CHAT_API_KEY"); ok || err != nil {
		t.Fatalf("absent key: ok=%v err=%v", ok, err)
	}
}

func TestEnvironmentWinsIncludingEmpty(t *testing.T) {
	setup(t, `{"CHAT_API_KEY":"from-file","OPENSEARCH_PASSWORD":"from-file"}`, 0o600)
	t.Setenv("OPENSEARCH_PASSWORD", "from-env")
	t.Setenv("CHAT_API_KEY", "")
	if v, _, _ := Lookup("OPENSEARCH_PASSWORD"); v != "from-env" {
		t.Fatalf("env should win, got %q", v)
	}
	if v, ok, _ := Lookup("CHAT_API_KEY"); !ok || v != "" {
		t.Fatalf("explicit empty env should win, got %q %v", v, ok)
	}
}

func TestInvalidFileUnreadWhenEnvironmentSuffices(t *testing.T) {
	setup(t, `not json`, 0o644)
	t.Setenv("OPENSEARCH_USERNAME", "admin")
	t.Setenv("OPENSEARCH_PASSWORD", "pw")
	for _, k := range []string{"OPENSEARCH_USERNAME", "OPENSEARCH_PASSWORD"} {
		if _, _, err := Lookup(k); err != nil {
			t.Fatalf("%s: unexpected error %v", k, err)
		}
	}
}

func TestMissingFileAndDisabled(t *testing.T) {
	setup(t, "", 0)
	if _, ok, err := Lookup("OPENSEARCH_PASSWORD"); ok || err != nil {
		t.Fatalf("missing file: ok=%v err=%v", ok, err)
	}
	Enable("")
	if _, ok, err := Lookup("OPENSEARCH_PASSWORD"); ok || err != nil {
		t.Fatalf("disabled (ragd): ok=%v err=%v", ok, err)
	}
}

func TestInvalidFiles(t *testing.T) {
	cases := map[string]struct {
		content string
		mode    os.FileMode
		want    string
	}{
		"group readable": {`{"OPENSEARCH_PASSWORD":"` + secret + `"}`, 0o640, "chmod 600"},
		"malformed":      {`{"OPENSEARCH_PASSWORD":"` + secret + `"`, 0o600, "not valid JSON"},
		"not an object":  {`["` + secret + `"]`, 0o600, "single JSON object"},
		"unknown key":    {`{"OPENSEARCH_PASSWORD":"` + secret + `","AWS_SECRET":"x"}`, 0o600, `"AWS_SECRET"`},
		"non-string":     {`{"OPENSEARCH_PASSWORD":42}`, 0o600, "must be a string"},
		"null file":      {`null`, 0o600, "single JSON object"},
		"null value":     {`{"OPENSEARCH_PASSWORD":null}`, 0o600, "must be a string"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			p := setup(t, c.content, c.mode)
			_, _, err := Lookup("OPENSEARCH_PASSWORD")
			if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), p) {
				t.Fatalf("want error containing %q and path, got %v", c.want, err)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("error leaks secret: %v", err)
			}
		})
	}
}

func TestSymlinkRejected(t *testing.T) {
	p := setup(t, "", 0)
	target := filepath.Join(t.TempDir(), "real.json")
	if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, p); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Lookup("OPENSEARCH_PASSWORD"); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("want regular-file error, got %v", err)
	}
}

func TestWrongOwnerRejected(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root to create a file owned by another user")
	}
	p := setup(t, `{"OPENSEARCH_PASSWORD":"`+secret+`"}`, 0o600)
	if err := os.Chown(p, 65534, 65534); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Lookup("OPENSEARCH_PASSWORD"); err == nil || !strings.Contains(err.Error(), "owned by the current user") {
		t.Fatalf("want owner error, got %v", err)
	}
}

func TestKapaAPIKeyFromFile(t *testing.T) {
	setup(t, `{"KAPA_API_KEY":"from-file"}`, 0o600)
	if v, ok, err := Lookup("KAPA_API_KEY"); err != nil || !ok || v != "from-file" {
		t.Fatalf("got %q %v %v", v, ok, err)
	}
	t.Setenv("KAPA_API_KEY", "from-env")
	if v, _, _ := Lookup("KAPA_API_KEY"); v != "from-env" {
		t.Fatalf("env should win, got %q", v)
	}
}
