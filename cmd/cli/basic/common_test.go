package basic

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/jpnorenam/rag-snap/pkg/credentials"
	"github.com/jpnorenam/rag-snap/pkg/storage"
)

// TestBuildKapaClientReadsCredentialsFile covers the CLI resolving KAPA_API_KEY
// from its credentials file when the variable is not exported, while the
// project id still comes from config.
func TestBuildKapaClientReadsCredentialsFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config")
	if err := os.WriteFile(cfgPath, []byte(`kapa.project.id="proj"`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := storage.NewFileConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := &common.Context{Config: cfg}

	credPath := filepath.Join(dir, credentials.FileName)
	if err := os.WriteFile(credPath, []byte(`{"KAPA_API_KEY":"from-file"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"KAPA_API_KEY", "KAPA_PROJECT_ID"} {
		t.Setenv(k, "")
		if err := os.Unsetenv(k); err != nil {
			t.Fatal(err)
		}
	}

	if client := buildKapaClient(ctx); client != nil {
		t.Fatal("built a kapa client with no API key anywhere")
	}
	credentials.Enable(credPath)
	t.Cleanup(func() { credentials.Enable("") })
	if client := buildKapaClient(ctx); client == nil {
		t.Fatal("no kapa client, want one built from the credentials file key")
	}
}
