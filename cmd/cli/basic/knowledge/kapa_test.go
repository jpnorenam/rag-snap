package knowledge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// TestResolveKapaClient pins the one rule both the CLI and the daemon use to
// decide whether a kapa client exists.
func TestResolveKapaClient(t *testing.T) {
	tests := []struct {
		name      string
		enabled   string // raw kapa.enabled value
		projectID string
		apiKey    string
		want      bool
	}{
		{name: "enabled with both values", enabled: "true", projectID: "proj", apiKey: "key", want: true},
		{name: "unset enabled defaults to true", enabled: "", projectID: "proj", apiKey: "key", want: true},
		{name: "missing key", enabled: "true", projectID: "proj", apiKey: "", want: false},
		{name: "missing project", enabled: "true", projectID: "", apiKey: "key", want: false},
		{name: "explicitly disabled", enabled: "false", projectID: "proj", apiKey: "key", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := ResolveKapaClient(KapaEnabled(tt.enabled), tt.projectID, tt.apiKey)
			if got := client != nil; got != tt.want {
				t.Fatalf("client built = %v, want %v", got, tt.want)
			}
			if client != nil && (client.projectID != tt.projectID || client.apiKey != tt.apiKey) {
				t.Errorf("client = {project %q, key %q}, want {%q, %q}",
					client.projectID, client.apiKey, tt.projectID, tt.apiKey)
			}
		})
	}
}

func TestKapaEnabled(t *testing.T) {
	for raw, want := range map[string]bool{"": true, "true": true, "1": true, "false": false, "0": false} {
		if got := KapaEnabled(raw); got != want {
			t.Errorf("KapaEnabled(%q) = %v, want %v", raw, got, want)
		}
	}
}

// TestKapaStatus checks that the status names what is missing, and is
// configured exactly when ResolveKapaClient builds a client.
func TestKapaStatus(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		projectID string
		apiKey    string
		want      KapaStatus
	}{
		{name: "configured", enabled: true, projectID: "proj", apiKey: "key",
			want: KapaStatus{Enabled: true, Configured: true}},
		{name: "disabled wins over missing values", enabled: false, want: KapaStatus{}},
		{name: "missing key", enabled: true, projectID: "proj",
			want: KapaStatus{Enabled: true, Missing: []string{EnvKapaAPIKey}}},
		{name: "missing project", enabled: true, apiKey: "key",
			want: KapaStatus{Enabled: true, Missing: []string{ConfKapaProjectID}}},
		{name: "missing both", enabled: true,
			want: KapaStatus{Enabled: true, Missing: []string{ConfKapaProjectID, EnvKapaAPIKey}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewKapaStatus(tt.enabled, tt.projectID, tt.apiKey)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewKapaStatus() = %+v, want %+v", got, tt.want)
			}
			built := ResolveKapaClient(tt.enabled, tt.projectID, tt.apiKey) != nil
			if built != got.Configured {
				t.Errorf("ResolveKapaClient built=%v but Configured=%v", built, got.Configured)
			}
		})
	}
}

// TestKapaProjectName checks the project lookup against a stand-in kapa.ai:
// the request path and key, the name it reads, and the short error on failure.
func TestKapaProjectName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-KEY") != "key" {
			http.Error(w, "<html>forbidden</html>", http.StatusForbidden)
			return
		}
		if r.URL.Path != "/org/v1/projects/proj/" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"id":"proj","team":"t","project_name":"Test FE Internal Project","pii_config":null,"is_internal":true}`))
	}))
	t.Cleanup(srv.Close)

	name, err := NewKapaClientAt(srv.URL, "proj", "key").ProjectName(context.Background())
	if err != nil || name != "Test FE Internal Project" {
		t.Fatalf("ProjectName() = %q, %v", name, err)
	}

	_, err = NewKapaClientAt(srv.URL, "proj", "wrong").ProjectName(context.Background())
	if err == nil || err.Error() != "kapa: unexpected status 403" {
		t.Fatalf("rejected key: err = %v, want a short status error without the body", err)
	}
}
